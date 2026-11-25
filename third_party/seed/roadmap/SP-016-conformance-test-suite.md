# SP-016: Conformance Test Suite

## Summary

Build a language-agnostic conformance test suite that exercises every public
behavior of the `seed` conversation engine through a CLI binary speaking
JSON-over-stdin/stdout. When `seed-js`, `seed-swift`, `seed-rust`, etc. are
created, each project ships a CLI that speaks the same protocol. Running the
shared test suite against each CLI proves functional equivalence.

---

## Motivation

The Go `seed` library is approaching steady state. It will serve as the
reference implementation for identical engines in other languages. We need a
mechanism to verify that every port behaves identically without coupling tests
to any language's internal structure.

---

## Architecture

### The CLI (`cmd/seed-cli`)

A thin binary wrapping the `core` and `events` packages. It exposes every
public method and observable behavior through a **newline-delimited JSON-RPC**
protocol over stdin/stdout:

```
→ {"id":1,"method":"agent.new","params":{"systemPrompt":"You are helpful."}}
← {"id":1,"result":{"ok":true}}

→ {"id":2,"method":"mock.addTextResponse","params":{"content":"Hello!"}}
← {"id":2,"result":{"ok":true}}

→ {"id":3,"method":"agent.run","params":{"query":"Hi"}}
← {"event":"query_started","data":{"query":"Hi","model":"mock-model"}}
← {"event":"metrics_update","data":{...}}
← {"id":3,"result":{"content":"Hello!"},"error":null}
← {"event":"query_completed","data":{"query":"Hi","response":"Hello!",...}}
```

Each test gets a **fresh process** — no state leaks between tests.

**Context cancellation protocol:** The test runner cancels a running query by
closing the CLI's stdin. The CLI interprets stdin close as a cancellation
signal and returns `ErrInterrupted` on the pending `agent.run`. Alternatively,
`agent.interrupt` achieves the same result without closing stdin.

**Streaming completion protocol:** For `agent.runStream`, the CLI emits
`stream_chunk` events as `{"event":...}` lines during streaming. The final
`{"id": N, "result": {...}}` response signals completion. The runner collects
events until it receives the response with the matching ID.

### Mock Provider & Executor

The CLI embeds a scriptable mock (equivalent to `internal/test.MockProvider` +
`MockExecutor`). The test driver configures mock behavior through CLI methods
*before* calling `agent.run`. This gives deterministic, fast, credential-free
tests that can simulate every error condition and edge case.

### Test Specs

Declarative JSON files describing setup, actions, and assertions. Each spec is
a standalone `.json` file:

```json
{
  "name": "basic text query returns response",
  "description": "Agent returns text content from provider",
  "setup": {
    "options": {"systemPrompt": "You are helpful."},
    "providerResponses": [{"type": "text", "content": "Hello!"}]
  },
  "actions": [
    {"method": "agent.run", "params": {"query": "Hi"}, "id": 1}
  ],
  "assertions": [
    {"type": "response", "id": 1, "result": {"content": "Hello!"}},
    {"type": "event", "eventType": "query_started"},
    {"type": "event", "eventType": "query_completed"},
    {"type": "mock", "path": "callCount", "equals": 1}
  ]
}
```

### Test Runner

A Go program (`conformance/runner`) that reads specs, spawns the CLI, sends
actions, collects events, evaluates assertions, and reports TAP output.

```bash
# Run against Go implementation
make conformance

# Run against any language's CLI
conformance/runner --cli ./seed-cli-js
```

---

## CLI Method Surface

Every method maps 1:1 to a `core` public API. Note: `Executor` and `UI` are
construction-time only — only `Provider` can be swapped at runtime via
`agent.setProvider`.

### Agent Lifecycle

| Method | Core API | Purpose |
|--------|----------|---------|
| `agent.new` | `NewAgent(Options)` | Create agent with specified options |
| `agent.run` | `Run(ctx, query)` | Synchronous query |
| `agent.runStream` | `RunStream(ctx, query)` | Streaming query |
| `agent.interrupt` | `Interrupt()` | Cancel running query |
| `agent.resetInterrupt` | `ResetInterrupt()` | Reset interrupt state |
| `agent.pause` | `Pause()` | Pause agent |
| `agent.resume` | `Resume()` | Resume agent |
| `agent.isPaused` | `IsPaused()` | Check paused state |

### `agent.new` Options

