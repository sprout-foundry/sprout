# Repository Guidelines

This guide helps contributors work effectively on ledit, an AI‑assisted code editor and agent.

## Project Structure & Module Organization
- `cmd/`: Cobra CLI commands (e.g., `agent`, `shell`, `commit`).
- `pkg/agent/`: Core agent (conversation, tools, streaming, MCP, change tracking).
- `pkg/agent_api/`: LLM provider clients (OpenAI, OpenRouter, DeepInfra, Ollama).
- `pkg/console/`: Terminal UI framework and components (agent console, input, footer).
- `pkg/tools/`: Built‑in tool implementations (file ops, shell, web, validation, rollback).
- Other key packages: `pkg/agent_commands/`, `pkg/configuration/`, `pkg/workspace/`, `pkg/history/`.
- Tests: Go unit tests co‑located; E2E runner `test_runner.py`; integration tests in `integration_tests/`.

## Build, Test, and Development Commands
- Build: `go build` (binary: `ledit`), `go install` to GOPATH/bin.
- Unit tests: `go test ./...` or verbose `go test ./... -v`.
- Critical UI tests: `go test ./pkg/console/components/ -v` (run after UI changes).
- E2E suite: `python3 test_runner.py` (supports single test mode and model override).
- Local run: `./ledit agent "your intent"` or interactive `./ledit`.

## Coding Style & Naming Conventions
- Language: Go 1.24. Format with `gofmt`; validate with `go vet`.
- Packages: lowercase names; files use `snake_case.go`.
- Exports: `PascalCase` for exported, `camelCase` for unexported; add GoDoc comments for exported APIs.
- Tests: `_test.go` files; test funcs `TestXxx`.

## Testing Guidelines
- Prefer focused unit tests near the code under test.
- Use `stretchr/testify` for assertions where helpful.
- Always run `go test ./pkg/console/components/ -v` after modifying console UI.
- In PRs, include a brief test plan and any UI screenshots/asciinema if UI changed.

## Commit & Pull Request Guidelines
- Users will do commits, never, ever do commits, notify the user when you think a commit is needed.
- 
## Security & Configuration Tips
- Never commit secrets. API keys live in `~/.ledit/api_keys.json` or env vars (e.g., `OPENROUTER_API_KEY`).
- Config at `~/.ledit/config.json`; first run selects provider/model. CI runs non‑interactive.
- Useful envs: `LEDIT_DEBUG=1` (verbose), `LEDIT_UI=1` (force UI), `CI=1` (CI behavior).

## Agent‑Specific Notes
- Select provider/model via flags: `ledit agent --provider openrouter --model qwen/qwen3-coder-30b "..."`.
- After file edits, the system tracks revisions and supports rollback via slash commands and tools.

