# SP-057: CLI Output Consistency — Glyph Migration & Unified Picker

**Status:** ✅ Shipped (all 5 phases, 2026-05-25)
**Date:** 2026-05-24 (proposal), 2026-05-25 (shipped)
**Follow-ups landed in same session:** security-elevation prompt format + `clihooks.PauseSteer`/`ResumeSteer` hook so `AskForConfirmation` / `AskUser` / git approval can read stdin while a turn is in flight (the steer reader's raw-mode hold previously auto-rejected all mid-turn prompts with "stdin unavailable - rejecting for safety"). Bracketed paste support added to `SteerInputReader` so multi-line pastes survive embedded newlines.
**Depends on:** Glyph vocabulary (`pkg/console/glyphs.go`, shipped), `SteerInputReader` raw-mode reader (`pkg/console/steer_input.go`, shipped), `ReplaceLastN` in-place row update (`pkg/console/activity_indicator.go`, shipped), `StatusFooter` reserved-row management (shipped).
**Priority:** High — slash commands and pickers are daily-driver surfaces and the visual inconsistency between "modern" tool rendering and "legacy" bracket-tag command output is jarring enough to read as bugs.
**Effort Estimate:** Multi-phase. Phases 1–2 are pure text substitution + small format refactors. Phase 3 introduces one new primitive (`pkg/console/select_list.go`) and migrates four call sites. Phases 4–5 build on that primitive.

## Problem

The CLI has two visual eras coexisting on the same screen:

- **Modern (post-SP-053/SP-055):** Tool calls, streaming output, status footer, and steer panel use the 8-glyph semantic vocabulary (`✓ ✗ ⚠ ⓘ → ⏸ ⏹ ·`) with consistent color treatment, in-place collapse via `ReplaceLastN`, and threshold-tinted timings.
- **Legacy:** Slash commands (`/commit`, `/review`, `/persona`, `/help`), the TODO tool, paste/editor input chrome, and pickers (provider, model, sessions) still emit bracket tags like `[OK]`, `[FAIL]`, `[edit]`, `[~]`, `[bot]`, `[doc]`, `[empty]`, `[up]`, `[ok]`, `[role]`, `[i]`, `[img]`, `[editor]`, `[paste]`. They use raw `fmt.Println` with no glyph prefix, hardcoded `═══════` separators, and inconsistent column alignment.

The pickers compound the problem. Five distinct input idioms exist for the same conceptual interaction ("pick one of these"):

| Picker | Input | Search | Arrows | Notes |
|---|---|---|---|---|
| `/provider select` | direct command (`/provider openai`) | none | none | **not actually interactive** — list + tells you to retype |
| `/model select` | numeric or substring filter | substring | none | line-based, no live update, truncates OpenRouter's 200+ models to 10 visible |
| `/sessions` | numeric | none | none | `bufio.Scanner` + `strconv.Atoi` |
| Recent-sessions resume | numeric | none | none | separate code path at stderr level |
| Commit confirm | `y/n/e/r` OR numeric (TUI) | n/a | n/a | two completely different render paths depending on `AGENT_CONSOLE` |

The `SteerInputReader` shipped in SP-055 already gives us a clean raw-mode reader with arrow-key dispatch, mode toggle, history recall, and termios that preserves OPOST. That same foundation can power a single picker primitive.

## Current State

### What works (don't change)

- The glyph package itself: 8 glyphs, NO_COLOR/FORCE_COLOR honored, `Prefix()` / `Print()` / `Printf()` / `Fprintln()` / `Fprintf()` covers every site we need. (`pkg/console/glyphs.go`)
- Tool-call rendering: `formatToolStartLine` / `formatToolEndLine` / `formatToolRunLine` at `cmd/agent_modes.go:1499-1568`. Collapse via `ReplaceLastN` is solid.
- `SteerInputReader` raw-mode reader with `enterSteerMode` / `exitSteerMode` preserving OPOST. (`pkg/console/steer_input.go`)
- `PromptChoice` agent-console dropdown when `AGENT_CONSOLE=1`. The dropdown UX is fine; the problem is that the *fallback* path is a wholly different `y/n/e/r` loop with no shared chrome.

### What's wrong

#### Slash command chrome inventory

- `pkg/agent_commands/commit_flow_console.go` — 23 bracket-tag instances (`[empty]`, `[edit]`, `[up]`, `[ok]`, `[~]`, `[*]`, `[OK]`)
- `pkg/agent_commands/persona.go` — `[OK]` at lines 53, 74, 95, 108, 182; `[role]` header at line 188
- `pkg/agent_commands/help.go` — `[bot]` prefix at lines 32, 127; raw `fmt.Println` block at 86–103
- `pkg/agent_commands/review.go` — hardcoded `═══════════════════════════════════════════════════════` separator at lines 257–284, no glyphs anywhere
- `pkg/console/input_paste.go`, `pkg/console/input_editor_escape.go` — `[paste]`, `[editor]` tags

#### TODO tool rendering

- `pkg/agent/tool_executor_todo_events.go` emits a verbose `[edit] Todo update: 8 total | [ ] 2 pending | [~] 1 in_progress | [x] 4 completed | [-] 1 cancelled` line plus per-change rows like `✓ Fix login bug (pending -> completed)` mixing bracket and glyph styles inconsistently.
- Status symbols `[ ] [~] [x] [-]` don't map to the semantic glyph vocabulary even though `GlyphDim · GlyphAction → GlyphSuccess ✓ GlyphStopped ⏹` would cover them.

#### Picker dysfunction

- `pkg/agent_commands/providers.go:157-184` — `/provider select` literally prints "Interactive provider selection not available" then lists the providers and instructs the user to call `/provider <name>` directly. This is a stub, not a feature.
- `pkg/agent_commands/models.go:162-268` — `/model select` falls back to a line-based interface that requires the user to type `clear` / a number / `quit`. No arrow navigation. For OpenRouter, the 200+ models are filtered by substring with a 10-result cap (line 675), but there's no way to see the second page of matches.
- `pkg/agent_commands/sessions.go:64-117` — numeric only via `ui.PromptForSelection`.
- `cmd/recent_sessions.go:46-151` — separate stderr-level numeric picker.
- `pkg/agent_commands/commit_command.go:500-583` — two completely different confirm paths depending on `AGENT_CONSOLE`.

## Proposed Solution

Five phases. Phase 1 is mostly mechanical; Phase 3 is the meaningful piece because every later picker improvement builds on it.

### Phase 1 — Glyph migration for slash command chrome

Replace bracket tags with glyph calls in:

- `commit_flow_console.go` — `[empty]` → `GlyphInfo`, `[up]` → `GlyphAction`, `[ok]` → `GlyphSuccess`, `[~]` → `GlyphDim`, `[OK]` → `GlyphSuccess`, `[edit]` → `GlyphAction`
- `persona.go` — `[OK]` → `GlyphSuccess`, `[role]` header → `GlyphInfo`
- `help.go` — `[bot]` → `GlyphInfo`, plus glyph-prefix each section header
- `review.go` — replace `═══` block with `GlyphInfo` framed header + glyph-tinted per-file summaries
- `input_paste.go` / `input_editor_escape.go` — `[paste]` / `[editor]` → `GlyphAction`

This is pure text + import edits. The glyph package's `Fprintf` / `Printf` matches the call sites' existing `fmt.Printf` shape so the diff is local.

**Acceptance:** `grep -rE '\[(OK|FAIL|WARN|edit|empty|up|ok|bot|doc|role|i|img|editor|paste)\]' pkg/agent_commands pkg/console cmd` returns no human-facing instances (test fixtures and TODO body text are exempt).

### Phase 2 — TODO tool rendering

Two changes in `pkg/agent/tool_executor_todo_events.go`:

1. Replace the `[edit] Todo update: 8 total | [ ] 2 pending | [~] 1 in_progress | [x] 4 completed | [-] 1 cancelled` summary line with a glyph-tinted single line:

   ```
   ⓘ Todos: 8 total · · 2 pending · → 1 in progress · ✓ 4 done · ⏹ 1 cancelled
   ```

2. Map status symbols in per-change rows to glyphs:
   - `pending` → `GlyphDim ·`
   - `in_progress` → `GlyphAction →`
   - `completed` → `GlyphSuccess ✓`
   - `cancelled` → `GlyphStopped ⏹`

   So `✓ Fix login bug (pending → completed)` becomes the consistent rendering.

3. Keep the structured `todo_update` event payload unchanged — webui rendering is independent and already correct.

### Phase 3 — Unified picker primitive (`SelectList`)

New file `pkg/console/select_list.go`. The shape:

```go
type SelectItem struct {
    Label   string // primary label, glyph-prefixable
    Detail  string // optional dim-rendered suffix ("anthropic · sonnet-4-6")
    Value   string // payload returned on selection
}

type SelectListOptions struct {
    Title       string         // shown above the list, glyph-prefixed
    Items       []SelectItem
    Searchable  bool           // enable live filter (Phase 4 turns this on for models)
    PageSize    int            // default 10
    Footer      string         // hint line ("↑↓ select · enter confirm · esc cancel")
}

// Run blocks until the user selects an item or cancels. Returns the selected
// item's Value (or "" on cancel) and ok=true/false. Honors NO_COLOR; degrades
// to numeric entry on non-TTY.
func (s *SelectList) Run(ctx context.Context) (string, bool, error)
```

**Mechanism:**
- Reuses `enterSteerMode` / `exitSteerMode` from `pkg/console/steer_termios_*.go` so raw-mode handling is shared with the steer reader.
- Arrow keys move the cursor; Enter confirms; Esc/Ctrl+C cancels.
- When `Searchable=true`, typed characters append to a filter buffer and the list re-filters in place using the same substring/fuzzy matcher already in `pkg/agent_commands/models.go:646-680` (extracted to `pkg/console/fuzzy.go`).
- Rendering uses `ReplaceLastN` to overwrite the prior frame so the list updates in place without scrollback churn.
- Non-TTY degrades to numbered list + `bufio.Scanner` numeric entry (preserves scriptability).

**Migration:**
- `/sessions` → `SelectList` with `Title="ⓘ Sessions"`, `Searchable=false`.
- Recent-sessions resume picker → `SelectList` with `PageSize=3`, no filter.
- `/provider select` → `SelectList` over `provider.AvailableNames()`. **This makes `/provider select` actually work.**
- Commit-confirm Approve/Retry/Edit/Cancel → `SelectList` with 4 items (replaces both the `AGENT_CONSOLE` dropdown and the `y/n/e/r` fallback; one path).

### Phase 4 — Model picker with live search

`/model select` → `SelectList` with `Searchable=true`, `PageSize=12`.

The existing fuzzy matcher in `models.go:646-680` already handles substring scoring; lift it into `pkg/console/fuzzy.go` so `SelectList` can call it directly. The pain point isn't the matching, it's that the current line-based fallback requires typing `clear` between searches and never shows page-2 matches. Arrow-key paging on a live filter solves both at once.

For OpenRouter's 200+ models, also surface:
- A `Detail` field per item showing `provider · context · pricing` so users can pick by capability not just name.
- A keyboard shortcut (`/` to focus filter, `g` to jump to top, `G` to jump to bottom) — implemented in `SelectList`'s key dispatch so all pickers inherit them.

### Phase 5 — Provider picker as full interactive selection

Remove the "Interactive provider selection not available" stub at `providers.go:163`. Replace with a `SelectList` call. `Detail` shows each provider's configured-or-not status (`✓ ready`, `⚠ needs API key`, `· not configured`) so the user can pick *and* the picker doubles as a configuration overview.

When the user picks a provider that needs an API key, chain directly into the credentials flow rather than failing with "you need to configure this first." That's a separate small refactor in `pkg/configuration/credentials.go` but worth doing while we're already touching the picker.

## Visual

```
ⓘ Select model                                      ← Title, GlyphInfo
  filter: claude-                                    ← live filter (Phase 4)

→ claude-opus-4-7              anthropic · 200k · $$$  ← cursor row, GlyphAction
  claude-sonnet-4-6            anthropic · 200k · $$
  claude-haiku-4-5             anthropic · 200k · $
· claude-3-5-sonnet            anthropic · 200k · $$   ← dim = already-selected/used

· ↑↓ select · enter confirm · / filter · esc cancel    ← footer hint, GlyphDim
```

## Out of Scope

- **Bubbletea / huh / promptui adoption.** The existing termios + ANSI primitives we shipped for SP-055 already cover what we need. Pulling in a TUI framework would be 100kB of binary growth for one widget.
- **Reskinning the streaming output path.** That's already modern. This spec is about catching the legacy surfaces up.
- **Cost/pricing accuracy in the model picker's `Detail` field.** Use a coarse tier (`$ / $$ / $$$`) from existing model metadata, not live pricing lookups.
- **Webui parity for the slash commands.** Webui renders these via its own components; this spec is CLI-only. The shared payload structure (e.g. `todo_update` events) stays unchanged.
- **Persona picker.** `/persona` currently takes a positional arg; making it a picker is a separate UX question — leave it for now.

## Success Criteria

1. **Glyph audit clean.** `grep -rE '\[(OK|FAIL|WARN|edit|empty|up|ok|bot|doc|role|i|img|editor|paste)\]'` across `pkg/agent_commands`, `pkg/console`, and `cmd/` returns no human-facing instances. Fixture files and unrelated string content exempt.
2. **TODO output cohesion.** A TODO update line in the CLI uses the same glyph vocabulary as a tool-end line. A user skimming a transcript can't tell which subsystem emitted which row from style alone.
3. **One picker primitive.** `/sessions`, recent-sessions resume, `/provider select`, `/model select`, and commit-confirm all route through `SelectList`. No `bufio.Scanner` + numeric-only pickers remain.
4. **Live search works.** `/model select` against OpenRouter's full model list lets the user type to filter, arrow-key through matches, and reach the 47th match without retyping. No "type clear to reset" required.
5. **`/provider select` actually selects.** Pressing Enter on a provider switches to it (and triggers credential prompt if needed) rather than printing instructions.
6. **NO_COLOR / non-TTY clean.** Every picker degrades to numbered-list + numeric stdin entry. Glyph migrations honor the existing `NoColor` detection from `pkg/console/glyphs.go`.
7. **No regressions in scriptability.** Existing `sprout --output-json` / non-interactive paths don't hit any picker — those paths already gate on `term.IsTerminal`, and `SelectList.Run` inherits that gating.

## Notes for the Implementer

- The fuzzy matcher at `pkg/agent_commands/models.go:646-680` is the right starting point for `pkg/console/fuzzy.go`. Lift it as-is, then have `models.go` call into the lifted version so the model picker doesn't double-implement.
- `SteerInputReader` and the new `SelectList` will both want raw mode at different times. Both should call into the same `enterSteerMode` / `exitSteerMode` pair — those functions are already idempotent-on-reentry. If you find a case where they aren't, fix at the primitive level, not by adding a flag at `SelectList`.
- Commit-confirm currently has the `AGENT_CONSOLE=1` branch using `chatAgent.PromptChoice` and a separate `y/n/e/r` branch otherwise (`commit_command.go:500-583`). The migration deletes both and routes through `SelectList`. Leave `PromptChoice` in place for now — it's used by other agent-driven prompts — but the commit flow stops calling it.
- The `Detail` column in `SelectList` should be right-aligned to a column width so a list of mixed-length labels still aligns cleanly. Use `pkg/console/line_cap.go`'s width helper if you need terminal width detection.
- When deciding between `GlyphAction →` and `GlyphInfo ⓘ` for a slash-command header, the rule is: `→` for things the user is *about to do* (staging, switching), `ⓘ` for things being *reported* (current state, list of choices). The existing usage in `agent_modes.go` follows this convention.
- The current persona ack lines use `[OK] Switched to <persona>` — those become `✓ Switched to <persona>` which reads better and matches how the tool path already announces success.
- Phases 1 and 2 are independent and can ship in either order. Phase 3 unblocks 4 and 5; don't ship 4 or 5 without Phase 3 because the third re-implementation of a numeric picker would be worse than the second.
