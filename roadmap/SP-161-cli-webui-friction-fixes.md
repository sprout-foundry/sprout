# SP-161 — CLI & WebUI Friction Fixes

Status: Implemented (2026-10-19). All seven items landed with tests; the
whole repo builds, `pkg/console` and `pkg/agent_commands` suites pass, the
WebUI suite (8386 tests) passes, and `make lint` / `make vet` / `make
fmt-check` are clean. Split from a 17-item user nit list; the larger items
from the same list are their own specs: SP-162 (turn lifecycle signal),
SP-163 (session summaries), SP-164 (MCP config resolution), SP-165
(`mcp add` HTTP servers), SP-166 (scroll and wheel routing).

Two spec assumptions were corrected during implementation:

- **161c**: the legibility floor is not a fixed width. A column may not be
  clamped below the width it needs to render every cell in full, so a
  table that cannot fit at those widths stacks instead of truncating.
- **161g**: `GetAvailableProviders` already included custom providers, so
  `/provider list` was not missing them. The real gap was that nothing
  distinguished a user-registered endpoint from a built-in one, and the
  unknown-provider error never mentioned `/custom add`.

## Problem

A user ran a session and filed 17 gripes. Seven of them are small, local, and
unambiguous — each is a wrong default, a missing affordance, or a silent
behavior that should be announced. They share no design question and no
architectural risk, so they ship together as one pass.

### 161a. Security prompt: the chain stepper hides the command it summarizes

`writeSecurityAnalysisChainStepper` (`pkg/console/security_prompt.go`)
truncates each subcommand to `MaxChainStepperSubcommandWidth` (80 runes) and
collapses chains past `MaxChainStepperVisible` (3) to
`(+N more — see full command above)`. The full command *is* printed
untruncated by `writeSecurityHeader`, so the pointer is technically valid —
but the stepper is where the user looks, and the escape hatch is easy to miss
on a long command. The user's report ("truncates long scripts with no way to
see what that long script actually is") is this.

Fix: when the stepper truncates anything (a subcommand clipped *or* entries
hidden), make the omission explicit and self-describing rather than a bare
`(+N more)`. The stepper must never be the only view of a subcommand it
clipped.

### 161b. Failed tool calls render as red errors

`getStatusIcon` (`webui/src/components/contextPanel/helpers.tsx`) returns
`<XCircle>` for `status === 'error'`; `ToolTimelineBar.tsx` does the same in
its card. `Chat.css` colors both `--accent-error` (red). Most tool failures
are *expected* — a grep with no matches, a test run that fails on purpose, a
`git diff` on a clean tree. Red reads as "something is broken and needs
fixing."

The CLI already has the right vocabulary (`GlyphWarning` amber vs
`GlyphError` red, `pkg/console/glyphs.go`), and the WebUI already has
`--accent-warning` (`webui/src/index.css`). The WebUI is the one that's
wrong.

Fix: failed tool calls use the warning tone and a neutral glyph. Reserve red
for genuine failures (the turn itself failing, a blocked operation).

### 161c. Table columns collapse to one character

`clampColumnWidths` (`pkg/console/markdown_formatter_tables.go`) shrinks the
widest column one character at a time until the total fits, with a floor of
**1 character per column**. `padCell` then hard-truncates. A wide table can
therefore render a column as a single character plus `…` — the user cannot
tell what the options are.

Fix: a per-column floor that keeps a column legible, and when the table still
cannot fit, degrade honestly (fewer columns, or a stacked layout) rather than
shipping unreadable cells.

### 161d. Wrapped composer rows have no continuation marker

The pinned input box wraps via `WrapSteerLayout` and reserves the right number
of rows, but `composerRule` (`pkg/console/composer.go`) draws a plain rule and
continuation rows are visually identical to the first row. The user reports
"no wrap-glyph nor any attempt to auto-adjust, which leads to misspelling,
uncertainty."

Fix: continuation rows carry a visible marker so the wrap point is obvious.

### 161e. `/info` omits the risk profile

`/info` is the one-stop overview (model, provider, context, cost, workspace,
persona, subagent config) but does not show the active shell-command risk
profile. `GetActiveRiskProfile()` already exists
(`pkg/agent/agent_risk.go`); `/risk-profile` shows it, but `/info` is where
users look.

