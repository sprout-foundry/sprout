# SP-074: Finish the Tool-Registry Migration — Retire the Dual-Dispatch Shim

**Status:** ✅ Implemented (2026-06-26; single registry path, all TODO(SP-038) markers removed)

The agent previously ran two tool registries in parallel during an unfinished migration from SP-038, bridged by a dual-dispatch shim. This spec finished the migration so execution, definitions, filtering, output routing, and approvals all flow through the new `pkg/agent_tools` registry. Three rough edges were resolved: `ForPersona` was a stub that returned all tools (now does real allowlist filtering), `ToolEnv.OutputWriter` was hardcoded to `os.Stdout` (now routes through `OutputRouter`), and `ToolEnv.ApprovalManager` was `nil` (now wired via an adapter). The dual-dispatch shim was collapsed and all `TODO(SP-038)` markers removed.

## Key decisions

- `ForPersona` takes an explicit `[]string` allowlist parameter instead of importing config/persona — keeps `pkg/agent_tools` decoupled from config.
- Output routing uses an agent accessor returning an `io.Writer` backed by `PrintLine`/`PrintLineAsync` so streamed tool output reaches the WebUI and is event-correlated.
- `toolsApprovalAdapter` wraps the agent's `security.ApprovalManager`, translating method signatures so migrated tools request approvals uniformly through `ToolEnv`.
- Legacy func-style handlers still pending migration are kept in the shim with documentation — this spec finishes dispatch plumbing, not every handler rewrite.

## Artifacts

- code: `pkg/agent_tools/registry.go` — real `ForPersona` allowlist filtering
- code: `pkg/agent/tool_security.go` — `OutputWriter` routed through `OutputRouter`, `ApprovalManager` adapter wired
- code: `pkg/agent/tool_security.go` — `toolsApprovalAdapter` type implementing `tools.ApprovalManager`
- tests: `pkg/agent_tools/registry_test.go` — `ForPersona` allowlist, empty, and unknown-tool cases

Full specification archived — see git history for original content.
