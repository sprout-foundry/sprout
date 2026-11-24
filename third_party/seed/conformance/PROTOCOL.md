# seed-cli JSON-RPC Protocol Reference

Language-agnostic protocol for the `seed-cli` binary.  Every language port
(`seed-go`, `seed-js`, `seed-rust`, …) must implement a CLI that speaks this
protocol so the shared conformance test suite can verify equivalence.

---

## Overview

The CLI is a **thin process wrapper** around the `core` conversation engine.
It reads JSON-RPC requests from **stdin**, writes JSON-RPC responses and async
events to **stdout**, and exits when stdin closes.

Each conformance test spawns a fresh process — no state leaks between tests.

```
→ {"id":1,"method":"agent.new","params":{"systemPrompt":"You are helpful."}}
← {"id":1,"result":{"ok":true}}
← {"event":"query_started","data":{"query":"Hi"}}
← {"id":2,"result":{"result":{"content":"Hello!","finishReason":"stop"}}}
← {"event":"query_completed","data":{"query":"Hi","response":"Hello!"}}
```

---

## Wire Format

Every line on stdin and stdout is a single JSON object terminated by `\n`.
Line length is not bounded by the protocol, but implementors should handle
lines up to 1 MiB.

### Request (stdin)

```json
{"id": N, "method": "method.name", "params": {"key": "value"}}
```

| Field    | Type   | Required | Description                          |
|----------|--------|----------|--------------------------------------|
| `id`     | number | yes      | Request ID echoed in the response    |
| `method` | string | yes      | Method name (see method table below) |
| `params` | object | yes      | Method-specific parameters           |

`params` is always an object, even when empty.

### Response (stdout)

**Success:**

```json
{"id": N, "result": {"key": "value"}, "error": null}
```

**Error:**

```json
{"id": N, "result": null, "error": {"code": -32601, "message": "method not found: foo"}}
```

| Field    | Type   | Description                              |
|----------|--------|------------------------------------------|
| `id`     | number | Echoes the request ID                    |
| `result` | object | Method result (null on error)            |
| `error`  | object | Error details (null on success)          |
|          |        | `error.code`: integer error code         |
|          |        | `error.message`: human-readable string   |

### Event (stdout)

Async events published by the engine are written as separate lines **between**
requests and responses.  Events carry no `id`:

```json
{"event": "event_type", "data": {"key": "value"}}
```

| Field   | Type   | Description                              |
|---------|--------|------------------------------------------|
| `event` | string | Event type name (e.g. `query_started`)   |
| `data`  | object | Event payload (always an object)         |

Events are emitted in the order they are published.  The runner must collect
all events between a request and its response.

---

## JSON-RPC Error Codes

| Code   | Meaning                                    |
|--------|--------------------------------------------|
| -32700 | Parse error (invalid JSON)                 |
| -32600 | Invalid request (missing `method`)         |
| -32601 | Method not found                           |
| -32602 | Invalid parameters                         |
| -32603 | Internal error (agent not created, panic)  |

---

## Context Cancellation

Two mechanisms exist to cancel a running query:

1. **`agent.interrupt`** — Send the method; the running `Run` or `RunStream`
   returns `ErrInterrupted`.  Subsequent calls to `agent.resetInterrupt`
   clear the flag.

2. **Closing stdin** — The CLI treats stdin closure as a cancellation signal
   and cancels the context of any in-flight query.  For `agent.runStream` the
   CLI uses a per-call context with a **30-second grace period** after stdin
   closes.

---

## Method Table

Methods are grouped by namespace.  Every method returns `{"ok": true}` unless
noted otherwise.

### Agent Lifecycle

