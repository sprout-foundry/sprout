# Conformance Test Spec Format

JSON schema and assertion reference for writing conformance test specs against `seed-cli`.

## Spec File Layout

Each spec is a JSON file under `conformance/specs/<category>/<name>.json`.

```json
{
  "name": "my_test",
  "description": "What this test validates",
  "actions": [ ... ],
  "assertions": [ ... ]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique test identifier (snake_case) |
| `description` | string | yes | Human-readable explanation |
| `actions` | array | yes | Ordered JSON-RPC actions sent to CLI stdin |
| `assertions` | array | yes | Conditions evaluated after all actions complete |

---

## Actions

One JSON-RPC request per line on stdin:

```json
{ "id": 1, "method": "agent.new", "params": { "maxIterations": 3 } }
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | int | yes | Unique request ID. Responses carry the same `id` for correlation |
| `method` | string | yes | CLI method (e.g. `agent.new`, `mock.addTextResponse`) |
| `params` | object | no | Method parameters. Defaults to `{}` |
| `wait` | int | no | Wait for response `id` before sending the next action |

**Auto-wait**: The runner automatically waits for a response after `agent.run` or
`agent.runStream` before sending the next action (up to 15 s). Use `wait` to override
which ID to wait for.

---

## Assertion Types

### `noError` — response has no error

```json
{ "type": "noError", "id": 3 }
```

### `response` — check result fields (or expected error)

```json
{ "type": "response", "id": 3, "result": { "result": "Hello world!" } }
```

To assert an expected error code/message:

```json
{ "type": "response", "id": 4, "error": { "code": -32601, "message": "unknown method" } }
```

### `error` — assert an error was returned

```json
{ "type": "error", "id": 4, "errorType": "max_iterations" }
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | int | yes | Response ID to check |
| `errorType` | string | no | Predefined category (see Error Types) |
| `contains` | string | no | Substring that must appear in the error message |

### `event` — at least one event of the given type

```json
{ "type": "event", "eventType": "query_started" }
```

| Field | Type | Required | Description |
|---|---|---|---|
| `eventType` | string | yes | Event name to match |
| `contains` | string | no | Substring to search recursively in event `data` |
| `count` | int | no | Exact number of matching events expected |

### `events` — exactly N events of the given type

```json
{ "type": "events", "eventType": "text_chunk", "count": 5 }
```

| Field | Type | Required | Description |
|---|---|---|---|
| `eventType` | string | yes | Event name to match |
| `count` | int | yes | Exact count expected |

### `state` — query CLI state and assert a value

```json
{ "type": "state", "id": 0, "path": "tokens", "greaterThan": 0 }
```

### `mock` — query mock state and assert a value

```json
{ "type": "mock", "id": 0, "path": "executorCallCount", "equals": 1 }
```

Both `state` and `mock` share these fields:

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | int | no | Response ID (see Injection below). Default `0` |
| `path` | string | yes | Dot-separated path or special key |
| `equals` | any | no | Exact value match |
| `contains` | string | no | Substring match on the value |
| `notEmpty` | bool | no | Assert the path resolves to a non-nil value |
| `greaterThan` | number | no | Assert the numeric value is strictly greater |

---

## Mock / State Injection (id = 0)

When a `mock` or `state` assertion has `id: 0` (the default), the runner
automatically injects a query action **after** the `agent.run` response arrives.
Injected IDs start at `9001` to avoid collisions.

| `path` | Injected method |
|---|---|
| `callCount` | `mock.callCount` |
| `executorCallCount` | `mock.executorCallCount` |
| `tokens` | `state.tokens` |
| `cost` | `state.cost` |
| `len` | `state.len` |
| `sessionId` | `state.sessionId` |
| `messages` | `state.messages` |
| `checkpoints` | `agent.checkpoints` |

---

## Error Types

The `errorType` field maps to substrings that must appear in the error message.

| errorType | Matches |
|---|---|
| `no_provider` | `no provider configured` |
| `no_executor` | `no tool executor configured` |
| `interrupted` | `interrupted` |
| `max_iterations` | `maximum iterations exceeded` |
| `paused` | `agent is paused` |
| `zero_choices` | `zero choices` |
| `transient` | `transient error` |
| `rate_limit` | `rate limit`, `too many requests` |
| `context_overflow` | `context window exceeded` |
| `auth` | `authentication failed` |
| `client` | `client error` |
| `content_filtered` | `content filter`, `content_filtered`, `content policy` |
| `blank_response` | `blank`, `repetitive` |

---

## Mock Response Setup

### `mock.addTextResponse` — queue a plain text response

```json
{ "id": 2, "method": "mock.addTextResponse", "params": { "content": "Hello!" } }
```

### `mock.addTool` — register a tool definition

```json
{
  "id": 2,
  "method": "mock.addTool",
  "params": {
    "name": "weather",
    "description": "Get weather for a location",
    "parameters": { "type": "object", "properties": { "location": { "type": "string" } } }
  }
}
```

### `mock.addToolCallResponse` — queue tool calls from the provider

Two formats are accepted:

**Flat** (most specs):

```json
{
  "id": 3,
  "method": "mock.addToolCallResponse",
  "params": {
    "calls": [
      { "id": "call_1", "name": "weather", "arguments": { "location": "NYC" } }
    ]
  }
}
```

**Nested** (OpenAI-compatible):

```json
{
  "id": 3,
  "method": "mock.addToolCallResponse",
  "params": {
    "toolCalls": [
      {
        "id": "call_1",
        "type": "function",
        "function": { "name": "weather", "arguments": "{\"location\":\"NYC\"}" }
      }
    ]
  }
}
```

| Format | Field | `arguments` type |
|---|---|---|
| Flat (`calls`) | object (parsed JSON) |
| Nested (`toolCalls`) | string (JSON-encoded) |

### `mock.addToolResult` — provide a result for a tool call

```json
{ "id": 4, "method": "mock.addToolResult", "params": { "toolCallId": "call_1", "content": "Sunny, 72\u00b0F" } }
```

---

## Runner Timing

- **Spec timeout**: 30 s
- **Auto-wait after `agent.run` / `agent.runStream`**: up to 15 s
- **Between actions**: 10 ms delay
- **Stdout read deadline**: 10 s after stdin close

---

## Complete Example

```json
{
  "name": "single_tool_call",
  "description": "Tool call -> execute -> loop continues to final text response",
  "actions": [
    { "id": 1, "method": "agent.new", "params": {} },
    {
      "id": 2,
      "method": "mock.addTool",
      "params": {
        "name": "weather",
        "description": "Get weather for a location",
        "parameters": { "type": "object", "properties": { "location": { "type": "string" } } }
      }
    },
    {
      "id": 3,
      "method": "mock.addToolCallResponse",
      "params": { "calls": [{ "id": "call_1", "name": "weather", "arguments": { "location": "NYC" } }] }
    },
    { "id": 4, "method": "mock.addToolResult", "params": { "toolCallId": "call_1", "content": "Sunny, 72\u00b0F" } },
    { "id": 5, "method": "mock.addTextResponse", "params": { "content": "The weather in NYC is sunny and 72\u00b0F." } },
    { "id": 6, "method": "agent.run", "params": { "query": "What's the weather in NYC?" } }
  ],
  "assertions": [
    { "type": "noError", "id": 1 },
    { "type": "noError", "id": 6 },
    { "type": "response", "id": 6, "result": { "result": "The weather in NYC is sunny and 72\u00b0F." } },
    { "type": "mock", "id": 0, "path": "executorCallCount", "equals": 1 }
  ]
}
```
