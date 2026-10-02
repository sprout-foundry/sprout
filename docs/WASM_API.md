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

## Testing

Pure-Go helpers underneath the JS bridge have unit tests in
`cmd/wasm/*_test.go`. Because the package is gated to
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

The js.Value-bound entries (the actual
js.FuncOf wrappers, `asPromise`, `marshalJS`, `argString`/`argInt`/
`argFloat32`) need a full WASM-in-browser harness to test meaningfully
and are validated by the integration tests in the host page instead.

## Tier 1 — Workspace sync (browser-primary model)

Spec: `roadmap/SP-046-workspace-sync-model.md`.

The sync transport itself (WebSocket, container lifecycle) lives in the
sprout-foundry platform. The WASM build only exposes the agent-side hooks
the transport plugs into. **None of these need to be called for free-tier
WASM to work** — the agent operates as a single replica when no sync is
configured.

### `setSyncEndpoint(url): {ok: true, url: string}`

Records the WebSocket URL the host page wants the sync transport to use.
Free-tier WASM never calls this; paid-tier calls it once at session boot.

### `getSyncEndpoint(): string`

Returns the currently-configured endpoint (or `""` if unset).

### `applyFileMetadata(path, metadataJSON): {ok: true, path: string}`

The platform's WS layer calls this when it learns about a new
`WorkspaceFileMetadata` for a file (e.g. browser-side sequence bumps after
the user types). The Go-side agent consults this on every `write_file` to
detect unsynced browser edits and refuse with the
`ErrWriteHasUnsyncedEdits` sentinel — agent reasoning should ask the user
rather than retry.

```typescript
type WorkspaceFileMetadata = {
  browser_seq:            number;
  container_seq:          number;
  last_synced_browser:    number;
  last_synced_container:  number;
  modified_at:            string; // RFC3339
};

SproutWasm.applyFileMetadata('src/main.go', JSON.stringify({
  browser_seq: 7, container_seq: 3,
  last_synced_browser: 5, last_synced_container: 3,
  modified_at: new Date().toISOString(),
}));
```

### `onSessionMoved(handler): {ok: true}`

Registers a JS function that fires when the platform's WS layer notifies
this browser that the user took over the session on another device. The
host page typically renders a "session moved" overlay and disables UI
interactivity. Single-handler — calling again replaces.

### `sessionMoved(): {ok: true} | {error: string}`

Platform-driven counterpart: the WS layer calls this when it receives the
server-side "moved" control message. Triggers the registered handler.

### `startHeartbeat(pingFn): {ok, interval_ms} | {already_running: true}`

Starts a 15s-interval ticker that invokes `pingFn` repeatedly. The
platform's container reaps long-running jobs after 60s of missed
heartbeats (SP-046 §4). Idempotent — calling while already running is a
no-op; `stopHeartbeat` first to swap the ping function.

### `stopHeartbeat(): {ok, was_running}`

Stops the ticker. Idempotent: safe to call when nothing is running.

## Free-tier degenerate mode

A page can load `sprout.wasm` and use the JS API surface without any of
the platform-side infrastructure:

- **No `setSyncEndpoint` / `applyFileMetadata`** — the agent's staleness
  rule still applies WITHIN a session (must read before write this turn),
  but the conflict rule's "unsynced browser edits" branch is never
  triggered. Single-replica semantics.
- **No `startHeartbeat`** — no container to reap; no-op.
- **No `onSessionMoved`** — no platform session; single-device by design.

So the minimum viable bring-up for a free-tier host page is:

```javascript
// Boot the WASM + MEMFS
go.run(wasmInstance.instance);
const err = SproutWasm.init();
if (err) throw new Error(err);

// Done. getConfig, setConfig, etc. work.
```

## Tier 2b — LLM proxy routing (foundation)

The sprout-foundry platform holds per-user encrypted API keys server-side
and proxies LLM requests so no keys ever touch the browser. The Go-WASM
side installs an HTTP `RoundTripper` (see `pkg/llmproxy/`) at init time
that rewrites direct calls to known LLM provider hosts to instead go
through the platform's `/api/proxy/llm/{provider}/*` path. Browser cookies
handle auth automatically.

### `setPlatformEndpoint(url): {ok: true, url: string}`

Configures the sprout-foundry platform base URL (e.g.
`"https://platform.sprout-foundry.com"`). Once set, every LLM API call
from the Go-WASM agent path is rewritten to route through the platform.
Pass `""` to disable rewriting (returns to direct provider calls — only
useful for tests or air-gapped configurations).