| Method             | Params                                       | Result                                         |
|--------------------|----------------------------------------------|------------------------------------------------|
| `agent.new`        | `systemPrompt`, `maxIterations`, `maxTokens`, `disableFallbackParser`, `disableValidator`, `disableNormalizer`, `compactionTriggerFraction`, `initialMessages`, `initialCheckpoints`, `retryConfig`, `optimizer`, `debug` | `{"ok": true}` |
| `agent.run`        | `query` (string)                             | `{"result": {content, finishReason, ...}}` or error |
| `agent.runStream`  | `query` (string)                             | `{"result": {content, finishReason, ...}}` or error. Chunks emitted as `stream_chunk` events. |
| `agent.interrupt`  | —                                            | `{"ok": true}`                                  |
| `agent.resetInterrupt` | —                                       | `{"ok": true, "context": true\|false}`          |
| `agent.pause`      | —                                            | `{"ok": true}`                                  |
| `agent.resume`     | —                                            | `{"ok": true}`                                  |
| `agent.isPaused`   | —                                            | `{"paused": true\|false}`                       |
| `agent.exportState`| —                                            | `{"state": "<json string>"}`                    |
| `agent.importState`| `state` (string — JSON)                      | `{"ok": true}`                                  |
| `agent.setSystemPrompt` | `prompt` (string)                    | `{"ok": true}`                                  |
| `agent.checkpoints`| —                                            | `{"checkpoints": [...]}`                        |
| `agent.state`      | —                                            | `{"messageCount": N, "sessionID": "...", "totalTokens": N, "totalCost": N}` |

### State Management

| Method               | Params                              | Result                                          |
|----------------------|-------------------------------------|-------------------------------------------------|
| `state.messages`     | —                                   | `{"messages": [...], "len": N}`                  |
| `state.sessionId`    | —                                   | `{"sessionId": "..."}`                           |
| `state.setSessionId` | `sessionId` (string)                | `{"ok": true}`                                   |
| `state.ensureSessionId` | —                                | `{"ok": true, "sessionId": "..."}`               |
| `state.tokens`       | —                                   | `{"tokens": N}`                                  |
| `state.cost`         | —                                   | `{"cost": N}`                                    |
| `state.addMessage`   | `role` (string), `content` (string) | `{"ok": true}`                                   |
| `state.len`          | —                                   | `{"len": N}`                                     |
| `state.clearCheckpoints` | —                              | `{"ok": true}`                                   |

### Configuration

| Method               | Params                               | Result       |
|----------------------|--------------------------------------|--------------|
| `agent.setProvider`  | `model` (string), `contextSize` (int), `hasVision` (bool) | `{"ok": true}` |
| `agent.setFlushCallback` | —                              | `{"ok": true}` |

### Steering & Injection

| Method             | Params                                | Result                     |
|--------------------|---------------------------------------|----------------------------|
| `agent.steer`      | `role` (string, default "system"), `content` (string) | `{"ok": true}` |
| `agent.steerSystem`| `content` (string)                    | `{"ok": true}`             |
| `agent.injectInput`| `input` (string)                      | `{"accepted": true\|false}` |

### Checkpoints / Provider / Streaming

| Method                 | Params                               | Result                                          |
|------------------------|--------------------------------------|-------------------------------------------------|
| `agent.providerInfo`   | —                                    | `{"model": "...", "contextSize": N, "hasVision": bool}` |
| `agent.estimateTokens` | `messages` (array)                   | `{"count": N}`                                   |
| `agent.streamingBuffer`| —                                    | `{"content": "..."}`                             |
| `agent.reasoningBuffer`| —                                    | `{"content": "..."}`                             |

### Output Manager

| Method               | Params                               | Result                                          |
|----------------------|--------------------------------------|-------------------------------------------------|
| `output.setMetadata` | `key` (string), `value` (string)     | `{"ok": true}`                                   |
| `output.getMetadata` | `key` (string)                       | `{"value": "..."}`                               |
| `output.flush`       | —                                    | `{"ok": true}`                                   |
| `output.reset`       | —                                    | `{"ok": true}`                                   |

### Mock Configuration

