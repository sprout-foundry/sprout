# Contributing to Sprout

Thanks for considering a contribution to Sprout! Whether it's a bug fix, a new
feature, or improved documentation, we're glad you're here. Please read the
guidelines below to help keep things smooth for everyone.

## Development Setup

### Prerequisites

- **Go 1.25** — backend and CLI
- **Node.js 22** — React frontend and shared packages
- **Python 3.11** — integration and smoke test runners

### Cloning and Building

```bash
git clone https://github.com/sprout-foundry/sprout.git
cd sprout

# Full build: React UI + WASM shell + Go binary
make build-all
```

The `make build-all` target builds everything in the right order. Use it to
verify your changes compile cleanly before committing or opening a PR.

### Running Tests

```bash
# Quick feedback (unit tests only)
go test ./...

# Full verification (build + tests + coverage check)
make test-all

# Coverage check (fails if below 40%)
make test-coverage
```

See [docs/TESTING.md](docs/TESTING.md) for the full testing strategy and
command reference.

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `pkg/` | Go backend — core engine, providers, shell, web UI server |
| `cmd/` | CLI entry points |
| `webui/` | React (Vite/TypeScript) frontend |
| `packages/events` | Shared events transport (`@sprout/events`) |
| `packages/ui` | Shared React component library (`@sprout/ui`) |
| `desktop/` | Electron desktop app |
| `scripts/` | Build, verification, and release scripts |
| `docs/` | Design docs and testing strategy |

## Making Changes

### Branch Naming

Use descriptive branch names that indicate the kind of change:

```
feature/ssh-workspace-support
fix/terminal-scrollback
docs/update-testing-strategy
```

### Commit Messages

Follow [conventional commits](https://www.conventionalcommits.org/):

```
feat: add SSH workspace support for remote editing
fix: handle null provider in agent initialization
docs: update testing strategy with coverage thresholds
```

### Code Quality

- **File size target**: Under 500 lines per file
- **Single Responsibility**: Each type/file should have one primary concern
- **No duplication**: Use existing utilities before writing new ones
- **Self-documenting code**: Prefer descriptive names; add comments only for
  "why" decisions, not "what"

## Code Style

### Go

- Format with `gofmt` (or `goimports` to also tidy imports)
- Follow [Effective Go](https://go.dev/doc/effective_go) conventions
- Write tests alongside code in `*_test.go` files

### TypeScript / Frontend

- The project uses **ESLint + Prettier** for the React frontend
- Run `make lint` to check style and `make lint-fix` to auto-format
- Type-check with `cd webui && npm run type-check`

## Pull Request Process

When you open a PR, CI will run:

1. **`make lint`** — frontend linting (warnings only, non-blocking)
2. **Duplicate component check** — enforces no redundant components
3. **`make build-all`** — full build pipeline (blocking)
4. **`make test-coverage`** — unit tests with race detection, ≥ 40% coverage
5. **`make test-integration`** — integration tests with mocked AI
6. **`make test-smoke`** — basic functionality checks
7. **Cross-platform builds** — Linux, macOS, Windows × amd64/arm64

Before you submit:

- [ ] Run `make build-all` locally and confirm it succeeds
- [ ] Run `make test-coverage` to ensure coverage is above the threshold
- [ ] Update or add tests for new functionality
- [ ] Document any user-facing changes in `CHANGELOG.md`

### What Reviewers Look For

- Does the change follow the existing patterns in the codebase?
- Are edge cases and errors handled?
- Is there test coverage for the new code?
- Does the PR scope match what the title and description claim?

## License

Sprout is licensed under the [MIT License](LICENSE). By contributing, you agree
that your contributions will be licensed under the same terms.
