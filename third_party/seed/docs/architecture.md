# Architecture

Seed is a minimal, self-contained Go library that implements an LLM-powered conversation engine. It handles the full query → tool-call → response loop, emits structured UI events, tracks conversation state, and supports pluggable LLM providers and tool executors.

Package: `github.com/sprout-foundry/seed`

---

## Package Layout

| Package | Path | Responsibility |
|---------|------|----------------|
| `core` | `core/` | Agent engine, conversation loop, streaming, state, types, interfaces, fallback parser, validator, normalizer |
| `events` | `events/` | EventBus — pub/sub for structured UI events |
| `test` | `test/` | Mocks, test harness, e2e integration tests |

---

## Core Types

Defined in `core/types.go` and `core/state.go`.

- **`Agent`** (`core/agent.go`) — Entry point. Holds provider, executor, UI, event bus, state, output manager, fallback parser, normalizer, validator, optimizer. Created via `NewAgent(Options)`.
- **`Options`** (`core/agent.go`) — Configuration struct for `NewAgent`. Required: `Provider`, `Executor`. Optional: `UI`, `SystemPrompt`, `MaxIterations`, `MaxTokens`, `Debug`, `EventPublisher`, `OnIteration`, `Optimizer`, `RetryConfig`, `InitialMessages`, `DisableFallbackParser`, `DisableValidator`, `DisableNormalizer`, `OnCheckpoint`.
- **`State`** (`core/state.go`) — Thread-safe conversation state. Tracks message history, token counts (prompt/completion/total), cost, session ID, and turn checkpoints. Supports `ExportState()` / `ImportState()` for serialization.
- **`Message`** (`core/types.go`) — Single conversation message. Fields: `Role`, `Content`, `ReasoningContent`, `ToolCallID`, `ToolCalls`, `Images`, `Meta`.
- **`Tool`** (`core/types.go`) — Tool definition matching OpenAI function-calling wire format. Contains `Type` and `Function` (name, description, parameters).
- **`ToolCall`** (`core/types.go`) — Model-requested function call. Fields: `ID`, `Type`, `Function` (name, arguments).
- **`ChatResponse`** (`core/types.go`) — LLM response. Fields: `ID`, `Object`, `Created`, `Model`, `Choices` ([]ChatChoice), `Usage` (token counts, cost). `ToMessage()` extracts the first choice's message.
- **`ChatRequest`** (`core/types.go`) — Request to the LLM. Fields: `Model`, `Messages`, `Tools`, `ToolChoice`, `MaxTokens`, `Reasoning`, `Stream`.
- **`ChatChoice`** (`core/types.go`) — One completion. Fields: `Index`, `Message`, `FinishReason`.
- **`ChatUsage`** (`core/types.go`) — Token usage and cost. Fields: `PromptTokens`, `CompletionTokens`, `TotalTokens`, `EstimatedCost`, `Cost`, `CachedTokens`, `CacheWriteTokens`.
- **`RetryConfig`** (`core/agent.go`) — Retry behavior for transient errors. Fields: `MaxAttempts` (default 3), `InitialDelay` (100ms), `MaxDelay` (5s), `Multiplier` (2.0), `Jitter` (0.0).
- **`StreamingBuffer`** (`core/streaming.go`) — Thread-safe buffer for streamed output. Wraps `bytes.Buffer` with mutex.

---

## Interfaces

Defined in `core/interfaces.go`.

### Provider

LLM abstraction. Implement `Chat()` and `ChatStream()`.

- `Chat(ctx, *ChatRequest) (*ChatResponse, error)` — Synchronous chat completion.
- `ChatStream(ctx, *ChatRequest, StreamHandler) error` — Streaming chat completion.
- `EstimateTokens(*ChatRequest) int` — Approximate token count for a request.
- `Info() ProviderInfo` — Model name, context size, vision support.

### ToolExecutor

Tool execution abstraction.

- `GetTools() []Tool` — Returns available tool definitions.
- `Execute(ctx, []ToolCall) []Message` — Runs tool calls, returns result messages.

### UI

Optional interactive interface. Use `NoopUI` for headless mode.

- `Prompt(string) (string, error)` — Display prompt, return user input.
- `Confirm(string) (bool, error)` — Display confirmation, return choice.
- `Print(string)` — Output without newline.
- `PrintLine(string)` — Output with newline.

### StreamHandler

Handles streamed responses from a Provider.

