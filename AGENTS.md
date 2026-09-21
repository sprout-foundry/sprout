# AGENTS.md

Guidance for AI agents working in this repository.

## Workflow

- **Subagents**: serialized only — `run_subagent`, never `run_parallel_subagents`.
- **Build**: `make build-all` after every code change.
- **Roadmap**: `ls roadmap/` before touching an area; `SP-###.md` files are authoritative.
- **First-time setup**: `make prepare-grammars` (needed for IDE; Make targets do it automatically).

## Testing

- Go unit: `go test ./...`; smoke: `make test-smoke`; WebUI unit: `make test-webui-vitest`; WebUI e2e: `npx playwright test --project=webui test/webui/<spec>.spec.ts` (backend + Vite stack auto-starts).
- **New e2e specs must launch with `chromium.launch({ channel: 'chrome' })` falling back to `chromium.launch()`** — the Playwright browser download is absent on some dev machines; system Chrome works.
- **Browser-dependent Go tests** (`pkg/agent/design_e2e_test.go` render cases) skip when no headless browser is reachable. Set `SPROUT_REQUIRE_BROWSER=1` to turn that skip into a failure — CI does this on Linux, where Chromium is installed, so "the render path ran" is asserted rather than hidden behind a green suite that skipped it.
- The e2e stack runs `sprout agent --daemon`, which is **shared-agent mode**: chat-session create/modify APIs 403 with `shared_mode`. Pin shared-mode UX in e2e; cover multi-chat logic in vitest.
- Local-LLM selection skips model dirs with a corrupt `config.json` (`validModelConfig` in pkg/localmodel) — if a local model panics at load, check the local model store's `config.json` files for truncated downloads before debugging code.
- Pre-push gates: `make vet && make fmt-check && make lint && make lint-go-new && make build-all`. Details: `docs/internal/ci-pipeline.md`.
- **Flaky-test policy**: no unbounded waits or live network in unit tests; close every pipe/pty in `t.Cleanup`; timing contract tests must loop. Pattern catalog: `docs/internal/test-flakiness.md`.

## Critical Git Rules

- **NEVER FORCE PUSH** or rebase in any form. Use `git merge` to integrate upstream.
- **NEVER COMMIT OR PUSH** without an explicit user request.
- `git add <file>` only; `git add .`/`-A` is blocked.
- Pre-push: `git fetch origin` → `git log HEAD..FETCH_HEAD` → merge if non-empty → `make build-all` → push.
- Commit messages are shell-safe (temp file, not `-m`).

## Test Isolation

- Use `newTestAgent(t)` / `createTestAgentWithTempConfig(t)`, never `agent.NewAgent()`.
- Scope env with `t.Setenv`; set `SPROUT_CONFIG` to temp dir. `configuration.NewTestManager(t)` isolates config in one call.
- Never persist `api.TestClientType` ("test") to provider config.
- Guard network tests behind `SKIP_NETWORK_TESTS` or credential skip.
- Test artifacts (`*_test.go`) must be committed or removed, not left in tree.

## Code Conventions

- **No comments** unless explaining a non-obvious "why". Self-documenting code is the default.
- Under 500 lines per file. Split before exceeding.
- `fmt.Errorf("doing X: %w", err)` — wrap at boundaries, return raw at source.
- Conventional Commits (`feat:`, `fix:`, `refactor:`, `chore:`).
- Read `CONTRIBUTING.md`, `docs/TESTING.md`, `docs/ARCHITECTURE.md` before major changes.

## Frontend (webui / packages/ui)

- `webui`'s `tsc` resolves `@sprout/ui` types from `packages/ui/dist` (symlinked via workspaces; dist is gitignored). **After changing `packages/ui/src`, run `cd packages/ui && npm run build` or webui type-check fails on stale types.**
- Gates: `make lint` (eslint + prettier + tsc). No raw hex/rgba in CSS — design tokens only (`docs/internal/design-system.md`).
- CodeMirror: pass config objects (`{doc, extensions}`) to `MergeView`/unified constructors — a pre-created `EditorState` silently drops its extension list (only `.doc`/`.selection` are read), killing history/listeners/readOnly.
- React state updaters must stay pure — never fire side effects (`closeBuffer`, event dispatches, toasts) inside `setX(updater)`; StrictMode double-invokes them.

## Incident / User-Data Hygiene (public repo)

- **NEVER commit incident writeups, debugging narratives, or references to specific incidents** (no `INCIDENT-YYYY-MM-DD` files/ids) — this is a public repo.
- **NEVER commit user-identifying data**: session IDs, workspace/host paths, customer domains, infra identifiers (pool/tenant IDs), credentials, transcripts, or tool-output excerpts from real sessions. Test fixtures must be synthetic.
- Comments and commit messages describe the _mechanism_, never the _incident_: "a JWT inside a serialized JSON string", not "session X's token dump".
- If debugging requires real session data, keep it out of the tree entirely (read from state dirs at runtime in throwaway local tests, delete before commit).

## Integration with Sprout Foundry

This repo's binary and packages (`@sprout/events`, `@sprout/ui`) are consumed by the sister `sprout-foundry` checkout. When changing contracts (`packages/ui/src/types`, event schemas, HTTP API shapes), bump versions and run foundry's `make test-integration` from that checkout. Optional/backward-compatible props still warrant the run when the sibling checkout exists — if it doesn't, note the pending integration check in the commit message. See foundry's `COMPATIBILITY.md` for the compatibility rules.
