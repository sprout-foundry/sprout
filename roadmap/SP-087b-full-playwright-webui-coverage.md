# SP-087: Full Playwright Coverage of the WebUI

**Status:** ✅ Implemented (2026-06-30; acceptance criterion 3 partial — trace/video/screenshot config deferred, see SP-087-acceptance.md)
**Date:** 2026-06-27
**Depends on:** existing Playwright dep (`@playwright/test ^1.57.0`); existing `sprout` Go binary; existing Vite dev server on port 3000
**Priority:** High (large test-coverage gap; 111 webui components have no test at all, and even the tested ones are jsdom-only with mocked APIs)
**Effort Estimate:** ~2–3 weeks (phased; ~150 component areas × 3 scenarios avg)

## Problem

The webui (`webui/src/`) has **152 component tests** (jsdom-based Vitest) but **zero Playwright tests of the running SPA**. The Vitest tests mock everything — APIs, timers, the WebSocket — so they verify that components *render correctly with mocked data*, not that the full system actually works end-to-end.

This leaves several classes of bugs completely uncovered:

1. **Integration bugs** — A component that renders fine in isolation but breaks when wired to the real WebSocket message format. (We've fixed several of these historically, e.g. the recent `fix(webui): fix api response format and resolve several ui bugs`.)
2. **Routing/navigation bugs** — Sidebar links, deep links, browser back/forward behavior. The jsdom tests don't exercise the router.
3. **Real-data edge cases** — Empty states, very long session names, special characters in filenames. Mocked test data is usually "clean."
4. **Async race conditions** — The WebSocket event handler order, debounced input, optimistic UI updates that may revert.
5. **Cross-component interactions** — Drag-and-drop, file-tree → editor → terminal flows.
6. **Real authentication / API failures** — 401 redirects, 500 responses, network timeouts.

The existing Playwright tests (`test/desktop-smoke.spec.js`, `test/desktop-e2e-smoke.spec.js`, `test/full-user-flow.spec.js`) cover the **Electron launcher and desktop app shell** — they don't open the webui SPA in a browser and exercise it. The `test/ui_e2e_workflow.js` script is a one-shot webui smoke probe, not a test suite.

## Goals

1. New test directory: `test/webui/` with `*.spec.ts` files (TypeScript, matches existing webui conventions).
2. Update `playwright.config.js` to add a `webui` project that targets these specs and starts both the Go backend and the Vite dev server as fixtures.
3. Cover the **top ~50 user-facing surfaces** in the first pass: chat, sessions, settings, model picker, file tree, editor, terminal, onboarding, command palette, worktree management.
4. Tests run **against a real backend** (a `sprout` binary started in test mode with a temp workspace and mock provider credentials) and a **real Vite dev server** so the actual webui code is exercised.
5. CI integration: a new GitHub Actions workflow `webui-e2e.yml` runs the suite on PRs; the existing `build.yml` continues to run fast unit + Vitest tests.
6. Failure artifacts (screenshots, traces, videos) saved to `test-results-html/` so failures are debuggable from CI artifacts.

## Design

### Test fixtures

A new package `test/webui/fixtures/` provides reusable setup:

```typescript
// test/webui/fixtures/sprout.ts
export type SproutHandle = {
  baseUrl: string;        // e.g. http://localhost:56000
  workspaceDir: string;   // temp dir the backend is rooted in
  apiKey: string;         // mock LLM provider key
  shutdown: () => Promise<void>;
};

export async function startSprout(opts?: { port?: number }): Promise<SproutHandle> { ... }

// test/webui/fixtures/vite.ts
export async function startViteDevServer(opts?: { port?: number; backendUrl?: string }): Promise<ViteHandle> { ... }

// test/webui/fixtures/page.ts
export async function gotoWebui(page: Page, opts?: { workspace?: string }): Promise<void> { ... }
```

The `startSprout` fixture:
1. Allocates a free port.
2. Creates a temp workspace directory with a small fixture project (e.g., `package.json`, a few `.go` and `.ts` files).
3. Spawns `./sprout serve --port=<port> --workspace=<tmpdir> --test-mode` with mock LLM credentials injected via env.
4. Polls `/health` until ready (or fails after 30s).
5. Returns the handle; `shutdown()` kills the process and cleans the temp dir.

The `startViteDevServer` fixture:
1. Allocates a free port.
2. Spawns `npm run dev` from `webui/` with `SPROUT_DEV_BACKEND_URL` set to the sprout handle's base URL.
3. Waits for the dev server to respond with the index HTML.
4. Returns a handle with the URL and a `shutdown()`.

### Playwright config

Update `playwright.config.js` to register a new `webui` project:

```js
{
  name: 'webui',
  testDir: './test/webui',
  testMatch: ['**/*.spec.ts'],
  timeout: 60000,
  retries: process.env.CI ? 1 : 0,
  workers: 1,           // single-worker to avoid port collisions
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'test-results-html' }]],
  use: {
    baseURL: process.env.WEBUI_BASE_URL || 'http://localhost:3000',
    actionTimeout: 15000,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  webServer: {
    command: 'node test/webui/start-stack.mjs',
    url: 'http://localhost:3000',
    reuseExistingServer: !process.env.CI,
    timeout: 60000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
}
```

The `start-stack.mjs` script:
1. Spawns `sprout serve --test-mode --port=<free> --workspace=<temp>`.
2. Spawns `npm run dev --prefix webui -- --port=3000` with `SPROUT_DEV_BACKEND_URL` pointed at the sprout backend.
3. Forwards both processes' stdout/stderr to the test runner's stdout (with prefixes).
4. Writes the allocated ports to `test/webui/.ports.json` so tests can read them.
5. On SIGTERM, kills both child processes.

### Test coverage (first pass, prioritized)

**Tier 1 — Core flows (must work):**

1. **Chat** — type a message, receive a streamed response (mocked LLM), verify the message appears in the chat list with the right role. (3 tests)
2. **Sessions** — `/sessions` picker appears, lists at least one session, click loads it. (2 tests)
3. **Settings — Providers** — open settings, see provider list, edit Anthropic API key field, save, see updated state. (3 tests)
4. **Settings — Model picker** — change model, verify it's reflected in the status bar. (2 tests)
5. **File tree** — open a workspace, expand a folder, click a file, see it in the editor. (3 tests)
6. **Editor** — type text, see it appear; Cmd+Z undoes; Cmd+S saves (verifies file on disk). (4 tests)
7. **Terminal** — open terminal pane, run `echo hello`, see output. (2 tests)
8. **Onboarding** — first-run flow renders all steps; skipping works. (2 tests)
9. **Command palette** — Cmd+Shift+P opens it; type a command name; Enter executes. (3 tests)
10. **Worktree management** — create a worktree, switch to it, see files change. (3 tests)

**Tier 2 — Adjacent flows (should work):**

11. **MCP server management** — list, test, remove.
12. **Background tasks** — start a long-running shell, see it in the panel, kill it.
13. **Git operations** — view diff, stage, commit, push (against a local bare remote).
14. **Cost/status bar** — session cost updates after a chat turn.
15. **Steer input** — type while agent is running, verify queue.
16. **Multi-chat** — open second chat, switch between them, verify isolation.
17. **Workspace picker** — open picker, see recent workspaces, click one.
18. **Search panel** — open, search across files, see results.
19. **Markdown viewer** — open a `.md` file, see rendered.
20. **Binary file viewer** — open an image, see preview.
21. **Theme toggle** — switch light/dark, verify CSS variable changes.
22. **Notifications** — trigger an OS notification, verify it appears.

**Tier 3 — Edge cases (nice to have):**

23. Empty workspace, empty session list, 1000-message session, very long file path, special characters in filename, network failure during chat, etc.

### Mock LLM

To avoid hitting real APIs (and to make tests deterministic), the Go binary supports a `--mock-llm` flag that swaps in a fake provider returning canned responses. Verify this exists; if not, add it as part of this spec's Phase 0.

### Test pattern

A representative test:

```typescript
// test/webui/chat.spec.ts
import { test, expect } from '@playwright/test';
import { gotoWebui } from './fixtures/page';

test('user can send a chat message and see a response', async ({ page }) => {
  await gotoWebui(page);
  await page.getByTestId('chat-input').fill('Hello, world.');
  await page.getByTestId('chat-send').click();

  const lastMessage = page.getByTestId('chat-message').last();
  await expect(lastMessage).toContainText('mock response', { timeout: 15000 });
});
```

Selectors come from `data-testid` attributes added to the webui (see "Required webui changes" below).

### Required webui changes

To make the webui Playwright-testable, several `data-testid` attributes need to be added to interactive elements. This is a low-risk, additive change (no behavior impact). The `test/webui/testids.ts` file declares the canonical names; contributors add the `data-testid` attributes as they touch components.

A subset of essential test IDs:

```typescript
export const TESTIDS = {
  chatInput: 'chat-input',
  chatSend: 'chat-send',
  chatMessage: 'chat-message',
  sessionsPicker: 'sessions-picker',
  settingsProvidersTab: 'settings-providers-tab',
  modelPicker: 'model-picker',
  statusBar: 'status-bar',
  fileTree: 'file-tree',
  fileTreeItem: 'file-tree-item',
  editor: 'editor',
  terminalPane: 'terminal-pane',
  terminalOutput: 'terminal-output',
  commandPalette: 'command-palette',
  worktreePanel: 'worktree-panel',
  // ... ~80 total
} as const;
```

A small ESLint rule (or just a pre-commit grep) ensures each `data-testid` in the webui exists in `TESTIDS`.

### CI integration

New file `.github/workflows/webui-e2e.yml`:

```yaml
name: WebUI E2E
on:
  pull_request:
    branches: [main]
  push:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with: { go-version: '1.25' }
      - uses: actions/setup-node@v5
        with: { node-version: '22', cache: 'npm' }
      - run: go build -o sprout ./cmd/sprout
      - run: npm ci
      - run: npx playwright install --with-deps chromium
      - run: npm run test:webui-e2e
        env:
          CI: 'true'
      - uses: actions/upload-artifact@v4
        if: failure()
        with:
          name: playwright-results
          path: test-results-html/
```

New `package.json` script:

```json
"test:webui-e2e": "playwright test --config=playwright.config.js --project=webui"
```

### Phase plan

| Phase | Scope | Acceptance |
|-------|-------|------------|
| 0 | Verify `--mock-llm` exists; add if missing. Verify the webui can be served by both Go binary and Vite dev server in headless mode. | `sprout serve --mock-llm --port=56000` works; `npm run dev` starts cleanly. |
| 1 | Fixtures: `startSprout`, `startViteDevServer`, `gotoWebui`. `start-stack.mjs` orchestration script. Playwright config updated. CI workflow added. | Fixtures work locally; CI green on a no-op run. |
| 2 | Add `data-testid` attributes to Tier 1 components (chat, sessions, settings, file tree, editor, terminal, onboarding, command palette, worktree management). Add `test/webui/testids.ts`. | Grep shows every `data-testid` in `webui/src/` is declared in `testids.ts`. |
| 3 | Write Tier 1 tests (~27 tests across 10 spec files). | All green locally and in CI. |
| 4 | Write Tier 2 tests (~20 tests across 11 spec files). | All green. |
| 5 | Write Tier 3 edge-case tests (~10 tests). | All green. |
| 6 | Document the suite in `docs/testing.md` (or a new `docs/webui-e2e.md`): how to run locally, how to write a new test, how to debug a failure, how to add a new `data-testid`. | Doc complete; new contributors can extend the suite. |

## Success Criteria

- `npm run test:webui-e2e` runs locally and in CI, finishes in <5 min, all green.
- New contributors can run a single spec with `npx playwright test --project=webui test/webui/chat.spec.ts`.
- Failing tests produce a Playwright HTML report at `test-results-html/` with screenshots, traces, and videos.
- 50+ user-facing surfaces covered (Tier 1 + Tier 2 complete).
- `data-testid` discipline: every `data-testid` in `webui/src/` is declared in `test/webui/testids.ts`.
- CI runs on every PR; failures block merge.
- Documentation at `docs/webui-e2e.md` explains how to run, write, and debug tests.

## Risks

- **Flaky tests** — Real backends + Vite + WebSocket + React state machines are inherently more flaky than jsdom mocks. Mitigation: use `expect(...).toHaveScreenshot()` for visual stability; use `waitFor` with generous timeouts; mark known-flaky tests with `test.fixme()` until reliable; CI retries once.
- **Long CI time** — A 50-test suite against a real backend can take 5–10 min. Mitigation: shard across 4 GitHub Actions jobs (each runs ~12 tests); use `playwright test --shard=N/M`.
- **Port conflicts in CI** — Multiple test runs in parallel on the same runner can collide on ports. Mitigation: allocate free ports in the fixture (`getPort()` from `get-port` package); single `workers: 1` in playwright config so within a runner there's no collision.
- **Vite dev server cold start** — First Vite request after a fresh `npm run dev` can take 5–10s for the dev bundle to transform. Mitigation: warmup step in `start-stack.mjs` that hits `/` and waits for a 200 before tests start.
- **State pollution between tests** — A test that creates a session in workspace A might leak into another test's workspace B. Mitigation: each test gets a fresh temp workspace via the fixture; cleanup is automatic on test end.
- **WebUI changes break tests** — Adding a `data-testid` is fine; renaming a button label could break a `getByText` selector. Mitigation: prefer `getByTestId` over `getByText` in all new tests; ESLint rule discourages `getByText` in spec files.

## Open Questions

1. Should we use **Playwright's component testing** (`@playwright/experimental-ct-react`) for some scenarios? **Recommendation:** no — it doesn't exercise the real backend, which is the whole point. Stick to full-page tests.
2. Should the webui Playwright suite replace the existing Vitest component tests? **Recommendation:** no — Vitest tests are fast and run on every save; Playwright is slow and runs in CI. The two are complementary. The Vitest tests stay for fast feedback; Playwright adds real-backend confidence.
3. Should we test the **cloud mode** (where the webui talks to a remote `sprout-foundry` backend)? **Recommendation:** yes, but in a separate suite (`test/webui-cloud/`) gated by a `RUN_CLOUD_E2E=1` env var. Cloud E2E requires credentials and is too brittle for every-PR CI.
