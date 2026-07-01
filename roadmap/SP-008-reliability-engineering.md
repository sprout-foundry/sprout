# SP-008: Reliability Engineering — Concurrency & Observability

**Status:** ⚠️ Partially Shipped — Track A concurrency tests, semantic-recall instrumentation, and vision metrics shipped (2026-06-24 → 2026-06-30); structured-log context propagation and typed error hierarchy pending (see SP-094 / SP-099)  
**Depends on:** None  
**Priority:** High  
**Effort Estimate:** ~3-4 weeks (2 tracks, parallelizable)

## Problem

Two foundational reliability gaps make the agent harder to debug, test, and scale:

1. **Concurrency**: The agent uses ~25 mutexes across sub-managers with inconsistent locking patterns. Channel-based communication is limited (only `inputInjectionChan`, `asyncOutput`, security approval responses). Known race conditions have been patched individually (`file_watcher.go`), but no systematic audit exists. CI runs `-race` only in the Makefile `test-race` target, not in the default `test` target.

2. **Observability**: Errors use ad-hoc `fmt.Errorf` wrapping (366 occurrences in `pkg/agent/` alone) with no classification. Logging is a mix of `fmt.Printf` (61 sites), `log.Printf`, and `pkg/logging.Logger` (file-based). There is no structured context (sessionID, iteration, provider, model) attached to log entries. The agent has no typed error hierarchy — only `RateLimitExceededError` exists. This makes intelligent retry/recovery impossible to implement systematically.

## Current State

### Concurrency Inventory

**Mutexes (~25 total):**

| Location | Mutex | Purpose |
|----------|-------|---------|
| `agent.go` | `inputInjectionMutex` | Input channel injection |
| `agent.go` | `preparedTools` (RWMutex) | Tool registry cache |
| `agent.go` | `debugLogMutex` | Debug logging |
| `conversation_handler.go` | `transientMessagesMu` | Transient message list |
| `output_buffer.go` | `mu` | Streaming buffer |
| `output_router.go` | `mu` (RWMutex) | Output routing table |
| `submanager_state.go` | `checkpointMu` (RWMutex) | Turn checkpoints |
| `submanager_state.go` | `taskActionsMu` (RWMutex) | Task actions |
| `submanager_state.go` | `historyMu` | Message history |
| `submanager_state.go` | `pauseMutex` | Pause/resume |
| `submanager_output.go` | `outputMutex` | Streaming output |
| `submanager_output.go` | `eventMetadataMu` (RWMutex) | Event metadata |
| `submanager_security.go` | `securityBypassMu` (RWMutex) | Bypass state |
| `submanager_security.go` | `ignoredSecurityMu` (RWMutex) | Ignored concerns |
| `submanager_mcp.go` | `initMu` | MCP initialization |
| `security_approval.go` | `mu` | Approval map |
| `tool_definitions.go` | `registryOnce` (Once) | Tool registry init |
| `tool_executor.go` | `idCounterMu` | Tool execution IDs |
| `scripted_client.go` | `mu` | Test scripting |

**Channels (limited use):**

| Channel | Purpose | Location |
|---------|---------|----------|
| `inputInjectionChan` | Inject prompts into running conversation | `agent.go` |
| `asyncOutput` | Async output streaming | `submanager_output.go` |
| `pending` (map of chans) | Security approval request/response | `security_approval.go` |
| `resultChan` | Tool execution results | `tool_executor_sequential.go` |
| `workers` (semaphore) | Parallel tool execution limit | `tool_executor_parallel.go` |

**Race detection in CI:**
- `Makefile` `test-race` target: `go test -race -tags ollama_test ./pkg/... ./cmd/... -timeout=120s -short`
- `.github/workflows/build.yml` and `release.yml` both include `-race` step
- However, CI uses `-short` which skips long-running tests, potentially missing races

### Error Handling Inventory

**Typed errors (only 1):**

```go
// pkg/agent/api_client.go:101
type RateLimitExceededError struct {
    Attempts  int
    LastError error
}
```

**Error handling location:**

```go
// pkg/agent/error_handler.go
type ErrorHandler struct { agent *Agent }
func (eh *ErrorHandler) HandleAPIFailure(apiErr error, messages []api.Message) (string, error)
```

`HandleAPIFailure` classifies errors via string matching (`strings.Contains`):
- `"timeout"` / `"deadline exceeded"` → timeout
- `"401"` / `"unauthorized"` / `"authentication"` → auth failure
- `"context window"` / `"maximum context length"` → context overflow
- Everything else → generic API error

