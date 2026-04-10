# AGENTS.md

This file provides guidance to AI agents working on code in this repository.

## Build Verification Requirement

**You MUST run `make build-all` after making any code changes.** This builds both the React UI (deployed into Go embed) and the Go binary. A successful build confirms:
- Frontend TypeScript compiles without errors
- React UI bundles successfully
- Go binary compiles and embeds the UI

Run it at the end of every implementation task, before reporting work as complete:

```bash
make build-all
```

## Testing

```bash
go test ./...                   # Run unit tests
python3 test_runner.py          # Run E2E tests
```

## Git Operations Policy

**NEVER COMMIT OR PUSH CHANGES** via shell_command for non-`repo_orchestrator` personas. Only the repository owner decides when to commit.

**Staging specific files is always allowed.** `git add <filepath>` may be used via shell_command by any persona. However, broad patterns (`git add .`, `git add -A`, `git add --all`) are always blocked — use the git tool with specific file paths instead.

**`repo_orchestrator` privileges**: This persona can stage files, commit (via the commit tool), and push without interactive approval. However, operations that discard or alter history (checkout, restore, reset) always require the git tool pathway with explicit user approval, regardless of persona.

## Code Quality

- **File size target**: Under 500 lines per file
- **SRP**: Each type/file should have one primary responsibility
- **No code duplication**: Use existing utilities
- **Self-documenting code**: Descriptive names; comments only for "why"
- **Incremental refactoring**: Build after each extraction step
