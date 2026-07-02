# SP-078: Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion

**Status:** ✅ Implemented (2026-06-30; wrap-aware render, shared completion, regression tests)

The steer panel (`SteerInputReader`) was purpose-built as a single 1,428-line file and intentionally diverged from the `InputReader` architecture to keep mid-turn input lightweight. Over time the regular prompt gained wrap-aware rendering, tab completion, and other UX features that the steer panel never absorbed, creating a noticeable parity gap. This spec shipped wrap-aware rendering for the steer panel (extracted from `input_render.go` into shared `wrap.go`), tab completion on `Ctrl+]` (preserving `Tab` as the STEER↔QUEUE mode toggle), and regression test coverage.

## Key decisions

- Extracted `wrappedGeometry`, `cursorLineIndex`, `cursorColumnOffset`, `writeWithHardBreaks`, and `visibleRuneWidth` from `input_render.go` into `pkg/console/wrap.go` — both readers now share the same wrap math.
- Tab completion uses `Ctrl+]` instead of `Tab` — `Tab` is already the documented STEER↔QUEUE mode toggle; reassigning it would break muscle memory.
- `CompletionProvider` and cycle state moved into `pkg/console/completion.go` so both `InputReader` and `SteerInputReader` share one implementation.
- `maxSteerRows` stays at 6 — the soft-scroll via leading `…` handles longer content, and the conversation area above is more valuable than extra input rows.
- Mouse support and context menu remain out of scope for the steer panel — it's a single pinned row on a TTY; the implementation cost outweighs the benefit.

## Artifacts

- code: `pkg/console/wrap.go` — shared wrap-aware rendering functions extracted from `input_render.go`
- code: `pkg/console/completion.go` — shared completion provider used by both readers
- code: `pkg/console/steer_input.go` — steer input reader with wrap-aware rendering and completion
- code: `pkg/console/status_footer.go` — footer with wrap-aware cursor placement for steer panel
- tests: `pkg/console/steer_input_test.go` — existing steer tests (still green)

Full specification archived — see git history for original content.
