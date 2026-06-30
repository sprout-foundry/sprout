# SP-048: CLI Delight — Terminal UX Polish

**Status:** ✅ Partially Implemented (status footer + glyph vocabulary shipped; tool timeline + silence-fill pending → see SP-101)
**Date:** 2026-05-22
**Depends on:** None (touches existing console/agent code)
**Priority:** Medium-High
**Effort Estimate:** ~1-2 weeks (5 phases, each independently shippable)

## Problem

`sprout agent` has competent terminal-UI bones — bracketed paste, image paste,
mouse tracking, markdown rendering, an InputReader that handles arrow keys,
history, and multi-line editing — but the *moment-to-moment* feel of using
it is silent and opaque:

1. **Silence between submit and first stream chunk.** `cmd/agent_modes.go:563`
   does a bare `fmt.Print(chunk)` callback. From hitting Enter to the first
   token there is zero feedback. With slow models, slow networks, or long
   thinking blocks this reads as "is it broken?"
2. **No tool execution timeline.** When the agent runs 5 tool calls in a
   row, the output is a wall of text with no visual hierarchy, no per-tool
   elapsed time, no success/failure indicators. `PublishToolStart` /
   `PublishToolEnd` events at `pkg/agent/tool_executor.go:77-80` fire for
   the WebUI subscriber but nothing renders them in the terminal.
3. **Slash commands are invisible.** 31 commands are registered at
   `pkg/agent_commands/commands.go:42-95`; the only way to discover them is
   `/help`. No tab completion, no inline hints, no "did you mean" for
   typos. Tab keystroke is currently a noop in `pkg/console/input_core.go`.
4. **No persistent status awareness.** Users don't know how many tokens
   they've used, what model is active, or how much the session has cost
   until they run `/stats`. `summary.go:173-202` has a one-shot
   `PrintCompactProgress()` but it only fires between prompts.
