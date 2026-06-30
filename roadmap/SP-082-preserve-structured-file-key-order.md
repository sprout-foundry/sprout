# SP-082: Preserve Key Insertion Order in Structured File Tools

**Status:** ✅ Implemented (2026-06-30) — supersedes the original SP-066 key-order proposal
**Date:** 2026-06-27
**Depends on:** none
**Priority:** Medium (UX / diff hygiene — affects every `write_structured_file` / `patch_structured_file` call)
**Effort Estimate:** ~half a week

## Problem

`write_structured_file` and `patch_structured_file` serialize JSON and YAML using Go's `map[string]interface{}` as the internal data representation. Go maps have **no iteration order guarantee**, and the standard library JSON and YAML encoders both produce **alphabetically sorted keys**.

This means:

1. **`write_structured_file`** — The LLM sends keys in a logical order (e.g., `name`, `version`, `description` for a `package.json`), but the written file has them alphabetized (`description`, `name`, `version`).
2. **`patch_structured_file`** — Even if the original file had a sensible key order, reading it into `map[string]interface{}` and re-serializing destroys that order entirely. Every patch operation rewrites the file with alphabetical keys.

For many structured file formats, **key order is semantically meaningful**:

- `package.json` — `name`/`version` first by convention
- `go.mod` (if it were structured) — `module` first
- `docker-compose.yml` — `services` before `volumes`
- `tsconfig.json` — `compilerOptions` before `include`/`exclude`
- CI/CD configs — `on`/`trigger` before `steps`
- Any file where humans read top-to-bottom and expect logical grouping

The current implementation silently reorders keys on every write, which:
- Makes diffs noisy (key-order changes show as modifications even when values didn't change)
- Breaks conventions in formats where key order carries meaning
- Degrades readability of generated structured files

## Goals

1. `write_structured_file` emits JSON/YAML in the order the LLM provided the keys.
2. `patch_structured_file` preserves the on-disk key order for any keys it doesn't modify.
3. Both tools work for both JSON and YAML with one implementation.
4. The output of either tool is byte-identical (modulo the patched field) to what was sent.

## Design

### Approach: `yaml.Node` as the order-preserving intermediate

The original SP-066 proposed `github.com/iancoleman/orderedmap` for JSON only. The better choice for *both* JSON and YAML is `gopkg.in/yaml.v3`'s `yaml.Node`, which:

- Is already a dependency (we use it transitively through config files).
- Carries order natively — `yaml.Node.Content` is an ordered slice.
- Marshals to JSON via `yaml.Node.Encode()` → preserves order.
- Marshals to YAML directly via `yaml.Encoder.Encode(node)` → preserves order.
- Reads existing JSON/YAML preserving order (round-trip-safe).

### Internal type change

Replace `map[string]interface{}` with `*yaml.Node` throughout `pkg/agent_tools/write_structured_file.go` and `pkg/agent_tools/patch_structured_file.go`. The `ToolEnv` and `args` path through `extractStructured` already supports generic JSON unmarshal; the change is at the internal storage and serialization layer.

For input, parse the LLM-supplied JSON/YAML into a `yaml.Node` (preserving order from the source). For output, marshal the `yaml.Node` back to the requested format.

### Patch path

`patch_structured_file` currently:
1. Reads the file into `map[string]interface{}`.
2. Applies JSON Patch operations.
3. Re-serializes (destroying original order).

New flow:
1. Read the file into a `yaml.Node` (preserving order).
2. Apply JSON Patch operations against the node tree (using `json-patch` which operates on `any` — the same patch ops still work, just against a `*yaml.Node` instead of `map[string]interface{}`).
3. Re-serialize the node tree to the file's original format (JSON or YAML detected from extension or magic bytes).

A small adapter (`yamlNodeFromMap` / `mapFromYamlNode`) handles the patch op library's expectation of `map[string]interface{}`. Round-trip tests verify that `yamlNodeFromMap(mapFromYamlNode(n))` is identity-equivalent in structure and order.

### Tool definition update

Update the tool descriptions for both tools to note "key order is preserved" — small but useful documentation.

## Phase plan

| Phase | Scope |
|-------|-------|
| 1 | Replace internal `map[string]interface{}` with `*yaml.Node` in both tools; add round-trip tests for JSON and YAML. |
| 2 | Add `package.json`-style ordering tests (write → read → assert order matches input). |
| 3 | Update `patch_structured_file` to read the original file as a `yaml.Node`; add a test that a 1-field patch leaves the other keys' order intact. |
| 4 | Update tool definitions to mention key-order preservation. |

## Success Criteria

- `write_structured_file` with input `{"name": "x", "version": "1.0", "dependencies": {}}` produces a file with `name`/`version`/`dependencies` in that order (not alphabetical).
- `patch_structured_file` of a 1-field change on a 10-key file produces a 1-line diff instead of re-sorting the whole file.
- Both round-trip tests (JSON and YAML) pass.
- `go test ./pkg/agent_tools/...` green; existing patch tests still pass.
- `make build-all` clean.

## Risks

- **`yaml.Node` API friction.** Some JSON-Patch operations assume `map[string]interface{}` and may need adapter helpers. Mitigation: the adapter is small (`yamlNodeFromMap` / `mapFromYamlNode`); tests verify parity.
- **Loss of order during JSON-Patch.** The patch library may walk the tree in ways that lose order. Mitigation: run the patch through the `yaml.Node` adapter at boundaries, not internally.
- **Indentation / formatting drift.** `yaml.Node` carries style hints (`Style: LiteralBlockStyle` etc.) that need to be preserved on round-trip. Mitigation: tests cover common formatting cases; if drift surfaces, use `gofmt`-style normalization.

## Open Questions

1. Should the patch operations be applied against the `yaml.Node` tree directly, or via a `map[string]interface{}` view with adapter round-trip? **Recommendation:** adapter approach — simpler, leverages the existing patch library unchanged.

## References

- Original proposal: `roadmap/SP-066-structured-file-key-order.md`
- `gopkg.in/yaml.v3` `yaml.Node` docs: https://pkg.go.dev/gopkg.in/yaml.v3#Node
