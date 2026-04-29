# SP-005: Supporting Systems & Infrastructure

**Status:** ✅ Active  
**Location:** `pkg/events/`, `pkg/prompts/`, `pkg/tools/`, `pkg/codereview/`, `pkg/webcontent/`, `cmd/`, build system

## Current State

Supporting systems provide event bus, prompts, shared tool implementations, code review, browser automation, CLI commands, and the build/test infrastructure.

## Event Bus (`pkg/events/`)

### System

Central publish/subscribe event system connecting agent internals to the webui via WebSocket.

```go
type EventBus struct {
    subscribers map[string][]chan Event
    mu           sync.RWMutex
}

type Event struct {
    Type      string
    Data      map[string]interface{}
    Timestamp time.Time
}
```

### Event Types

| Event | Publisher | Consumer |
|-------|-----------|----------|
| `agent_message` | Agent | Webui (streaming response) |
| `agent_metrics` | Agent | Webui (token/cost) |
| `tool_start` / `tool_end` | ToolExecutor | Webui (tool tracking) |
| `subagent_activity` | SubagentHandler | Webui (subagent output) |
| `notification` | Various | Webui (toast notifications) |
| `git_status` | GitAPI | Webui (git refresh) |
| `prompt_request` | SecurityManager | Webui (security prompts) |

### WebSocket Publishing (`pkg/webui/websocket.go`)

- 1266 lines — largest Go file in webui package
- Manages WebSocket connections from browser clients
- Publishes events to connected clients via channel-based fan-out
- Handles reconnection, client lifecycle, ping/pong

## Prompts (`pkg/prompts/`)

### System

System prompt templates that define the agent's behavior, capabilities, and constraints.

### Structure

- Base system prompt (comprehensive, ~2000+ lines markdown)
- Persona-specific prompt overrides (`personas/` directory)
- Prompt composition: base → persona override → skill injections → system supplements

## Shared Tool Implementations (`pkg/tools/`)

### System

Shared tool logic used by the agent but not directly registered as agent tools.

- **File reading/writing:** Used by tool handlers in `pkg/agent/`
- **File discovery:** Workspace file listing
- **Filesystem operations:** Copy, move, rename helpers

## Code Review (`pkg/codereview/`)

### Self-Review Gate

- **Purpose:** After agent completes work, optionally review the output for quality
- **Modes:** `off`, `code`, `always`
- **Implementation:** Sends modified files to a reviewer persona for quality assessment
- **Integration:** Called at end of conversation flow, before final response

## Browser Automation (`pkg/webcontent/`)

### System

Browser automation via [go-rod](https://github.com/go-rod/rod) for:
- Screenshot capture
- Web content extraction
- PDF rendering

### Integration

- `analyze_ui_screenshot` tool — takes screenshots, analyzes UI state
- `analyze_image_content` tool — OCR and image understanding
- PDF OCR for document processing

## CLI Commands (`cmd/`)

### Command Structure

```
main.go
├── cmd/agent.go          — sprout agent (interactive/non-interactive)
├── cmd/service_darwin.go — macOS daemon (launchd)
├── cmd/service_linux.go  — Linux daemon (systemd)
├── cmd/wasm/             — WASM shell entry point
├── cmd/model_registry_server/ — Static model registry
└── cmd/refresh_provider_catalog/ — Refresh model catalogs
```

### Agent Command

Entry point for interactive and non-interactive agent usage. Key flags:

| Flag | Purpose |
|------|---------|
| `--provider` / `-p` | Select LLM provider |
| `--model` / `-m` | Select model |
| `--persona` | Activate a persona at startup |
| `--daemon` / `-d` | Run in daemon mode (webui) |
| `--system-prompt` | Custom system prompt file |
| `--max-iterations` | Limit conversation loops |
| `--no-subagents` | Disable subagent tools |
| `--output-json` | Structured JSON output for CI |
| `--dry-run` | Simulate tool execution |
| `--unsafe` | Bypass security checks |
| `--web-port` | Port for webui |

### Runtime Modes

1. **Interactive:** Terminal REPL with slash commands
2. **Non-interactive:** `--prompt-stdin` for scripting/CI
3. **Daemon:** WebUI server with WebSocket
4. **Output JSON:** Structured result for SaaS integration

## Build System

### Makefile Targets

| Target | Purpose |
|--------|---------|
| `build-all` | React UI + WASM + Go binary |
| `build-wasm` | WASM shell module |
| `build-webui-dist` | Cloud-mode distributable |
| `build-webui-dist-local` | Local-mode distributable |
| `lint` | ESLint + Prettier + TypeScript checks |
| `test` | Go tests |

### Build Pipeline

```
1. React UI (webui/) — npm run build → webui/build/
2. WASM shell — GOOS=js GOARCH=wasm → webui/public/wasm/sprout.wasm
3. Static assets — webui/build/ → pkg/webui/static/ (embedded via Go embed)
4. Go binary — go build → ./sprout (includes embedded UI + WASM)
```

### Embed System

React UI build artifacts are embedded into the Go binary at compile time via `//go:embed`. The webui server serves these from memory in production, or from the filesystem during development.

## Test Infrastructure

### Go Tests

- **316 test files** across all packages
- **`pkg/agent/`**: 70+ test files covering conversation flow, E2E patterns, tool execution, persona behavior
- **Scripted client** (`scripted_client.go`): Mock LLM client for deterministic test scenarios
- **E2E tests**: Full conversation flow tests with scripted responses

### E2E Python Tests

- `test_runner.py` — Main E2E test runner
- `e2e_test_runner.py` — E2E-specific runner
- Tests the full system through the CLI

### Test Patterns

```
Unit tests     → pkg/**/*_test.go (fast, deterministic)
Integration   → agent_e2e_*.go (scripted client, no real API calls)
E2E Python    → test_runner.py (full system through CLI)
WebUI tests   → webui/src/**/*.test.tsx (Jest + React Testing Library)
```

## Key Files

| File | Purpose |
|------|---------|
| `pkg/events/events.go` | Event types and EventBus |
| `pkg/webui/websocket.go` | WebSocket server + fan-out |
| `pkg/prompts/` | System prompt templates |
| `pkg/tools/` | Shared tool implementations |
| `pkg/codereview/` | Self-review gate |
| `pkg/webcontent/` | Browser automation |
| `cmd/agent.go` | CLI agent command |
| `main.go` | Root entry point + cobra setup |
| `Makefile` | Build system |
| `go.mod` | Go module dependencies |
