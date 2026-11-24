# seed

A minimal, self-contained Go library that implements an LLM-powered conversation engine. It handles the full query → tool-call → response loop, emits structured UI events, tracks conversation state, and supports pluggable LLM providers and tool executors.

**Zero external dependencies.**

## Quick Start

```bash
go get github.com/sprout-foundry/seed/core
```

```go
package main

import (
    "context"
    "fmt"

    "github.com/sprout-foundry/seed/core"
)

func main() {
    agent, err := core.NewAgent(core.Options{
        Provider: &myProvider{},  // implement core.Provider
        Executor: core.NoopExecutor,
    })
    if err != nil {
        panic(err)
    }

    result, err := agent.Run(context.Background(), "Explain Go generics")
    if err != nil {
        panic(err)
    }
    fmt.Println(result)
}
```

See [example/minimal/main.go](example/minimal/main.go) for a complete runnable example.

## Core Interfaces

| Interface | Purpose |
|-----------|---------|
| [Provider](https://pkg.go.dev/github.com/sprout-foundry/seed/core#Provider) | LLM backend (Chat, ChatStream, Info, EstimateTokens) |
| [ToolExecutor](https://pkg.go.dev/github.com/sprout-foundry/seed/core#ToolExecutor) | Tool execution (GetTools, Execute) |
| [UI](https://pkg.go.dev/github.com/sprout-foundry/seed/core#UI) | Output and prompts (use `NoopUI` for headless) |
| [EventPublisher](https://pkg.go.dev/github.com/sprout-foundry/seed/core#EventPublisher) | Optional event bus (any type with `Publish(string, any)`) |
| [StreamHandler](https://pkg.go.dev/github.com/sprout-foundry/seed/core#StreamHandler) | Streaming response callbacks |

## Features

- **Conversation loop** — query → LLM → tool execution → response, with retries
- **Streaming** — `RunStream()` for incremental output via `ChatStream()`
- **Retry with backoff** — automatic retry on transient errors and rate limits
- **Context management** — automatic compaction when context window is exceeded
- **Response validation** — detects truncation and tentative responses
- **Typed errors** — `TransientError`, `RateLimitError`, `AuthError`, `ContextOverflowError`
- **State export/import** — serialize and restore conversation state
- **Event-driven** — pluggable event publisher for UI integration

## Packages

| Package | Purpose |
|---------|---------|
| `core` | Conversation engine (zero deps) |
| `events` | Optional EventBus implementation |
| `internal/test` | Test harness and mocks (internal) |

## API Reference

Full documentation: https://pkg.go.dev/github.com/sprout-foundry/seed