| Method                     | Params                                                                       | Result                                          |
|----------------------------|------------------------------------------------------------------------------|-------------------------------------------------|
| `mock.addTextResponse`     | `content` (string)                                                           | `{"ok": true}`                                   |
| `mock.addTextResponseWithFinish` | `content` (string), `finishReason` (string, default "stop")          | `{"ok": true}`                                   |
| `mock.addToolCallResponse` | `content` (string, optional), `toolCalls` or `calls` (array)                 | `{"ok": true}` — see tool call formats below    |
| `mock.addMalformedResponse`| `content` (string — tool calls embedded in plain text)                       | `{"ok": true}`                                   |
| `mock.addError`            | `message` (string), `errorType` (string, optional — see error types below)   | `{"ok": true}`                                   |
| `mock.addStreamChunks`     | `chunks` (array of strings or objects `{content}` or `{reasoning}`)          | `{"ok": true}` — enables streaming mode          |
| `mock.withInfo`            | `model` (string), `contextSize` (int), `hasVision` (bool)                   | `{"ok": true}`                                   |
| `mock.withTokenEstimate`   | `count` or `estimate` (number)                                               | `{"ok": true}`                                   |
| `mock.withStreaming`       | —                                                                            | `{"ok": true}`                                   |
| `mock.blockUntil`          | —                                                                            | `{"blockId": "1"}` — blocks next provider call   |
| `mock.blockOnCallN`        | `call` (number — 1-based call index)                                         | `{"ok": true}`                                    |
| `mock.release`             | —                                                                            | `{"ok": true}` — unblocks a pending call         |
| `mock.addTool`             | `name` (string), `description` (string), `parameters` (object)               | `{"ok": true}`                                   |
| `mock.addToolResult`       | `toolCallId` (string), `content` (string)                                    | `{"ok": true}`                                   |
| `mock.reset`               | —                                                                            | `{"ok": true}` — clears mock, agent, state       |
| `mock.callCount`           | —                                                                            | `{"count": N}`                                   |
| `mock.lastRequest`         | —                                                                            | `{"request": {model, messages, maxTokens, tools, ...}}` |
| `mock.executorCallCount`   | —                                                                            | `{"count": N}`                                   |
| `mock.executorLastCalls`   | —                                                                            | `{"calls": [{id, type, name, args}, ...]}`       |

---

### Tool Call Formats

`mock.addToolCallResponse` accepts two `toolCalls`/`calls` array formats:

**Nested format** (`toolCalls` key):

```json
{
  "toolCalls": [
    {"id": "call_1", "type": "function", "function": {"name": "search", "arguments": "{\"q\":\"test\"}"}}
  ]
}
```

**Flat format** (`calls` key):

```json
{
  "calls": [
    {"id": "call_1", "name": "search", "arguments": {"q": "test"}}
  ]
}
```

In the flat format, `arguments` is a JSON object that the CLI serializes
to a string.

---

## `agent.new` Options

| Field                        | Type     | Default                  | Description                                        |
|------------------------------|----------|--------------------------|----------------------------------------------------|
| `systemPrompt`               | string   | `DefaultSystemPrompt`    | System prompt text                                 |
| `maxIterations`              | int      | 0 (unlimited)            | Max conversation loop iterations                   |
| `maxTokens`                  | int      | 0 (provider default)     | Max output tokens per response                     |
| `disableFallbackParser`      | bool     | false                    | Disable fallback tool-call parser                  |
| `disableValidator`           | bool     | false                    | Disable response validator                         |
| `disableNormalizer`          | bool     | false                    | Disable tool call normalizer                       |
| `compactionTriggerFraction`  | float64  | 0.85                     | Fraction of context size that triggers compaction  |
| `initialMessages`            | array    | []                       | Seed messages: `[{role, content}]`                 |
| `initialCheckpoints`         | array    | []                       | Seed checkpoints: `[{start_index, end_index, user_message, summary, actionable_summary}]` |
| `retryConfig`                | object   | (defaults)               | `maxAttempts`, `initialDelay` (ms), `maxDelay` (ms), `multiplier`, `jitter` |
| `optimizer`                  | object   | null                     | `toolCategories`: map of tool name → `"file_read"` / `"shell_command"` |
| `debug`                      | bool     | false                    | Accepted but ignored (headless CLI)                |

