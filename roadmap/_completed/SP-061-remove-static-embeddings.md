# SP-061: Remove Static Embedding Provider, Consolidate on ONNX

**Status:** ✅ Implemented (Static embedding provider removed via SP-091-2)

The static embedding provider shipped as an early stop-gap before ONNX
embeddings were production-ready. By 2026-06 the two providers had drifted
in quality (static used a 384-dim model with truncated semantic coverage;
ONNX uses a proper sentence-transformer model) and the static code path was
becoming a maintenance burden — every embedding call site needed to handle
two providers' worth of edge cases. SP-091-2 removed the static provider
entirely; ONNX is now the sole embedding path, and any code that referenced
the static backend fails fast at compile time rather than silently degrading
embedding quality at runtime.

## Key decisions

- **No fallback path.** If ONNX is unavailable the embedding call returns an
  error; the alternative (silent static-provider fallback) was the actual
  bug pattern.
- **Compile-time removal, not runtime gating.** All references to the static
  provider were removed, not feature-flagged off. Dead code accumulates.
- **Embeddings are required, not best-effort.** Any code path that depended
  on best-effort static embeddings was rewritten to either ensure ONNX is
  available or surface a clear error to the caller.
- **WASM dimension alignment.** Static used 384-dim; ONNX produces 768-dim
  (or whatever the model ships). All cached-vector lookup paths had to
  re-embed on the new dimension; no silent zero-padding.

## Artifacts

- code: `pkg/embedding/` — single ONNX provider (static files deleted)
- code: `pkg/agent_api/embedding.go` — single dispatch path
- tests: `pkg/embedding/onnx_test.go` (was `static_test.go`)
- companion: WASM shell `wasmshell` switched to ONNX lazy-load (SP-100 / SP-058)

Full specification archived — see git history for original content.