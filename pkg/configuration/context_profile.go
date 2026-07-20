// Package configuration: SP-125 low-context mode (LCM) abstraction.
//
// ContextProfile is the resolved shape every downstream call site reads when
// it needs to know whether sprout is operating in full-context mode
// (default) or low-context mode. The profile is computed once at agent
// creation by ResolveContextProfile and stored on the Agent — call sites
// must never re-derive it (see SP-125 R5 / "Resolution is centralized" in
// the roadmap).
//
// The split between Config.ContextMode (the user-facing selector) and
// ContextProfile (the resolved lever values) mirrors the existing
// Config.RiskProfile / AutoApproveRules split: a small user-facing knob
// expands into a struct full of concrete preset values. A future medium
// mode is one new preset value, not a new config field.
package configuration

import "fmt"

// ContextMode is the user-facing context-engine selector. The empty string
// ("") is treated identically to "full" for resolution — it's the zero
// value of the field and must be safe to leave unset. Persisted as
// json:"context_mode,omitempty"; any value that doesn't match one of the
// two named constants falls through to auto-detection via
// ResolveContextProfile (so a typo in the config file degrades gracefully
// to the default rather than becoming a hard error).
type ContextMode string

const (
	// ContextModeFull is the default sprout mode: all 44 tools, the full
	// orchestrator system prompt, project AGENTS.md injected, proactive
	// context enabled, and the standard compaction trigger (0.70).
	// Resolved by passing a zero-value ContextProfile — every lever
	// reads as empty/zero/false and downstream code already treats that
	// as "use built-in default".
	ContextModeFull ContextMode = "full"

	// ContextModeLowContext activates the LCM levers (curated 8-tool
	// allowlist, lite prompt, no proactive context, compaction trigger
	// 0.85, recency 2, repo-map depth 1). AGENTS.md is still injected
	// (project conventions are mandatory in every mode). Activated
	// explicitly via config, or auto-detected when the selected model
	// reports a context window below SubagentMinContext (64K).
	ContextModeLowContext ContextMode = "low_context"
)

// ContextFloor is the absolute minimum context window at which sprout will
// start at all, regardless of profile. Below this, even the lite prompt
// (~1.5K tokens) plus a single read_file call (~2.5K tokens) plus a
// minimal response leaves no room to operate. ResolveContextProfile
// hard-errors when the caller reports a known window below this floor so
// the user gets a clear directive rather than a silent broken session.
//
// Set at 8K: comfortably above the ~4K absolute minimum (prompt + one
// tool I/O + response) but below the smallest practical model context
// windows on the market. Models in the 8K–32K band get LCM; below 8K
// gets refused. Deliberately not user-tunable; the floor is a
// guardrail, not a knob.
const ContextFloor = 8_000

// ContextProfile is the resolved shape of every context-engine lever.
// Constructed exactly once via ResolveContextProfile at agent creation
// and read downstream at every call site that depends on it. A zero
// value is intentionally safe and means "use all defaults" — i.e. full
// mode, no overrides.
//
// Field semantics:
//
//   - Mode: which preset was selected (Full vs LowContext). Call sites
//     that want to branch on *intent* read this; call sites that want
//     to branch on *behavior* read the boolean/value fields below.
//
//   - ToolAllowlist: if non-empty, downstream tool registration filters
//     BuildToolDefinitions to only these names. Order is preserved when
//     exposed to the model. Empty means "all tools available" — the
//     zero/full default.
//
//   - SystemPromptPath: the embedded prompt path to load (relative to
//     pkg/agent/prompts). Empty means use the default full prompt;
//     downstream code maps the path suffix to the right //go:embed
//     variable. The two currently-known values are
//     "prompts/system_prompt.md" (default, empty string equivalent)
//     and "prompts/system_prompt.lite.md" (LCM).
//
//   - SkipProactiveContext: when true, downstream prompt builders
//     skip the semantically-recalled prior-turn block injected after
//     turn 1. Disables cross-session recall in this session.
//
//   - CompactionTriggerFraction: when non-zero, overrides the
//     default trigger fraction (1.0 - totalReservedFraction(), 0.70
//     in full mode). LCM uses 0.85 to push compaction closer to the
//     window edge so the model has more working room per turn.
//
//   - RecentTurnsToPreserve: when non-zero, overrides the default
//     recent-turn count kept at full fidelity during rollups
//     (default 5 in full mode). LCM uses 2 because LCM sessions are
//     short (2–4 round-trips) and the recency window is almost the
//     whole conversation.
//
//   - RepoMapDefaultDepth: when non-zero, overrides the default
//     depth passed to repo_map (default 3 in full mode). LCM uses 1
//     (directory tree only, no symbols) to keep repo_map output
//     under ~800 tokens.
type ContextProfile struct {
	Mode                      ContextMode `json:"mode,omitempty"`
	ToolAllowlist             []string    `json:"tool_allowlist,omitempty"`
	SystemPromptPath          string      `json:"system_prompt_path,omitempty"`
	SkipProactiveContext      bool        `json:"skip_proactive_context,omitempty"`
	CompactionTriggerFraction float64     `json:"compaction_trigger_fraction,omitempty"`
	RecentTurnsToPreserve     int         `json:"recent_turns_to_preserve,omitempty"`
	RepoMapDefaultDepth       int         `json:"repo_map_default_depth,omitempty"`
}

