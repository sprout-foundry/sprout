# SP-106: CLI Output Polish + SelectList Touch Scroll

**Status:** 🔵 Proposed

The agent CLI's markdown rendering is solid for prose and code blocks but
has two visible gaps in common agent output: GitHub-flavored markdown
tables render as raw pipes, and nested lists lose their indentation.
Separately, the SelectList (used by `/model`, `/provider`, and the new
`/settings` pickers) parses mouse wheel events but silently drops them —
tapping a trackpad or scrolling on a laptop does nothing.

This spec closes both gaps with low-risk, additive changes:

- **F1** — Table rendering in `MarkdownFormatter`
- **F3** — Nested list indentation in `MarkdownFormatter`
- **T1** — Wire wheel events to `SelectList` scroll
- **T3** — Enable mouse tracking in `SelectList` mode

## Problem

### F1: Tables

The agent frequently outputs markdown tables — file comparisons, metrics
summaries, option lists. The current `MarkdownFormatter` (line-by-line
regex) doesn't recognize the `|` pipe syntax, so tables render as:

```
| File | Lines | Status |
|------|-------|--------|
| a.go | 42 | ok |
```

...with raw pipes and no column alignment. A proper renderer detects the
header separator row (`|---|---|`), calculates column widths, and pads
cells so columns align visually.

### F3: Nested lists

The list regex in `formatMarkdownLine` matches `^\s*[-*+]\s` but only
colors the bullet — it doesn't preserve the leading whitespace that
creates visual nesting. A child item indented 2-4 spaces under a parent
renders at the same column as the parent.

### T1/T3: SelectList mouse scroll

`SelectList` enters its own raw mode (`enterSteerMode`) which doesn't
enable SGR mouse tracking. Even if it did, `dispatchCSI` only handles
arrow keys (A/B/C/D), Home (H), End (F), and PgUp/PgDn prefixes — it
ignores the `<CSI <` SGR mouse escape sequences entirely. The result:
scrolling a trackpad over a model picker does nothing.

## Design

### F1: Table rendering

Add a table-detection pass to `MarkdownFormatter.Format`:

1. When a line starts with `|`, buffer lines until the table ends (a
   line not starting with `|` or a blank line).
2. Detect the separator row (`|---|---|` or `|:--|--:|:--:|`).
3. Calculate column widths from the widest cell in each column (capped
   at a max to avoid terminal overflow).
4. Render with proper padding: header row bold, separator as a dim
   rule line, data rows normal.
5. Respect the formatter's `width` setting for column-width clamping.

Output format:

```
  File           Lines  Status
  ─────────────────────────────
  a.go              42  ok
  very-long-name   100  ok
```

The pipe borders are dropped in favor of clean column alignment — the
pipe gutter adds visual noise without aiding readability in a terminal.

### F3: Nested list indentation

In `formatMarkdownLine`, the list regex already captures `matches[1]`
(leading whitespace). The fix is to convert that whitespace to a
structured indent: 2 spaces of leading whitespace → 1 level of
indentation, rendered as 2 visible spaces (not 4, to keep narrow lists
compact). Mixed indentation (tabs, 4-space) is normalized.

### T1: Wire wheel events to SelectList

`SelectList.dispatchCSI` currently handles `A`/`B`/`H`/`F`. Extend the
escape-sequence reader to detect SGR mouse format (`\x1b[<`).

When mouse tracking is enabled and a `MouseEventWheelUp` or
`MouseEventWheelDown` event arrives, call `moveCursor(-1)` or
`moveCursor(+1)` respectively, then re-render. This reuses the existing
scroll-offset logic — no new scrolling code needed.

For coarser scroll (wheel with momentum / trackpad), batch multiple
wheel events in a short window (e.g. 3 events = page scroll). This is
optional polish — the single-step version works and matches how most
TUIs handle wheel events.

### T3: Enable mouse tracking in SelectList mode

`SelectList.runTTY` calls `enterSteerMode` which sets raw mode. Add the
SGR mouse enable sequence (`\x1b[?1006h\x1b[?1000h`) after entering raw
mode, and the disable sequence (`MouseTrackingDisable`) before exiting.
This is the same pattern `input_terminal.go` uses.

## Key decisions

- **Tables drop pipe borders.** Aligned columns are more readable in a
  terminal than fenced pipes. The header rule (dim `─` line) provides
  the visual separation that borders would.
- **Table column width is clamped.** Long cells are truncated with `…`
  at a max width (default: `termWidth / numColumns`). This prevents a
  single long URL or file path from blowing out the layout.
- **Nested lists use 2-space indents per level.** Matches CommonMark's
  minimum 2-space convention and keeps deep lists readable.
- **Mouse tracking is scoped to SelectList.** Not enabled during
  streaming output (would interfere with copy/paste).
- **Wheel events move one item at a time.** Matches `j`/`k` behavior.
  No inertia — predictable and simple.

## Phasing

### Phase 1 — Markdown table rendering (F1)

**Files:**
- `pkg/console/markdown_formatter.go` — add `renderTable` method, detect
  table blocks in `Format`
- `pkg/console/markdown_formatter_test.go` — table rendering tests

### Phase 2 — Nested list indentation (F3)

**Files:**
- `pkg/console/markdown_formatter.go` — fix list indent in
  `formatMarkdownLine`
- `pkg/console/markdown_formatter_test.go` — nested list tests

### Phase 3 — SelectList mouse scroll (T1 + T3)

**Files:**
- `pkg/console/select_list.go` — enable mouse tracking, parse wheel
  events, wire to `moveCursor`
- `pkg/console/select_list_test.go` — wheel event tests

## Success Criteria

- `make build-all` clean.
- `go test ./pkg/console/...` green.
- A markdown table in agent output renders as aligned columns with a
  header rule, not raw pipes.
- A nested list (`- parent` / `  - child`) renders the child indented.
- Scrolling a trackpad over a `/model` or `/settings` picker moves the
  selection cursor.
- Non-TTY / non-mouse terminals are unaffected (no mouse escape
  sequences emitted, no rendering changes).

## Out of Scope

- **Diff syntax highlighting** (F5) — deferred; can be added to the
  existing `formatCodeLine` later without architectural changes.
- **Additional language highlighters** (F4) — Rust, Java, etc. can be
  added incrementally to the existing pattern.
- **Tap-to-select in SelectList** (T2) — requires row-geometry tracking
  that adds complexity for marginal benefit over Enter-to-confirm.
- **Mouse support during streaming output** — would interfere with
  terminal text selection / copy-paste.