**Retry/Backoff (only API client):**

```go
// pkg/agent/api_client.go:90
rateLimiter *utils.RateLimitBackoff

// pkg/agent/api_client.go:212
func (ac *APIClient) SendWithRetry(...)
```

Retry is limited to LLM API calls. Tool execution has no retry logic.

**Logging patterns:**

| Pattern | Count | Example |
|---------|-------|---------|
| `fmt.Printf` | ~40 | `fmt.Printf("\n[WARN] Skipping provider...")` |
| `log.Printf` | ~5 | `log.Printf("[debug] failed to write API response dump...")` |
| `pkg/logging.Logger` | Used via `utils.GetLogger()` | File-based, level/timestamp format |
| `debugLog()` | ~15 | Debug-only, writes to temp file when `--debug` |

**`pkg/logging`** is a simple file logger:
- Writes to `~/.sprout/sprout.log`
- Format: `[timestamp] [LEVEL] message`
- No structured fields, no context attachment
- No log levels filtering

## Proposed Solution

### Track A: Concurrency Hardening

**Goal:** Eliminate data races, add `-race` tests to default CI, document concurrent access patterns.

#### A1: Channel-Based Cross-Component Communication

Replace direct method-call-from-goroutine patterns with channel-based communication where appropriate:

```go
// Before: goroutine calls method directly
go func() {
    result := a.state.SomeMethod() // potential race if state mutated concurrently
}()

// After: communicate via channel
resultCh := make(chan SomeResult, 1)
go func() {
    resultCh <- a.state.SomeMethod()
}()
```

Targets:
- `ProcessQuery` → tool executor feedback loop
- Async output worker (`utils.go:ensureAsyncOutputWorker`)
- MCP initialization callbacks

#### A2: Locking Audit

Systematic audit of every field access in concurrent code paths:

1. `CheckFileContentSecurity` — reads security bypass state, ignored concerns map
2. `ProcessQuery` — reads/writes state, output, security managers
3. Tool handlers — read agent fields, write change tracker
4. Compaction — reads and mutates message history

For each: verify the correct mutex is held, document the invariant.

#### A3: Race Detector Tests

- Add `-race` to the default `make test` target (not just `test-race`)
- Add dedicated concurrency test suite in `pkg/agent/concurrency_test.go`
- Remove `-short` from CI race detector step to catch more races
- Document known concurrent access patterns in code comments

**New files:**
- `pkg/agent/concurrency_test.go` — focused race detection tests

**Modified files:**
- `Makefile` — add `-race` to default test target  
- `.github/workflows/build.yml` — remove `-short` from race step

### Track B: Structured Observability

**Goal:** Typed error hierarchy, structured logging with context, Agent retry logic based on error type.

#### B1: Error Types Package

```go
// pkg/errors/types.go

// Categorized errors for intelligent retry/recovery
type TransientError struct {       // Retry with backoff
    Op       string
    Provider string
    RetryAfter time.Duration
    Wrapped  error
}

type RateLimitError struct {        // Retry with provider-specific backoff
    Provider   string
    RetryAfter time.Duration
    Attempt    int
    Wrapped    error
}

type SecurityViolationError struct { // Stop and prompt user
    Tool  string
    Risk  string
    File  string
    Wrapped error
}

type InvalidInputError struct {     // Don't retry, fix input
    Field   string
    Message string
}

type ContextOverflowError struct {   // Compact and retry
    TokensUsed  int
    TokensLimit int
    Wrapped     error
}

type AuthError struct {             // Re-auth or prompt user
    Provider string
    Wrapped  error
}
```

#### B2: Structured Logging Interface

```go
// pkg/logging/structured.go

type LogEntry struct {
    Timestamp  time.Time
    Level      string
    Message    string
    SessionID  string
    Iteration  int
    Provider   string
    Model      string
    Tool       string
    Duration   time.Duration
    Fields     map[string]interface{}
}

type StructuredLogger interface {
    WithContext(sessionID string, iteration int) *LogContext
    WithProvider(provider, model string) *LogContext
}

type LogContext struct {
    logger  *StructuredLoggerImpl
    context map[string]interface{}
}

func (lc *LogContext) Info(msg string, fields ...Field)
func (lc *LogContext) Warn(msg string, fields ...Field)
func (lc *LogContext) Error(msg string, err error, fields ...Field)
func (lc *LogContext) WithTool(tool string) *LogContext
```

Extend existing `pkg/logging` rather than replacing it. Both `fmt.Printf` debug statements and the file logger emit through the structured interface.