// fullContextProfile is the baked full-context preset. Every lever field
// is its zero value, meaning "use the built-in default" — which is the
// whole point of the zero-value-is-safe design (the roadmap's lever 5).
// Returned by ResolveContextProfile whenever the user hasn't opted into
// LCM and the model's context window is >= SubagentMinContext (64K), or
// when ContextMode == "".
var fullContextProfile = ContextProfile{
	Mode: ContextModeFull,
}

// lowContextProfile is the baked LCM preset. Order of tools in
// ToolAllowlist matters: the model sees them in this order in its tool
// list, and the edit-test-commit flow benefits from listing the edit
// primitives before the safety-net tools (`list_changes`, `recover_file`)
// so the model reaches for writes before recovery. Source: roadmap
// "Tool Set Decision" (Option B).
var lowContextProfile = ContextProfile{
	Mode: ContextModeLowContext,
	ToolAllowlist: []string{
		"shell_command",
		"read_file",
		"write_file",
		"edit_file",
		"search_files",
		"commit",
		"list_changes",
		"recover_file",
	},
	SystemPromptPath:          "prompts/system_prompt.lite.md",
	SkipProactiveContext:      true,
	CompactionTriggerFraction: 0.85,
	RecentTurnsToPreserve:     2,
	RepoMapDefaultDepth:       1,
}

// subagentContextThreshold mirrors modelcontract.SubagentMinContext (64K)
// locally so this package does not introduce a cfg→modelcontract import
// edge. The two constants track each other intentionally; if they ever
// diverge, the resolution function should be updated to import the
// canonical modelcontract value. Kept as a private var (not exported)
// because the threshold is an implementation detail of ResolveContextProfile,
// not a knob callers need.
const subagentContextThreshold = 64_000

// ResolveContextProfile picks the effective profile from the user's
// config plus the detected model context window. Called once at agent
// creation; the result is stored on the Agent and read by every
// downstream call site.
//
// Precedence (highest first):
//
//  1. Hard floor — if modelContextWindow is a known positive value below
//     ContextFloor (8K), return an error. This is unconditional: even
//     explicit "full" cannot rescue the session because no amount of
//     lever-pulling fits prompt + one tool round-trip + a response
//     below ~4K tokens. The caller is expected to surface the error
//     to the user.
//
//  2. Explicit cfg.ContextMode — "low_context" or "full" both win
//     outright over auto-detection. A user who explicitly sets the
//     field has overridden any window-based guess.
//
//  3. Auto-detect — a known context window below subagentContextThreshold
//     (64K) flips LCM on with a strong warning (callers can detect the
//     auto-detect case by comparing the returned Mode to what cfg
//     requested, or via a future explicit-notice hook).
//
//  4. Default — fullContextProfile. Applies when cfg is nil, when
//     cfg.ContextMode is empty or unrecognized, or when the model
//     context window is unknown (0 / negative).
//
// Both cfg==nil and modelContextWindow<=0 are tolerated — those are
// the "we don't know yet" inputs and they must not error. They
// resolve to the full preset (step 4).
func ResolveContextProfile(cfg *Config, modelContextWindow int) (ContextProfile, error) {
	// Hard floor: below this, no amount of lever-pulling makes the
	// agent usable. The system prompt alone is ~1.5K tokens even in
	// lite mode; below ~8K a model cannot fit prompt + one tool
	// round-trip + a response. Fail loudly so the user knows to
	// switch models rather than wonder why sprout is producing
	// empty/truncated output.
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
