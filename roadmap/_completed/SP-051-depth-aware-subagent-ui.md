# SP-051: Depth-Aware Subagent UI — Visible Nesting in the CLI

**Status:** ✅ Implemented (events tagged with depth + persona, CLI renders indented timeline with persona badges)

When subagents ran, their tool calls were visually indistinguishable from the parent's in the CLI timeline — a flat wall of `[OK] read_file`, `[OK] edit_file` lines with no indication of which agent did which thing. This spec tagged every event with `subagent_depth` and `active_persona` via `SetEventMetadata` at subagent creation (reusing the existing `decorateEventPayload` pipeline). The CLI tool subscriber reads these fields to indent by depth (`strings.Repeat("  ", depth)`) and prepend a colored `[persona]` badge. On first arrival of a new (depth, persona) pair, a `↳ persona spawned (provider · model)` line is emitted. The status footer shows `· N sub` when subagents are active. Depth-0 events produce no badge and no extra indent (backwards-compatible).

## Key decisions

- Phase 1 (event tagging) is structural and low-risk — one `SetEventMetadata` call enables everything in Phase 2.
- Indent-by-depth (2 spaces per level) instead of box-drawing characters — simpler and survives resize.
- Persona color map is deterministic (coder=cyan, tester=green, etc.) and shared between CLI (`persona_style.go`) and WebUI (`personaColors.ts`).
- Spawn line fires on first event from a new (depth, persona) pair, tracked via a `map[depth]persona` in the subscriber.
- Status footer subagent count uses an atomic counter on `Agent` (increment on start, decrement on finish).

## Artifacts

- code: `pkg/agent/agent_events.go` — `decorateEventPayload` merges `subagent_depth` + `active_persona` into every event
- code: `cmd/agent_modes.go` — `startTerminalToolSubscriber` renders depth indent + persona badge + spawn lines
- code: `pkg/console/persona_style.go` — persona ID → ANSI color map
- code: `pkg/console/status_footer.go` — `ActiveSubagents()` method, `· N sub` suffix
- tests: `pkg/agent/subagent_runner_test.go` — event metadata assertions on spawned subagent events

Full specification archived — see git history for original content.