5. **`NO_COLOR` is actively stripped.** `pkg/agent/agent_exec_utils.go:50`
   unsets `NO_COLOR` and forces `FORCE_COLOR=1`. This is hostile to
   standards-aware users (see [no-color.org](https://no-color.org)) and to
   CI logs.
6. **Power-user input gaps.** No Ctrl-R reverse history search, no
   `$EDITOR` escape (Ctrl-X Ctrl-E) for composing long prompts, no smart
   paste handling for >100-line text pastes (image paste already does the
   right thing — save to file and reference — at
   `pkg/console/image_paste.go`).
7. **First-run experience is bare.** `Welcome to sprout!` then a blank
   prompt. No nudge about `/help`, no recent-session resume, no example
   prompts, no hint about pasting / `$EDITOR` / autocomplete.
8. **Approval prompts don't highlight defaults.** `pkg/ui/prompt.go`
   renders `[y/n]` with no visual weight on the safe default.

The cumulative effect: the CLI is *capable* but feels *quiet*. The existing
plumbing already supports much of what's needed — events fire, history is
tracked, the markdown renderer exists — the gaps are in *legibility* during
real-time interaction.

## Current State

### What Works

| Area | File | Notes |
|------|------|-------|
| Markdown renderer | `pkg/console/markdown_formatter.go` | 660 lines, ANSI colors, headers, code blocks, lists |
| Input editor | `pkg/console/input_core.go` | Custom raw-term, bracketed paste, image paste, mouse |
| History | `pkg/console/input_history.go` | Up/down nav, 100 entries, dedup |
| Slash commands | `pkg/agent_commands/commands.go` | 31 registered, descriptions, `!` bang alias |
| Tool events | `pkg/agent/tool_executor.go:77-80` | `PublishToolStart` / `PublishToolEnd` fire for WebUI |
| Stats command | `pkg/agent_commands/info.go` | `/stats` shows conversation summary |
| Compact progress | `pkg/agent/summary.go:173-202` | One-shot `PrintCompactProgress()` |
| Approval prompts | `pkg/ui/prompt.go`, `pkg/agent/secret_prompter.go` | 4-choice secret prompts, y/n confirmation |

### What's Missing

| Gap | Impact | Effort |
|-----|--------|--------|
| Spinner / "thinking" indicator | High | S |
| Tool execution timeline (✓/✗, elapsed, indent) | High | M |
| Slash command tab completion | High | S |
| "Did you mean" for typo'd commands | Medium | XS |
| Persistent status footer (model · tokens · cost · dir) | High | M |
| `NO_COLOR` / `FORCE_COLOR` env respect | Low | XS |
| Default-choice highlighting in approval prompts | Low | XS |
| Smart paste (>100 lines → save-as-file) | Medium | S |
| Ctrl-R reverse history search | Medium | S |
| `$EDITOR` escape (Ctrl-X Ctrl-E or `/edit`) | Medium | S |
| Time-aware greeting + session resume on startup | Medium | S |
| Per-turn cost line (`⎯ this turn: 1.2k in / 4.8k out · $0.04 · 6.1s ⎯`) | Low | XS |
| Model name in prompt prefix | Low | XS |
| `/help <command>` per-command usage | Low | XS |
| Short aliases (`/m`, `/p`, `/x`) | Low | XS |
| Strip ANSI from non-TTY stdout | Low | XS |

## Proposed Solution

Five phases, each independently shippable. Phases 1-3 are the highest-impact
"delight" wins; Phases 4-5 are fit-and-finish.

### Phase 1: Spinner + tool execution timeline

Wire a terminal subscriber to the existing `PublishToolStart` /
`PublishToolEnd` events. When the agent is between prompts but before the
first stream chunk, show a one-line transient indicator:

```
⠹ Thinking (2.4s · claude-opus-4-7)
```

As tool events fire, render a per-tool timeline with elapsed time and result
icon:

```
  ⠴ read_file (pkg/console/input_core.go) · 0.3s
  ✓ read_file (pkg/console/input_core.go) · 0.4s · 1.2k tokens
  ⠦ shell_command: `go test ./pkg/console/` · 1.8s
```

Erase the spinner line (`\r\033[K`) when the next event transitions in. Use
braille spinner frames (`⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏`) at 80ms cadence. Suppress
entirely when output is not a TTY (so piped output stays clean).

### Phase 2: Slash command discoverability

- **Tab completion**: when the input buffer starts with `/`, Tab cycles
  through matching command names with an inline ghost suggestion. Repeat
  Tab cycles further matches; non-Tab keystrokes accept the current
  suggestion.
- **Did you mean**: when an unknown `/foo` is submitted, run Levenshtein
  distance against registered command names and print the top 2 suggestions
  with their descriptions.
- **`/help <command>`**: per-command usage text (`Usage:` line plus any
  examples baked into the Command type).
- **Short aliases**: `/m` → `/models`, `/p` → `/providers`, `/x` → `/exit`,
  `/?` → `/help`. Document in `/help` output.

### Phase 3: Persistent status footer

A single line pinned at the bottom of the terminal showing:

```
─ claude-opus-4-7 · 14.2k/200k ctx · $0.18 · ~/dev/sprout (main) ─────────
```

Uses terminal scrolling region (`\033[1;<rows-1>r`) so output scrolls above
the footer. Updated on every PublishToolEnd and after each assistant turn.
Cost colored yellow >$1, red >$5 (configurable). Suppressed when output is
not a TTY.

Implementation needs to handle:
- Terminal resize (SIGWINCH)
- Footer redraw after every line of output
- Clean restoration on exit (`\033[r` resets scroll region)

### Phase 4: Input ergonomics

- **`NO_COLOR` / `FORCE_COLOR`**: honor [no-color.org](https://no-color.org)
  in `NewMarkdownFormatter`. Stop unsetting `NO_COLOR` and stop forcing
  `FORCE_COLOR=1` in `pkg/agent/agent_exec_utils.go:50` unless the user
  explicitly opted in.
- **Default-choice highlight**: bold the capitalized default letter in
  `[y/N]` prompts; bold the safe default option in the 4-choice secret
  prompt.
- **Smart paste**: when bracketed paste delivers >100 lines or >5KB, show a
  one-line confirmation: `[paste] 247 lines · 8.3KB detected. [Use] [Save
  as file & reference] [Cancel]`. The "Save" path drops to
  `./.sprout/pastes/paste_<ts>.txt` and inserts `@.sprout/pastes/...` as a
  file reference — mirrors `pkg/console/image_paste.go`.
- **Ctrl-R reverse search**: incremental substring search over history,
  Ctrl-R cycles older matches, Enter submits, Esc cancels.
- **`$EDITOR` escape**: Ctrl-X Ctrl-E (or `/edit`) opens `$EDITOR` with the
  current buffer pre-filled. Save & quit submits the contents.

### Phase 5: Onboarding + per-turn polish

- **Recent-session greeting**: on startup, if there are sessions from the
  last 7 days, show up to 3 with their first user message, turn count, and
  cost. `1` / `2` / `3` resume them.
- **First-run hint**: only the first time `sprout agent` is run in a fresh
  workspace, print `Press Tab for slash commands, Ctrl-D to exit, or just
  start typing.` Stored in `~/.sprout/state.json`.
- **Per-turn cost line**: after each assistant turn completes, print a
  single dim line: `⎯ this turn: 1.2k in / 4.8k out · $0.04 · 6.1s ⎯`.
- **Model in prompt**: change `sprout> ` to `claude-opus-4-7 ▸ ` (or
  user-configurable format string).
- **Strip ANSI on non-TTY stdout**: when stdout is piped, drop colors
  automatically.

## Out of Scope

Deferred to a future spec or explicitly rejected for this round:

- **Inline diff preview on write_file/edit_file** — valuable but a much
  bigger lift; deserves its own spec.
- **Command palette (Ctrl-K modal)** — would need a small modal renderer
  over the raw terminal; out of scope for "polish."
- **Vim-mode input** — substantial implementation; opt-in feature, not
  delight.
- **`/why`, `/replay`, `/redo` commands** — useful but new functionality,
  not polish.

## Success Criteria

- A new user can run `sprout agent`, type `/`, hit Tab, and discover the
  command list without reading docs.
- A typo'd `/cmoit` returns "Did you mean `/commit`?" instead of "unknown
  command."
- The user always knows the agent is alive: spinner during thinking, tool
  timeline during tool calls, footer showing live state.
- `NO_COLOR=1 sprout agent --pipe < script.txt > out.txt` produces clean,
  uncolored output.
- The CLI feels at least as polished as Aider/Claude Code when used
  side-by-side.
