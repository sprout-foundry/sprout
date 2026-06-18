# SP-045-2: tinygo Spike — Go/No-Go Decision

**Date:** 2026-06-17
**Decision:** **NO-GO** — tinygo is not viable for `cmd/wasm` at this time.
**Spec reference:** SP-045 §5 (Distribution)

## Context

The stripped WASM binary (`-ldflags="-s -w"`) is ~43MB. SP-045 §5 suggests
tinygo could dramatically reduce this, but flags "compatibility risk with the
Go standard library features `pkg/agent` uses — needs a spike."

## Methodology

1. Enumerated the full dependency tree of `cmd/wasm` via
   `go list -deps -tags 'js wasm' ./cmd/wasm/` (505 packages).
2. Cross-referenced critical packages against the tinygo stdlib support
   matrix (https://tinygo.org/docs/reference/lang-support/stdlib/, v0.41.0).
3. Checked for `syscall/js` usage (the standard Go WASM interop layer).

## Blocking Incompatibilities

### 1. `net/http/cookiejar` — NOT IMPORTABLE (hard blocker)

tinygo's stdlib support matrix lists `net/http/cookiejar` as **not
importable**. Our HTTP client stack (`pkg/agent_providers`) depends on
`net/http` with cookie jar support for provider session management. This
alone makes tinygo a no-go for the agent/LLM features.

### 2. `syscall/js` — not supported by tinygo

`cmd/wasm` heavily uses `syscall/js` (18+ files: `store.go`,
`llm_funcs.go`, `embedding_funcs.go`, `agent_funcs.go`, etc.) for
JavaScript interop — registering the `SproutWasm` global, callbacks,
IndexedDB access, ONNX bridge, etc. tinygo uses its own WASM interop
(`runtime/volatile`, direct function table exports) and does **not**
support Go's `syscall/js` package. Every `syscall/js` call site would
need to be rewritten.

### 3. `reflect` limitations — breaks encoding/json at runtime

tinygo's `reflect` implementation is incomplete (`NumIn()`, `NumOut()`
unimplemented). The stdlib test matrix shows `encoding/json` tests fail
at runtime due to reflect gaps. Our codebase uses JSON extensively for
config, tool arguments, API responses, and state serialization.

### 4. `database/sql` — tests fail

Importable but doesn't pass tests. Used indirectly via configuration
and storage paths.

### 5. Third-party library compatibility unknown

The WASM module pulls in complex third-party libraries:
- `github.com/odvcencio/gotreesitter` (pure-Go tree-sitter)
- `github.com/coder/hnsw` (vector search)
- `github.com/santhosh-tekuri/jsonschema/v6`
- `gopkg.in/natefinch/lumberjack.v2`

None of these are tested against tinygo. Given the reflect/json gaps
above, several are likely to fail.

## Size Savings (theoretical, not achievable)

tinygo typically produces 1-5MB WASM binaries for simple programs vs
Go's 10-50MB. If it worked, we'd expect a ~5-10× reduction (~5-8MB
vs current 43MB). **This is not achievable** given the blocking issues.

## Recommendation

**Do not pursue tinygo for `cmd/wasm`.** The migration cost (rewriting
all `syscall/js` interop, working around reflect/json limitations,
validating third-party libraries) far exceeds the benefit. The binary
size reduction from `-ldflags="-s -w"` (already applied, ~25% saving)
plus the planned WASM module split (SP-045-3: lazy-loaded
`embedding.wasm`) is the more pragmatic path.

### Revisit criteria

Re-evaluate tinygo if ALL of the following change:
1. tinygo adds `syscall/js` support (or we rewrite the entire JS interop layer)
2. tinygo's `reflect` implementation reaches feature parity (enabling JSON)
3. `net/http/cookiejar` becomes importable
4. Go version alignment (tinygo currently tracks Go 1.26.2 / v0.41.0)

Until then, the standard Go compiler (`GOOS=js GOARCH=wasm`) remains
the only viable toolchain.