- `OnContent(string)` — Called for each content chunk.
- `OnReasoning(string)` — Called for each reasoning chunk.
- `OnDone(*ChatResponse)` — Called when streaming completes.
- `OnError(error)` — Called on streaming errors.

### EventPublisher

Minimal interface for event publishing. Satisfied by `events.EventBus` or any custom system.

- `Publish(eventType string, data any)` — Publish an event.

---

## EventBus

Defined in `events/events.go`. Thread-safe pub/sub system.

- **`Subscribe(name)`** — Returns a buffered channel (capacity 100) for receiving events.
- **`Unsubscribe(name)`** — Removes subscriber and closes its channel.
- **`Publish(eventType, data)`** — Broadcasts to all subscribers. Non-blocking for normal events (dropped if channel full). **Critical events** (`security_approval_request`, `security_prompt_request`, `ask_user_request`) use a drain-then-deliver strategy: if the channel is full, one stale event is drained to make room, ensuring critical dialogs always reach the client.

### Event Types

**Generic** (defined in both `core/interfaces.go` and `events/events.go`): `query_started`, `query_completed`, `error`, `tool_start`, `tool_end`, `stream_chunk`, `metrics_update`, `compaction`.

**Sprout-specific** (defined in `events/events.go`): `query_progress`, `tool_execution`, `subagent_activity`, `todo_update`, `file_changed`, `file_content_changed`, `validation`, `security_approval_request`, `security_prompt_request`, `ask_user_request`, `agent_message`, `workspace_changed`, `session_terminated`.

---

## OutputManager

Defined in `core/output_manager.go`. Manages all output streams from the agent.

- **Streaming buffers** — Separate `StreamingBuffer` instances for content and reasoning output.
- **Async output channel** — Buffered channel (capacity 256) for goroutine-safe background delivery. Events are silently dropped if the channel is full or closed.
- **Flush callback** — Registered via `SetFlushCallback(fn)`. Invoked on `Flush()`.
- **Event metadata** — Key-value pairs (`SetEventMetadata`, `GetEventMetadata`) merged into each output event.
- **`PublishOutput(OutputEvent)`** — Routes events to both the async channel and the event publisher. Content events publish `stream_chunk` (text) + `agent_message`; reasoning events publish `stream_chunk` (reasoning); error events publish the error type.
- **`Reset()`** — Clears buffers and drains pending async events.
- **`Close()`** — Shuts down the async channel. Idempotent.

---

## Convenience Types

Defined in `core/noop.go`.

- **`NoopUI`** — Headless UI that discards all output and never prompts. `Confirm()` returns `true`.
- **`NoopExecutor`** — ToolExecutor with no tools. `GetTools()` returns nil; `Execute()` returns nil.

---

## Feature Gates

Configured via `Options` fields in `core/agent.go`:

- **`DisableFallbackParser`** (default `false`) — When enabled, the fallback parser (`core/fallback_parser.go`) is not created. Malformed tool calls in model responses will not be recovered.
- **`DisableValidator`** (default `false`) — When enabled, the response validator (`core/response_validator.go`) is not created. Incomplete responses will not trigger automatic continuation.
- **`DisableNormalizer`** (default `false`) — When enabled, the tool call normalizer (`core/tool_call_normalizer.go`) is not created. Structured tool calls will not be cleaned before execution.
- **`Optimizer`** (default `nil`) — A `*ConversationOptimizer` for optimizing conversation history across iterations. `nil` means optimization is disabled.

---

## Error Types

Defined in `core/errors.go`.

- **`ErrNoProvider`** / **`ErrNoExecutor`** — Returned by `NewAgent` when required options are missing.
- **`ErrInterrupted`** — Context cancellation during conversation.
- **`ErrMaxIterations`** — Maximum iteration count exceeded.
- **`ErrPaused`** — `Run()` called while agent is paused.
- **`ErrZeroChoices`** — Provider returned a valid response with zero choices.
- **`TransientError`** — Temporary failure; retryable with backoff.
- **`RateLimitError`** — Provider rate limit hit; retryable.
- **`ContextOverflowError`** — Context window exceeded; not retryable.
- **`AuthError`** — Authentication failure; not retryable.
- **`ClientError`** — Invalid request (4xx) or not implemented (501); not retryable.
- **`ContentFilteredError`** — Provider content filter blocked response after retry.
- **`BlankResponseError`** — Model produced consecutive blank/repetitive responses.
