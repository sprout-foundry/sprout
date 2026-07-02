# SP-045: WASM Build Feature Parity

**Status:** ✅ Shipped (Tiers 1-3 complete 2026-06)

Sprout ships a WASM build (`cmd/wasm/`) for in-browser use. Originally
the WASM build was feature-poor — many `pkg/` capabilities weren't
exposed because they required external services or native binaries.
SP-045 closed the gap in three tiers: (1) pure-Go features reachable
via WASM-exposed functions (`getConfig`, `setConfig`, `ConversationStore`
access, `workspaceAnalysis`); (2a) ONNX embedding provider usable in
WASM via `__sproutONNX` bridge (no native binary needed); (2b) agent
and LLM command surface (`runAgent`, `runPlan`, `runQuestion`,
`runCommit`, `runReview`) including streaming, CORS, and tool
execution; (3) module splitting for distribution. `tinygo` was
evaluated and rejected (CC: not viable); pure-Go wasm build is the path.

## Key decisions

- **Pure-Go WASM via standard `GOOS=js GOARCH=wasm`** rather than
  tinygo — avoids the C compiler dependency, easier CI.
- **Tier 2a's ONNX bridge** runs the embedding model in-browser via
  onnxruntime-web; `__sproutONNX` is the JS-callable bridge.
- **Streaming is event-based, not chunked HTTP** — browser receives
  partial responses as they happen, no buffering.
- **CORS + auth handled at the bridge layer**, not the WASM module.
- **Build matrix sweep** consolidated feature flags so each build
  declares what it includes; the matrix is documented in the spec.

## Artifacts

- code: `cmd/wasm/config_funcs.go`, `conversation_funcs.go`,
  `agent_funcs.go`, `llm_funcs.go`
- code: `pkg/embedding/onnx_wasm_bridge.go` — JS bridge
- code: `pkg/embedding/onnx_wasm_bridge_test.go` — bridge tests
- tests: cross-build verification per tier
- docs: build matrix and feature flags in spec

Full specification archived — see git history for original content.