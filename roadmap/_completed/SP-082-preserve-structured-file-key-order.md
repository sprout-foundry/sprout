# SP-082: Preserve Key Insertion Order in Structured File Tools

**Status:** ✅ Implemented (2026-06-30; key order preserved in write and patch operations)

`write_structured_file` and `patch_structured_file` previously used Go's `map[string]interface{}` internally, which has no iteration order guarantee, causing JSON/YAML output to be alphabetically sorted. This made diffs noisy and broke conventions in formats where key order carries meaning (e.g., `package.json`, `docker-compose.yml`). The fix replaced the internal `map[string]interface{}` with `*yaml.Node` from `gopkg.in/yaml.v3`, which carries order natively via `yaml.Node.Content` as an ordered slice. The `yaml.Node` approach handles both JSON and YAML with one implementation — it marshals to JSON preserving order and to YAML directly. `patch_structured_file` now reads the original file as a `yaml.Node`, applies JSON Patch operations via a small adapter, and re-serializes preserving the original key order for unmodified keys.

## Key decisions

- Use `yaml.Node` instead of `github.com/iancoleman/orderedmap` — handles both JSON and YAML natively, already a transitive dependency, round-trip-safe.
- JSON Patch operations applied via adapter (`yamlNodeFromMap` / `mapFromYamlNode`) at boundaries rather than rewriting the patch library — simpler, leverages existing patch ops unchanged.
- Format detected from file extension or magic bytes for re-serialization.
- Tool definitions updated to note "key order is preserved" in descriptions.

## Artifacts

- code: `pkg/agent_tools/write_structured_handler.go` — uses `*yaml.Node` internally, preserves input key order
- code: `pkg/agent_tools/patch_structured_file_handler.go` — reads as `yaml.Node`, preserves original key order for unmodified keys
- tests: `pkg/agent_tools/write_structured_handler_test.go` — round-trip tests for JSON and YAML, `package.json`-style ordering tests

Full specification archived — see git history for original content.
