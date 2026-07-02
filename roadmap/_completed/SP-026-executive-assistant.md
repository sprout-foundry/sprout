# SP-026: Coordinator Persona (formerly "Executive Assistant")

**Status:** ✅ Implemented (renamed 2026-06-03, see commit `516a9d41`)

The "Executive Assistant" persona — a headless, task-queue-driven agent mode
designed for autonomous workflows like `sprout-automate` — was renamed to
"Coordinator" because the original name was misleading. Users assumed EA
mode meant a polite office-assistant style of interaction; in reality it
means a non-interactive loop that pulls tasks from a queue, executes them
through the full agent dispatch surface, and emits structured events.
"Coordinator" captures the actual behavior. The rename touched persona
JSON, prompt template paths, persona-resolution code, and CLI/WebUI labels.
Legacy "Executive Assistant" aliases were preserved in the resolver to keep
old configs and task queues working without migration.

## Key decisions

- **The rename is a label, not a behavior change.** Underneath, the persona
  config (`persona_id: "coordinator"`, task-queue consumption, headless
  event emission) is identical to the old EA implementation. The spec was
  about framing, not redesign.
- **Legacy aliases preserved** in `pkg/agent/persona.go` so old config files
  with `persona: executive_assistant` continue to resolve. No migration
  script needed; the resolver does it.
- **Persona prompt template lives in `pkg/agent/prompts/subagent_prompts/coordinator.md`**,
  not the daemon prompt directory. The sub-agent context is distinct from
  the primary-agent context.
- **The Coordinator persona is the default for `sprout-automate`.** Task-queue
  workflows explicitly want the headless coordinator mode; the rename makes
  the config self-documenting.
- **WebUI label and CLI label changed together** so users don't see "EA"
  in the WebUI but "coordinator" in the CLI, which would be confusing.

## Artifacts

- code: `pkg/agent/prompts/subagent_prompts/coordinator.md` — prompt template
- code: `pkg/agent/persona.go` — persona registry + legacy aliases
- code: `pkg/agent/agent_getters.go` — persona resolution
- rename commit: `516a9d41`
- companion: SP-050 (orchestrator persona collapse) — different rename,
  same patterns

Full specification archived — see git history for original content.