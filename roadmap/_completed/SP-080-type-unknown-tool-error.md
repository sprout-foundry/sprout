# SP-080: Type the Unknown-Tool Error in ToolRegistry

**Status:** ✅ Implemented (2026-06-30; unknown-tool errors use typed InvalidInputError)

The new tool registry at `pkg/agent_tools/registry.go` was returning plain `fmt.Errorf` errors for unknown tools, forcing the dispatch shim at `pkg/agent/tool_executor_sequential.go` to string-match `err.Error()` to detect that case. This was the kind of pattern SP-008-2d was meant to retire. The registry now returns `agenterrors.NewInvalidInputError("unknown tool: "+name, nil)` for missing tools, the dispatch shim relies solely on `agenterrors.IsInvalidInput(err)`, and the TODO comment and string-match fallback were removed. Tests asserting on the literal `"unknown tool"` substring were updated to use `agenterrors.IsInvalidInput`.

## Key decisions

- Use existing `agenterrors.NewInvalidInputError` rather than creating a new error type — it's the canonical classifier already used elsewhere.
- Remove the `strings.Contains(err.Error(), "unknown tool")` branch entirely in favor of typed error checking.
- Update tests to assert on `agenterrors.IsInvalidInput(err)` rather than string equality for unknown-tool errors.

## Artifacts

- code: `pkg/agent_tools/registry.go` — returns `agenterrors.NewInvalidInputError` for unknown tools
- code: `pkg/agent/tool_executor_sequential.go` — dispatch shim uses only `agenterrors.IsInvalidInput(err)`
- tests: `pkg/agent_tools/registry_integration_test.go` — updated to assert on typed error

Full specification archived — see git history for original content.
