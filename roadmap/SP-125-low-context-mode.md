# SP-125 — Low-Context Mode (32K Context Support)

**Status:** 🔵 Scoping — not yet approved for implementation
**Created:** 2026-07-20
**Renumbered:** 2026-07-20 (was draft SP-115; SP-115 was in use for the
StateManager refactor + CLI keyboard hints, which surfaced during pre-commit
git inspection. Renumbered to SP-125, the next free slot.)
**Type:** Feature scoping / design exercise

## Context Window Bands

The design defines three thresholds that carve the context-window spectrum:

| Band | Range | Behavior |
|---|---|---|
| **Full** | ≥ 64K (`SubagentMinContext`) | Default sprout — all tools, full prompt, AGENTS.md, proactive context |
| **Low-Context (LCM)** | 8K–64K | Lite prompt, 8-tool allowlist, AGENTS.md skipped, tighter compaction |
| **Refused** | < 8K (`ContextFloor`) | Hard error at agent creation — sprout will not start |

The 8K floor is non-negotiable and not user-tunable. Below it, even the lite
prompt (~1.5K tokens) plus a single `read_file` (~2.5K) plus a minimal response
leaves no room to operate. Rather than silently producing a broken experience
(empty responses, immediate truncation, tool calls that don't fit), sprout
errors out with a directive message:

```
ERROR: model context window 4096 is below the 8000-token minimum for sprout.
Even Low-Context Mode needs room for the lite prompt (~1.5K tokens) plus at
least one tool round-trip and a response.
Switch to a larger-context model (/model) or raise the model's context limit.
```

This floor lives in `ResolveContextProfile` (the single point where the decision
is made), not scattered across call sites. See §"Implementation Design".

## Problem

Sprout is tuned for large-context models (128K+). The orchestrator system
prompt alone is ~6.6K tokens; with `AGENTS.md`, all tool schemas, and proactive
context, the **fixed overhead per turn is ~13–16K tokens before the model sees a
single user message or tool result**. On a 128K window that's ~12% — fine. On a
32K window it's ~45%, leaving only ~18K for conversation + tool I/O + response,
which collapses the agent loop within 3–4 turns.

Today, `pkg/modelcontract/eligibility.go` **hard-blocks** any model below
`SubagentMinContext` (64K):

```go
case m.ContextWindow >= SubagentMinContext:
    return []string{RoleSubagent}
default:
    return nil // ← 32K models land here, ineligible for anything
```

The user wants to know **what it would take** to make sprout usable at 32K —
not as a primary workflow, but as a deliberate "low-context mode" for isolated
scenarios (quick edits, single-file questions, constrained environments, cheap
models, offline/local inference).

## Goal

Define what a **Low-Context Mode (LCM)** would look like: which levers reduce
fixed overhead, what the usable envelope is at 32K, which scenarios stay
viable, and what the implementation cost is per lever. Produce a decision-ready
scoping doc — not an implementation plan.

## Non-Goals

- **Replace the large-context workflow.** LCM is an opt-in fallback, not the
  default. No regression for 128K+ models.
- **Full orchestrator parity at 32K.** Subagent delegation, multi-file refactors,
  and the Code→Test→Review loop are off the table at this size.
- **New provider integrations.** LCM works with any provider that reports a
  context window; no new adapters.

---

## Current Token Budget — Where The Tokens Go

Measured against a 32K window. Token estimates use chars/4 for prose and
chars/3.5 for JSON (accounts for structural overhead in tool schemas).

### Fixed per-turn overhead (sent on EVERY request)

| Component | Source | Tokens | Notes |
|---|---|---:|---|
| Orchestrator system prompt | `pkg/agent/prompts/system_prompt.md` | ~6,640 | 26.5KB; you're reading it |
| Project `AGENTS.md` | injected if present in repo | ~3,930 | 15.7KB; mandatory conventions |
| **All 44 tool schemas** | `BuildToolDefinitions()` | ~4,500 | JSON-serialized; see breakdown below |
| Proactive context (embedded recall) | `proactive_context.go`, default 4000 chars | ~1,000 | Only after turn 1; capped |
| Conversation preamble (tool results, history) | grows per turn | 0 → N | The variable cost |
| **Total fixed floor** | | **≈ 16,000** | **50% of a 32K window** |

### Variable per-turn costs

| Component | Typical | Notes |
|---|---:|---|
| One `read_file` of a 500-line Go file | ~2,500 | Sprout's file-size cap |
| One `repo_map` at depth=3 | ~1,500–4,000 | Scales with repo size |
| One `shell_command` with build output | ~500–3,000 | `go build ./...` on this repo ≈ 800 |
| One assistant reasoning turn | ~800–2,000 | Model thinks before tool calls |
| One tool round-trip (call + result) | ~1,500 avg | Dominates variable cost |

**The math at 32K:**

```
Fixed floor:       ~16,000 tokens  (50%)
Reserved for resp:  ~4,800 tokens  (15%)  ← context_budget.go
Reserved for think: ~3,200 tokens  (10%)  ← context_budget.go
Reserved for tool:  ~1,600 tokens  ( 5%)  ← context_budget.go
─────────────────────────────────────────
Usable for conversation:  ~6,400 tokens  (20%)
= roughly 2–4 tool round-trips before compaction
```

That's the core problem: **the fixed floor consumes the window before the
agent does any work.** Every lever below targets the fixed floor.

---

## The Levers — Ordered by Impact

### Lever 1: Curated Tool Subset  ⭐ highest impact / lowest cost

**Today:** all 44 tools are always registered (minus persona filtering).
**Per-tool schema cost** (top 10 by size):

| Tool | Schema tokens |
|---|---:|
| `browse_url` | ~322 |
| `shell_command` | ~317 |
| `get_callees` | ~224 |
| `analyze_image_content` | ~223 |
| `get_callers` | ~160 |
| `find_dead_code` | ~158 |
| `git` | ~153 |
| `analyze_ui_screenshot` | ~152 |
| `repo_map` | ~147 |
| `run_subagent` | ~140 |

**Proposal:** Define a `low_context` tool allowlist of 8 tools:

```
shell_command, read_file, write_file, edit_file, search_files, commit,
list_changes, recover_file
```

This is the **8-tool "Option B" set** (see §"Tool Set Decision" below for the full analysis that arrived here). It covers the pure edit-test-commit loop plus the change-tracking safety net (`list_changes` + `recover_file`) that lets the user say "undo the model's last edit" without `git checkout` discarding their own uncommitted work. Everything else either has a shell equivalent (`git`, `ls`, `git log`, `git status`) or belongs to a subsystem dropped in LCM (subagents, skills, memory, vision, web).

**Savings:** ~4,500 → ~815 tokens. **−3,685 tokens (~11.5% of window).**

**Cost:** One config-driven allowlist + a branch in `conversation.go:113` where persona filtering already happens. Mechanically trivial — the `filterToolsByName` pattern already exists.

**Trade-off:** No `run_subagent` (no delegation), no `repo_map` (manual exploration via `ls` + `read_file`), no vision/web/skills/memory tools. No `TodoWrite` — the model tracks 2–3 steps inline for the short sessions LCM targets. No `git` *tool* — but read-only git (`status`, `diff`, `log`, `show`) is freely available via `shell_command` per AGENTS.md, and write ops (checkout/reset/restore) aren't part of the LCM envelope anyway.

### Tool Set Decision

The choice came down to three options:

| Option | Tools | Tokens | Keeps |
|---|---|---:|---|
| **A — Minimal** | 6 | 668 | edit-test-commit only; no safety net |
| **B — Minimal + safety net** ⭐ | 8 | 815 | + `list_changes`, `recover_file` |
| **C — Minimal + todos** | 10 | 946 | + `TodoWrite`, `TodoRead` |

**Why B over A:** `list_changes` + `recover_file` (146 tokens) preserve sprout's core "undo without losing user work" promise. Without them, the only recovery path is `git checkout -- <file>`, which discards the user's own uncommitted edits alongside the agent's — a real foot-gun in short sessions where the user is actively iterating.

**Why B over C:** Todos (132 tokens) don't earn their keep at 32K. LCM sessions target 2–4 tool round-trips; the model can hold that in-context. Todos pay off in 8+ step workflows, which aren't viable at 32K anyway.

**The dropped tools, by category:**
- *Shell-replaceable* (516 tokens): `git`, `list_directory`, `view_history`, `rollback_changes`, `revert_my_changes` — all have shell equivalents (`git status`/`diff`/`log`, `ls`, `git checkout`).
- *Luxury* (3,103 tokens): subagents, skills, memory, vision, web, codegraph, structured-file tools, `repo_map` — each belongs to a subsystem that LCM excludes by design.

---

### Lever 2: Slimmed System Prompt  ⭐ highest impact / medium cost

**Today:** `system_prompt.md` is 418 lines / ~6,640 tokens. It encodes the
full orchestrator workflow: delegation rules, Code→Test→Review, persona
selection, subagent guidelines, refactoring protocols, error recovery,
duplicate detection, redaction handling, change tracking, memory system.

**Proposal:** Ship a `system_prompt.lite.md` (~80–120 lines, ~1,500 tokens)
that strips to the essentials for a no-subagent, direct-edit workflow:

- Core identity (you are an agent, act immediately, use tools)
- Tool usage (batch reads, exact-string edits, proof of success)
- Git safety rules (non-negotiable — keep verbatim)
- Error recovery (terse)
- Completion criteria (terse)

**Removed in lite:**
- Entire "Subagent Guidelines" section (no subagents in LCM)
- "Code → Test → Review → Iterate" workflow (replaced with "write, test, iterate")
- Persona Selection Guide (no delegation)
- Memory system docs (memory tools excluded from allowlist)
- Duplicate Detection / Redacted Tool Output / Change History sections
  (internal mechanics; the model doesn't need the theory)

**Savings:** ~6,640 → ~1,500. **−5,100 tokens (~16% of window).**

**Cost:** Writing and maintaining a second prompt. Risk of drift between
the two. Mitigation: factor shared sections (git rules, tool guidelines)
into included partials if a third prompt ever appears; for now, a single
forked file is simpler.

**Trade-off:** Lite mode loses the orchestration intelligence. The model
won't delegate, won't run formal reviews, won't follow the 4-phase process.
That's the point — it's for direct work.

---

### Lever 3: Skip `AGENTS.md` Injection

**Today:** `AGENTS.md` (~3,930 tokens) is unconditionally injected when
present in the repo root.

**Proposal:** In LCM, skip `AGENTS.md` unless the user explicitly requests
it (e.g. `--include-agents-md` flag or a `/context full` slash command).

**Savings:** **−3,930 tokens (~12% of window).**

**Cost:** Near zero — one condition in the prompt-build path.

**Trade-off:** Model loses project-specific conventions (design system
tokens, test isolation rules, context architecture notes). For a quick
single-file edit, these usually don't matter. For anything touching the
design system or test infrastructure, the user opts back in. **This is the
most dangerous lever** — silently dropping conventions causes subtle
violations. Should probably default to "warn and skip" rather than "skip
silently."

---

### Lever 4: Aggressive Compaction Trigger

**Today:** `computeCompactionTriggerFraction()` returns 0.70 (reserves
30% for response + thinking + tool I/O). Compaction fires via the SP-066
substitute → rollup → compact pipeline.

**Proposal:** In LCM, raise the trigger to 0.85 (reserve only 15%) and
reduce `recentTurnsToPreserve` from 5 → 2.

**Savings:** Indirect — lets the model work closer to the window edge
before compacting. Effectively +4,800 tokens of working room.

**Cost:** Config tweak in `context_budget.go`. Low.

**Trade-off:** Tighter margins mean the model may truncate mid-tool-call
more often. Reducing `recentTurnsToPreserve` means the model loses recent
context sooner — it may repeat itself or forget a just-read file. For short
sessions (the LCM use case) this is acceptable.

---

### Lever 5: Disable Proactive Context

**Today:** `proactive_context.go` injects up to 4,000 chars (~1,000 tokens)
of semantically-recalled prior turns after turn 1.

**Proposal:** In LCM, disable proactive context entirely.

**Savings:** **−1,000 tokens (~3% of window).**

**Cost:** One feature flag. Trivial.

**Trade-off:** No cross-session recall. For a quick edit session, prior
context is usually noise. Acceptable.

---

### Lever 6: Reduce `repo_map` Default Depth

**Today:** Default `depth=3` (full symbols). A depth=3 map of this repo
is ~3,000–4,000 tokens.

**Proposal:** In LCM, default `repo_map` to `depth=1` (directory tree only,
no symbols). ~400–800 tokens.

**Savings:** ~2,000–3,000 tokens per `repo_map` call (variable, per-call).

**Cost:** Parameter default change. Trivial.

**Trade-off:** Model gets the repo shape but not function signatures. Has
to `read_file` to find symbols — more tool calls, but each smaller. Net
positive at 32K.

---

## Combined Budget at 32K with All Levers

```
Lever 1 (tool subset, 8 tools):  −3,685
Lever 2 (lite prompt):          −5,100
Lever 3 (skip AGENTS.md):       −3,930
Lever 4 (tighter trigger):      +4,800 effective
Lever 5 (no proactive ctx):     −1,000
─────────────────────────────────────────
New fixed floor:  ~2,500 tokens  (8% of 32K)   [was ~16,000 / 50%]
New usable room: ~24,000 tokens  (75% of 32K)  [was ~6,400 / 20%]
```

**A 32K model with all levers has ~3.7× more working room than stock sprout.**
That's enough for a real session: 8–12 tool round-trips, or reading 3–4
files and editing 2 of them.

---

## Activation: How a User Enters Low-Context Mode

Three options, in order of preference:

### Option A: Auto-detect from model context window  ⭐ recommended

When the selected model reports `ContextWindow < 64_000` (already known via
`modelcontract`), sprout automatically activates LCM with a visible notice:

```
⚠ 32K context detected — activating Low-Context Mode
  (12 tools, lite prompt, AGENTS.md skipped)
  Run /context full to override, or /model to switch.
```

This requires **lifting the hard block** in `eligibility.go`. Instead of
returning `nil` below 64K, return a new role:

```go
const RoleLowContext = "low_context"

case m.ContextWindow >= LowContextMinContext:  // new: 16_000
    return []string{RoleLowContext}
```

Below `LowContextMinContext` (16K), `ClassifyEligibleRoles` still returns `nil` —
the model is not *recommended* for any role. But `ResolveContextProfile` (above)
takes precedence at agent-creation time: below `ContextFloor` (8K) it hard-errors
with a clear message; in the 8K–16K band it activates LCM anyway with a strong
warning, since the user explicitly selected the model and may have a narrow task
in mind. The two functions have different jobs: `eligibility.go` answers "should
we recommend this model?", `ResolveContextProfile` answers "given that the user
chose it, how do we make it work — or do we refuse?"

**Cost:** Modify `ClassifyEligibleRoles` + add `LowContextMinContext = 16_000` +
add `ContextFloor = 8_000` constant + error branch in `ResolveContextProfile`.
The probe still gates — a model must still pass the capability probe (tool
calling works) to be eligible at all.

### Option B: Explicit flag / slash command

`sprout --low-context` or `/context low` forces LCM regardless of model.
Useful for testing on large models or when a user knows their task is small.

### Option C: Config setting

`config.low_context_mode: auto | always | never` in `sprout.json`.
`auto` = Option A. `always` = force on. `never` = current behavior.

**Recommendation:** Ship A + B. C is unnecessary config surface.

---

## Viable Scenarios at 32K (with all levers)

✅ **Good fits:**
- Single-file edits and bug fixes
- "What does this function do?" — read + explain
- Run a specific test and fix the failure
- Small refactors within one file
- Git operations (status, diff, commit, simple branch work)
- Answering questions about a specific file's content
- Scratch prototyping / throwaway scripts

⚠️ **Marginal (works but fragile):**
- 2–3 file changes with cross-references
- Running a build and fixing the errors
- Writing tests for one function

❌ **Does not work at 32K (needs 64K+):**
- Subagent delegation (tool excluded)
- Code → Test → Review workflow (no reviewer persona)
- Multi-file refactors (>3 files)
- Full repo onboarding (`repo_map` + AGENTS.md + proactive context)
- Anything requiring sustained context across 5+ turns
- Debugging complex issues requiring broad code reading

---

## Implementation Cost Estimate

With the config-driven `ContextProfile` design (above), the cost shifts: more upfront design (one new type, one new config field, one resolution function) but fewer scattered changes downstream.

| Work item | Files | Effort | Notes |
|---|---|---|---|
| `ContextProfile` type + 2 presets + `ResolveContextProfile` | new `pkg/configuration/context_profile.go` | ~3 hrs | The core abstraction |
| `Config.ContextMode` field + merge logic | `config.go`, `config_merge.go` | ~1 hr | Mirror `RiskProfile` pattern |
| Lite prompt (`system_prompt.lite.md`) | new prompt file | ~4 hrs | Writing + tuning the prose |
| Wire profile into `Agent` | `agent_creation.go`, `agent.go` | ~2 hrs | Store resolved profile on agent |
| Lever 1 (tool subset) — read `ToolAllowlist` | `conversation.go` | ~1 hr | One `if len(allow) > 0` block |
| Lever 2 (lite prompt) — read `SystemPromptPath` | `embedded_prompts.go` | ~1 hr | Second `//go:embed`, select in extractor |
| Lever 3 (skip AGENTS.md) — read `SkipAgentsMd` | `embedded_prompts.go` | ~30 min | One `if !profile.SkipAgentsMd` |
| Lever 4 (compaction trigger) — read `CompactionTriggerFraction` | `context_budget.go` | ~1 hr | Override default if set |
| Lever 5 (no proactive ctx) — read `SkipProactiveContext` | `seed_query.go` | ~30 min | One condition in existing `&&` chain |
| Lever 6 (repo_map depth) — read `RepoMapDefaultDepth` | `repo_map.go` | ~30 min | Default param override |
| Rollup recency — read `RecentTurnsToPreserve` | `rollup.go` | ~1 hr | Const → var + accessor |
| Eligibility — `RoleLowContext` + `LowContextMinContext` | `eligibility.go` | ~2 hrs | Lift hard block below 64K |
| Tests | new `context_profile_test.go` + per-lever tests | ~6 hrs | Unit-test each field independently |
| Activation notice | agent creation path | ~1 hr | One-time stderr print on auto-detect |
| **Total** | | **~25 hrs (3–4 days)** | |

The config-driven approach adds ~6 hours over the naive if-check approach (the abstraction + wiring) but pays it back in testability, maintainability, and zero full-mode regression risk. Each lever's test is a 10-line unit test that constructs a `ContextProfile{SkipAgentsMd: true}` and asserts the prompt-builder skips the file — no full agent spin-up needed.

The levers remain independently shippable. Lever 1 + 2 (tool subset + lite prompt) recover ~8.8K tokens — over half the fixed floor — and can ship first behind the `ContextProfile` abstraction even before levers 3–6 are wired.

---

## Implementation Design — Config-Driven, Not Code-Littered

The naive implementation litters `if lowContext { ... }` across `conversation.go`, `embedded_prompts.go`, `context_discovery.go`, `proactive_context.go`, `context_budget.go`, and `eligibility.go`. That's ~6-8 call sites, each a foot-gun for drift. Instead, LCM should be a **named profile** that resolves once at agent creation and produces a `ContextProfile` struct consumed downstream. The risk-profile system (`Config.RiskProfile` + `Config.RiskProfiles` map) is the existing precedent — mirror it.

### The `ContextProfile` type

A single struct that captures every lever as a field. No methods that branch on mode — just data.

```go
// pkg/configuration/context_profile.go
package configuration

// ContextMode names a prompt-shaping strategy.
type ContextMode string

const (
    ContextModeFull       ContextMode = "full"        // default: 128K+ behavior
    ContextModeLowContext ContextMode = "low_context" // 32K-optimized
)

// ContextProfile bundles every lever that shapes prompt size. Call sites
// read fields; they never switch on mode. Unknown/zero values resolve to
// the full-context defaults (see ResolveContextProfile), so a zero-value
// ContextProfile is always safe.
type ContextProfile struct {
    Mode ContextMode `json:"mode,omitempty"`

    // ToolAllowlist, when non-empty, restricts registered tools to these
    // names. Empty = all tools (full mode).
    ToolAllowlist []string `json:"tool_allowlist,omitempty"`

    // SystemPromptPath selects which embedded prompt to load
    // ("system_prompt.md" vs "system_prompt.lite.md"). Empty = default.
    SystemPromptPath string `json:"system_prompt_path,omitempty"`

    // SkipAgentsMd disables AGENTS.md/Claude.md/cursor.md injection.
    SkipAgentsMd bool `json:"skip_agents_md,omitempty"`

    // SkipProactiveContext disables embedding-based recall injection.
    SkipProactiveContext bool `json:"skip_proactive_context,omitempty"`

    // CompactionTriggerFraction overrides context_budget.go's default
    // (0.70 full → 0.85 low). Zero = use computed default.
    CompactionTriggerFraction float64 `json:"compaction_trigger_fraction,omitempty"`

    // RecentTurnsToPreserve overrides rollup.go's default (5 → 2).
    // Zero = use default.
    RecentTurnsToPreserve int `json:"recent_turns_to_preserve,omitempty"`

    // RepoMapDefaultDepth overrides the repo_map tool's default depth.
    // Zero = use default (3).
    RepoMapDefaultDepth int `json:"repo_map_default_depth,omitempty"`
}
```

### Two presets, no third mode

Bake the presets into code (not user-editable config) — they're tightly coupled to the prompts and tool allowlists, which are sprout's responsibility, not the user's. Users pick a mode; they don't author one.

```go
// fullContextProfile is the implicit default. All fields zero/empty, which
// means every call site uses its own built-in default — no behavior change
// from today. Returned when mode is "" or "full".
var fullContextProfile = ContextProfile{Mode: ContextModeFull}

// lowContextProfile is the 32K preset. Values are the SP-125 levers.
var lowContextProfile = ContextProfile{
    Mode:                      ContextModeLowContext,
    ToolAllowlist: []string{
        "shell_command", "read_file", "write_file", "edit_file",
        "search_files", "commit", "list_changes", "recover_file",
    },
    SystemPromptPath:          "prompts/system_prompt.lite.md",
    SkipAgentsMd:              true,
    SkipProactiveContext:      true,
    CompactionTriggerFraction: 0.85,
    RecentTurnsToPreserve:     2,
    RepoMapDefaultDepth:       1,
}
```

### Resolution — one function, called once at agent creation

```go
// ResolveContextProfile picks the effective profile from config + detected
// model context window. Called once in agent_creation.go; the result is
// stored on the Agent and read by every downstream call site.
//
// Precedence (highest wins):
//   1. Config.ContextMode explicitly set ("full" | "low_context")
//   2. Auto-detect from model context window (< 64K → low_context)
//   3. Default (full)
//
// Returns an error if modelContextWindow is known and below the absolute
// minimum (ContextFloor). This is a hard stop — the caller must surface a
// clear error to the user rather than attempting to proceed.
func ResolveContextProfile(cfg *Config, modelContextWindow int) (ContextProfile, error) {
    // Hard floor: below this, no amount of lever-pulling makes the agent usable.
    // The system prompt alone is ~1.5K tokens even in lite mode; below ~8K a
    // model cannot fit prompt + one tool round-trip + a response. Fail loudly.
    if modelContextWindow > 0 && modelContextWindow < ContextFloor {
        return fullContextProfile, fmt.Errorf(
            "model context window %d is below the %d-token minimum for sprout; "+
                "the agent cannot operate — even Low-Context Mode needs room for the "+
                "lite prompt (~1.5K tokens) plus at least one tool round-trip and a response. "+
                "Switch to a larger-context model (/model) or raise the model's context limit",
            modelContextWindow, ContextFloor,
        )
    }
    switch {
    case cfg != nil && cfg.ContextMode == ContextModeLowContext:
        return lowContextProfile, nil
    case cfg != nil && cfg.ContextMode == ContextModeFull:
        return fullContextProfile, nil
    case modelContextWindow > 0 && modelContextWindow < SubagentMinContext:
        return lowContextProfile, nil
    default:
        return fullContextProfile, nil
    }
}
```

The floor is a deliberately conservative guardrail, not a tuning knob:

```go
const (
    // ContextFloor is the absolute minimum context window at which sprout will
    // start at all, regardless of profile. Below this, even the lite prompt
    // (~1.5K tokens) plus a single read_file call (~2.5K tokens) plus a minimal
    // response leaves no room to operate. The error message directs the user
    // to switch models rather than silently producing a broken experience.
    //
    // Set at 8K: comfortably above the ~4K absolute minimum (prompt + one tool
    // I/O + response) but below the smallest practical model context windows
    // on the market. Models in the 8K–32K band get LCM; below 8K gets refused.
    ContextFloor = 8_000
)

### Config surface — one field on `Config`

Add `ContextMode` to `Config` alongside the existing `RiskProfile`:

```go
// pkg/configuration/config.go — add to Config struct
type Config struct {
    // ... existing fields ...
    ContextMode ContextMode `json:"context_mode,omitempty"` // "" | "full" | "low_context"
}
```

One field, one selector. No `LowContextToolAllowlist`, no `SkipAgentsMd` flag, no per-lever toggles exposed to the user. The profile owns those; the user picks a mode. If a power user wants to mix-and-match (e.g. low-context tools but keep AGENTS.md), they'd need a user-defined profile map — deliberately out of scope for v1. Keep the surface tiny.

### Call-site changes — read the field, don't branch on mode

Each lever becomes a single field read. No `if` statements checking mode.

**Tool filtering** (`pkg/agent/conversation.go:81`):
```go
// Before:
tools := BuildToolDefinitions()

// After:
tools := BuildToolDefinitions()
if allow := a.contextProfile.ToolAllowlist; len(allow) > 0 {
    tools = filterToolsByName(tools, makeAllowedToolSet(allow))
}
```

**System prompt** (`pkg/agent/embedded_prompts.go:42`):
```go
// Before:
//go:embed prompts/system_prompt.md
var systemPromptContent string

// After: add the lite variant alongside, select by profile
//go:embed prompts/system_prompt.md
var systemPromptContent string
//go:embed prompts/system_prompt.lite.md
var systemPromptLiteContent string

// extractSystemPrompt picks the profile-selected variant. The function
// already exists; it gains one early branch on path, not a scatter of ifs.
func extractSystemPromptFor(profile ContextProfile) (string, error) {
    src := systemPromptContent
    if strings.HasSuffix(profile.SystemPromptPath, "lite.md") {
        src = systemPromptLiteContent
    }
    return extractFromString(src)
}
```

**AGENTS.md injection** (`pkg/agent/embedded_prompts.go:54`, inside `GetEmbeddedSystemPrompt`):
```go
// Before:
contextFiles, err := LoadContextFiles()
if err == nil && contextFiles != "" {
    promptContent += contextFiles
}

// After:
if !profile.SkipAgentsMd {
    contextFiles, err := LoadContextFiles()
    if err == nil && contextFiles != "" {
        promptContent += contextFiles
    }
}
```

**Proactive context** (`pkg/agent/seed_query.go:153`):
```go
// Before:
shouldInjectProactiveContext := !alreadyInjected && /* conditions */

// After:
shouldInjectProactiveContext := !alreadyInjected &&
    !a.contextProfile.SkipProactiveContext &&
    /* existing conditions */
```

**Compaction trigger** (`pkg/agent/context_budget.go:49`):
```go
// Before:
func (a *Agent) computeCompactionTriggerFraction() float64 {
    return 1.0 - totalReservedFraction() // 0.70
}

// After:
func (a *Agent) computeCompactionTriggerFraction() float64 {
    if f := a.contextProfile.CompactionTriggerFraction; f > 0 {
        return f // profile override (0.85 in LCM)
    }
    return 1.0 - totalReservedFraction() // default 0.70
}
```

**Rollup recency** (`pkg/agent/rollup.go:42`):
```go
// Before:
const recentTurnsToPreserve = 5

// After:
var recentTurnsToPreserveDefault = 5

func (a *Agent) recentTurnsToPreserve() int {
    if n := a.contextProfile.RecentTurnsToPreserve; n > 0 {
        return n
    }
    return recentTurnsToPreserveDefault
}
```

**Eligibility** (`pkg/modelcontract/eligibility.go`):
```go
const RoleLowContext = "low_context"

func ClassifyEligibleRoles(m CanonicalModel) []string {
    if IsKnownFalse(m.Capabilities.Tools) {
        return nil
    }
    switch {
    case m.ContextWindow >= PrimaryMinContext:
        return []string{RolePrimary, RoleSubagent}
    case m.ContextWindow >= SubagentMinContext:
        return []string{RoleSubagent}
    case m.ContextWindow >= LowContextMinContext: // new: 16_000
        return []string{RoleLowContext}
    default:
        return nil
    }
}
```

### Why this is clean

1. **One struct, one field on Config.** No explosion of boolean flags. A new lever is one field on `ContextProfile` + one preset value — not a new config key.
2. **Call sites read data, not mode.** `if profile.SkipAgentsMd` reads a boolean; it never asks "am I in low context mode?". A future "medium context" profile sets the same fields differently with zero call-site changes.
3. **Presets are code, not config.** The tool allowlist and prompt path are tightly coupled to what the prompt actually says and which tools exist. Letting users edit them via config would let them request `system_prompt.lite.md` while keeping all 44 tools — a broken combination. Presets stay in sprout's control.
4. **Resolution is centralized.** `ResolveContextProfile` is the *only* place that decides which profile is active. Every call site gets a resolved `ContextProfile` from the agent; none of them re-derive it.
5. **Full mode is untouched.** `fullContextProfile` is the zero-value default. Every field is empty/zero/false, which every call site already treats as "use built-in default." There is literally no behavior change for 128K+ models unless the user explicitly opts in via `config.context_mode = "low_context"`.
6. **Testable in isolation.** Each lever's effect can be unit-tested by constructing a `ContextProfile` with one field set and asserting the call-site output — no need to spin up a full agent in a specific mode.

### What this deliberately does NOT do

- **No dynamic per-turn adjustment.** The profile resolves once at agent creation and stays fixed for the session. Mid-session mode-switching (e.g. "start light, upgrade to full when context fills") is a future possibility but adds re-entrancy complexity — the prompt, tools, and compaction params would all need to re-resolve. Out of scope for v1.
- **No user-defined profiles.** `Config.ContextMode` accepts `"full"` or `"low_context"` — two values. A `ContextProfiles` map (mirroring `RiskProfiles`) is the obvious extension if a third mode ever materializes, but adding it now is YAGNI.
- **No subagent-profile inheritance.** A primary agent in full mode that delegates to a 32K subagent does *not* automatically get LCM on the subagent (see R4). That needs a separate decision and a hook in `subagent_creation.go`.

### Activation notice

On resolution, if the profile came from auto-detect (not explicit config), emit a one-time notice so the user knows their experience is shaped:

```
⚠ 32K context detected — Low-Context Mode active
  8 tools, lite prompt, AGENTS.md skipped
  /context full to override, /model to switch
```

If the user explicitly set `context_mode: "low_context"`, no notice — they asked for it.



### R1: Prompt drift
The lite prompt will drift from the full prompt as the full one evolves.
**Mitigation:** A CI check that diffs shared sections (git rules, tool
guidelines) between the two prompts, or a periodic manual review cadence.

### R2: Silent convention violations
Skipping `AGENTS.md` means the model won't know about, e.g., the design
system token rules or test isolation conventions. Edits to those areas
may violate conventions silently.
**Mitigation:** Warn when skipping. Recommend `--include-agents-md` for
tasks touching CSS, tests, or config. The `/context full` escape hatch
should be prominent.

### R3: Does the capability probe work at 32K?
The probe (`pkg/modelprobe/`) runs a multi-step agentic test to verify
tool calling. If the probe itself needs >32K tokens, it will fail for
reasons unrelated to the model's actual capability.
**Open question:** Need to measure probe token usage. May need a lite
probe variant.

### R4: What about subagents on large-context primaries?
If a user runs sprout with a 128K primary model and delegates to a 32K
subagent, should the subagent auto-activate LCM? Probably yes — but this
needs a separate decision because subagent context limits flow through
`subagent_creation.go`, not the primary eligibility path.

### R5: Stream ordering
Tool filtering happens in `conversation.go:80-130`. Prompt selection and
proactive-context gating need to happen in the same path. The ordering
matters: tool subset → prompt selection → skip AGENTS.md → proactive ctx.
All four are in the prompt-build hot path; no architectural change needed,
just careful sequencing.

### R6: Should LCM exist at all?
The strongest argument against: 32K models are a shrinking minority, and
the maintenance cost of a second prompt + tool allowlist + eligibility
branch is real. The strongest argument for: local/offline inference
(Ollama, LM Studio, llama.cpp) defaults to 32K for many popular models
(Llama 3 8B, Qwen 2.5 7B, Mistral 7B), and that audience is growing.
**This scoping doc exists to let the user make that call.**

---

## Recommendation

**If pursuing:** Ship levers incrementally. Lever 1 (tool subset) + Lever 2
(lite prompt) together recover ~8.4K tokens — more than half the fixed
floor — and are the two highest-impact, most independent changes. Start
there, measure real usage on a 32K model, then decide whether levers 3–6
are worth it.

**If not pursuing:** The existing `ContextWarning` in `eligibility.go`
already tells 32K users their model "is usable only in a pinch." That
honest framing may be sufficient — no code change needed, just clear docs
that 32K is below the supported floor.

---

## Appendix: Measurement Methodology

Token estimates in this doc were derived by:
1. `wc -c` on prompt files → chars/4 for prose
2. Parsing `ToolDefinition` literals in `pkg/agent_tools/*.go` → chars/3.5
   for JSON schema overhead
3. Reading `context_budget.go` for reservation fractions
4. Reading `proactive_context.go` for the 4000-char default cap

These are estimates, not measurements from a live 32K session. Before
implementing, run a real session against a 32K model (e.g. via Ollama)
with sprout's existing token-count instrumentation to validate the budget
math. The `pkg/agent/metrics.go` `GetMaxContextTokens` and the context-
usage percentage in `output_router.go:410` provide the instrumentation.