The `agent.new` method accepts all `core.Options` fields:

| Field | Type | Default | Purpose |
|-------|------|---------|---------|
| `systemPrompt` | string | `DefaultSystemPrompt` | System prompt |
| `maxIterations` | int | 0 (unlimited) | Max conversation loop iterations |
| `maxTokens` | int | 0 (provider default) | Max output tokens |
| `disableFallbackParser` | bool | false | Disable fallback tool-call parser |
| `disableValidator` | bool | false | Disable response validator |
| `disableNormalizer` | bool | false | Disable tool call normalizer |
| `initialMessages` | []Message | [] | Seed conversation history |
| `initialCheckpoints` | []TurnCheckpoint | [] | Seed checkpoint state |
| `compactionTriggerFraction` | float64 | 0.85 | Compaction trigger threshold |
| `retryConfig` | object | (defaults) | `maxAttempts`, `initialDelay`, `maxDelay`, `multiplier`, `jitter` |
| `optimizer` | object/null | null | `toolCategories` map (name → `"file_read"`/`"shell_command"`) |
| `debug` | bool | false | Enable debug logging |

The CLI auto-wires `UI` to `NoopUI` and `EventPublisher` to an internal
`EventBus` that forwards events to stdout. `OnIteration` and `OnCheckpoint`
callbacks are auto-wired to emit `on_iteration` and `on_checkpoint` events.
`LLMSummarizer` and `Pruner` are deferred — not configurable via the CLI in
this spec version.

### State Management

| Method | Core API | Purpose |
|--------|----------|---------|
| `state.export` | `ExportState()` | Serialize state to JSON |
| `state.import` | `ImportState(data)` | Restore state from JSON |
| `state.messages` | `State().Messages()` | Get message history |
| `state.sessionId` | `State().SessionID()` | Get session ID |
| `state.setSessionId` | `State().SetSessionID(id)` | Set session ID |
| `state.ensureSessionId` | `State().EnsureSessionID()` | Auto-generate session ID |
| `state.tokens` | `State().TotalTokens()` | Get total tokens |
| `state.cost` | `State().TotalCost()` | Get total cost |
| `state.addMessage` | `State().AddMessage(msg)` | Add message to state |
| `state.len` | `State().Len()` | Message count |
| `state.clearCheckpoints` | `State().ClearCheckpoints()` | Remove all checkpoints |

### Configuration

| Method | Core API | Purpose |
|--------|----------|---------|
| `agent.setSystemPrompt` | `SetSystemPrompt(prompt)` | Update system prompt |
| `agent.setProvider` | `SetProvider(p)` | Swap provider at runtime (only runtime-swappable component) |
| `agent.setFlushCallback` | `SetFlushCallback(fn)` | Register flush callback |

### Steering & Injection

| Method | Core API | Purpose |
|--------|----------|---------|
| `agent.steer` | `Steer(msg)` | Queue transient steering message |
| `agent.steerSystem` | `SteerSystem(content)` | Queue system steering message |
| `agent.injectInput` | `InjectInput(input)` | Inject mid-conversation input |

### Checkpoints

| Method | Core API | Purpose |
|--------|----------|---------|
| `agent.checkpoints` | `Checkpoints()` | Get all turn checkpoints |

### Provider Access

| Method | Core API | Purpose |
|--------|----------|---------|
| `agent.providerInfo` | `Provider().Info()` | Model metadata |
| `agent.estimateTokens` | `Provider().EstimateTokens(req)` | Token estimation |

### Streaming Buffers

| Method | Core API | Purpose |
|--------|----------|---------|
| `agent.streamingBuffer` | `StreamingBuffer()` | Content buffer state |
| `agent.reasoningBuffer` | `ReasoningBuffer()` | Reasoning buffer state |

### Output Manager

| Method | Core API | Purpose |
|--------|----------|---------|
| `output.setMetadata` | `OutputManager.SetEventMetadata(k,v)` | Set event metadata |
| `output.getMetadata` | `OutputManager.GetEventMetadata(k)` | Get event metadata |
| `output.flush` | `OutputManager.Flush()` | Flush output |
| `output.reset` | `OutputManager.Reset()` | Reset buffers and drain |

### Mock Configuration