### `getPlatformEndpoint(): string`

Returns the currently-configured platform endpoint, or `""` when unset.

### Recognized providers

| Origin URL                                       | Rewritten to                                                          |
|--------------------------------------------------|-----------------------------------------------------------------------|
| `https://api.openai.com/...`                     | `{platform}/api/proxy/llm/openai/...`                                 |
| `https://api.anthropic.com/...`                  | `{platform}/api/proxy/llm/anthropic/...`                              |
| `https://openrouter.ai/api/...`                  | `{platform}/api/proxy/llm/openrouter/...` (strips the leading `/api`) |
| `https://api.deepinfra.com/...`                  | `{platform}/api/proxy/llm/deepinfra/...`                              |
| `https://api.mistral.ai/...`                     | `{platform}/api/proxy/llm/mistral/...`                                |
| `https://api.cerebras.ai/...`                    | `{platform}/api/proxy/llm/cerebras/...`                               |
| `https://api.groq.com/...`                       | `{platform}/api/proxy/llm/groq/...`                                   |
| `https://api.together.xyz/...`                   | `{platform}/api/proxy/llm/together/...`                               |

Unknown providers pass through unchanged. To add a provider, extend
`knownProviders` in `pkg/llmproxy/providers.go`; the test suite already
checks every registered entry round-trips correctly.

### `runChat(provider, model, messagesJSON, options?, onChunk?): Promise<ChatResult>`

A single-shot chat completion call. The minimal building block on top of
the proxy: validates the JS → Go → llmproxy → platform → upstream →
response path actually works end-to-end without dragging in the full
agent loop, tool execution, MCP, etc. Useful for "ping the provider" UX
in the host page and for smoke-testing the platform proxy.

| Arg | Type | Required | Description |
|---|---|---|---|
| `provider` | `string` | yes | One of the keys from `pkg/llmproxy/providers.go` (e.g. `"openai"`, `"anthropic"`, `"openrouter"`). |
| `model` | `string` | yes | Provider-specific model id. Pass `""` to let the provider client pick its default. |
| `messagesJSON` | `string` | yes | JSON-encoded `[]agent_api.Message` — must be non-empty. |
| `options` | `object` | optional | `{reasoning?: string, disableThinking?: bool, vision?: bool}`. Reasoning maps to provider-specific reasoning levels ("low"/"medium"/"high" depending on provider). |
| `onChunk` | `(content, contentType) => void` | optional | Streaming callback. When provided, runs through `SendChatRequestStream` and invokes the callback per chunk. Omitted → non-streaming. |

```javascript
const result = await SproutWasm.runChat(
  'openai', 'gpt-5',
  JSON.stringify([
    {role: 'system', content: 'You are concise.'},
    {role: 'user',   content: 'Reply with one word: ack'},
  ]),
  {reasoning: ''},
  (chunk, type) => console.log('[stream]', type, chunk),
);
// result === {
//   content: 'ack',
//   reasoning_content: '',
//   finish_reason: 'stop',
//   provider: 'openai',
//   model: 'gpt-5',
//   prompt_tokens: 24,
//   completion_tokens: 1,
//   total_tokens: 25,
// }
```

The streaming callback is invoked synchronously from the Go goroutine
that owns the stream, so don't perform heavy work in it — defer to a
microtask if you need to update React/Vue state.

Vision: pass `options.vision = true` to route through `SendVisionRequest`
instead. Vision streaming isn't part of the ClientInterface today, so the
implementation falls back to non-streaming and delivers the final
response to `onChunk` as a single chunk when both `vision: true` and
`onChunk` are set.

### `runAgent(provider, model, query, onEvent?): Promise<AgentResult>`

The full sprout agent loop. Where `runChat` is one HTTP request to a
provider, `runAgent` constructs an `agent.Agent` and runs `ProcessQuery`
against it — multi-turn conversation, system prompt, persona, tool
calling, etc. The same loop native `sprout agent` drives.

| Arg | Type | Required | Description |
|---|---|---|---|
| `provider` | `string` | yes | Provider name (same keys as `runChat`). |
| `model` | `string` | yes | Model id, or `""` for the provider's default. |
| `query` | `string` | yes | User prompt for the agent. |
| `onEvent` | `(jsonString) => void` | optional | Forwarded `events.UIEvent` payloads (one per published event). |

