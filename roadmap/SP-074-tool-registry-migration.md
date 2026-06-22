# SP-074: Finish the Tool-Registry Migration — Retire the Dual-Dispatch Shim

**Status:** 📋 Proposed
**Date:** 2026-06-15
**Depends on:** none (completes the deferred SP-038 work)
**Priority:** Medium (tech debt; removes a class of "which registry?" bugs)
**Effort Estimate:** ~2-3 days

## Problem

The agent runs **two tool registries** in parallel during an unfinished
migration (SP-038): the canonical `ToolConfig` registry
(`pkg/agent/tool_definitions.go` → drives LLM definitions + persona filtering)
and the new interface-based registry (`pkg/agent_tools` → drives execution
dispatch). A dual-dispatch shim bridges them. Three `TODO(SP-038)` markers flag
the loose ends, and they cause real rough edges:

| Loose end | File | Symptom |
|---|---|---|
| `ToolRegistry.ForPersona()` returns **all** tools | `pkg/agent_tools/registry.go:77` | Per-persona filtering only happens in the *other* registry (`BuildToolDefinitions` + `getActivePersonaToolAllowlist`); the new registry's `ForPersona` is a stub, so any code that trusts it leaks every tool. |
| `ToolEnv.OutputWriter` hardcoded to `os.Stdout` | `pkg/agent/tool_security.go:229` | New-interface tools that stream output bypass the agent's `OutputRouter` (no WebUI routing, no event correlation). |
| `ToolEnv.ApprovalManager` passed `nil` | `pkg/agent/tool_security.go:239` | `security.ApprovalManager` and `tools.ApprovalManager` have different signatures, so migrated tools can't request approvals through the env — they fall back to ad-hoc paths. |

The split means a contributor must know *which* registry owns a given concern,
and the SP-063 work had to register tools into both by hand. Finishing the
migration removes that hazard.

## Proposed Solution

### Phase 1: Real per-persona filtering in the new registry

Replace the `ForPersona` stub with actual filtering driven by the persona
allowlist (the same source `getActivePersonaToolAllowlist` uses):

```go
func (r *ToolRegistry) ForPersona(allowlist []string) map[string]ToolHandler
```

Take the allowlist as input (the registry shouldn't import config/persona).
Empty allowlist → all tools (current behavior for unrestricted personas).
Callers pass the resolved allowlist. Add tests covering allowlist, empty, and
unknown-tool cases.

### Phase 2: Output routing through the agent

Give `ToolEnv` an output sink that routes through the agent's `OutputRouter`
instead of raw `os.Stdout`:

- Add an agent accessor that returns an `io.Writer` backed by
  `PrintLine`/`PrintLineAsync` (so streamed tool output reaches the WebUI and
  is event-correlated).
- Wire it into `env.OutputWriter` in `tool_security.go`; keep `os.Stdout` only
  as the nil-agent fallback.

### Phase 3: ApprovalManager adapter

Add an adapter that implements `tools.ApprovalManager` over the agent's
`security.ApprovalManager`, translating the method signatures, and set
`env.ApprovalManager` to it. Migrated tools then request approvals through the
env uniformly (CLI + WebUI), removing the bespoke approval paths.

### Phase 4: Collapse the shim + delete the TODOs

- Once execution, definitions, filtering, output, and approvals all flow
  through one path, retire the dual-dispatch fallback for migrated tools (keep
  it only for the genuinely legacy func-style handlers still pending, document
  which remain).
- `grep -rn "TODO(SP-038)" pkg/` returns nothing.

## Success Criteria

- `ForPersona` returns exactly the allowlisted tools (tested).
- New-interface tools' streamed output appears in the WebUI (routed via
  `OutputRouter`), not just stdout.
- A migrated tool can request an approval via `env.ApprovalManager` and it
  surfaces through the normal CLI/WebUI prompt.
- `grep -rn "TODO(SP-038)" pkg/` is empty.
- `make build-all` + `go test ./...` green; no behavior change to existing
  tool gating (regression-tested against the current persona allowlists).

## Out of Scope

- Migrating the remaining legacy func-style handlers to the new interface (the
  thin wrappers noted in `all.go`) — track separately; this spec finishes the
  *dispatch/env* plumbing, not every handler's rewrite.

## Open Questions

1. Should `ForPersona` live on the registry (taking an allowlist) or move to a
   small filter helper in `pkg/agent` that already has config access?
   Recommendation: keep it on the registry taking `[]string` to avoid a config
   import in `pkg/agent_tools`.