| Method | Purpose |
|--------|---------|
| `mock.addTextResponse` | Queue text-only LLM response |
| `mock.addTextResponseWithFinish` | Queue response with custom finish reason |
| `mock.addToolCallResponse` | Queue response with structured tool calls |
| `mock.addMalformedResponse` | Queue response with tool calls embedded in content string |
| `mock.addError` | Queue error from provider |
| `mock.addStreamChunks` | Configure streaming chunks |
| `mock.withInfo` | Set provider metadata |
| `mock.withTokenEstimate` | Set token estimate |
| `mock.blockUntil` | Block next call until released (returns block ID) |
| `mock.blockOnCallN` | Block Nth call until released (returns block ID) |
| `mock.release` | Release a blocked call (accepts block ID) |
| `mock.addTool` | Register tool with executor (`{name, description, parameters}`) |
| `mock.addToolResult` | Queue executor result (`{toolCallId, content, error?}`) |
| `mock.reset` | Clear all mock state |
| `mock.callCount` | Provider call count |
| `mock.lastRequest` | Last ChatRequest |
| `mock.executorCallCount` | Executor call count |
| `mock.lastExecutorCalls` | Last tool calls received |

---

## Assertion Types

| Type | Fields | Purpose |
|------|--------|---------|
| `response` | `id`, `result`, `error` | Assert on method response |
| `event` | `eventType`, `contains`, `count` | Assert event was published |
| `events` | `eventType`, `count` | Assert N events of type |
| `state` | `path`, `equals`, `notEmpty`, `contains`, `greaterThan` | Assert on agent state |
| `mock` | `path`, `equals`, `notEmpty` | Assert on mock internals |
| `noError` | `id` | Assert no error in response |
| `error` | `id`, `errorType`, `contains` | Assert error with type and/or substring. `errorType` values: `no_provider`, `no_executor`, `interrupted`, `max_iterations`, `paused`, `zero_choices`, `transient`, `rate_limit`, `context_overflow`, `auth`, `client`, `content_filtered`, `blank_response` |

---

## Serialization Notes

- `Message.Meta` has `json:"-"` tag and is **not** preserved across `ExportState`/`ImportState`. All language implementations must agree on this behavior. The `state.export` → `state.import` test (04-01) must not assert on Meta preservation.

---

## Test Categories & Cases (103 tests)

### 00 — Smoke (1 test)

| ID | Test | Validates |
|----|------|-----------|
| 00-01 | `protocol_plumbing` | Send/receive JSON-RPC, basic `agent.new` + `agent.run` roundtrip |

### 01 — Agent Lifecycle (8 tests)

| ID | Test | Validates |
|----|------|-----------|
| 01-01 | `basic_text_query` | `Run()` returns provider text content |
| 01-02 | `agent_new_requires_provider` | `NewAgent` → `ErrNoProvider` |
| 01-03 | `agent_new_requires_executor` | `NewAgent` → `ErrNoExecutor` |
| 01-04 | `agent_pause_resume` | Pause → `ErrPaused`; Resume → works |
| 01-05 | `agent_interrupt` | `Interrupt()` → `ErrInterrupted` |
| 01-06 | `agent_run_stream` | `RunStream()` returns same content |
| 01-07 | `agent_reset_interrupt` | Reset → next `Run()` succeeds |
| 01-08 | `agent_multiple_queries` | Sequential runs accumulate state |

### 02 — Conversation Loop (12 tests)

| ID | Test | Validates |
|----|------|-----------|
| 02-01 | `single_tool_call` | Tool call → execute → loop continues |
| 02-02 | `multi_tool_call` | Multiple tool calls in one response |
| 02-03 | `tool_call_then_text` | Tool → result → text response |
| 02-04 | `max_iterations_exceeded` | `ErrMaxIterations` when limit reached |
| 02-05 | `context_cancellation` | Stdin close → `ErrInterrupted` |
| 02-06 | `finish_reason_length` | Continuation prompt on `length` |
| 02-07 | `finish_reason_content_filter` | Retry once, then `ContentFilteredError` |
| 02-08 | `finish_reason_stop` | Normal completion |
| 02-09 | `finish_reason_unknown` | Treated as incomplete |
| 02-10 | `zero_choices` | `ErrZeroChoices` |
| 02-11 | `blank_response_handling` | Reminder → `BlankResponseError` |
| 02-12 | `repetitive_response` | Detects repetition → `BlankResponseError` |

### 03 — Tool Execution (11 tests)

