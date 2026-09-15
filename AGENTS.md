# AGENTS.md

Guidance for AI agents working in this repository.

## Workflow

- **Subagents**: serialized only — `run_subagent`, never `run_parallel_subagents`.
- **Build**: `make build-all` after every code change.
- **Roadmap**: `ls roadmap/` before touching an area; `SP-###.md` files are authoritative.
- **First-time setup**: `make prepare-grammars` (needed for IDE; Make targets do it automatically).

## Testing

- Go unit: `go test ./...`; smoke: `make test-smoke`.
- WebUI unit (vitest, jsdom): `make test-webui-vitest` (runs `webui/src/**/*.test.*`).
- WebUI e2e (Playwright, `test/webui/*.spec.ts`): `npx playwright test --project=webui test/webui/<spec>.spec.ts`. The backend + Vite stack auto-starts (`test/webui/start-stack.mjs`).
- **New e2e specs must launch with `chromium.launch({ channel: 'chrome' })` falling back to `chromium.launch()`** — the Playwright browser download is absent on some dev machines; system Chrome works.
- The e2e stack runs `sprout agent --daemon`, which is **shared-agent mode**: chat-session create/modify APIs 403 with `shared_mode`. Multi-chat happy paths can't run on the standard stack — pin shared-mode UX in e2e and cover multi-chat logic in vitest.
- Local-LLM selection skips model dirs with a corrupt config.json (`validModelConfig` in pkg/localmodel) — if a local model panics in sinter LoadConfig, check `~/.sprout-local/models/*/"config.json` for truncated downloads before debugging code.

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

## Frontend (webui / packages/ui)

- `webui`'s `tsc` resolves `@sprout/ui` types from `packages/ui/dist` (symlinked via workspaces; dist is gitignored). **After changing `packages/ui/src`, run `cd packages/ui && npm run build` or webui type-check fails on stale types.**
- Frontend gates: `make lint` (webui eslint + prettier + tsc), prettier: `cd webui && npx prettier --check "src/**/*.{ts,tsx,css,json}"`. No raw hex/rgba in CSS — design tokens only (`docs/internal/design-system.md`).
- CodeMirror: pass config objects (`{doc, extensions}`) to `MergeView`/unified constructors — a pre-created `EditorState` silently drops its extension list (only `.doc`/`.selection` are read), killing history/listeners/readOnly.
- React state updaters must stay pure — never fire side effects (`closeBuffer`, event dispatches, toasts) inside `setX(updater)`; StrictMode double-invokes them.

## Incident / User-Data Hygiene (public repo)

- **NEVER commit incident writeups, debugging narratives, or references to specific incidents** (no `INCIDENT-YYYY-MM-DD` files/ids) — this is a public repo.
- **NEVER commit user-identifying data**: session IDs, workspace/host paths, customer domains, infra identifiers (pool/tenant IDs), credentials, transcripts, or tool-output excerpts from real sessions. Test fixtures must be synthetic.
- Comments and commit messages describe the *mechanism*, never the *incident*: "a JWT inside a serialized JSON string", not "session X's token dump".
- If debugging requires real session data, keep it out of the tree entirely (read from state dirs at runtime in throwaway local tests, delete before commit).

## Design System

Full rules: `docs/internal/design-system.md` (token reference lives there; see also Frontend section above).

## Integration with Sprout Foundry

This repo's binary and packages (`@sprout/events`, `@sprout/ui`) are consumed by `../sprout-foundry`. When changing contracts (`packages/ui/src/types`, event schemas, HTTP API shapes), bump versions and run `cd ../sprout-foundry && make test-integration`. Optional/backward-compatible props still warrant the run when the sibling checkout exists — if it doesn't, note the pending integration check in the commit message. See `../sprout-foundry/COMPATIBILITY.md`.
