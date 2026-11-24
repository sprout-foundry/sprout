# Tool Registry

`ToolRegistry` (`core/tool_registry.go`) is the primary implementation of `ToolExecutor`. It manages tool registration, argument parsing, parallel execution, circuit breaking, and lifecycle hooks.

## Registration

- **`Register(config ToolConfig)`** — adds a single tool. Validates name is non-empty and handler is provided. Creates a `Tool` definition with JSON schema from `ParameterConfig`. Registers aliases and initializes a circuit breaker per tool.
- **`RegisterAll(configs []ToolConfig)`** — registers multiple tools sequentially; returns first error.
- **`Unregister(name string)`** — removes a tool, its aliases, and its circuit breaker by canonical name. Returns `true` if found.
- **`GetTools() []Tool`** — returns all tool definitions sorted by name (for `provider.Chat()` / `EstimateTokens()`).

## Lookup

- **`GetTool(name)`** — returns `*Tool` definition, resolving aliases. Returns `nil` if not found.
- **`HasTool(name)`** — reports whether tool exists by canonical name or alias.
- **`ToolNames()`** — returns all canonical names.

## ToolConfig

```
Name              string              // required
Description       string              // shown to model
Parameters        []ParameterConfig   // schema for argument parsing
Handler           ToolHandler         // func(ctx, args) (string, error)
HandlerWithImages ToolHandlerWithImages // func(ctx, args) ([]ImageData, string, error)
Aliases           []string            // alternative names
Timeout           time.Duration       // per-tool override (default: 5 min)
MaxResultSize     int                 // result truncation limit (default: 50KB)
SafeForParallel   bool                // eligible for concurrent execution
```

`ParameterConfig` supports `Name`, `Type`, `Required`, `Alternatives` (alias parameter names), and `Description`.

## Execution Pipeline

`executeSingle()` runs each tool call through this pipeline:

1. **Name resolution** — strips `<|channel|>N` suffix, resolves aliases (case-insensitive)
2. **Argument parsing** — `parseAndValidateArgs()`: JSON parse with repair (trailing commas, single quotes, bare content), alternative name resolution, type coercion (string→number/boolean), required parameter validation
3. **Call metadata context** — `WithToolCallMetadata()` attaches the call ID, resolved name, and call index so handlers can correlate their own events back to this call (`ToolCallIDFromContext()` et al.)
4. **Circuit breaker check** — if open and timeout elapsed, allows one probe request (half-open); otherwise rejects immediately
5. **PreExecuteHook** — if set, called with `(name, args)`; can return error to block execution
6. **Timeout context** — `context.WithTimeout` using per-tool or default (5 min) timeout
7. **Handler call** — runs in goroutine; result delivered via channel (supports both `Handler` and `HandlerWithImages`)
8. **Result truncation** — truncated to `MaxResultSize` (per-tool or global 50KB default) with `(truncated, N chars total)` marker
9. **PostExecuteHook** — if set, called with `(name, result)`; return value replaces result
10. **Circuit breaker update** — `RecordSuccess()` or `RecordFailure()` on outcome
11. **Event publishing** — `tool_start` before execution, `tool_end` after with status/duration

## Parallel Execution

`Execute()` partitions tool calls into parallel and sequential batches:

- **Parallel tools** — `SafeForParallel == true`: run concurrently with a semaphore of **max 4 goroutines**
- **Sequential tools** — all others: run after parallel batch completes
- **Context check** — if `ctx.Err()` is set before spawning goroutines, parallel tools fall back to sequential
- **Panic recovery** — each goroutine recovers panics and records an error result
- **Result ordering** — results are placed by original index; order is preserved in the returned slice

## Circuit Breaker

`circuitBreaker` (`core/circuit_breaker.go`) — 3-state pattern per tool:

- **Closed** — normal operation. Failures counted; transitions to open when count reaches threshold (default 5)
- **Open** — rejects all requests immediately. After reset timeout (default 30s), transitions to half-open
- **Half-Open** — allows exactly one request. Success → closed; failure → reopens
- **Methods**: `Allow()`, `RecordSuccess()`, `RecordFailure()`, `State()`

Configured via `ToolRegistryOptions.CircuitBreakerThreshold` and `CircuitBreakerTimeout`.

## Hooks

- **`PreExecuteHook(name, args)`** — called before handler. Return error to block execution with a descriptive message. Can be used for validation, security checks, or resource gating.
- **`PostExecuteHook(name, result)`** — called after handler, after truncation. Return value replaces the result. Can be used for redaction, formatting, or metadata injection.
- Both hooks are set via `ToolRegistryOptions` and shared across all tools.

## Event Publishing

Uses `EventPublisher` interface (decoupled from `events` package):

- **`tool_start`** — published before execution. Keys: `tool_name`, `tool_call_id`, `arguments`, `tool_index`
- **tool_end** — published after execution. Keys: `tool_call_id`, `tool_name`, `status` ("success"/"error"), `result`, `duration_ms`
- Falls back to `noopEventPublisher` when no publisher is provided