| ID | Test | Validates |
|----|------|-----------|
| 03-01 | `tool_register` | `Register()` → `GetTools()` returns it |
| 03-02 | `tool_unregister` | `Unregister()` removes tool |
| 03-03 | `tool_alias` | Aliases resolve to canonical name |
| 03-04 | `tool_result_ordering` | Results in original call order |
| 03-05 | `tool_timeout` | Execution times out |
| 03-06 | `tool_result_truncation` | Large results truncated |
| 03-07 | `circuit_breaker_opens` | N failures → opens → rejects |
| 03-08 | `circuit_breaker_half_open` | Timeout → probe → close on success |
| 03-09 | `pre_execute_hook` | Hook blocks execution |
| 03-10 | `post_execute_hook` | Hook modifies result |
| 03-11 | `parallel_execution` | SafeForParallel tools concurrent |

### 04 — State Management (8 tests)

| ID | Test | Validates |
|----|------|-----------|
| 04-01 | `export_import` | Export → Import = identical state (excluding Meta) |
| 04-02 | `session_id_auto` | `EnsureSessionID()` auto-generates |
| 04-03 | `token_accumulation` | Tokens accumulate across queries |
| 04-04 | `cost_accumulation` | Cost accumulates across queries |
| 04-05 | `messages_immutable` | `Messages()` returns copy |
| 04-06 | `add_checkpoint` | Checkpoints stored/retrieved |
| 04-07 | `clear_checkpoints` | Removes all |
| 04-08 | `initial_messages` | Seeds conversation |

### 05 — Streaming (6 tests)

| ID | Test | Validates |
|----|------|-----------|
| 05-01 | `stream_content` | Content chunks delivered |
| 05-02 | `stream_reasoning` | Reasoning chunks delivered |
| 05-03 | `stream_on_done` | `OnDone` fires with response |
| 05-04 | `stream_on_error` | `OnError` fires on failure |
| 05-05 | `stream_buffer` | Content in StreamingBuffer |
| 05-06 | `stream_events` | `stream_chunk` events published |

### 06 — Events (7 tests)

| ID | Test | Validates |
|----|------|-----------|
| 06-01 | `query_started` | Published with query + model |
| 06-02 | `query_completed` | Published with tokens/cost/duration |
| 06-03 | `tool_start_end` | Published around tool execution |
| 06-04 | `metrics_update` | Published with token counts |
| 06-05 | `compaction_event` | Published when compaction runs |
| 06-06 | `error_event` | Published on errors |
| 06-07 | `critical_delivery` | Many rapid events → critical event still arrives |

### 07 — Compaction (10 tests)

| ID | Test | Validates |
|----|------|-----------|
| 07-01 | `phase0_checkpoint_sub` | Oldest checkpoints substituted |
| 07-02 | `phase0_masking` | Consumed tool results masked |
| 07-03 | `phase1_drop_summaries` | Checkpoint summaries dropped |
| 07-04 | `phase15_drop_turns` | Oldest turns dropped |
| 07-05 | `phase2_emergency` | Emergency truncation |
| 07-06 | `trigger_fraction` | Custom fraction honored |
| 07-07 | `max_tokens_derivation` | Correct max_tokens math |
| 07-08 | `context_overflow_recovery` | Overflow → aggressive compaction |
| 07-09 | `turn_checkpoint` | Checkpoint built on completed turn |
| 07-10 | `on_checkpoint_callback` | Callback fires with checkpoint |

### 08 — Fallback Parser (8 tests)

