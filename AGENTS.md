# AGENTS.md

Guidance for AI agents working in this repository.

## Workflow

- **Subagents**: serialized only — `run_subagent`, never `run_parallel_subagents`.
- **Build**: `make build-all` after every code change.
- **Test**: `go test ./...` (unit), `make test-smoke` (smoke).
- **Roadmap**: `ls roadmap/` before touching an area; `SP-###.md` files are authoritative.
- **First-time setup**: `make prepare-grammars` (needed for IDE; Make targets do it automatically).

## Critical Git Rules

- **NEVER FORCE PUSH** or rebase in any form. Use `git merge` to integrate upstream.
- **NEVER COMMIT OR PUSH** without an explicit user request.
- `git add <file>` only; `git add .`/`-A` is blocked.
- Pre-push: `git fetch origin` → `git log HEAD..FETCH_HEAD` → merge if non-empty → `make build-all` → push.
- Commit messages are shell-safe (temp file, not `-m`).

## Test Isolation

- Use `newTestAgent(t)` / `createTestAgentWithTempConfig(t)`, never `agent.NewAgent()`.
- Scope env with `t.Setenv`; set `SPROUT_CONFIG` to temp dir.
- `configuration.NewTestManager(t)` isolates config in one call.
- Never persist `api.TestClientType` ("test") to provider config.
- Guard network tests behind `SKIP_NETWORK_TESTS` or credential skip.
- Test artifacts (`*_test.go`) must be committed or removed, not left in tree.

## CI Pipeline

Run gates before pushing: `make vet && make fmt-check && make lint && make build-all`.

Details, hermetic test requirements, and platform workarounds: `docs/internal/ci-pipeline.md`.

## Code Conventions

- **No comments** unless explaining a non-obvious "why". Self-documenting code is the default.
- Under 500 lines per file. Split before exceeding.
- `fmt.Errorf("doing X: %w", err)` — wrap at boundaries, return raw at source.
- Conventional Commits (`feat:`, `fix:`, `refactor:`, `chore:`).
- Read `CONTRIBUTING.md`, `docs/TESTING.md`, `docs/ARCHITECTURE.md` before major changes.

## Incident / User-Data Hygiene (public repo)

- **NEVER commit incident writeups, debugging narratives, or references to specific incidents** (no `INCIDENT-YYYY-MM-DD` files/ids) — this is a public repo.
- **NEVER commit user-identifying data**: session IDs, workspace/host paths, customer domains, infra identifiers (pool/tenant IDs), credentials, transcripts, or tool-output excerpts from real sessions. Test fixtures must be synthetic.
- Comments and commit messages describe the *mechanism*, never the *incident*: "a JWT inside a serialized JSON string", not "session X's token dump".
- If debugging requires real session data, keep it out of the tree entirely (read from state dirs at runtime in throwaway local tests, delete before commit).

## Design System

No raw hex/rgba in CSS. Use design tokens from `App.css`. Full rules: `docs/internal/design-system.md`.

## Integration with Sprout Foundry

This repo's binary and packages (`@sprout/events`, `@sprout/ui`) are consumed by `../sprout-foundry`. Bump versions and run `cd ../sprout-foundry && make test-integration` when changing contracts. See `../sprout-foundry/COMPATIBILITY.md`.
