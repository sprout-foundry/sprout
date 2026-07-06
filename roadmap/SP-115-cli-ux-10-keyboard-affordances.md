# SP-115: CLI-UX-10 — Keyboard Shortcut Affordances Row

_Surface-only fix in the terminal footer (~2-4 hours focused work)._ The
infrastructure for keybindings + the global keymap registry + the
`KeymapHelpTable()` formatter already ship, but the CLI footer never
displays them. Users have no visible hint that `Ctrl+C` interrupts or
`/` opens steer — discovered only by accident.

2026-07-05 audit (`pkg/console/` files) found:
- `KeymapHelpTable()` exists at `pkg/console/input_keymap.go:131-167` but is
  only invoked from one test (`pkg/console/keymap_tooltip_test.go:129-153`),
  not from production code.
- `RegisterKeymapForFooter` already wires `Alt+T → footer.tooltip.toggle`
  into `GlobalKeymap` (`pkg/console/keymap_registration.go:24-38`).
- Footer (`pkg/console/status_footer.go`) renders model/ctx/cost/cwd + optional
  sub/queued/todo badges — no shortcut hint row.

The audit's initial S-effort estimate was wrong. The right fix touches the
scroll-region state machine in `status_footer_scroll.go:53 reservedRows()`,
`status_footer_steer.go:25 steerRowFor`, and the `Stop`/`Resize`/
`ClearSteerLine` clear-row paths. Get this wrong → broken steer, residual
lines after resize, terminal flicker.

## Scope

**Phase 1 — formatter (S):** add `KeymapHintRow() string` to
`pkg/console/input_keymap.go` next to `KeymapHelpTable()`. Returns a single
line like `"^C interrupt · / steer · Alt+T breakdown"` (or empty if no
bindings are registered). Don't reuse `KeymapHelpTable()` — that's a
multi-line fixed-column table, not a single-line affordance.

**Phase 2 — status_footer field (S):** add `showKeymapHint bool` and
`SetShowKeymapHint(bool)` on `StatusFooter`. Parallel to existing
`WarnCost`/`AlertCost` knobs at `pkg/console/status_footer.go:75-79`. Don't
pull `pkg/configuration` into `pkg/console` — let the caller set the flag.

**Phase 3 — scroll-region rewrite (M, the riskiest phase):**
- `pkg/console/status_footer_scroll.go:53 reservedRows()` → bump from
  `2 + steerRowCount()` to `3 + steerRowCount()`.
- `pkg/console/status_footer_steer.go:25 steerRowFor` → rewrite absolute-row
  math (hint at `rows-2`, rule at `rows-1`, content at `rows`).
- `pkg/console/status_footer.go:561 drawLocked` → insert the hint render
  block between the `steerActive` branch (`:573-625`) and the rule write
  at `:640`.
- `pkg/console/status_footer.go:300-315 Resize` and `:359-367 Stop` and
  `:457-475 ClearSteerLine` → also clear the hint row when present.

**Phase 4 — REPL bootstrap (S):** `RegisterKeymapForFooter` at
`pkg/console/keymap_registration.go:26` already has config access; read
`cfg.OutputVerbosity != "compact"` once and call `footer.SetShowKeymapHint(...)`.

**Phase 5 — tests (S):** mirror existing patterns in
`pkg/console/status_footer_test.go`:
- `:285-303` `TestStatusFooter_ComposeLine_QueueBadge_HiddenWhenZero/ShownWhenNonZero`
  for the `composeLine` pure-path (uses `nonTTYWriter`).
- `:778-806` todo-badge embedded-stub pattern.
- `pkg/console/keymap_tooltip_test.go:211-247` `TestFooterTooltip_ShowHideToggle`
  for the draw-path test (uses `bytes.Buffer`).

## Phase order

1. `KeymapHintRow()` formatter + `KeymapHintRow_Test`.
2. `showKeymapHint` field + setter + setter test.
3. Scroll-region rewrite (the riskiest phase — pair coder + reviewer).
4. REPL wiring in `RegisterKeymapForFooter`.
5. `drawLocked` block to render the hint.
6. Footer-render tests: hint visible, hint hidden, hint cleared on resize/stop.

## Acceptance

- `go build ./...` clean.
- `go test ./pkg/console/... -count=1` PASS — no regression in
  `TestStatusFooter_Steer_*`.
- `KeymapHintRow()` returns a stable single-line string when bindings are
  registered; empty when none are.
- Footer renders the hint row above the rule in non-compact verbosity,
  hides it in `compact`.
- Resize/Stop correctly clear all four row positions
  (steer / hint / rule / content) when present.

## Refs

- Original audit: TODO.md CLI-UX-10
- Cross-references: CLI-UX-2 (already shipped, marked 2026-07-05),
  CLI-UX-8 (already shipped, marked 2026-07-05).
- Prior commit `4f50e704` set the suspended-indicator / steered-stdin /
  streaming prose dance that's already in place — this spec only needs to
  show what those hooks bind to.