Fix: one line in `/info`.

### 161f. `/keys` writes to a workspace-scoped store silently

`/keys set` calls `credentials.SetToActiveBackend`, which resolves
`envutil.ConfigDir()` → `SPROUT_CONFIG`. Inside a git repo the root command
auto-detects the repo and sets `SPROUT_CONFIG=<repo>/.sprout`
(`cmd/root.go`), so the credential lands in the *repo's* store. A key set in
one repo is invisible in another, with no indication.

Fix: the success message names the resolved path. The user is told where the
key went.

### 161g. `/custom` is undiscoverable from `/provider`

`/custom` manages custom OpenAI-compatible providers; `/provider` selects
among providers. Different verbs — merging them is the wrong fix. The real
problem is that `/provider <name>` fails with "not registered" and never
mentions that `/custom add` is how you register one.

Fix: cross-link. `/provider list` includes custom providers, and the
not-registered error points at `/custom add`.

## Acceptance criteria

- [x] 161a: a security prompt whose chain is clipped or collapsed states the
      omission explicitly; no subcommand is reachable only through a clipped
      rendering (test over a long chained command).
      → clipped subcommands are spelled out on `↪ ` continuation lines;
      the collapse marker now reads "(+N more steps — the full command
      printed above lists them)".
      Tests: `TestChainStepper_ClippedSubcommandSpellsOutFullText`,
      `TestChainStepper_CollapsedChainStatesOmissionExplicitly`,
      `TestChainStepper_NoContinuationLinesWhenEverythingFits`.
- [x] 161b: a failed tool call renders with `--accent-warning` and a neutral
      glyph in both `ToolDetailInline` and `ToolTimelineBar`; a turn-level
      failure still renders red (test).
      → `XCircle` → `AlertTriangle`; the card, glyph, status and output
      rules all moved to `--accent-warning`; `.error-indicator` stays red.
      Tests: `toolCallFailureTone.test.ts` (4 cases),
      `helpers.test.tsx` error case, `ToolTimelineBar.test.tsx` error case.
- [x] 161c: a table too wide for the terminal keeps every column at or above
      the legibility floor; a table that cannot fit degrades to a layout where
      every value is readable (test with a wide fixture table).
      → the floor is the column's full width, so clamping never truncates;
      an unfittable table renders stacked (one header/value pair per line)
      in both the colored and plain renderers.
      Tests: `TestMarkdownFormatter_TableColumnLegibilityFloor`,
      `TestMarkdownFormatter_TableStaysTabularWhenItFits`.
- [x] 161d: a wrapped composer row is visually distinguishable from its first
      row (render test).
      → continuation rows carry a `↪ ` marker, taken out of the row's width
      budget so rows still end exactly at the terminal edge.
      Tests: `TestDrawComposer_WrappedRowCarriesContinuationMarker`,
      `TestDrawComposer_UnwrappedRowHasNoMarker`.
- [x] 161e: `/info` reports the active risk profile (test).
      → text and `--json` forms both carry it.
      Tests: `TestInfoCommand_ShowsRiskProfile`,
      `TestInfoCommand_JSONIncludesRiskProfile`.
- [x] 161f: `/keys set` names the store path it wrote to (test).
      → prints "Stored in <path>" plus the active backend.
      Test: `TestKeysCommand_SetReportsStorePath`.
- [x] 161g: `/provider list` shows custom providers; `/provider <unknown>`
      names `/custom add` (test).
      → custom entries are marked `[custom]`; the unknown-provider error
      points at `/custom add`.
      Tests: `TestProvidersCommand_UnknownProviderPointsAtCustom`,
      `TestProvidersCommand_ListMarksCustomProviders`.

## Non-goals

- No change to the security *decision* path — 161a is presentation only.
- No new glyph vocabulary; 161b reuses the existing warning tone.
- No merge of `/custom` into `/provider` (see 161g).
- No session-summary, turn-marker, MCP, or scroll work — those are SP-162…166.

## Open questions

- 161c: whether to stack a too-wide table (one record per block) or drop
  low-priority columns. Stacking is lossless; dropping is compact. Leaning
  lossless.
- 161d: marker choice — a leading `↪` on continuation rows, or a hanging
  indent. The marker is more explicit; the indent is quieter.
