# WebUI E2E Testing

## Overview

The Sprout WebUI uses Playwright for end-to-end testing of the browser UI. The suite lives under `test/webui/` and currently covers 64 tests across 27 spec files. Tests run against the real sprout backend (in `--mock-llm` mode) and the Vite dev server, so they exercise the full stack: backend API, WebSocket protocol, and React UI. The suite runs on every push and PR to `main` via CI, and can be run locally for development and debugging.

## Running locally

### Prerequisites

- **Node.js 22+** (the project uses Node 22; earlier versions may work but are untested)
- **Go 1.25+** (needed to build the `sprout` binary that tests spawn)
- **Chromium** — install once with:

  ```bash
  npx playwright install --with-deps chromium
  ```

  This downloads Chromium (~150 MB) plus any required system libraries. You only need to do this once per machine.

- **Build the shared packages** (required before running tests):

  ```bash
  npm ci
  npm run build -w @sprout/events
  npm run build -w @sprout/ui
  ```

### Quick run

```bash
npm run test:webui-e2e
```

This boots the full stack automatically (sprout backend + Vite dev server) via `test/webui/start-stack.mjs`, runs all 64 tests in headless Chromium, and exits. The `webServer` block in `playwright.config.js` handles startup and teardown.

### Headed (visible browser)

To see the browser while tests run:

```bash
npm run test:webui-e2e:headed
```

Or use Playwright's debug mode (opens the Playwright Inspector with step-through controls):

```bash
PWDEBUG=1 npm run test:webui-e2e -- --headed --debug
```

### Single spec

Run one spec file:

```bash
npx playwright test --project=webui test/webui/chat.spec.ts
```

Run a single test by name (grep):

```bash
npx playwright test --project=webui --grep "should render chat shell"
```

### Custom ports

The start-stack script honors `SPROUT_PORT` and `VITE_PORT` environment variables:

```bash
SPROUT_PORT=8123 VITE_PORT=5174 npm run test:webui-e2e
```

### Skip the auto-started stack

If you want to run sprout and Vite manually (e.g., to debug the backend separately):

```bash
# Terminal 1: start sprout manually
sprout serve --web-port 8123

# Terminal 2: start Vite manually
cd webui && npm run dev

# Terminal 3: run tests (they will reuse the running servers)
SPROUT_SKIP_WEBSERVER=1 npm run test:webui-e2e
```

Set `SPROUT_SKIP_WEBSERVER=1` to skip the `webServer` block in `playwright.config.js`. The tests will connect to whatever sprout + Vite instances are already running.

## Writing a test

### Template

Every webui e2e spec follows this pattern. Here's a minimal annotated example:

```typescript
import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import { TESTIDS } from './testids';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

test.beforeAll(async () => {
  browser = await chromium.launch();
  sprout = await startSprout();
  vite = await startViteDevServer();
  handle = await newWebuiPage({ browser, url: vite.url });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe('My Feature', () => {
  test('should do something when condition', async () => {
    // Navigate to the relevant page
    await page.goto(vite.url);

    // Use TESTIDS for locators — never hardcode data-testid strings
    const element = page.getByTestId(TESTIDS.chat.shell);
    await expect(element).toBeVisible();
  });
});
```

### Key points

- **Fixtures**: The three fixture modules are at `test/webui/fixtures/sprout.ts`, `test/webui/fixtures/vite.ts`, and `test/webui/fixtures/page.ts`. They handle spawning the backend, the dev server, and the browser page respectively.
- **Where to put specs**: `test/webui/<area>-*.spec.ts`. Use an area prefix that matches existing conventions (`chat`, `tier2-mcp-servers`, `tier3-empty-workspace`).
- **Importing TESTIDS**: Always import from `./testids` and use `TESTIDS.chat.shell` etc. Never hardcode `data-testid` string literals in test code.
- **Naming**: Test names use `should X when Y` style. Spec filenames end in `.spec.ts`.

