# SP-048: CLI Delight — Terminal UX Polish

**Status:** ✅ Partially Implemented (status footer + glyph vocabulary shipped; tool timeline + silence-fill deferred to SP-101)

The CLI was capable but felt quiet — no feedback between submit and first stream chunk, no tool execution timeline, no persistent status awareness, invisible slash commands. This spec shipped the persistent status footer (model, tokens, cost, directory), `NO_COLOR`/`FORCE_COLOR` env respect, default-choice highlighting in approval prompts, smart paste for large text, Ctrl-R reverse history search, `$EDITOR` escape, time-aware greeting with session resume, per-turn cost lines, model name in prompt prefix, and slash command tab completion with "did you mean" suggestions. The tool execution timeline and spinner/silence-fill were deferred to SP-101.

## Key decisions

- Status footer uses terminal scrolling region (`\033[1;<rows-1>r`) to pin a single line at the bottom — output scrolls above it, footer stays visible. Suppressed when output is not a TTY.
- `NO_COLOR` is honored (no-color.org standard); `FORCE_COLOR=1` is no longer forced unconditionally in `agent_exec_utils.go`.
- Slash command tab completion cycles through matches with inline ghost suggestions; "did you mean" uses Levenshtein distance against registered command names.
- Smart paste detects >100 lines or >5KB bracketed pastes and offers to save as file — mirrors the existing `image_paste.go` pattern.
- Tool timeline and spinner (Phase 1) deferred to SP-101 — the event plumbing exists (`PublishToolStart`/`PublishToolEnd`) but the terminal subscriber rendering was punted to avoid scope creep.

## Artifacts

- code: `pkg/console/status_footer.go` — persistent status footer with model/tokens/cost/dir display
- code: `pkg/console/input_core.go` — Ctrl-R search, `$EDITOR` escape, smart paste, tab completion
- code: `pkg/console/markdown_formatter.go` — `NO_COLOR`/`FORCE_COLOR` respect
- code: `pkg/agent_commands/commands.go` — 31 slash commands with descriptions and aliases
- code: `pkg/agent/summary.go` — per-turn cost line and compact progress display

Full specification archived — see git history for original content.