`UI` is always wired to `NoopUI`.  `EventPublisher` is wired to the internal
event bus.  `OnIteration` and `OnCheckpoint` are wired to emit
`on_iteration` and `on_checkpoint` events.

---

## Error Types for `mock.addError`

The `errorType` parameter creates typed error instances that the agent
classifies during its retry loop:

| errorType          | Core Error Class          | Retry Behavior                         |
|--------------------|---------------------------|----------------------------------------|
| `transient`        | `TransientError`          | Retried with backoff                   |
| `rate_limit`       | `RateLimitError`          | Retried with backoff                   |
| `auth`             | `AuthError`               | Fails immediately                      |
| `context_overflow` | `ContextOverflowError`    | Triggers aggressive compaction, fails  |
| `client`           | `ClientError`             | Fails immediately                      |
| `content_filtered` | `ContentFilteredError`    | Retried once, then fails               |
| `blank`            | `BlankResponseError`      | Reminder injected, then fails          |

If `errorType` is omitted, a plain Go `error` is created (treated as
transient by the classifier).

---

## Error Response Types for Assertions

Conformance test assertions use these `errorType` values in the `error`
assertion to match against `err.Error()` messages and Go sentinel errors:

| errorType           | Sentinel / Check                               |
|---------------------|------------------------------------------------|
| `no_provider`       | `errors.Is(err, core.ErrNoProvider)`           |
| `no_executor`       | `errors.Is(err, core.ErrNoExecutor)`           |
| `interrupted`       | `errors.Is(err, core.ErrInterrupted)`          |
| `max_iterations`    | `errors.Is(err, core.ErrMaxIterations)`        |
| `paused`            | `errors.Is(err, core.ErrPaused)`               |
| `zero_choices`      | `errors.Is(err, core.ErrZeroChoices)`          |
| `transient`         | `core.IsTransient(err)`                        |
| `rate_limit`        | `core.IsRateLimit(err)`                        |
| `context_overflow`  | `core.IsContextOverflow(err)`                  |
| `auth`              | `core.IsAuthError(err)`                        |
| `client`            | `core.IsClientError(err)`                      |
| `content_filtered`  | `*core.ContentFilteredError`                   |
| `blank_response`    | `*core.BlankResponseError`                     |

---

## Streaming Protocol

For `agent.runStream`, the CLI:

1. Dispatches the call in a goroutine with a 30-second grace period context.
2. Emits `{"event": "stream_chunk", "data": {"content": "..."}}` (or
   `{"reasoning": "..."}`) for each chunk as the provider streams.
3. Writes the final `{"id": N, "result": {"content": "...", "finishReason": "stop"}}`
   response line when streaming completes.

The runner collects all `stream_chunk` events until it receives the response
with the matching `id`.

---

## Known Limitations

- **`agent.injectInput`** requires a concurrent action (the agent must be
  mid-loop to accept injection).  The sequential runner cannot reliably
  test this without blocking primitives (`mock.blockUntil` / `mock.release`).
  Tests that use injection must pair `injectInput` with a blocking mock call
  and `agent.runStream` to keep the dispatch loop responsive.

- **`mock.addStreamChunks`** objects can use `{content: "..."}` for content
  chunks or `{reasoning: "..."}` for reasoning chunks.  The CLI also enables
  streaming mode (`mock.withStreaming`) automatically when chunks are added.

- **`state.export` / `state.import`** do not preserve `Message.Meta`
  (it has `json:"-"` tag).  All language implementations must agree on
  this behavior — tests must not assert on Meta preservation.

- **`agent.setProvider`** is the only runtime-swappable component.
  `Executor` and `UI` can only be set at construction time via `agent.new`.

- **`output.*` methods** are stubs in the Go reference — they store
  metadata in the CLI's local state rather than accessing the core
  `OutputManager` directly.  Other language ports should implement the
  equivalent behavior.
