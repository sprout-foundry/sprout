# SP-045: WASM Build Feature Parity

**Status:** 📋 Proposed
**Priority:** Medium

## 1. Executive Summary

`cmd/wasm` builds today and exposes a unix-shell-style API (36 POSIX commands plus
filesystem CRUD). What it doesn't expose is **any of the sprout-specific
functionality** that defines the product: no agent loops, no semantic search,
no memory, no embeddings, no skills, no LLM-backed commands. This spec
brings the WASM build to as-close-to-feature-parity as the browser sandbox
allows.

Several blockers have already been removed in the lead-up to this spec:

- `pkg/embedding` now builds for `js/wasm` (CGO-only ONNX paths are
  stubbed; pure-Go static-provider path runs unchanged).
- `pkg/agent` builds for `js/wasm` after fixing `!windows` build tags that
  unintentionally included `js`.
- A first slice of the embedding/memory API is exposed to JS via
  `cmd/wasm/embedding_funcs.go` (see Phase 1 below).

The remaining work is tractable but substantial — by my read, several days
of focused implementation plus design decisions on a few sub-items.

## 2. Goal & Non-Goals

**Goal**: every sprout feature that's logically expressible in a browser
sandbox should be reachable from the WASM build via the `SproutWasm` JS
global, with parity-quality semantics (static-embedding only is acceptable
where ONNX isn't available).

**Non-goals**:
- Replacing the native CLI for production use cases. WASM is a
  browser-side complement, not a substitute.
- Shipping things that fundamentally don't work in a browser sandbox
  (MCP subprocess servers, system keychain, terminal pty wrappers,
  external binary exec).
- LLM-quality parity. Native sprout will have access to local models via
  ONNX Runtime and onnxruntime-genai; WASM will have at best
  onnxruntime-web (slower, more constrained).

## 3. Tier Map

Tiers reflect implementation difficulty and dependency on browser-side
infrastructure.

### Tier 1 — Pure-Go sprout features (in progress)

Pieces of sprout that are pure Go and *should* work in WASM, but currently
aren't reachable from the `SproutWasm` JS surface. The compile blockers
are gone; what's missing is wiring.

| Feature | Status |
|---|---|
| Static-provider semantic search | **Wired (this branch)** — `SproutWasm.searchSemantic`, `.buildSemanticIndex`, `.getSemanticStatus`, `.updateSemanticFile` |
| Memory CRUD + search | **Wired (this branch)** — `SproutWasm.listMemories`, `.readMemory`, `.saveMemory`, `.deleteMemory`, `.searchMemories` |
| Conversation turn persistence | Not yet wired. Same `ConversationStore` plumbing, needs a JS entry point. |
| Configuration management | Not yet wired. `pkg/configuration` builds; expose `getConfig`, `setConfigValue`. |
| Workspace analysis (file walk) | Pure Go; already used by buildSemanticIndex. Direct JS API would let UIs build trees independently. |
| History | Already exposed (`getHistory`); leave as-is. |

### Tier 2a — ONNX bridge via onnxruntime-web

Replace the CGO-only ONNX provider with a `syscall/js` bridge that delegates
inference to a browser-side `onnxruntime-web` session that the host page
provides. The browser already loads exactly this — see
`webui/src/services/onnxEmbeddingProvider.ts`.

Sketch:

- Host page registers `globalThis.__sproutONNX` = an object exposing
  `embed(text: string): Promise<Float32Array>` and
  `embedBatch(texts: string[]): Promise<Float32Array[]>`.
- WASM-side `ONNXEmbeddingProvider` stub detects the global, marshals
  calls via `syscall/js`, and shuttles bytes back as `[]float32`.
- The tokenizer stays pure-Go (`pkg/embedding/onnx_tokenizer.go` already
  builds for WASM and has byte-identical output to HF's reference).
- RRF merge, dual-store memory writes, proactive context — all unchanged.

Outcome: ONNX-quality semantic search in the browser without forking the
indexer.

### Tier 2b — Agent / LLM commands

Plumb `agent`, `question`, `code`, `commit`, `review`, `plan` through to
WASM. These already work over HTTP to LLM providers; in a browser they
work if CORS is permitted by the provider.

Open design questions before implementation can start:

1. **Where do API keys live?** Native sprout uses `~/.config/sprout/api_keys.json`
   or the OS keychain. In WASM we have neither. Candidates:
   - `localStorage` (simple, vulnerable to XSS)
   - `IndexedDB` + Web Crypto AES-GCM (better; needs a key derivation
     flow — passphrase? bound to origin?)
   - Per-session injection from the host page (`__sproutKeys.openai = "..."`)
     so the page owns persistence
2. **Streaming**: native uses HTTP/2 + `text/event-stream` parsing. The
   Fetch API supports streaming response bodies but the existing
   provider code expects `*http.Response`-style readers; check whether
   `js/wasm` net/http handles SSE end-to-end.
3. **CORS**: not every provider serves CORS. Anthropic and OpenAI both
   do for direct calls; some self-hosted endpoints don't. May need a
   user-supplied proxy URL setting.
4. **Tool execution**: agent loops can invoke shell tools. In WASM
   those would need to route through the existing
   `SproutWasm.executeCommand` JS bridge — fine, but it changes the
   tool-handler shape.

### Tier 3 — File extractors (no work needed)

**Original assumption**: `pkg/embedding/extractor_go.go`,
`extractor_py.go`, and `extractor_ts.go` use
`github.com/odvcencio/gotreesitter`, which I incorrectly assumed was a
CGO binding (C-tradition naming, project naming pattern). Plan was to
write a regex/line-based fallback for `js/wasm`.

**What's actually true**: gotreesitter is a **pure-Go** tree-sitter
implementation. The extractors compile and run identically under
`js/wasm`. The full `pkg/embedding` extractor test suite passes when
executed via `GOOS=js GOARCH=wasm go test -exec go_js_wasm_exec`:
`TestExtractGoFile`, `TestExtractPy*`, `TestExtractTS*`.

So there is no Tier 3 fallback to write. The original spec text would
have flagged a non-existent gap; this section is kept to document the
verification rather than removing it (so future readers can see that the
verification was actually done).

### Tier 4 — Browser-side replacements for native-only systems

Features that need a different transport entirely:

- **MCP servers** → a hosted MCP gateway over WebSocket (separate spec,
  out of scope here).
- **System keychain** → Web Crypto + IndexedDB envelope (designed in
  Tier 2b above).
- **Shell tool exec** → already routed through `SproutWasm.executeCommand`
  via Tier 2b.
- **External binary integrations** (`git`, `gh`, `gcloud`) → out of
  scope; users will be unable to invoke these from the WASM build.

## 4. Build matrix hygiene

Some packages still don't build for `js/wasm` because of unrelated
imports of unix-only or CGO-only dependencies. These don't currently
block `cmd/wasm` (which doesn't import them) but they make
`go build ./...` noisy in WASM mode.

- `pkg/webui/terminal_create.go`, `terminal_resize.go`, `terminal_types.go`
  unconditionally import `github.com/creack/pty`. Tag `!js`.
- Sweep the rest of `pkg/` for `//go:build !windows` patterns and
  replace with `unix && !js` per the `pkg/utils` and `pkg/console` fixes
  in this branch.

## 5. Distribution

The WASM binary today is 102MB (with embedding/agent pulled in). That's too
large for casual page loads. Concrete reductions in priority order:

- Build flags / `ldflags="-s -w"` strip (~25% saving).
- Use `tinygo` if feasible (huge saving, but compatibility risk with the
  Go standard library features `pkg/agent` uses — needs a spike).
- Split into two WASM modules: a small shell-only one for casual use,
  plus a larger `embedding.wasm` that lazy-loads when the user actually
  performs a semantic search.

## 6. Implementation Order

1. **Phase 1**: finish Tier 1 (config + conversation persistence wiring). **(done)**
2. **Phase 2**: Tier 3 — verified extractors already work on WASM via
   pure-Go gotreesitter. No code to write. **(done as verification only)**
3. **Phase 3**: Tier 2a (onnxruntime-web bridge) — quality parity.
4. **Phase 4**: Tier 2b (agent/LLM) — biggest user-facing feature jump
   but largest design surface; do after the foundation is solid.
5. **Phase 5**: Build matrix cleanup + distribution sizing.

## 7. Out-of-scope

- Cross-browser FS abstractions beyond the existing IndexedDB-backed
  MEMFS. If a feature needs OPFS or File System Access API for performance
  reasons, that's a separate spec.
- Service Worker integration for offline mode. Worth doing but is its
  own design question.
- Differential serving (.wasm.gz vs .wasm.br). Pure ops concern.
