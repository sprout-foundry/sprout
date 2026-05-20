# `SproutWasm` JS API

The WASM build (`cmd/wasm`) exposes a `SproutWasm` global object on
`window` (or `globalThis`) that host pages call into. Every entry below is
described in three parts: signature, what it actually does, and which
SP-045 tier it belongs to.

All functions that touch disk or run inference return Promises. Errors
surface as rejected promises whose value is a plain string. Synchronous
functions (currently only the shell helpers in Tier 0) return native JS
values directly.

> **Build state**: this surface is the WASM build of sprout. The native
> `sprout` binary has dozens more features (agent loops, MCP, etc.) that
> Tier 2b will eventually wire into the WASM build — see
> `roadmap/SP-045-wasm-feature-parity.md` for the phased plan.

## Initialization

The host page must instantiate the WASM module and call `init` before any
other entry returns useful results. A minimal bootstrap:

```javascript
const go = new Go();
const wasmInstance = await WebAssembly.instantiateStreaming(
  fetch("/sprout.wasm"),
  go.importObject,
);
go.run(wasmInstance.instance);              // starts the runtime
// SproutWasm is now defined on globalThis.
const err = SproutWasm.init();              // returns "" on success
if (err) throw new Error(err);
```

`init()` is currently the only synchronous entry — it wires the
IndexedDB-backed MEMFS that everything else depends on.

## Tier 0 — Shell

These were present before SP-045 and are unchanged.

| Function | Args | Returns | Notes |
|---|---|---|---|
| `init()` | — | string (empty on success) | Synchronous. Must be called first. |
| `executeCommand(line)` | `string` | object `{stdout, stderr, code}` | Runs one shell line through the wasmshell parser. |
| `autoComplete(prefix)` | `string` | string[] | Tab-completion candidates. |
| `getCwd()` | — | string | Current working directory. |
| `changeDir(path)` | `string` | string (error or empty) | Synchronous chdir. |
| `writeFile(path, data)` | `string, string` | string (error or empty) | Writes to MEMFS + IndexedDB. |
| `readFile(path)` | `string` | string | Returns file contents. |
| `listDir(path)` | `string` | string[] | One entry per file/dir. |
| `deleteFile(path)` | `string` | string (error or empty) | Removes from MEMFS + IndexedDB. |
| `getHistory()` | — | string[] | Shell history entries. |
| `getEnv(name)` | `string` | string | Returns env var value. |

## Tier 1 — Semantic search

Static-provider only on WASM today; quality matches what native sprout
sees when ONNX isn't installed. Tier 2a will lift this to ONNX-quality
via the onnxruntime-web bridge.

### `buildSemanticIndex(): Promise<BuildStats>`

Walks the current working directory and builds the static embedding
index. The first call from a session does a full rebuild; subsequent
calls are incremental (only re-embeds changed files via content hashes).

```typescript
type BuildStats = {
  filesProcessed: number;
  unitsExtracted: number;
  unitsEmbedded:  number;
  durationMs:     number;
};
```

### `getSemanticStatus(): Promise<Status>`

Cheap status read for UI affordances.

```typescript
type Status = {
  initialized: boolean;
  building:    boolean;
  indexSize:   number; // number of records currently in the vector store
};
```

### `searchSemantic(query, topK?, threshold?): Promise<Result[]>`

Returns the top-K matches above `threshold` (0.0 by default; reasonable
values are 0.5-0.85). The shape mirrors `embedding.QueryResult` minus the
embedding vector (which we strip — it's large and useless to the
browser).

```typescript
type Result = {
  id:         string;  // "<file>:<symbol>" or "memory:<name>"
  file:       string;
  name:       string;
  type:       string;  // "code_unit" | "file" | "conversation_turn" | "memory"
  signature:  string;
  startLine:  number;
  endLine:    number;
  similarity: number;  // cosine similarity, 0..1
};
```

### `updateSemanticFile(filePath): Promise<{ok: boolean}>`

Incrementally re-indexes a single file (after the host knows it's
changed). Drops the file's old records and embeds the new content.

## Tier 1 — Memory CRUD

Memories are markdown files in `~/.config/sprout/memories/` (on the
MEMFS-backed home dir). Each is stored alongside an embedding so semantic
search across them just works.

### `listMemories(): Promise<Memory[]>`

```typescript
type Memory = {
  name:    string; // filename without `.md`
  path:    string; // absolute MEMFS path
  content: string;
};
```

### `readMemory(name): Promise<{name, content}>`

Returns one memory by name. Rejects if missing.

### `saveMemory(name, content): Promise<{ok, name}>`

