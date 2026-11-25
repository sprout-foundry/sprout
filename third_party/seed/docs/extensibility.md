# Extensibility

The `core` package is designed for library integrability: zero external dependencies, interface-based decoupling, and configurable behavior at every layer.

## Error Handling

### Typed Errors

`core/errors.go` defines typed error structs with `Unwrap()` and `Is*()` helpers:

- **`TransientError`** — temporary failure (network, timeout, 5xx). Retriable.
- **`RateLimitError`** — provider rate limit (429, quota). Retriable with `RetryAfter` hint.
- **`ContextOverflowError`** — context window exceeded. Not retriable.
- **`AuthError`** — authentication failure (401, invalid key). Not retriable.
- **`ClientError`** — invalid request (4xx), or a server that does not offer what was asked (501). Not retriable.
- **`ContentFilteredError`** — content filter blocked after retry exhausted.
- **`BlankResponseError`** — consecutive blank/repetitive responses triggered force-finalize.

### ClassifyError

`ClassifyError(err, provider)` (`core/error_classifier.go`) wraps raw errors into typed errors via pattern matching on error messages and HTTP status codes. Returns typed errors unchanged. Defaults unknown errors to `TransientError`.

### Retry with Backoff

`ExponentialBackoff` (`core/backoff.go`) supports configurable initial delay, max delay, multiplier, max attempts, and jitter (partial and full jitter modes).

Retry loop in `ProcessQuery()` / `doChatWithRetry()`:
- **Retriable**: `TransientError`, `RateLimitError` — retries with exponential backoff
- **Fail-fast**: `AuthError`, `ContextOverflowError`, `ClientError` — returns immediately without retry
- Configured via `RetryConfig` in `Options` (defaults: 3 attempts, 100ms initial, 5s max, 2x multiplier, no jitter)

### Sentinel Errors

- `ErrNoProvider`, `ErrNoExecutor` — constructor validation
- `ErrInterrupted` — context cancellation
- `ErrMaxIterations` — max iteration count exceeded, returned by `runLoop` and published as `EventTypeError`
- `ErrPaused` — `Run()` called while agent is paused
- `ErrZeroChoices` — provider returned zero choices

## Configuration

### RetryConfig

Configurable via `Options.RetryConfig`:
- `MaxAttempts` — total attempts (default 3)
- `InitialDelay` — delay before first retry (default 100ms)
- `MaxDelay` — backoff cap (default 5s)
- `Multiplier` — exponential growth factor (default 2.0)
- `Jitter` — randomness factor: 0 = none, (0,1) = partial, ≥1 = full (default 0)

### Feature Gates

`Options` provides disable flags:
- `DisableFallbackParser` — skip fallback tool-call parsing from model content
- `DisableValidator` — skip truncation/tentative response detection
- `DisableNormalizer` — skip tool call normalization before execution

## Steering

Transient message injection for one-shot guidance:

- **`Steer(msg Message)`** — queues a message appended to the next API call only. Consumed once; not persisted in state.
- **`SteerSystem(content string)`** — convenience for system-role steering messages.
- Messages are drained into `ConversationHandler` when `Run()` starts.
- Must be called between `Run()` calls; messages queued during an active `Run()` are held until the next call.

## Provider Switching

- **`SetProvider(p Provider)`** — swaps provider at runtime. Safe between queries; undefined behavior during active query. Panics on nil.

## Context Cancellation

- `ctx.Done()` is checked at the top of each loop iteration and before tool execution
- Returns `ErrInterrupted` wrapped with `ctx.Err()` on cancellation
- **`InjectInput(input string)`** — buffered channel (capacity 1) for mid-conversation user input. Returns `true` if accepted, `false` if a prior injection is pending. The injected message is picked up on the next iteration check.

## Hooks

- **`OnIteration`** — callback `(iteration, messages, tokenEstimate, contextSize)` invoked synchronously at the start of each loop iteration. Fire-and-forget with panic recovery. Token estimate is computed from the prepared message list before compaction.
- **`OnCheckpoint`** — callback `(TurnCheckpoint)` invoked synchronously after each completed turn in `finalize()`. Fires immediately after the checkpoint is built and stored. Panic recovery applied.

## Library Integrability

### No Events Dependency

- `EventPublisher` interface (`core/interfaces.go`) is satisfied by `events.EventBus` but does not import the `events` package
- Falls back to `noopEventPublisher` when nil is provided
- Event type constants (e.g., `EventTypeQueryStarted`) are defined in `core/interfaces.go`

### Noop Implementations

- **`NoopUI`** — headless UI that discards output and never prompts
- **`NoopExecutor`** — tool executor with no tools; use when agent only produces text

### Internal Test Package

- `internal/test/` — contains test utilities; not imported by production code

### Example

- `example/minimal/` — runnable example with no external dependencies beyond `core`

## Recent Additions

- **`query_completed` event**: published in `finalize()` with query, response, tokens, cost, and duration.
- **`Interrupt()` / `ResetInterrupt()`**: external cancellation via independent `interruptCtx`, mutex-protected, auto-reset at each `Run()` call.
- **`Agent.Checkpoints()`**: delegates to `State.GetCheckpoints()`, returning a copy of all recorded `TurnCheckpoint` structs.
- **Async checkpoint recording**: `finalize()` calls `RecordTurnCheckpointAsync()` — non-blocking, with 5s timeout fallback and panic-safe `OnCheckpoint` callback.
