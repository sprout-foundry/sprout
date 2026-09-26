package automate

// discovery_summarize.go — workflow summarization: Summarize (the step
// walker + preview/validation helpers) and the system-path warning
// heuristics. Split out of discovery.go.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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
		ContinueOnError        bool             `json:"continue_on_error,omitempty"`
		NoWebUI                bool             `json:"no_web_ui,omitempty"`
		Initial                *initialRaw      `json:"initial,omitempty"`
		Steps                  []stepRaw        `json:"steps,omitempty"`
		Budget                 *budgetRaw       `json:"budget,omitempty"`
		RequiresApproval       *bool            `json:"requires_approval,omitempty"`
		SubagentTimeoutSeconds *int             `json:"subagent_timeout_seconds,omitempty"`
		AllowedPaths           []allowedPathRaw `json:"allowed_paths,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	out := &Summary{
		Description:            raw.Description,
		ContinueOnError:        raw.ContinueOnError,
		NoWebUI:                raw.NoWebUI,
		RequiresApproval:       raw.RequiresApproval,
		SubagentTimeoutSeconds: raw.SubagentTimeoutSeconds,
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
