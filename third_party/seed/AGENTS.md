# AGENTS.md — Seed Agent Engine

## 📖 Project Overview
`seed` is a minimal, self-contained Go library that implements an LLM-powered conversation engine. It handles the full query → tool-call → response loop, emits structured UI events, tracks conversation state, and supports pluggable LLM providers and tool executors.

**Package:** `github.com/sprout-foundry/seed`

---

## 🏗 Architecture & Core Concepts

| Component | File | Responsibility |
|-----------|------|----------------|
| `Agent` | `core/agent.go` | Entry point. Holds provider, executor, UI, event bus, state, and output manager. Exposes `Run(ctx, query)`. |
| `ConversationHandler` | `core/conversation.go` | Drives the main loop: builds prompts, calls provider, executes tools, emits events, finalizes response. |
| `State` | `core/state.go` | Tracks message history, token counts, session ID. Supports export/import. |
| `Provider` | `core/interfaces.go` | LLM abstraction. `Chat()`, `ChatStream()`, `EstimateTokens()`, `Info()`. |
| `ToolExecutor` | `core/interfaces.go` | Executes tool calls returned by the model. |
| `OutputManager` | `core/output_manager.go` | Manages content/reasoning buffers, async output channel, flush callbacks, and event metadata. |
| `AgentStreamHandler` | `core/streaming.go` | Concrete `StreamHandler` that writes chunks to buffers, publishes events, and flushes. |
| `EventBus` | `events/events.go` | Thread-safe pub/sub for UI events (`query_started`, `tool_start`, `stream_chunk`, etc.). |
| `Harness` | `internal/test/harness.go` | E2E test harness wiring `MockProvider`, `MockExecutor`, `MockUI`, and `EventBus`. |

---

## 📁 Directory Structure
```
.
├── core/               # Agent engine, state, conversation loop, streaming, output manager
├── docs/               # Documentation of current implemented state (see below)
├── events/             # EventBus and structured event types
├── internal/test/      # Mocks, test harness, e2e integration tests
├── example/minimal/    # Runnable example
├── go.mod / go.sum
├── Makefile            # Build, test, vet, format targets
└── AGENTS.md           # This file
```

## 📚 Documentation

`docs/` documents the current implemented state of the project:

| File | Covers |
|------|--------|
| [docs/architecture.md](./docs/architecture.md) | Core types, interfaces, EventBus, OutputManager, convenience types |
| [docs/conversation-flow.md](./docs/conversation-flow.md) | Main loop, streaming, fallback parsing, validation, hardening |
| [docs/compaction.md](./docs/compaction.md) | Checkpoints, compaction algorithm, conversation optimizer |
| [docs/tool-registry.md](./docs/tool-registry.md) | Tool registry, circuit breaker, execution pipeline, hooks |
| [docs/extensibility.md](./docs/extensibility.md) | Errors, retry, steering, hooks, library integrability, known gaps |

`roadmap/` tracks **incomplete** work. Completed specs are removed or reduced to a brief description with a `docs/` reference. See [roadmap/README.md](./roadmap/README.md).

---

## 🛠 Development Workflow

| Task | Command |
|------|---------|
| Build | `make build` |
| Test | `make test` |
| Format | `make fmt` |
| Vet | `make vet` |
| Run e2e | `make test-e2e` |
| Full check | `make check` |

---

## 📝 Coding Conventions
- **Interfaces over implementations:** Always code against `Provider`, `ToolExecutor`, `UI`, `StreamHandler`, `OutputManager`.
- **Error wrapping:** Use `fmt.Errorf("context: %w", err)` consistently.
- **State immutability:** `State` methods mutate internally; callers should not slice/append to `state.Messages()` directly.
- **Event emission:** Publish events via `agent.eventBus.Publish(type, payload)`. Never block on full channels; drop non-critical events.
- **Streaming:** Use `AgentStreamHandler` for chunked output. It writes to `OutputManager` buffers and triggers flush callbacks.
- **Concurrency:** `EventBus`, `OutputManager`, and `State` are goroutine-safe. `ConversationHandler` is single-goroutine per query.

---

## 🧪 Testing Guidelines
- Use `test.NewHarnessWithT(t)` for all new e2e tests.
- Configure mocks via `h.Provider().AddTextResponse(...)` or `h.Executor().AddToolResult(...)`.
- Assert events with `h.AssertEventPublished(events.EventTypeX)`.
- Keep tests deterministic: avoid real network calls, real file I/O, or time-dependent logic without mocking.
- Run `make check` before committing.

---

## ✅ CI Requirements (Required Before Completion)

Every change **must** pass all CI checks before work is considered complete. Run these steps in order:

1. **Format** — `gofmt -l .` must return empty. Fix with `go fmt ./...` if not.
2. **Vet** — `go vet ./...` must pass with no issues.
3. **Build** — `go build ./...` must compile cleanly.
4. **Test** — `go test -race -count=1 ./...` must pass (race detector enabled, no caching).
5. **Example** — `go run ./example/minimal/` must run without error.

Convenience target: `make check` runs vet, format, build, and test.

**Do not mark any task complete until all of the above pass.**

---

## 🗺 Roadmap

See [roadmap/README.md](./roadmap/README.md) for incomplete work. Completed features are documented in [docs/](./docs/).

---

## 🤖 AI Agent Instructions
1. **Never modify `go.mod`** unless adding a direct dependency required for a new feature.
2. **Always use the test harness** (`test/harness.go`). Do not write tests that instantiate `Agent` manually.
3. **Streaming changes** must update `ProcessQuery` to call `provider.ChatStream()` and wire `AgentStreamHandler`.
4. **Error handling** must introduce typed errors (`TransientError`, `RateLimitError`, etc.) before adding retry logic.
5. **Context cancellation** requires checking `ctx.Done()` in the main loop and exposing `agent.Interrupt()`.
6. **Keep `conversation.go` focused.** If it exceeds ~150 lines, extract `prepareMessages`, `compactMessages`, or `finalize` into separate files.
7. **Run `make check`** before marking any task complete.
8. **Max 400 lines per file.** If a file exceeds this, extract logical units into separate files.
9. **Follow SRP.** Each file should have one clear responsibility; don't mix concerns.
10. **Code should be self-documenting.** Use clear names and small, focused functions. Add comments only when the *why* isn't obvious from the code. When you touch old code, trim its comment blocks. Rationale goes in specs and commit messages instead.
