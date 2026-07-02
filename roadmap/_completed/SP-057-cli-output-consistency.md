# SP-057: CLI Output Consistency — Glyph Migration & Unified Picker

**Status:** ✅ Shipped (all 5 phases, 2026-05-25)
**Date:** 2026-05-24 (proposal), 2026-05-25 (shipped)
**Follow-ups landed in same session:** security-elevation prompt format + `clihooks.PauseSteer`/`ResumeSteer` hook so `AskForConfirmation` / `AskUser` / git approval can read stdin while a turn is in flight (the steer reader's raw-mode hold previously auto-rejected all mid-turn prompts with "stdin unavailable - rejecting for safety"). Bracketed paste support added to `SteerInputReader` so multi-line pastes survive embedded newlines.
**Depends on:** Glyph vocabulary (`pkg/console/glyphs.go`, shipped), `SteerInputReader` raw-mode reader (`pkg/console/steer_input.go`, shipped), `ReplaceLastN` in-place row update (`pkg/console/activity_indicator.go`, shipped), `StatusFooter` reserved-row management (shipped).
**Priority:** High — slash commands and pickers are daily-driver surfaces and the visual inconsistency between "modern" tool rendering and "legacy" bracket-tag command output is jarring enough to read as bugs.
**Effort Estimate:** Multi-phase. Phases 1–2 are pure text substitution + small format refactors. Phase 3 introduces one new primitive (`pkg/console/select_list.go`) and migrates four call sites. Phases 4–5 build on that primitive.

Full specification archived. See git history for original content.