```javascript
const result = await SproutWasm.runAgent(
  'openai', 'gpt-5',
  'Read README.md and summarize it in 3 bullet points.',
  (eventJSON) => {
    const ev = JSON.parse(eventJSON);
    // ev.type is one of: query_started, tool_start, tool_end,
    // stream_chunk, agent_message, query_progress, error, ...
    console.log('[event]', ev.type, ev.data);
  },
);
// result === {response: '...', provider: 'openai', model: 'gpt-5'}
```

Events: every entry in `pkg/events/events.go:EventType*` may be emitted.
The shape is `{id: string, type: string, timestamp: string, data: any}`.
Forwarding happens from a worker goroutine; the JS callback is invoked
synchronously (Go-WASM doesn't have async JS calls), so heavy work
should be deferred to a microtask on the JS side.

Payload size contracts (see `pkg/events/events_filter.go`):
- `tool_start.data.arguments` is capped at `MaxToolEventArgsLength`
  (8 KB) with an `arguments_truncated: true` marker; the head of the
  string is preserved so `path` extraction still works.
- `tool_end.data.result` is capped at 2000 chars with
  `result_truncated: true` / `result_length`.
- `workspace_patch` carries `file_path`, `action`, `seq`, `size`
  (and `conflict` / `theirs_path` on conflict) — never file content.
  Fetch content via the file API.

Timeout: 10 minutes per call. Agent loops with many tool calls can
approach this — file an issue if it bites and we'll make it configurable.

**Tool execution under WASM is partially supported.** File-system tools
work via MEMFS; memory and conversation tools work as expected; shell
tools route through `pkg/wasmshell` and support its curated builtin set
(`ls`, `cat`, `cd`, `pwd`, `mkdir`, `rm`, `cp`, `mv`, `touch`, `echo`,
`head`, `tail`, `wc`, `grep`, `sort`, `find`, `tree`, `date`, `whoami`,
`env`, `which`, `type`, `history`); MCP and other tools that require
process-spawning are no-op or fail with an error event. The
`pkg/agent_tools.RegisterWASMShellExecutor` hook is wired in
`cmd/wasm/shell_executor.go:init` — overriding it from JS would let host
pages curate the command set further (e.g. for sandboxing).

**Provider/key custody**: `runAgent` does **not** require an API key on
the WASM side. The expected wiring is that `SproutWasm.setPlatformEndpoint`
has already been called, so the underlying HTTP requests route through
the sprout-foundry platform, which attaches the user's encrypted key
server-side. For local testing with a direct key, the configuration's
key store (via `SproutWasm.setAPIKey`) is consulted by the provider
client; through the proxy it's not needed.

### `runPlan(provider, model, query, onEvent?): Promise<AgentResult>`

Same shape as `runAgent`, but with the planning system prompt
(`pkg/agent.GetEmbeddedPlanningPrompt(true)`) installed before
`ProcessQuery` runs. Matches what native `sprout plan` does in
`createPlanningAgent`, minus the readline loop (which a browser doesn't
have).

```javascript
const result = await SproutWasm.runPlan(
  'openai', 'gpt-5',
  'Add OAuth login to the existing Express app',
  (eventJSON) => { /* … */ },
);
// result === {response, provider, model, mode: 'plan'}
```

Multi-turn planning is driven by the host page: call `runPlan` again
with the user's follow-up response, treating the exchange like a chat.
Each call constructs a fresh agent — turn-to-turn continuity across
calls lives in the conversation store
(`SproutWasm.getConversationHistory`), not the in-process agent state.

### What's not here yet (Tier 2b continued)

- `runQuestion` / `runCode`: deliberately not added. Both are just
  `runAgent` with a particular query phrasing — host pages get the same
  behavior by prepending their own framing string. No native CLI
  counterpart to mirror.
- `runCommit` / `runReview`: blocked on in-WASM git. The native
  `sprout commit` and `sprout review` commands shell out to the `git`
  binary; under WASM there's no git binary and no `pkg/wasmgit` yet. The
  sprout-foundry platform side will eventually run these on the
  container, so paid-tier users get them via the WS sync transport
  rather than in-browser. Free-tier users hit this limitation today —
  documented but unimplemented.
- Shell-tool wiring done (SP-045-4e): shell commands route through
  `pkg/wasmshell`. What's still missing is MCP — process-spawning tools
  fail under WASM by design.
- SSE streaming through Go-WASM `net/http`: `runChat` exercises the
  streaming path under WASM, but each provider's SSE format will still
  need verification when the host page targets it for real.
- API-key UX (in the platform side, not this repo): user-self-serve
  rotation, multi-provider key management, audit log of per-request key
  attachment. See SP-045-4a in the roadmap for the design decisions.
