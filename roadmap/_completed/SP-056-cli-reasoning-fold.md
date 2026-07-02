# SP-056: CLI Reasoning Fold — Collapsed Thinking Indicator

**Status:** ✅ Implemented (2026-06-30; --reasoning=fold mode with live token counter)

Reasoning content in the CLI had only two modes: hidden (silence until answer) or full-stream (wall of dim text). This spec added a third "fold" mode: a single pinned line that updates in-place with token count and elapsed time during the thinking phase, then resolves to a summary line (`... thought for 1.2k tokens · 3.4s`) when assistant text begins. The `--show-reasoning` flag remains as a back-compat alias for `--reasoning=full`.

## Key decisions

- Replaced binary `--show-reasoning` with `--reasoning=<mode>`: hidden | fold | full
- Token estimate uses a cheap byte-length heuristic (~4 chars/token) — UX feel, not billing
- Fold line lives in the scrolling region (not pinned to bottom) using `...` glyph for ambient feel
- Multiple reasoning bursts in one turn each get their own resolved line in scrollback
- NO_COLOR / non-TTY degrades to one Fprintln per burst — stays correct in log captures
- Ctrl+C mid-thought resolves to `... thinking interrupted (N tokens)`

## Artifacts

- code: `pkg/console/reasoning_fold.go` — ReasoningFold struct with Start/Chunk/Resolve lifecycle
- code: `pkg/console/reasoning_fold_test.go` — lifecycle, NO_COLOR degradation, interrupt, multi-burst
- code: `cmd/agent_modes.go` — wire reasoning callback to fold mode

Full specification archived — see git history for original content.
