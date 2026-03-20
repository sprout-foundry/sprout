# Repository Guidelines

This guide helps contributors work effectively on ledit, an AI‑assisted code editor and agent.

## Project Structure & Module Organization
- `cmd/`: Cobra CLI commands (e.g., `agent`, `shell`, `commit`).
- `internal/domain/`: Core domain entities (agent, todo).
- `pkg/ui/`: UI framework and components.
- `pkg/`: Other core packages (currently under development).
- Tests: Go unit tests co‑located; E2E runner `test_runner.py`; integration tests in `integration_tests/`.

## Code Quality Evaluation Metrics

When evaluating and modifying this codebase, prioritize these metrics:

### 1. File Size
- **Target**: Files should generally be under 500 lines
- **Hard limit**: Files over 800 lines should be reviewed for splitting
- **Rationale**: Large files are harder to understand, test, and maintain

### 2. Single Responsibility Principle (SRP)
- Each type/struct should have one primary responsibility
- Each function should do one thing well
- Signs of SRP violations:
  - Files with multiple unrelated `type X struct` definitions
  - Functions exceeding 100 lines with multiple concerns
  - Files that import many unrelated packages

### 3. Code Duplication
- DRY principle: Don't Repeat Yourself
- Look for:
  - Similar logic patterns repeated across files
  - Duplicate utility functions
  - Repeated error handling patterns
  - Identical struct definitions

### 4. Self-Documenting Code
- Prefer descriptive names over comments
- Comments should only explain **why**, not **what** (the code explains what)
- Avoid comments that just restate the obvious
- Use well-named functions and variables to convey intent

## Build, Test, and Development Commands
- Build: `go build` (binary: `ledit`), `go install` to GOPATH/bin.
- Unit tests: `go test ./...` or verbose `go test ./... -v`.
- Critical UI tests: `go test ./pkg/console/ -v` (run after UI changes).
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
- Always run `go test ./pkg/console/ -v` after modifying console UI.
- In PRs, include a brief test plan and any UI screenshots/asciinema if UI changed.

## Test Debugging & OOM Investigation (2025-01-10) - RESOLVED

**Issue**: Tests were causing terminal crashes (suspected OOM).

**Root Cause Found**: `TestEscapeParserIncompleteSequences` in `pkg/console/escape_trace_test.go` had an **infinite loop bug**.
**The Bug**: Test nested loop had no proper exit condition - processed same byte repeatedly forever.
**Resolution**: Rewrite loop with proper byte-by-byte iteration and drain termination.
**Result**: Test now completes in 0.01s instead of infinite loop. All console tests pass.

**Note**: Benchmark in `pkg/agent_api/streaming_test.go` renamed to `_DISABLED` as a precaution.

## Code Writing Workflow: Code → Review → Fix

When implementing non-trivial features (multiple files, new logic, or architectural changes), use this structured workflow. It uses exactly two sequential subagent calls plus orchestrator verification — no more.

### Step 1 — Write Code (`coder` subagent)
Delegate to the `coder` persona with a clear description of what to build. A good coder writes tests for new behavior alongside the implementation naturally. Provide existing file paths, describe the expected API/behavior, and specify acceptance criteria.

For large features, break the work into sequential `coder` subagent calls — each scoped to one logical unit (e.g., a data structure, then the functions that use it, then the integration). After each call, read what was produced, run the build and tests, and verify progress before delegating the next unit. This catches problems early and keeps each subagent focused.

**Completion criteria per subagent call:** `go test ./...` passes and `go build ./...` compiles clean.

### Step 2 — Code Review (`code_reviewer` subagent)
Delegate to the `code_reviewer` persona to review **all** changed files — production code and tests. Provide the full list of changed file paths. Ask the reviewer to categorize findings as `MUST_FIX`, `SHOULD_FIX`, `VERIFY`, and `SUGGEST`.

### Step 3 — Fix Issues
Fix every `MUST_FIX` and `SHOULD_FIX`. Address `VERIFY` items by confirming acceptable or fixing. `SUGGEST` items may be deferred. Run `go test ./...` and `go build ./...` after each fix. For complex or numerous fixes, delegate to a `coder` subagent; for small fixes, do them directly.

### Step 4 — Re-Review (once, if fixes were non-trivial)
If the fixes in Step 3 were substantial or touched multiple files, run a single additional `code_reviewer` pass on the changed files. This re-review is a safety check, not an infinite improvement loop — accept that code can be "good enough" and shipped.

### Declare Success
Build passes, tests pass, no open `MUST_FIX`/`SHOULD_FIX`. Read the final files yourself to confirm. Summarize and recommend commit.

### When to Skip This Workflow
- Single-line fixes or trivial edits
- Pure refactoring with no logic changes (existing tests already pass)
- Bug fixes where a `debugger` subagent already identified root cause — fix, write regression test, run suite, single review pass
- Documentation-only changes

## Commit & Pull Request Guidelines
- Users will do commits, never, ever do commits, notify the user when you think a commit is needed.
- Recommend committing after:
  - Completing complex multi-file implementation tasks
  - After multiple subagent delegations with significant changes
  - When asked by the user or at natural section boundaries
- Skip commit reminders after:
  - Simple single-file edits (under 2 tool calls)
  - Quick troubleshooting or debugging tasks
  - When user has explicitly requested "keep going"
- Default behavior: Don't recommend commits after every action
- 
## Security & Configuration Tips
- Never commit secrets. API keys live in `~/.ledit/api_keys.json` or env vars (e.g., `OPENROUTER_API_KEY`).
- Config at `~/.ledit/config.json`; first run selects provider/model. CI runs non‑interactive.
- Useful envs: `LEDIT_DEBUG=1` (verbose), `CI=1` (CI behavior).

## Agent‑Specific Notes
- Select provider/model via flags: `ledit agent --provider openrouter --model qwen/qwen3-coder-30b "..."`.
- After file edits, the system tracks revisions and supports rollback via slash commands and tools.