#### B3: Replace fmt.Printf Debug Statements

Migration plan (incremental, not all at once):

1. Agent lifecycle events (init, shutdown, provider selection) → structured logger
2. Tool execution lifecycle (start, end, error) → structured logger  
3. Conversaton flow (compaction, checkpoint, summary) → structured logger
4. Remaining `fmt.Printf` calls → structured logger

Each replacement maintains existing console output behavior but adds structured fields.

#### B4: Agent Retry Logic

```go
// pkg/agent/retry.go

func (a *Agent) handleToolError(err error) (action RetryAction) {
    var transient *errors.TransientError
    var rateLimit *errors.RateLimitError
    var security *errors.SecurityViolationError
    var overflow *errors.ContextOverflowError

    switch {
    case errors.As(err, &transient):
        return RetryWithBackoff(transient.RetryAfter)
    case errors.As(err, &rateLimit):
        return RetryWithBackoff(rateLimit.RetryAfter)
    case errors.As(err, &security):
        return StopAndPrompt(security.Tool, security.Risk)
    case errors.As(err, &overflow):
        return CompactAndRetry
    default:
        return FailFast(err)
    }
}
```

## Implementation Phases

### Phase 1: Foundations (Week 1-2)

**Track A:**
- A1: Channel patterns for `ProcessQuery` feedback loop
- A3: Add `-race` to default test, write concurrency_test.go

**Track B:**
- B1: Create `pkg/errors/types.go` with all error categories
- B2: Create `pkg/logging/structured.go` interface

**New files:**
- `pkg/errors/types.go`
- `pkg/logging/structured.go`
- `pkg/agent/concurrency_test.go`

### Phase 2: Migration (Week 2-3)

**Track A:**
- A1: Channel patterns for async output and MCP callbacks
- A2: Locking audit of `CheckFileContentSecurity`, `ProcessQuery`, tool handlers

**Track B:**
- B3: Migrate agent lifecycle and tool execution logging to structured logger
- B4: Implement `handleToolError` retry logic
- Start using typed errors in `api_client.go` (replace string matching in `ErrorHandler`)

### Phase 3: Validation (Week 3-4)

- Full `-race` test suite passing in CI
- 100% of `pkg/agent/` errors use typed errors
- All `fmt.Printf` debug statements replaced with structured logger
- Verify debug log output includes session context (sessionID, iteration, provider, model)
- Integration test: agent recovers from transient, rate limit, and context overflow errors

## Success Criteria

| Metric | Target |
|--------|--------|
| `go test -race ./pkg/agent/...` | Pass consistently |
| Typed error coverage in `pkg/agent/` | 100% (no bare `fmt.Errorf` for return paths) |
| Structured log entries | Include sessionID, iteration, provider, model |
| `fmt.Printf` in `pkg/agent/` (non-test) | 0 (all migrated to structured logger) |
| Error-based retry | Agent retries on transient/rate-limit, stops on security, compacts on overflow |
| CI `-race` step | Runs without `-short`, full test suite |

## Open Questions

1. Should the structured logger output to both file and console simultaneously? → Yes, configurable via log level
2. Should typed errors be adopted by packages outside `pkg/agent/`? → Phase 4+ scope
3. Minimum Go version for `-race` improvements? → Already on Go 1.25, no issue

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/agent.go` | Audit: 3 mutexes, concurrent field access |
| `pkg/agent/submanager_state.go` | Audit: 4 mutexes, message/checkpoint access |
| `pkg/agent/submanager_output.go` | Audit: 3 mutexes, output streaming |
| `pkg/agent/submanager_security.go` | Audit: 2 mutexes, bypass/ignored state |
| `pkg/agent/submanager_mcp.go` | Audit: init mutex, tool cache |
| `pkg/agent/error_handler.go` | Modify: use typed errors instead of string matching |
| `pkg/agent/api_client.go` | Modify: return typed errors, add context to log entries |
| `pkg/agent/conversation_handler.go` | Modify: structured logging, typed error returns |
| `pkg/agent/conversation_optimizer.go` | Modify: replace fmt.Printf with structured logger |
| `pkg/logging/logger.go` | Modify: extend with structured interface |
| `pkg/errors/types.go` | Create: typed error hierarchy |
| `pkg/logging/structured.go` | Create: structured logging interface |
| `pkg/agent/concurrency_test.go` | Create: race detection tests |
| `Makefile` | Modify: add `-race` to default test |
| `.github/workflows/build.yml` | Modify: remove `-short` from race step |