### The `data-testid` rule

Every `data-testid="foo"` you reference in a test MUST be registered in `test/webui/testids.ts`. The registry is a plain object (`TESTIDS`) with kebab-case keys grouped by area. Tests access it as `TESTIDS.chat.shell`, `TESTIDS.sidebar.container`, etc.

## Adding a new `data-testid`

When you need a new testid for a component:

1. **Add the key to the registry** — edit `test/webui/testids.ts` and add the new key to the `TESTIDS` object under the appropriate area group.

2. **Wire it into the component** — add `data-testid={TESTIDS.my.newKey}` (or `data-testid="my-new-key"`) to the target element in `webui/src/<area>/<Component>.tsx`.

3. **Verify uniqueness** — run these grep commands to confirm the testid appears exactly once in the source and is registered:

   ```bash
   grep -rn "data-testid=\"my-new-key\"" webui/src/   # should return exactly 1 match
   grep -n "my-new-key" test/webui/testids.ts          # should return the registry entry
   ```

4. **Run the consistency test** — `testids.test.ts` scans `webui/src/` for every `data-testid` attribute and verifies it is registered in `TESTIDS_SET`. It also checks forward-references (every key in `TESTIDS` must be used by at least one component):

   ```bash
   npx vitest run test/webui/testids.test.ts
   ```

   If this test passes, your testid is correctly registered and wired.

**Note:** There is no pre-commit hook enforcing the testid registry (no husky/lint-staged integration for this check). The `vitest` consistency test above is the manual verification step.

## Debugging a failure

### HTML report

After a test run, the HTML report is generated at `test-results-html/index.html`. Open it in a browser:

```bash
open test-results-html/index.html    # macOS
xdg-open test-results-html/index.html  # Linux
```

The report shows all tests, their status, durations, and failure details.

### Traces, videos, screenshots

Playwright records video for every test and captures traces + screenshots on failure. These are stored under `test-results/` in the workspace root. The directory structure follows the test name:

```
test-results/
  chat-chat-shell-is-visible/
    video.webm
    trace.zip
    screenshot.png
```

### Headed + debug mode

To step through a test with the Playwright Inspector:

```bash
PWDEBUG=1 npm run test:webui-e2e -- --headed --debug
```

This pauses before each test and opens the Playwright UI where you can step through actions, inspect the DOM, and evaluate expressions.

### View a specific trace

```bash
npx playwright show-trace test-results/<test-name>/trace.zip
```

This opens an interactive trace viewer showing the full timeline of actions, network requests, console logs, and DOM snapshots.

## CI

### Workflow

The CI workflow is defined in `.github/workflows/webui-e2e.yml`. It runs on:

- Every push to `main`
- Every pull request targeting `main`
- Manual dispatch (`workflow_dispatch` in the Actions tab)

### Sharding

The suite is split across 4 parallel shards. Each shard has a 10-minute timeout. With `strategy.fail-fast: false`, all shards run to completion even if one fails, giving you the full picture of what broke.

### Flake retry

`playwright.config.js` sets `retries: process.env.CI ? 1 : 0`. Since GitHub Actions sets `CI=true` automatically, every test gets one retry on failure. A test that fails the first time but passes on retry is annotated as `[flaky]` in the list output.

### Artifacts on failure

When a shard fails:

- **`test-results-html/`** — the full HTML report (uploaded on every run via `if: always()`)
- **`test-results/`** — traces, videos, and screenshots (uploaded on failure via `if: failure()`)

Artifacts are retained for 7 days. Download them from the Actions run page under the "Artifacts" section.

### Concurrency

Runs on the same PR are cancelled automatically (`cancel-in-progress: true` for `pull_request` events). Pushes to `main` are not cancelled (they represent the final state).

### Paging

If the e2e suite fails in CI and you need help, ping the webui channel or open an issue referencing the failing Actions run URL.
