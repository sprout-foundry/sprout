# SP-059: Subagent ↔ Primary Interaction Overhaul + Delegate Retirement

**Status:** ✅ Implemented (Phases 1–6 complete; delegate tool retired; audited 2026-06-27)

The original sprout had two parallel agent-dispatch paths: `delegate` (an
in-process delegation tool that ran a child agent in the same address space
and returned its summary) and `run_subagent` (a newer dispatch tool that
spawned a sub-agent as a child process with proper isolation, depth tracking,
and risk-profile inheritance). SP-059 completed the unification: the
`delegate` tool was removed entirely, all delegation now flows through
`run_subagent`, and the sub-agent protocol was hardened across six phases
(depth-aware UI, persona inheritance, tool allowlist propagation, interrupt
propagation, scratch directory sharing, audit log). Phase 6 was an explicit
audit (`SP-059-6a-review.md`) confirming no needed delegate functionality was
missing from the `run_subagent` replacement.

## Key decisions

- **One dispatch path, not two.** Two parallel paths meant two sets of bugs,
  two security models, two UX surfaces. Killing `delegate` collapsed the
  matrix.
- **Depth tracking is the agent's responsibility**, not the dispatcher's.
  The sub-agent inherits its parent's depth + 1 and surfaces it in its own
  events so the WebUI can render nesting.
- **Persona + tool allowlist propagate to sub-agents.** A primary running as
  the orchestrator persona spawns sub-agents that inherit the orchestrator's
  capabilities, not the full set. This prevents accidental privilege
  escalation via `run_subagent`.
- **Scratch directory sharing.** Sub-agents inherit the parent's scratch
  dir, so a primary can hand off a long-lived work-in-progress file path
  without copying.
- **Interrupt propagation.** WebUI Stop on the primary cancels the sub-agent
  via `interruptCtx`. No orphaned subprocesses.
- **Audit-log unification.** Every sub-agent invocation emits the same
  event shape as primary tool calls, so the audit log has no special cases.

## Artifacts

- code: `pkg/agent/subagent_runner*.go` — `run_subagent` implementation
- code: `pkg/agent/persona.go` — persona inheritance + tool allowlist
- code: `pkg/agent_tools/run_subagent*.go` — handler + tests
- audit: `SP-059-6a-review.md` (Phase 6 retrospective)
- companion: SP-023 (in-process subagents), SP-051 (depth-aware CLI UI),
  SP-053 (WebUI parity)
- removed: `pkg/agent_tools/delegate*.go` (deleted)

Full specification archived — see git history for original content.