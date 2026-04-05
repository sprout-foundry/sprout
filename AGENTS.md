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

**NEVER COMMIT OR PUSH CHANGES.** Only the repository owner decides when to commit. Use `git status`, `git diff`, and other read-only git commands freely.

**Staging files is always allowed.** Although `git add` is technically a write operation, agents may always stage files to prepare commits for the repository owner to review and finalize.

## Code Quality

- **File size target**: Under 500 lines per file
- **SRP**: Each type/file should have one primary responsibility
- **No code duplication**: Use existing utilities
- **Self-documenting code**: Descriptive names; comments only for "why"
- **Incremental refactoring**: Build after each extraction step
