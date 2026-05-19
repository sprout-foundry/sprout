# SP-031: MCP Tool Input Validation Hardening

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** Medium-High (input boundary; affects every MCP-provided tool)
**Depends on:** None
**Related:** SP-004 (Security & MCP), SP-028 (test stabilization)

## Problem

Every MCP server tool advertises an `inputSchema` (JSON Schema, captured at `pkg/mcp/client.go:336`). Sprout receives that schema, stores it on the `MCPToolWrapper`, and **never validates a single argument against it**. Two explicit TODOs mark the gap:

```go
// pkg/mcp/tool_wrapper.go:171  (in CanExecute)
// TODO: Could add schema validation here based on w.mcpTool.InputSchema

// pkg/mcp/tool_wrapper.go:237  (in ValidateArgs)
// TODO: Implement JSON schema validation based on w.mcpTool.InputSchema
// For now, just return nil (no validation)
return nil
```

Worse: `grep` for production callers of `ValidateArgs` finds **zero hits outside the wrapper's own test file**. So even if the method were implemented, no part of the agent's tool-call dispatch would invoke it. The validation pipeline is wired neither at the schema layer nor at the call site.

### Why this matters

- MCP tools accept arbitrary JSON arguments produced by the LLM. Malformed arguments (wrong types, missing required fields, out-of-range values) currently flow straight to the MCP server.
- Each downstream server gets to define its own error behaviour — some are robust, some panic, some leak partial side effects before erroring.
- Without local validation, sprout can't distinguish "the LLM produced bad arguments" from "the MCP server is misbehaving," which makes error recovery and retry logic worse.
- A bad MCP server could declare a schema that doesn't match what it accepts, and we'd have no enforcement on the client side either way.

This is also a precondition for SP-008's typed-error work: until we can detect "InvalidInputError" at the call boundary, we can't classify it.

## Goals / Non-Goals

**Goals**
- Every argument map passed to an MCP tool is validated against the tool's `InputSchema` before the network call.
- A clear failure mode: validation errors return a typed error and never reach the MCP server.
- The validator runs in the agent tool-execution dispatch path, not as an after-the-fact check.
- Validation overhead is negligible for typical tool calls (schemas are small; cache compiled validators).