| ID | Test | Validates |
|----|------|-----------|
| 08-01 | `json_code_fence` | Extracts from ```json blocks |
| 08-02 | `bare_json` | Extracts from bare JSON |
| 08-03 | `xml_blocks` | Extracts from XML tags |
| 08-04 | `named_blocks` | Extracts named patterns |
| 08-05 | `tool_use_blocks` | Anthropic `<tool_use>` format |
| 08-06 | `deduplication` | Deduplicates by name+args |
| 08-07 | `validates_known_tools` | Checks against known tools |
| 08-08 | `disabled` | No parsing when disabled |

### 09 — Normalizer & Validator (6 tests)

| ID | Test | Validates |
|----|------|-----------|
| 09-01 | `strip_channel` | `<|channel|>N` removed |
| 09-02 | `repair_json` | Trailing commas, bare KV repaired |
| 09-03 | `generate_id` | Missing IDs generated |
| 09-04 | `normalizer_dedup` | Duplicate calls removed |
| 09-05 | `validator_truncated` | `...`, abrupt endings detected |
| 09-06 | `tentative_post_tool` | Planning-language rejected |

### 10 — Error Handling & Retry (12 tests)

| ID | Test | Validates |
|----|------|-----------|
| 10-01 | `retry_transient` | Transient errors retried |
| 10-02 | `retry_rate_limit` | Rate limit retried |
| 10-03 | `auth_fails_fast` | Auth errors return immediately |
| 10-04 | `overflow_fails_fast` | Context overflow returns immediately |
| 10-05 | `max_attempts` | Stops after MaxAttempts |
| 10-06 | `classify_transient` | 5xx → TransientError |
| 10-07 | `classify_auth` | 401 → AuthError |
| 10-08 | `classify_rate_limit` | 429 → RateLimitError |
| 10-09 | `backoff_exponential` | Delay grows across retries (tested via event timestamps) |
| 10-10 | `backoff_max_cap` | Delay never exceeds `maxDelay` |
| 10-11 | `backoff_jitter` | Jitter applied (observed via non-identical delays) |
| 10-12 | `backoff_reset` | After error resolution, next retry starts fresh |

### 11 — Steering & Injection (5 tests)

| ID | Test | Validates |
|----|------|-----------|
| 11-01 | `steer_message` | Appears in next request, consumed once |
| 11-02 | `steer_system` | System steer works |
| 11-03 | `steer_queue` | Multiple steers queue up |
| 11-04 | `inject_input` | Injected during loop |
| 11-05 | `inject_full` | Second inject returns false |

### 12 — Output Manager (4 tests)

| ID | Test | Validates |
|----|------|-----------|
| 12-01 | `publish_content` | Content events published |
| 12-02 | `publish_reasoning` | Reasoning events published |
| 12-03 | `flush_callback` | Flush callback invoked |
| 12-04 | `event_metadata` | Metadata merged into events |

### 13 — Conversation Optimizer (4 tests)

| ID | Test | Validates |
|----|------|-----------|
| 13-01 | `file_read_dedup` | Identical reads deduplicated |
| 13-02 | `shell_command_dedup` | Transient commands deduplicated |
| 13-03 | `masking_gated` | Masking only when triggered |
| 13-04 | `preserves_latest` | Latest preserved, older replaced |

### 14 — Configuration (1 test)

| ID | Test | Validates |
|----|------|-----------|
| 14-01 | `set_provider_swap` | `SetProvider` changes `providerInfo` output |

**Total: 103 conformance tests**

---

## Design Decisions

1. **Fresh process per test** — guaranteed isolation, no state leakage
2. **Mock provider, not real LLM** — deterministic, fast, no credentials
3. **Outcome assertions, not timing** — test what happened, not when
4. **Streaming: final content + event counts** — not chunk ordering
5. **JSON-RPC framing** — well-understood, handles async events
6. **Backoff tested through retry loop** — not as a standalone unit, using
   event timestamps to observe delay behavior
7. **`setProvider` is the only runtime-swappable component** — `Executor` and
   `UI` are construction-time only

---

## File Structure

```
seed/
├── cmd/seed-cli/main.go              # CLI binary
├── conformance/
│   ├── runner/main.go                # Test runner
│   ├── specs/
│   │   ├── 00_smoke/*.json
│   │   ├── 01_lifecycle/*.json
│   │   ├── 02_conversation/*.json
│   │   ├── 03_tools/*.json
│   │   ├── 04_state/*.json
│   │   ├── 05_streaming/*.json
│   │   ├── 06_events/*.json
│   │   ├── 07_compaction/*.json
│   │   ├── 08_fallback/*.json
│   │   ├── 09_normalize/*.json
│   │   ├── 10_errors/*.json
│   │   ├── 11_steering/*.json
│   │   ├── 12_output/*.json
│   │   ├── 13_optimizer/*.json
│   │   └── 14_config/*.json
│   ├── PROTOCOL.md                   # CLI protocol spec
│   └── SPEC_FORMAT.md                # Test file format
├── core/                             # (existing)
├── events/                           # (existing)
└── Makefile                          # Add: conformance target
```

---

## How New Languages Use This

1. **Port the library** — implement `core` and `events` interfaces idiomatically
2. **Build the CLI** — `seed-cli-{lang}` speaking the JSON-RPC protocol
3. **Run conformance** — `conformance/runner --cli ./seed-cli-{lang}`
4. **All 103 tests pass** = functionally equivalent to Go reference
