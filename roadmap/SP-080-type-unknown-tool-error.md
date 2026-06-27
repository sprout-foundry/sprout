# SP-080: Type the Unknown-Tool Error in ToolRegistry

**Status:** 📋 Spec
**Date:** 2026-06-27
**Depends on:** none
**Priority:** Low (cleanup; removes a string-matching fallback)
**Effort Estimate:** ~half a day

## Problem

The new tool registry at `pkg/agent_tools/registry.go::ExecuteTool` returns a plain `fmt.Errorf`-style error when a tool name isn't found. The dispatch shim in `pkg/agent/tool_executor_sequential.go:166` then has to *string-match* `err.Error()` to detect that case:

```go
// TODO: migrate registry to return InvalidInputError for unknown tools
// so this string check can be removed entirely.
if err != nil && (agenterrors.IsInvalidInput(err) || strings.Contains(err.Error(), "unknown tool")) {
    if fallbackResult, fallbackErr, handled := te.tryExecuteMCPTool(normalizedToolName, args); handled {
        ...
    }
}
```

`agenterrors.NewInvalidInputError` already exists at `pkg/errors/types.go:213` and `agenterrors.IsInvalidInput` is the canonical classifier. The string-match branch is a workaround for the registry not using it.

This is exactly the kind of pattern SP-008-2d was meant to retire: typed errors at boundaries, no string matching for control flow.

## Goals

1. Make `ToolRegistry.ExecuteTool` return `agenterrors.NewInvalidInputError("unknown tool: "+name, nil)` for missing tools.
2. Remove the `strings.Contains(err.Error(), "unknown tool")` branch from the dispatch shim; rely solely on `agenterrors.IsInvalidInput`.
3. Update tests that currently assert on the string `"unknown tool"` to assert on `agenterrors.IsInvalidInput`.

## Design

### Registry change

In `pkg/agent_tools/registry.go::ExecuteTool` (or wherever the unknown-tool error is constructed):

```go
// before
return nil, fmt.Errorf("unknown tool: %s", name)

// after
return nil, agenterrors.NewInvalidInputError("unknown tool: "+name, nil)
```

If the unknown-tool check happens in multiple places (the lookup, the validation, etc.), update all of them.

### Dispatch shim change

In `pkg/agent/tool_executor_sequential.go:166`:

```go
// before
if err != nil && (agenterrors.IsInvalidInput(err) || strings.Contains(err.Error(), "unknown tool")) {

// after
if err != nil && agenterrors.IsInvalidInput(err) {
```

The TODO comment above the line goes away with the fallback.

### Test updates

Audit tests that rely on the literal `"unknown tool"` substring:

```bash
grep -rn '"unknown tool"' pkg/agent_tools/ pkg/agent/
```

For each match in a test, switch to:

```go
if !agenterrors.IsInvalidInput(err) {
    t.Fatalf("expected InvalidInputError, got %v", err)
}
```

`pkg/agent_tools/registry_integration_test.go` and `pkg/agent_tools/new_tools_conformance_test.go` are the likely candidates.

## Success Criteria

- `grep -rn '"unknown tool"' pkg/agent_tools/ pkg/agent/` returns zero matches in non-test code; any remaining matches in tests use `agenterrors.IsInvalidInput` rather than string equality.
- `grep -rn 'strings.Contains.*"unknown tool"' pkg/` returns zero matches.
- The TODO comment at `pkg/agent/tool_executor_sequential.go:166` is removed.
- `go test ./pkg/agent_tools/... ./pkg/agent/...` all green.

## Risks

- **Hidden string-match in other code paths.** A `grep` for `"unknown tool"` should be exhaustive across `pkg/`, but it's possible a third party (an integration test, a benchmark, an example) relies on the literal. Mitigation: run the full test suite; expect zero diff.
- **Error wrapping changes the unwrap chain.** `NewInvalidInputError` wraps the cause with `fmt.Errorf("...: %w", cause)`. If any consumer was relying on `errors.Is(err, some_inner_error)`, this could break. Mitigation: `agenterrors.IsInvalidInput` already handles unwrap; that's why it's the canonical classifier.

## Open Questions

None.