**Non-Goals**
- Validating responses from MCP servers (separate concern; could be a future spec).
- Schema validation for non-MCP tools (sprout's native tools have hand-coded validation already).
- Authoring schemas for tools that don't advertise one — if an MCP server provides no schema, no validation is enforced (warn once, proceed).
- Refactoring the MCP wrapper structure beyond what's needed.

## Current State

| Component | File | Status |
|-----------|------|--------|
| Schema captured from server | `pkg/mcp/client.go:336, 355` | ✅ `InputSchema map[string]interface{}` populated from `tools/list` |
| Schema stored on wrapper | `pkg/mcp/tool_wrapper.go` (field `mcpTool MCPTool`) | ✅ Available at validation time |
| `ValidateArgs` method | `pkg/mcp/tool_wrapper.go:233-238` | ❌ Stub — returns `nil` unconditionally |
| `CanExecute` schema check | `pkg/mcp/tool_wrapper.go:171` | ❌ TODO comment only |
| Production callers of `ValidateArgs` | (none found) | ❌ Not wired into dispatch |
| Tool-call dispatch site | `pkg/agent/tool_executor*.go` | Needs validation hook |

## Proposed Solution

### A. Implement schema validation in `ValidateArgs`

Use a Go JSON Schema library. Two reasonable choices:

| Library | Pros | Cons |
|---------|------|------|
| `github.com/santhosh-tekuri/jsonschema/v6` | Pure Go, draft 2020-12 + earlier, well-maintained, no deps | Adds a real dependency to the binary |
| `github.com/xeipuuv/gojsonschema` | Widely used, draft-07 | Older, less actively maintained |

**Recommendation:** `santhosh-tekuri/jsonschema/v6`. It supports the JSON Schema drafts MCP servers actually use and has zero transitive deps.

Implementation sketch:

```go
// pkg/mcp/tool_wrapper.go

type MCPToolWrapper struct {
    // ... existing fields
    compiledSchema *jsonschema.Schema // compiled once, reused
}

func (w *MCPToolWrapper) ValidateArgs(args map[string]interface{}) error {
    if w.mcpTool.InputSchema == nil {
        return nil // no schema advertised; skip
    }
    if w.compiledSchema == nil {
        // lazy-compile on first use; cache result
        if err := w.compileSchema(); err != nil {
            // compilation failure: warn once, fall through (don't block tool use on our bug)
            return nil
        }
    }
    if err := w.compiledSchema.Validate(args); err != nil {
        return &InvalidArgsError{Tool: w.mcpTool.Name, Server: w.mcpTool.ServerName, Wrapped: err}
    }
    return nil
}
```

The `compiledSchema` lives on the wrapper. Wrappers are created once per tool per server initialization (`pkg/mcp/manager.go`), so compilation cost is paid once at startup.

### B. Wire `ValidateArgs` into the agent dispatch path

The agent's tool-call dispatcher calls into the wrapper via `Tool.Execute(ctx, params)` (the `tools.Tool` interface). Add a pre-execution validation hook so every MCP-provided tool runs `ValidateArgs` before its network call.

Two implementation options:

1. **Call from inside `MCPToolWrapper.Execute`** (the simplest path; the validation lives where the schema lives).
2. **Call from the dispatcher in `pkg/agent/tool_executor*.go`** (centralised; lets us add typed-error classification once for all tools).

**Recommendation:** Option 1 for the first cut. Option 2 becomes possible once SP-008's typed-error types exist.

### C. Define the typed error

```go
// pkg/mcp/errors.go
type InvalidArgsError struct {
    Tool    string
    Server  string
    Wrapped error
}

func (e *InvalidArgsError) Error() string { /* ... */ }
func (e *InvalidArgsError) Unwrap() error { return e.Wrapped }
```

Eventually subsumed by `pkg/errors.InvalidInputError` from SP-008's hierarchy.

### D. Surfacing failures to the LLM

When `ValidateArgs` fails, the agent must:
1. **Not invoke the MCP server.**
2. **Return the validation error to the LLM as the tool result** so the model can correct its arguments on the next iteration.
3. **Log the failure** with `(tool, server, validation_errors)` for debugging.

Format the LLM-visible message as a concise enumeration of failed paths:

```
Tool 'fetch_url' validation failed:
  - 'url' is required but missing
  - 'timeout_ms' must be a number, got string
```

This is more recoverable than just forwarding the raw `jsonschema` error.

### E. Tests

- Unit: `ValidateArgs` against a representative schema (required fields, types, enums, nested objects).
- Unit: `ValidateArgs` with `nil` schema — returns nil (skip).
- Unit: `ValidateArgs` with malformed schema — warns once, returns nil (fail-open on our bug).
- Integration: agent receives an `InvalidArgsError` from a stubbed MCP wrapper and surfaces a useful tool-result message to the LLM mock.

## Implementation Phases

### Phase 1: Implement validation (Day 1-2)
- [ ] Add `github.com/santhosh-tekuri/jsonschema/v6` to `go.mod`
- [ ] Implement `compileSchema()` + cache field on `MCPToolWrapper` (`pkg/mcp/tool_wrapper.go`)
- [ ] Implement `ValidateArgs` body (replaces stub at line 237)
- [ ] Add `InvalidArgsError` type in `pkg/mcp/errors.go`

### Phase 2: Wire into execution (Day 2-3)
- [ ] Call `ValidateArgs` at top of `MCPToolWrapper.Execute` (before network call)
- [ ] Update `CanExecute` to invoke `ValidateArgs` and return false on failure (closes line 171 TODO)
- [ ] Format validation errors into LLM-visible tool-result messages

### Phase 3: Tests (Day 3-4)
- [ ] Unit tests for `ValidateArgs` (required, types, enums, nested, nil schema, malformed schema)
- [ ] Update existing `TestMCPToolWrapper_ValidateArgs` (`pkg/mcp/tool_wrapper_test.go:124`) to assert real validation behaviour
- [ ] Integration test: agent receives validation failure, returns recoverable message to LLM

### Phase 4: Observability (Day 4-5)
- [ ] Structured log entry on validation failure with `{tool, server, errors}` fields
- [ ] Counter/metric for `mcp_validation_failures` (cooperates with SP-008's structured logging)

## Success Criteria

| Metric | Target |
|--------|--------|
| Every MCP tool call validates args | 100% (for tools that advertise a schema) |
| TODO comments at `tool_wrapper.go:171,237` | Removed |
| Production `.ValidateArgs(` call sites | ≥ 1 (the dispatcher), up from 0 |
| Unit-test coverage of `ValidateArgs` | All 4 cases above pass |
| Validator compile overhead | < 1ms per tool at MCP server init |
| Validation overhead per tool call | < 100µs for typical schemas |
| LLM-visible error message | Lists specific field paths and reasons (not raw JSON Schema output) |

## Risks

- **Schema strictness mismatch.** Some MCP servers may publish schemas that don't exactly match what they accept (e.g. accept extra fields, but schema says `additionalProperties: false`). Mitigation: validate **inputs from the LLM**, not what the server returns; but we may still reject calls a permissive server would have accepted. If this becomes a problem, gate strict mode behind a config flag and fall back to schema-as-warning for unknown-server-quirks cases.
- **Dependency footprint.** Adding `santhosh-tekuri/jsonschema/v6` brings ~30K LOC of validator code into the binary. Mitigation: it's pure Go with no transitive deps; one dependency for a foundational capability is acceptable.
- **Fail-closed vs fail-open.** If our validator has a bug compiling a valid schema, do we block the tool? Recommendation: fail-open on compile errors (the schema-compilation failure is *our* bug), fail-closed on validation errors (those are LLM-argument bugs).

## Files Reference

| File | Action |
|------|--------|
| `pkg/mcp/tool_wrapper.go` | Modify: implement `ValidateArgs` (line 237); call it from `Execute` and `CanExecute` (line 171) |
| `pkg/mcp/errors.go` | Create: `InvalidArgsError` typed error |
| `pkg/mcp/tool_wrapper_test.go` | Modify: replace trivial assertions at line 124-127 with real schema tests |
| `pkg/agent/tool_executor*.go` | (Optional, Phase 4) hook for typed-error classification at dispatch |
| `go.mod` | Modify: add `github.com/santhosh-tekuri/jsonschema/v6` |
