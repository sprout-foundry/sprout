// Package automate provides shared workflow discovery and validation for
// the automate/ feature used by both the CLI (cmd/automate.go) and the
// agent tool layer (pkg/agent/tool_handlers_automate.go).
package automate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Entry represents a discovered workflow file with its metadata.
type Entry struct {
	Filename    string `json:"name"`
	FilePath    string
	Description string `json:"description,omitempty"`
}

// isValidFilenamePattern matches safe workflow filenames: alphanumeric,
// dots, underscores, and hyphens followed by .json.
var isValidFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.json$`)

// Dir returns the default automate directory path (cwd + "/automate").
//
// CAUTION: in daemon mode (SPROUT_SERVICE=1), os.Getwd() returns the daemon
// root, not the active workspace. Tool handlers in the agent must use DirIn
// with the workspace root from the active Agent (a.GetWorkspaceRoot()) or
// context (pkg/filesystem.WorkspaceRootFromContext), not Dir(). See
// SP-119 for the workspace-aware flow.
func Dir() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "automate")
}

// DirIn returns the automate directory inside the given workspace directory.
// Returns the CWD-based Dir() when workspaceDir is empty (or whitespace-only),
// preserving CLI behavior where the user's shell CWD IS the workspace root.
//
// This is the workspace-aware counterpart to Dir(). Use DirIn from any code
// that knows the active workspace (e.g., agent tools running inside a daemon
// where os.Getwd() returns the daemon root, not the user's workspace).
func DirIn(workspaceDir string) string {
	if strings.TrimSpace(workspaceDir) == "" {
		return Dir()
	}
	return filepath.Join(workspaceDir, "automate")
}

// IsValidFilename checks if a filename is safe for use as a workflow filename.
// Only allows alphanumeric characters, dots, underscores, and hyphens,
// followed by .json. Prevents shell injection via filenames.
func IsValidFilename(name string) bool {
	return isValidFilenamePattern.MatchString(name)
}

// Discover scans the given directory for valid workflow JSON files.
func Discover(dir string) ([]Entry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var workflows []Entry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !IsValidFilename(name) {
			continue // Skip files with unsafe names
		}

		fullPath := filepath.Join(dir, name)
		desc, err := ExtractDescription(fullPath)
		if err != nil {
			continue // Not a valid workflow JSON
		}

		workflows = append(workflows, Entry{
			Filename:    name,
			FilePath:    fullPath,
			Description: desc,
		})
	}

	return workflows, nil
}

// ExtractDescription reads a workflow JSON file and returns its description field.
func ExtractDescription(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}

	// Must have "initial" or "steps" to be a workflow
	if _, ok := raw["initial"]; !ok {
		if _, ok := raw["steps"]; !ok {
			return "", fmt.Errorf("not a workflow config")
		}
	}

	var desc string
	if descRaw, ok := raw["description"]; ok {
		_ = json.Unmarshal(descRaw, &desc)
	}

	return desc, nil
}

// ResolvePath finds a workflow file by name, with or without .json extension,
// and verifies the resolved path stays under the given directory to prevent
// path traversal attacks.
func ResolvePath(dir string, name string) (string, error) {
	// Try exact filename match first. Normalize .json extension to
	// lowercase so case-insensitive filesystems (macOS, Windows) can't
	// bypass IsValidFilename by using .JSON variants.
	target := name
	if strings.HasSuffix(strings.ToLower(name), ".json") {
		// Normalize: strip whatever-case .json and re-add lowercase
		target = name[:len(name)-len(".json")] + ".json"
	} else {
		target = name + ".json"
	}

	candidate := filepath.Join(dir, target)

	// Verify the resolved path stays UNDER dir (path traversal protection).
	// The HasPrefix check with filepath.Separator ensures the candidate is
	// strictly inside the directory, not the directory itself.
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workflow path: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve automate directory: %w", err)
	}
	if !strings.HasPrefix(absCandidate, absDir+string(filepath.Separator)) {
		return "", fmt.Errorf("workflow path escapes automate directory")
	}

	// Filename validation also runs on the exact-match branch — Discover
	// already filters its results, but the exact-match path returns
	// whatever exists at `candidate`. Without this check, a planted file
	// like `legit;echo PWNED.json` would round-trip through ResolvePath
	// and end up embedded in the shell command line that BPM.Start hands
	// to `sh -c`, where the semicolon would execute injected commands.
	if !IsValidFilename(filepath.Base(candidate)) {
		return "", fmt.Errorf("unsafe workflow filename: %q", filepath.Base(candidate))
	}

	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	// Try substring match
	workflows, err := Discover(dir)
	if err != nil {
		return "", fmt.Errorf("no automate/ directory found")
	}

	var matches []Entry
	for _, wf := range workflows {
		if strings.Contains(strings.ToLower(wf.Filename), strings.ToLower(name)) {
			matches = append(matches, wf)
		}
	}

	if len(matches) == 1 {
		return matches[0].FilePath, nil
	}

	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Filename
		}
		return "", fmt.Errorf("multiple workflows match %q: %v — please specify the full filename", name, names)
	}

	return "", fmt.Errorf("no workflow matching %q found in %s/", name, dir)
}

// IsNotExists returns true if the error indicates a missing file or directory.
func IsNotExists(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// Summary describes the structure of a workflow file at a glance. It is
// produced by Summarize and used by the CLI to render a human-readable
// overview before kicking off the workflow. JSON tags use snake_case to
// match the rest of the WebUI API surface; nil pointers are serialized as
// `null` (no omitempty) so `requires_approval` and `subagent_timeout_seconds`
// are always visible to the frontend — the original 3-state semantics
// (unset = default, true, false) must round-trip through the wire without
// collapsing to "absent."
type Summary struct {
	Description     string `json:"description,omitempty"`
	ContinueOnError bool   `json:"continue_on_error,omitempty"`
	NoWebUI         bool   `json:"no_web_ui,omitempty"`
	Initial         *InitialSummary `json:"initial,omitempty"`
	Steps           []StepSummary    `json:"steps,omitempty"`
	Budget          *BudgetSummary   `json:"budget,omitempty"`
	// RequiresApproval reports whether the run_automate tool path should
	// prompt the user before launching this workflow. nil means the field
	// was unset in JSON (defaults to true). Explicit false marks the
	// workflow as agent-runnable without user confirmation. Serialized
	// as `null` when nil — the field is intentionally NOT omitempty so
	// the WebUI can distinguish "unset (defaults to required)" from
	// "absent (treated as not_required)".
	RequiresApproval *bool `json:"requires_approval"`
	// SubagentTimeoutSeconds overrides the per-run_subagent tool timeout
	// (default 1800 = 30 minutes). nil means use the default. Same nil-
	// semantics as RequiresApproval — serialized as `null` when unset.
	SubagentTimeoutSeconds *int `json:"subagent_timeout_seconds"`
	// AllowedPaths is the display-only view of the workflow's
	// declared allowed_paths entries. Populated by Summarize after
	// the same Validate() that the loader runs, so a malformed entry
	// surfaces as a parse error rather than silently dropping the
	// whole field. Entries are sorted by path for stable display.
	AllowedPaths []AllowedPathSummary `json:"allowed_paths,omitempty"`
	// Warnings collects advisory messages produced while building the
	// summary — currently the system-prefix warning when an
	// allowed_path falls under /etc, /usr, /var, etc. The CLI and
	// WebUI render these alongside the allowed_paths block so the
	// user sees the "this workflow touches platform infrastructure"
	// heads-up even when the path itself is well-formed.
	Warnings []string `json:"warnings,omitempty"`
}

// AllowedPathSummary is the display-only mirror of workflow.AllowedPath.
// It deliberately does NOT carry a Validate method — the parser runs
// workflow.AllowedPath.Validate() once during Summarize, so the summary
// only contains entries that already passed validation.
type AllowedPathSummary struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Reason string `json:"reason,omitempty"`
}

// allowedPathRaw is the raw parsed form of an allowed_path entry before
// validation. Defined at package level so it can be used by the helper
// functions parseSummaryAllowedPaths and extractSystemPathWarnings.
type allowedPathRaw struct {
	Path   string `json:"path,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// IsApprovalRequired returns true unless the workflow JSON explicitly
