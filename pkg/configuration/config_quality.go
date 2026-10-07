package configuration

// QualityConfig controls the quality-after-edits step: after a turn that
// changed application code, the runtime runs the project's formatter and
// linter and repairs the findings in the same turn. The section is off by
// default in the CLI; a config layer — global, project (workspace), or an
// embedding environment writing the same layers — opts in with
// `{"quality": {"enabled": true}}`.
//
// The section deliberately has no repair-budget fields: the quality repair
// loop reuses the verification section's repair_attempts /
// total_repair_rounds limits, so there is one repair-budget setting for both
// turn-end repair loops, not two. Enable the quality step on its own; its
// repair budget is whatever the verification section (or the defaults) says.
type QualityConfig struct {
	// Enabled gates the turn-end quality run. Off by default; an explicit
	// false in a narrower layer disables a broader layer's enable
	// (explicit-key merge semantics).
	Enabled bool `json:"enabled,omitempty"`

	// FormatCommand is the explicit formatter command for the project's
	// quality run. It is the "explicit project configuration" source: a
	// human sets it in the project layer (.sprout/workspace.json) or in
	// global config for projects without a starter manifest (or one that
	// declares no formatter). Model output never populates it — the
	// quality runner ignores commands the model proposes.
	FormatCommand string `json:"format_command,omitempty"`

	// LintCommand is the explicit linter command for the project's quality
	// run. Same source and semantics as FormatCommand.
	LintCommand string `json:"lint_command,omitempty"`
}

// QualityEnabled reports whether the quality-after-edits step is enabled for
// this config. Off by default in the CLI; it becomes enabled only when a
// config layer names "quality.enabled" with a truthy value that survives the
// layer merge. A nil config resolves to off.
func (c *Config) QualityEnabled() bool {
	if c == nil {
		return false
	}
	return c.Quality != nil && c.Quality.Enabled
}

// QualityFormatCommand returns the explicit formatter command for the
// quality-after-edits step after layer merge, or "" when no layer set it. It
// is the "explicit project configuration" source of commands, set by a human
// — never by model output.
func (c *Config) QualityFormatCommand() string {
	if c == nil || c.Quality == nil {
		return ""
	}
	return c.Quality.FormatCommand
}

// QualityLintCommand returns the explicit linter command for the
// quality-after-edits step after layer merge, or "" when no layer set it.
// Same source and semantics as QualityFormatCommand.
func (c *Config) QualityLintCommand() string {
	if c == nil || c.Quality == nil {
		return ""
	}
	return c.Quality.LintCommand
}