Writes (creates or replaces) and embeds. Names containing `/`, `\`, or
just being `.`/`..` are rejected to prevent directory traversal.

### `deleteMemory(name): Promise<{ok: true}>`

Removes the markdown file and its embedding from the conversation store.
Idempotent — deleting a missing memory still resolves with `ok: true`.

### `searchMemories(query, topK?, threshold?): Promise<MemoryResult[]>`

Semantic search restricted to records with `Type == "memory"`.

```typescript
type MemoryResult = {
  name:       string;
  similarity: number;
  preview:    string; // first 200 chars of the memory content
};
```

## Tier 1 — Configuration

Reads/writes through `pkg/configuration`. The on-disk file is
`~/.config/sprout/config.json`, persisted to IndexedDB via MEMFS.

### `getConfig(): Promise<Config>`

Returns the full sprout Config object as a plain JS object. Shape is too
large to enumerate here — refer to `pkg/configuration/config.go:Config`.

### `setConfig(jsonString): Promise<{ok: true}>`

Replaces the on-disk config with the JSON-string argument. The cached
config is invalidated so the next `getConfig` re-reads fresh.

### `getConfigPath(): Promise<{path: string}>`

Returns the absolute path of the config file (for UIs that want to show
the user where settings live).

### `resetConfig(): Promise<{ok: true}>`

Replaces the on-disk config with `NewConfig()` defaults.

### `getAPIKeys(): Promise<Record<string, boolean>>`

Returns a map of `provider → true` for every provider that has a key
configured. **Plaintext keys are deliberately not exposed back to JS** —
UIs that need to display "you have an X key configured" pattern use this;
UIs that need to update the actual value use `setAPIKey`.

### `setAPIKey(provider, key): Promise<{ok, provider}>`

Stores one API key. Note that on WASM these end up in IndexedDB — see
Tier 2b in SP-045 for the Web Crypto envelope design that will replace
this before agent commands ship.

### `removeAPIKey(provider): Promise<{ok, provider}>`

Removes one API key.

## Tier 1 — Conversation persistence

Builds on the same `ConversationStore` the native sprout uses for SP-027.
Turns live in the same JSONL file as memories but filtered by
`Type == "conversation_turn"`.

### `getConversationHistory(sessionId?): Promise<Turn[]>`

Returns every stored turn, optionally filtered to a specific session.
Without the arg, returns all turns. Embeddings are stripped before
returning.

```typescript
type Turn = {
  id:                string;
  userPrompt:        string;
  indexedAt:         string;  // RFC3339 nanosecond timestamp
  sessionId?:        string;
  turnNumber?:       number;
  workingDir?:       string;
  duration?:         number;
  tokenUsage?:       number;
  actionableSummary?: string;
  filesTouched?:     string[];
  deleted?:          boolean;
};
```

### `saveConversationTurn(jsonString): Promise<{ok, id}>`

Persists a turn. The JSON shape matches `agent.ConversationTurn` (see
`pkg/agent/conversation_turn.go`). Missing `id` and `timestamp` are
generated. Embedding and ONNX dual-write happen via the same path as
native sprout (`EmbedAndStoreTurn`).

### `searchConversations(query, topK?, threshold?, sessionId?): Promise<Turn[]>`

Semantic search across stored turns. Optional `sessionId` restricts to a
single session.

### `deleteConversationTurn(id): Promise<{ok, deleted}>`

Marks a turn as deleted. Note: the underlying `ConversationStore`
currently doesn't support hard-delete (`SP-045-1e` follow-up), so this
sets `metadata.deleted = true` and zeroes the embedding so the turn no
longer matches semantic queries. The record stays present in
`getConversationHistory` results with `deleted: true` so UIs can
hide it.

## Testing

Pure-Go helpers underneath the JS bridge have unit tests in
`cmd/wasm/wasm_funcs_test.go`. Because the package is gated to
`//go:build js && wasm`, running them requires the bundled exec helper:

```bash
GOROOT=$(go env GOROOT)
cp "${GOROOT}/lib/wasm/go_js_wasm_exec" /tmp/
cp "${GOROOT}/lib/wasm/wasm_exec_node.js" /tmp/
cp "${GOROOT}/lib/wasm/wasm_exec.js"      /tmp/
chmod +x /tmp/go_js_wasm_exec

GOOS=js GOARCH=wasm go test \
  -exec "/tmp/go_js_wasm_exec" \
  -count=1 -timeout=60s \
  ./cmd/wasm/
```

Tests cover: memory-name sanitization (path-traversal rejection),
`indexOfID`, `turnRecordToJS` (embedding strip + metadata propagation +
nil safety + deleted flag). The js.Value-bound entries (the actual
js.FuncOf wrappers, `asPromise`, `marshalJS`, `argString`/`argInt`/
`argFloat32`) need a full WASM-in-browser harness to test meaningfully
and are validated by the integration tests in the host page instead.

## What's not here yet

Listed in priority order from `roadmap/SP-045-wasm-feature-parity.md`:

- **Tier 2a**: ONNX-quality embeddings via an `onnxruntime-web` bridge
  (host page registers `globalThis.__sproutONNX`; the WASM stub forwards
  inference calls). Tokenizer + indexer stay pure Go.
- **Tier 2b**: Agent / LLM commands (`runAgent`, `runQuestion`, `runCode`,
  `runCommit`, `runReview`, `runPlan`). Blocked on the API-key storage
  design decision (SP-045-4a).
- **Tier 3**: Pure-Go file-extractor fallback so `buildSemanticIndex`
  produces meaningful results on arbitrary workspaces (today it depends
  on tree-sitter CGO, which doesn't ship in WASM).
