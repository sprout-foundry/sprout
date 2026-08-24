// Package configuration: low-context mode (LCM) abstraction.
// ContextProfile is the resolved shape every downstream call site reads.
package configuration

import "fmt"

// ContextMode is the user-facing context-engine selector. Empty string defaults to "full".
type ContextMode string

const (
	// ContextModeFull is the default: all tools, full prompt, proactive context enabled.
	ContextModeFull ContextMode = "full"

	// ContextModeLowContext activates the LCM levers: curated tool allowlist, lite prompt, etc.
	ContextModeLowContext ContextMode = "low_context"
)

// ContextFloor is the minimum context window at which sprout will start. Below this, the agent is unusable.
const ContextFloor = 8_000

// ContextProfile is the resolved shape of every context-engine lever.
// A zero value means "use all defaults" (full mode, no overrides).
type ContextProfile struct {
	Mode                      ContextMode `json:"mode,omitempty"`
	ToolAllowlist             []string    `json:"tool_allowlist,omitempty"`
	SystemPromptPath          string      `json:"system_prompt_path,omitempty"`
	SkipProactiveContext      bool        `json:"skip_proactive_context,omitempty"`
	CompactionTriggerFraction float64     `json:"compaction_trigger_fraction,omitempty"`
	RecentTurnsToPreserve     int         `json:"recent_turns_to_preserve,omitempty"`
	RepoMapDefaultDepth       int         `json:"repo_map_default_depth,omitempty"`
}

// fullContextProfile is the baked full-context preset (all defaults).
var fullContextProfile = ContextProfile{
	Mode: ContextModeFull,
}

// lowContextProfile is the baked LCM preset.
//
// ask_user is deliberately part of the allowlist: it is the agent's
// interactive channel with the human, and LCM must never disable the
// ability to ask the user a question (the cloud IDE's ask_user/edit-approval
// flows depend on it).
var lowContextProfile = ContextProfile{
	Mode: ContextModeLowContext,
	ToolAllowlist: []string{
		"shell_command",
		"read_file",
		"write_file",
		"edit_file",
		"search",
		"repo_map",
		"web_search",
		"fetch_url",
		"commit",
		"list_changes",
		"recover_file",
		"run_subagent",
		"ask_user",
	},
	SystemPromptPath:          "prompts/system_prompt.lite.md",
	SkipProactiveContext:      true,
	CompactionTriggerFraction: 0.85,
	RecentTurnsToPreserve:     2,
	RepoMapDefaultDepth:       1,
}

// subagentContextThreshold is the context window below which LCM auto-activates (132K).
const subagentContextThreshold = 132_000

// ResolveContextProfile picks the effective profile from the user's config plus the detected model context window.
// Precedence: hard floor > explicit ContextMode > auto-detect by window > full default.
func ResolveContextProfile(cfg *Config, modelContextWindow int) (ContextProfile, error) {
	// Hard floor: below ContextFloor the agent is unusable.
	if modelContextWindow > 0 && modelContextWindow < ContextFloor {
		return fullContextProfile, fmt.Errorf(
			"model context window %d is below the %d-token minimum for sprout; "+
				"the agent cannot operate — even Low-Context Mode needs room for the "+
				"lite prompt (~1.5K tokens) plus at least one tool round-trip and a response. "+
				"Switch to a larger-context model (/model) or raise the model's context limit",
			modelContextWindow,
			ContextFloor,
		)
	}

	switch {
	case cfg != nil && cfg.ContextMode == ContextModeLowContext:
		return lowContextProfile, nil
	case cfg != nil && cfg.ContextMode == ContextModeFull:
		return fullContextProfile, nil
	case modelContextWindow > 0 && modelContextWindow < subagentContextThreshold:
		return lowContextProfile, nil
	default:
		return fullContextProfile, nil
	}
}

// EffectiveContextCapMinimum is the minimum cap a user may set via Config.MaxContextTokens.
const EffectiveContextCapMinimum = 1024

// EffectiveContextCapErrorf builds the error message returned when a user-configured cap falls below the minimum.
func EffectiveContextCapErrorf(got int) error {
	return fmt.Errorf("value must be at least %d when setting a cap (got %d)", EffectiveContextCapMinimum, got)
}

// ResolveEffectiveContextCap returns the user's effective context cap:
// the smaller of the model's native window and the configured MaxContextTokens.
// Nil/zero cap means "no cap" (returns native window).
func ResolveEffectiveContextCap(cfg *Config, nativeContextWindow int) (int, error) {
	if cfg == nil || cfg.MaxContextTokens == nil || *cfg.MaxContextTokens <= 0 {
		return nativeContextWindow, nil
	}
	cap := *cfg.MaxContextTokens

	// Reject explicitly-set caps below the minimum.
	if cap < EffectiveContextCapMinimum {
		return 0, EffectiveContextCapErrorf(cap)
	}

	if nativeContextWindow <= 0 {
		return cap, nil
	}
	if cap < nativeContextWindow {
		return cap, nil
	}
	return nativeContextWindow, nil
}
