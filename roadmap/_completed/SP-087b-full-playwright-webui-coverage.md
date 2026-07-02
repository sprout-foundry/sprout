# SP-087b: Full Playwright Coverage of the WebUI

**Status:** ✅ Implemented (2026-06-30; 64 tests in 27 files, CI sharded across 4 parallel jobs; trace/video config deferred then shipped in SP-090)

The WebUI had 152 jsdom-based Vitest component tests but zero Playwright tests of the running SPA — leaving integration bugs, routing bugs, real-data edge cases, and async race conditions completely uncovered. This spec created a `test/webui/` directory with TypeScript Playwright specs that run against a real `sprout` backend (with `--mock-llm`) and a real Vite dev server. Test fixtures (`startSprout`, `startViteDevServer`, `gotoWebui`) manage the full stack lifecycle. A `data-testid` discipline was established via `test/webui/testids.ts` as the canonical registry. The suite covers 59 distinct user-facing sub-surfaces across 3 tiers (chat, sessions, settings, file tree, editor, terminal, onboarding, command palette, worktree management, + 20 Tier 2 surfaces + 10 Tier 3 edge cases). CI runs in 4 parallel shards with 10-minute timeouts.

## Key decisions

- Full-page Playwright tests (not component testing) — the real backend is the whole point.
- `--mock-llm` flag for deterministic tests without hitting real APIs.
- `data-testid` attributes preferred over `getByText` selectors — ESLint rule enforces registry discipline.
- 4-shard CI parallelism to keep wall time under 5 minutes.
- Fixtures allocate free ports dynamically to avoid port collisions.
- Each test gets a fresh temp workspace — no state pollution between tests.
- Trace/video/screenshot config initially deferred, then shipped in SP-090-1 (`trace: 'on-first-retry'`, `video: 'on-first-retry'`, `screenshot: 'only-on-failure'`).

## Artifacts

- code: `test/webui/fixtures/` — `startSprout`, `startViteDevServer`, `gotoWebui` fixtures
- code: `test/webui/testids.ts` — canonical `data-testid` registry (~80 entries)
- code: `test/webui/chat.spec.ts` — representative Tier 1 test (chat flow)
- code: `.github/workflows/webui-e2e.yml` — CI workflow with 4-shard parallelism
- code: `playwright.config.js` — `webui` project configuration
- code: `docs/webui-e2e.md` — documentation (248 lines: running locally, writing tests, debugging, CI)

Full specification archived — see git history for original content.
