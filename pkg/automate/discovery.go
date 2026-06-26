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
func Dir() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "automate")
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
// overview before kicking off the workflow.
type Summary struct {
	Description     string
	ContinueOnError bool
	NoWebUI         bool
	Initial         *InitialSummary
	Steps           []StepSummary
	Budget          *BudgetSummary
	// RequiresApproval reports whether the run_automate tool path should
	// prompt the user before launching this workflow. nil means the field
	// was unset in JSON (defaults to true). Explicit false marks the
	// workflow as agent-runnable without user confirmation.
	RequiresApproval *bool
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
	USD    float64
	WarnAt []float64
}

// InitialSummary describes the initial run.
type InitialSummary struct {
	Persona           string
	Provider          string
	Model             string
	MaxIterations     int
	RiskProfile       string
	HasPrompt         bool
	SubagentOverrides []SubagentOverrideSummary
}

// SubagentOverrideSummary describes one entry of subagent_overrides for display.
type SubagentOverrideSummary struct {
	Persona  string
	Provider string
	Model    string
}

// StepSummary describes a single workflow step.
//
// Kind is one of "agent" (LLM inference) or "shell" (raw command). For shell
// steps, CommandPreview holds a single-line excerpt of the command for display.
type StepSummary struct {
	Name           string
	Kind           string
	Persona        string
	Provider       string
	Model          string
	When           string
	CommandPreview string
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
	}

	type stepRaw struct {
		Name        string `json:"name,omitempty"`
		Persona     string `json:"persona,omitempty"`
		Provider    string `json:"provider,omitempty"`
		Model       string `json:"model,omitempty"`
		When        string `json:"when,omitempty"`
		Prompt      string `json:"prompt,omitempty"`
		PromptFile  string `json:"prompt_file,omitempty"`
		Command     string `json:"command,omitempty"`
		CommandFile string `json:"command_file,omitempty"`
	}

	type budgetRaw struct {
		USD    float64   `json:"usd,omitempty"`
		WarnAt []float64 `json:"warn_at,omitempty"`
	}

	var raw struct {
		Description      string      `json:"description,omitempty"`
		ContinueOnError  bool        `json:"continue_on_error,omitempty"`
		NoWebUI          bool        `json:"no_web_ui,omitempty"`
		Initial          *initialRaw `json:"initial,omitempty"`
		Steps            []stepRaw   `json:"steps,omitempty"`
		Budget           *budgetRaw  `json:"budget,omitempty"`
		RequiresApproval *bool       `json:"requires_approval,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	out := &Summary{
		Description:      raw.Description,
		ContinueOnError:  raw.ContinueOnError,
		NoWebUI:          raw.NoWebUI,
		RequiresApproval: raw.RequiresApproval,
	}
	if raw.Budget != nil && raw.Budget.USD > 0 {
		out.Budget = &BudgetSummary{
			USD:    raw.Budget.USD,
			WarnAt: append([]float64(nil), raw.Budget.WarnAt...),
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
		out.Initial = init
	}
	for _, s := range raw.Steps {
		kind := "agent"
		preview := ""
		if strings.TrimSpace(s.Command) != "" || strings.TrimSpace(s.CommandFile) != "" {
			kind = "shell"
			preview = previewCommand(s.Command, s.CommandFile)
		}
		out.Steps = append(out.Steps, StepSummary{
			Name:           s.Name,
			Kind:           kind,
			Persona:        s.Persona,
			Provider:       s.Provider,
			Model:          s.Model,
			When:           s.When,
			CommandPreview: preview,
		})
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