// declared requires_approval: false. Used by the agent tool path to
// decide whether to surface the intent-confirmation prompt.
func (s *Summary) IsApprovalRequired() bool {
	if s == nil || s.RequiresApproval == nil {
		return true
	}
	return *s.RequiresApproval
}

// BudgetSummary mirrors the cmd-level budget config in a package that has no
// cmd dependency, so the overview renderer can display it.
type BudgetSummary struct {
	USD    float64   `json:"usd"`
	WarnAt []float64 `json:"warn_at,omitempty"`
}

// InitialSummary describes the initial run.
type InitialSummary struct {
	Persona           string                    `json:"persona,omitempty"`
	Provider          string                    `json:"provider,omitempty"`
	Model             string                    `json:"model,omitempty"`
	MaxIterations     int                       `json:"max_iterations"`
	RiskProfile       string                    `json:"risk_profile,omitempty"`
	HasPrompt         bool                      `json:"has_prompt"`
	SubagentOverrides []SubagentOverrideSummary `json:"subagent_overrides,omitempty"`
	AllowedPaths      []AllowedPathSummary       `json:"allowed_paths,omitempty"`
}

// SubagentOverrideSummary describes one entry of subagent_overrides for display.
type SubagentOverrideSummary struct {
	Persona  string `json:"persona"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// StepSummary describes a single workflow step.
//
// Kind is one of "agent" (LLM inference) or "shell" (raw command). For shell
// steps, CommandPreview holds a single-line excerpt of the command for display.
type StepSummary struct {
	Name           string `json:"name,omitempty"`
	Kind           string `json:"kind"`
	Persona        string `json:"persona,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	When           string `json:"when,omitempty"`
	CommandPreview string `json:"command_preview,omitempty"`
	AllowedPaths   []AllowedPathSummary `json:"allowed_paths,omitempty"`
}

