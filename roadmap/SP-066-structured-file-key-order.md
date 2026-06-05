# SP-066: Preserve Key Order in Structured File Tools

**Status:** 📋 Proposed
**Date:** 2026-06-05
**Depends on:** none
**Priority:** Medium

## Background

`write_structured_file` and `patch_structured_file` serialize JSON and YAML using Go's `map[string]interface{}` as the internal data representation. Go maps have **no iteration order guarantee** — the standard library JSON and YAML encoders both produce **alphabetically sorted keys**.

This means:
1. **`write_structured_file`** — The LLM sends keys in a logical order (e.g., `name`, `version`, `description` for a `package.json`), but the written file has them alphabetically (`description`, `name`, `version`).
2. **`patch_structured_file`** — Even if the original file had a sensible key order, reading it into `map[string]interface{}` and re-serializing destroys that order entirely. Every patch operation rewrites the file with alphabetical keys.

For many structured file formats, **key order is semantically meaningful**:
- `package.json` — `name`/`version` first by convention
- `go.mod` (if it were structured) — `module` first
- `docker-compose.yml` — `services` before `volumes`
- `tsconfig.json` — `compilerOptions` before `include`/`exclude`
- CI/CD configs — `on`/`trigger` before `steps`
- Any file where humans read top-to-bottom and expect logical grouping

## Problem

The current implementation silently reorders keys on every write, which:
- Makes diffs noisy (key order changes show as modifications even when values didn't change)
- Breaks conventions in formats where key order carries meaning
- Degrrees readability of generated structured files

## Proposed Solution

Replace `map[string]interface{}` with an **ordered map** throughout the structured file handling pipeline. Use [`github.com/wk8/go-ordered-map/v2`](https://github.com/wk8/go-ordered-map) as the internal representation.

### Changes required

1. **Add `go-ordered-map/v2` dependency** (`go get`)
2. **Rewrite `serializeStructuredContent`** — Custom JSON encoder that walks the ordered map in insertion order; custom YAML marshaler that does the same
3. **Rewrite `deserializeStructuredContent`** — Parse JSON/YAML into ordered maps, preserving key order from the source
4. **Rewrite `normalizeYAMLValue`** — Convert `map[interface{}]interface{}` (YAML v3 output) into ordered maps while preserving key order
5. **Rewrite all patch mutation functions** (`applyMutation`, `mutateAtLeaf`, `applyPatchOperation`, `readPointerValue`) — Replace `map[string]interface{}` traversal with ordered map traversal
6. **Update `validateDataAgainstSchema`** — Accept ordered maps alongside the existing `map[string]interface{}` interface
7. **Add tests** — Verify key order is preserved through write, patch, and round-trip cycles

### Design decisions

- **JSON parsing order preservation**: The LLM sends tool args as JSON. Go's `json.Unmarshal` into `map[string]interface{}` loses order. We need to intercept at the tool argument parsing level (`pkg/agent/tools.go:24`) and parse into ordered maps before the handler receives them. This means the `args` parameter type changes from `map[string]interface{}` to `*orderedmap.OrderedMap[string, interface{}]` at the tool handler boundary.
- **YAML parsing**: `yaml.v3` unmarshals into `map[string]interface{}` or `map[interface{}]interface{}` — both unordered. We need a custom `Unmarshal` that populates an ordered map by walking the YAML node tree directly (`yaml.Node`).
- **Schema validation**: Currently accepts `map[string]interface{}`. Needs to also accept ordered maps.

### Scope boundaries

- **In scope**: `write_structured_file`, `patch_structured_file`, all internal helpers in `tool_handlers_structured.go`
- **In scope**: Tool argument parsing in `tools.go` (where `json.Unmarshal` into `map[string]interface{}` happens) — this is the entry point where order is first lost
- **In scope**: `normalizeYAMLValue` and all patch mutation functions
- **Out of scope**: `write_file` and `edit_file` (these work with raw strings, not structured data)
- **Out of scope**: Schema validation logic changes beyond accepting ordered maps as input

## Risks

- **Tool argument parsing is shared** — Changing `args` from `map[string]interface{}` to ordered map affects **all** tool handlers, not just structured file tools. Need to ensure other handlers still work (they access `args["key"]` which ordered maps support via `.Get()` or a map-like interface).
- **Performance** — Ordered maps have O(1) lookups but slightly higher allocation. Acceptable for file I/O paths.
- **Backward compatibility** — The tool handler signature (`func(ctx, *Agent, map[string]interface{})`) is used everywhere. Changing it requires updating all ~30+ handlers. **Mitigation**: Keep the handler signature as `map[string]interface{}` but add a parallel `orderedArgs` field on the Agent or context that structured file tools can opt into.

## Alternative Approaches Considered

1. **Keep `map[string]interface{}` but store key order separately** — Add a `[]string` key order list alongside the map. More complex to maintain through mutations.
2. **Only fix `patch_structured_file`** — Parse existing file's key order from raw text, preserve on re-serialize. Simpler but doesn't fix `write_structured_file`.
3. **Use `encoding/json` with `json.RawMessage`** — Parse into `map[string]json.RawMessage`, then re-encode values in order. Loses type information, adds complexity.
