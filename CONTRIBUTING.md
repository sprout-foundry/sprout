# Contributing to sprout

Welcome to the sprout project! This guide covers everything you need to start developing.

## Prerequisites

- **Go 1.25** (required, matches CI)
- **Node.js 22** (required, matches CI)
- **Python 3.11+** (for test runners)
- **npm** (comes with Node.js)

## Quick Start

```bash
git clone <repo-url>
cd sprout
go mod download
make build-all   # builds React UI → embeds into Go → compiles binary
./sprout          # runs the agent
```

## Project Structure

- `cmd/` — Main application entrypoints and CLI commands
- `pkg/` — Core Go packages (agent, git, webui, providers, tools, etc.)
- `webui/` — React-based web UI (Create React App + TypeScript + CodeMirror)
- `scripts/` — Build and utility scripts (build-webui-embed.mjs, build-wasm.sh, etc.)
- `smoke_tests/` — API smoke tests
- `test_runner.py` — E2E test runner (workspace-level tests, uses real AI models)
- `integration_test_runner.py` — Integration test runner (infrastructure/mechanics, mocked AI)
- `e2e_test_runner.py` — E2E test runner variant
- `.github/workflows/` — CI configuration

## The Embed System

The React UI gets embedded into the Go binary:

- `pkg/webui/static_loader.go` uses `//go:embed static` to embed the React build output
- `make deploy-ui` runs the React build (`webui/`), then copies output to `pkg/webui/static/` via `scripts/build-webui-embed.mjs`
- At compile time, Go embeds everything from `pkg/webui/static/` into the binary
- The `readStaticFile()` function has a runtime fallback: if the embedded file doesn't exist (e.g., fresh clone where `pkg/webui/static/` only has a placeholder), it reads from `webui/build/` on the local filesystem
- `pkg/webui/static/` is in `.gitignore` — generated artifacts are NOT committed. The only tracked file is `pkg/webui/static/placeholder`, which exists solely to satisfy the `//go:embed` directive on fresh clones (without at least one file, Go's embed fails with "no matching files found")
- `make build-all` = `deploy-ui` + `build-wasm` + `build` (the full pipeline)
- For development: `make dev` runs `deploy-ui` only, then you can use `make build` to compile the Go binary

## Development Workflow

**Go-only work** (no UI changes):

```bash
make build          # compiles Go binary (expects UI already deployed)
make test-unit      # fast unit tests
```

**UI development mode** (hot reload):

```bash
cd webui
npm install
npm start           # starts CRA dev server on port 3000
```

The dev server proxies API requests to `http://localhost:54000` (configured in `webui/package.json` proxy field). Run the Go backend with `./sprout` to serve the API.

**Full build** (before committing):

```bash
make build-all      # UI + WASM + Go binary
```

## Testing

- **Unit tests**: `make test-unit` or `go test -tags ollama_test ./pkg/... ./cmd/...`
- **Integration tests**: `make test-integration` (runs `integration_test_runner.py`)
- **Smoke tests**: `make test-smoke`
- **E2E tests**: `make test-e2e MODEL=openai:gpt-4` (requires real AI model, costs money)
- **Coverage**: `make test-coverage` (runs unit tests with `-race`, enforces 40% minimum)
- **All non-expensive tests**: `make test-all`
- **Frontend linting**: `make lint` (eslint + prettier format check + type-check)
- **Frontend lint fix**: `make lint-fix`

## Useful Make Targets

Reference `make help` for the full list:

- `make build` — Build sprout binary
- `make build-all` — Full build: React UI + WASM + Go binary
- `make deploy-ui` — Build React UI and deploy to Go static directory
- `make verify-ui-embedded` — Fail if embedded UI assets are stale
- `make build-wasm` — Build WASM shell module (sprout.wasm)
- `make build-ui` — Build React UI only (doesn't deploy to Go static)
- `make test` / `make test-unit` — Unit tests
- `make test-integration` — Integration tests
- `make test-e2e` — E2E tests (requires MODEL=<provider:model>)
- `make lint` / `make lint-fix` — Frontend linting
- `make clean` — Clean test artifacts
- `make dev` — Quick development build (deploy UI only)

## Code Style

- **Go**: follow standard Go conventions, run `goimports` for formatting
- **TypeScript/React**: Prettier + ESLint (enforced in CI via `make lint`)
- **Build tags**: use `ollama_test` build tag when building/testing (e.g., `go test -tags ollama_test`)
- **File size target**: under 500 lines per file
- **SRP**: each type/file should have one primary responsibility

## CI Pipeline

CI runs on push/PR to `main`:

1. Frontend lint + type-check
2. UI build + embed verification
3. Unit tests with race detection and 40% coverage enforcement
4. Integration tests
5. Smoke tests
6. Multi-platform builds (linux/darwin/windows × amd64/arm64)