// Summarize parses a workflow file and returns its high-level structure.
// Fields the JSON does not specify are left at their zero value.
func Summarize(path string) (*Summary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	type subagentOverrideRaw struct {
		Provider string `json:"provider,omitempty"`
		Model    string `json:"model,omitempty"`
	}

	type initialRaw struct {
		Persona           string                         `json:"persona,omitempty"`
		Provider          string                         `json:"provider,omitempty"`
		Model             string                         `json:"model,omitempty"`
		MaxIterations     *int                           `json:"max_iterations,omitempty"`
		RiskProfile       string                         `json:"risk_profile,omitempty"`
		Prompt            string                         `json:"prompt,omitempty"`
		PromptFile        string                         `json:"prompt_file,omitempty"`
		SubagentOverrides map[string]subagentOverrideRaw `json:"subagent_overrides,omitempty"`
		AllowedPaths      []allowedPathRaw               `json:"allowed_paths,omitempty"`
	}

	type stepRaw struct {
		Name         string           `json:"name,omitempty"`
		Persona      string           `json:"persona,omitempty"`
		Provider     string           `json:"provider,omitempty"`
		Model        string           `json:"model,omitempty"`
		When         string           `json:"when,omitempty"`
		Prompt       string           `json:"prompt,omitempty"`
		PromptFile   string           `json:"prompt_file,omitempty"`
		Command      string           `json:"command,omitempty"`
		CommandFile  string           `json:"command_file,omitempty"`
		AllowedPaths []allowedPathRaw `json:"allowed_paths,omitempty"`
	}

	type budgetRaw struct {
		USD    float64   `json:"usd,omitempty"`
		WarnAt []float64 `json:"warn_at,omitempty"`
	}

	var raw struct {
		Description            string           `json:"description,omitempty"`
		ContinueOnError       bool             `json:"continue_on_error,omitempty"`
		NoWebUI               bool             `json:"no_web_ui,omitempty"`
		Initial               *initialRaw      `json:"initial,omitempty"`
		Steps                 []stepRaw        `json:"steps,omitempty"`
		Budget                *budgetRaw       `json:"budget,omitempty"`
		RequiresApproval      *bool            `json:"requires_approval,omitempty"`
		SubagentTimeoutSeconds *int            `json:"subagent_timeout_seconds,omitempty"`
		AllowedPaths          []allowedPathRaw `json:"allowed_paths,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	out := &Summary{
		Description:             raw.Description,
		ContinueOnError:         raw.ContinueOnError,
		NoWebUI:                 raw.NoWebUI,
		RequiresApproval:        raw.RequiresApproval,
		SubagentTimeoutSeconds:  raw.SubagentTimeoutSeconds,
	}
	if raw.Budget != nil && raw.Budget.USD > 0 {
		out.Budget = &BudgetSummary{
			USD:    raw.Budget.USD,
			WarnAt: append([]float64(nil), raw.Budget.WarnAt...),
		}
	}
	if len(raw.AllowedPaths) > 0 {
		entries := make([]AllowedPathSummary, 0, len(raw.AllowedPaths))
		warnings := make([]string, 0)
		for i, ap := range raw.AllowedPaths {
			path := strings.TrimSpace(ap.Path)
			mode := strings.TrimSpace(ap.Mode)
			reason := strings.TrimSpace(ap.Reason)
			if err := validateSummaryAllowedPath(path, mode); err != nil {
				// Match the loader's behavior: a malformed entry
				// surfaces as a parse error rather than silently
				// dropping the whole field. The user gets a clear
				// attribution to the offending index. The rule set
				// is intentionally identical to
				// workflow.AllowedPath.Validate — Summarize runs
				// before LoadAgentWorkflowConfig in production paths
				// that go through the CLI, but Summarize is also
				// reachable on its own (e.g. discovery listing),
				// so duplicating the schema check here is cheap and
				// removes the need for a workflow → automate import
				// (which would be a cycle, since agent → automate
				// and workflow → agent already exist).
				return nil, fmt.Errorf("allowed_paths[%d]: %w", i, err)
			}
			if isSummarySystemPathPrefix(path) {
				warnings = append(warnings, fmt.Sprintf("allowed_paths[%d] %q falls under a system prefix; the workflow will be able to read/write platform infrastructure", i, path))
			}
			entries = append(entries, AllowedPathSummary{
				Path:   path,
				Mode:   mode,
				Reason: reason,
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		out.AllowedPaths = entries
		if len(warnings) > 0 {
			sort.Strings(warnings)
			out.Warnings = append(out.Warnings, warnings...)
		}
	}
	if raw.Initial != nil {
		init := &InitialSummary{
			Persona:     raw.Initial.Persona,
			Provider:    raw.Initial.Provider,
			Model:       raw.Initial.Model,
			RiskProfile: raw.Initial.RiskProfile,
			HasPrompt:   strings.TrimSpace(raw.Initial.Prompt) != "" || strings.TrimSpace(raw.Initial.PromptFile) != "",
		}
		if raw.Initial.MaxIterations != nil {
			init.MaxIterations = *raw.Initial.MaxIterations
		}
		// Sort persona keys for stable display ordering.
		personas := make([]string, 0, len(raw.Initial.SubagentOverrides))
		for k := range raw.Initial.SubagentOverrides {
			personas = append(personas, k)
		}
		sort.Strings(personas)
		for _, p := range personas {
			ov := raw.Initial.SubagentOverrides[p]
			init.SubagentOverrides = append(init.SubagentOverrides, SubagentOverrideSummary{
				Persona:  p,
				Provider: ov.Provider,
				Model:    ov.Model,
			})
		}
		// Parse initial-level allowed_paths.
		if len(raw.Initial.AllowedPaths) > 0 {
			entries, errs := parseSummaryAllowedPaths(raw.Initial.AllowedPaths, "initial")
			if len(errs) > 0 {
				return nil, errs[0]
			}
			init.AllowedPaths = entries
			// Append any system-prefix warnings to the summary.
			for _, w := range extractSystemPathWarnings(raw.Initial.AllowedPaths, "initial") {
				out.Warnings = append(out.Warnings, w)
			}
		}
		out.Initial = init
	}
	for i, s := range raw.Steps {
		stepPrefix := "steps[" + strconv.Itoa(i) + "]"
		kind := "agent"
		preview := ""
		if strings.TrimSpace(s.Command) != "" || strings.TrimSpace(s.CommandFile) != "" {
			kind = "shell"
			preview = previewCommand(s.Command, s.CommandFile)
		}
		stepSummary := StepSummary{
			Name:           s.Name,
			Kind:           kind,
			Persona:        s.Persona,
			Provider:       s.Provider,
			Model:          s.Model,
			When:           s.When,
			CommandPreview: preview,
		}
		// Parse step-level allowed_paths.
		if len(s.AllowedPaths) > 0 {
			entries, errs := parseSummaryAllowedPaths(s.AllowedPaths, stepPrefix)
			if len(errs) > 0 {
				return nil, errs[0]
			}
			stepSummary.AllowedPaths = entries
			// Append any system-prefix warnings to the summary.
			for _, w := range extractSystemPathWarnings(s.AllowedPaths, stepPrefix) {
				out.Warnings = append(out.Warnings, w)
			}
		}
		out.Steps = append(out.Steps, stepSummary)
	}
	return out, nil
}

// previewCommand returns a single-line excerpt suitable for terminal display.
func previewCommand(command, commandFile string) string {
	command = strings.TrimSpace(command)
	commandFile = strings.TrimSpace(commandFile)
	if command != "" {
		// Collapse to a single line.
		firstLine := command
		if idx := strings.IndexAny(command, "\r\n"); idx >= 0 {
			firstLine = strings.TrimSpace(command[:idx]) + " …"
		}
		if len(firstLine) > 72 {
			firstLine = firstLine[:71] + "…"
		}
		return "$ " + firstLine
	}
	if commandFile != "" {
		return "$ " + commandFile
	}
	return ""
}

// validateSummaryAllowedPath enforces the same rules as
// workflow.AllowedPath.Validate() for the JSON-parsed entry inside
// Summarize. The rule set is duplicated (rather than imported) because
// the dependency graph would otherwise create an import cycle:
// workflow → agent → agent_tools → automate, so automate cannot
// import workflow. Summarize must reject malformed entries so a
// workflow with a broken allowed_paths block doesn't silently
// launch — the same contract the loader enforces, just one level up
// the stack.
func validateSummaryAllowedPath(path, mode string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if strings.HasPrefix(path, "~") {
		return errors.New("path must not start with `~`; provide an absolute path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute; got %q", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned != path {
		return fmt.Errorf("path must already be cleaned (no `./`, `..`, or trailing separators); got %q", path)
	}
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("path must not contain `..` segments; got %q", path)
	}
	switch mode {
	case "read_only", "read_write":
		// ok
	default:
		return fmt.Errorf("mode must be \"read_only\" or \"read_write\"; got %q", mode)
	}
	return nil
}

// parseSummaryAllowedPaths validates and converts a raw allowed_paths slice
// into a sorted []AllowedPathSummary. Returns the first error found (if any).
// The scopePrefix is used in error messages (e.g., "initial", "steps[0]").
func parseSummaryAllowedPaths(rawPaths []allowedPathRaw, scopePrefix string) ([]AllowedPathSummary, []error) {
	if len(rawPaths) == 0 {
		return nil, nil
	}
	entries := make([]AllowedPathSummary, 0, len(rawPaths))
	var errs []error
	for i, ap := range rawPaths {
		path := strings.TrimSpace(ap.Path)
		mode := strings.TrimSpace(ap.Mode)
		reason := strings.TrimSpace(ap.Reason)
		if err := validateSummaryAllowedPath(path, mode); err != nil {
			errs = append(errs, fmt.Errorf("%s: allowed_paths[%d]: %w", scopePrefix, i, err))
			continue
		}
		entries = append(entries, AllowedPathSummary{
			Path:   path,
			Mode:   mode,
			Reason: reason,
		})
	}
	if len(errs) > 0 {
		return nil, errs
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// extractSystemPathWarnings returns a list of warning messages for any
// allowed_paths that fall under a system prefix. The scopePrefix is used
// in the warning message (e.g., "initial", "step").
func extractSystemPathWarnings(rawPaths []allowedPathRaw, scopePrefix string) []string {
	var warnings []string
	for i, ap := range rawPaths {
		path := strings.TrimSpace(ap.Path)
		if isSummarySystemPathPrefix(path) {
			warnings = append(warnings, fmt.Sprintf("%s: allowed_paths[%d] %q falls under a system prefix; the workflow will be able to read/write platform infrastructure", scopePrefix, i, path))
		}
	}
	return warnings
}

// isSummarySystemPathPrefix mirrors workflow.IsSystemPathPrefix for the
// Summarize-side rendering. The list must stay in sync with the
// system prefixes in workflow.IsSystemPathPrefix and
// pkg/agent/path_tier.go::systemPathPrefixes — three places that grow
// together when the OS adds a new platform-infrastructure directory.
// On the automate side the prefix list is local so the summary parser
// stays decoupled from the workflow package.
func isSummarySystemPathPrefix(p string) bool {
	if p == "" {
		return false
	}
	for _, prefix := range summarySystemPathPrefixList() {
		if p == prefix {
			return true
		}
		if strings.HasPrefix(p, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func summarySystemPathPrefixList() []string {
	return []string{
		"/etc",
		"/usr",
		"/var",
		"/bin",
		"/sbin",
		"/boot",
		"/proc",
		"/sys",
		"/dev",
		"/lib",
		"/lib64",
		"/opt",
		"/root",
		"/System",
		"/Library",
		"/private/etc",
		"/private/var",
		"/Applications",
	}
}
