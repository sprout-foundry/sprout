// ETH-2: "Run in cloud container" transactional escalation flow
//
// E2E coverage for the EscalationListener's ETH-2 action: when the browser
// shell cannot run a command (exit 127 — no compilers in the WASM sandbox),
// the toast's PRIMARY button runs it transactionally in the user's cloud
// workspace container: open txn → push browser deltas → run → pull container
// deltas back → finish (which stops the pay-per-run machine).
//
// The platform endpoints are stubbed with page.route (the Vite dev E2E
// environment has no Foundry platform). The escalation is driven by
// dispatching the `sprout:escalation-trigger` window event directly with a
// `command` payload — exactly what useEscalationTriggers produces for a 127
// exit (that translation is unit-tested in
// src/hooks/useEscalationTriggers.test.ts). The listener mounts
// unconditionally in App.tsx and reacts to blocking triggers regardless of
// cloud mode.
//
// NOTE: in the dev-stack (local mode) page the browser-git VFS bridge is not
// configured, so the push manifest is legitimately empty and pulled files are
// written to a no-op bridge — the observable assertions here are the pushed
// manifest body, the rendered run result and the "N files pulled back" line.
// The VFS write path itself is covered by EscalationListener.test.tsx.
//
// Run: npx playwright test --project=webui test/webui/escalation-txn.spec.ts

import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import TESTIDS from './testids';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

test.beforeAll(async () => {
  browser = await chromium.launch();
  sprout = await startSprout();
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  // Headless rAF throttling can freeze the toast's slide-in animation
  // mid-flight; the component disables it under prefers-reduced-motion.
  handle = await newWebuiPage({ browser, url: vite.url, reducedMotion: 'reduce' });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: 'serial' });
test.setTimeout(120_000);

const WORKSPACE_ID = 'e2e-ws-1';
const TXN_ID = 'e2e-txn-1';

/** Route globs — exact per-endpoint so app bootstrap traffic is untouched. */
const LIST_ROUTE = '**/workspace/fly';
const CREATE_ROUTE = '**/workspace/fly';
const TXN_OPEN_ROUTE = `**/workspace/fly/${WORKSPACE_ID}/txn`;
const TXN_ACTION_ROUTE = `**/workspace/fly/${WORKSPACE_ID}/txn/${TXN_ID}/*`;

/** Dismiss any toast left over from a previous test (serial page reuse). */
async function dismissToastIfOpen() {
  const dismiss = page.getByRole('button', { name: 'Dismiss' });
  if (await dismiss.isVisible().catch(() => false)) {
    await dismiss.click();
  }
}

/** Dispatch the blocking 127 escalation trigger on the page. */
async function dispatchUnavailableCommandTrigger(id: string, command = 'go build ./...') {
  await page.evaluate(
    ({ id, command }) => {
      window.dispatchEvent(
        new CustomEvent('sprout:escalation-trigger', {
          detail: {
            id,
            reason: 'command_unavailable_in_browser',
            severity: 'blocking',
            message: `“${command}” needs a real runtime. Run it in your cloud workspace (pay-per-run) or keep browsing.`,
            repoURL: 'https://github.com/example/repo',
            command,
          },
        }),
      );
    },
    { id, command },
  );
}

interface CapturedRequest {
  method: string;
  pathname: string;
  body: any;
}

test.describe('Escalation — Run in cloud container (ETH-2)', () => {
  test('open → push → run → pull → finish renders the result and stops the machine', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });

    const captured: CapturedRequest[] = [];
    let finishCalls = 0;

    // Workspace list: one matching workspace so no create is needed.
    await page.route(LIST_ROUTE, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      captured.push({ method: 'GET', pathname: '/workspace/fly', body: null });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          workspaces: [
            { workspace_id: WORKSPACE_ID, repo_url: 'https://github.com/example/repo', status: 'running' },
          ],
        }),
      });
    });

    await page.route(TXN_OPEN_ROUTE, async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      captured.push({ method: 'POST', pathname: '/workspace/fly/e2e-ws-1/txn', body: route.request().postDataJSON() });
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          txn_id: TXN_ID,
          status: 'push',
          expires_at: '2030-01-01T00:00:00Z',
          workspace_id: WORKSPACE_ID,
        }),
      });
    });

    await page.route(TXN_ACTION_ROUTE, async (route) => {
      const request = route.request();
      const action = new URL(request.url()).pathname.split('/').pop();
      captured.push({
        method: request.method(),
        pathname: new URL(request.url()).pathname,
        body: request.postDataJSON(),
      });
      if (action === 'push') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ applied: 0, deleted: 0, skipped: [], status: 'ok' }),
        });
      } else if (action === 'run') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            stdout: 'built main.go\n',
            stderr: '',
            exit_code: 0,
            duration_ms: 21400,
            timed_out: false,
            truncated: false,
          }),
        });
      } else if (action === 'pull') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          // One changed file pulled back into the browser VFS (binary-safe
          // base64 of "hello").
          body: JSON.stringify({
            base: { git_sha: '', client: 'container' },
            files: [{ path: 'main.go', content_base64: 'aGVsbG8=', size: 5, mode: '0644' }],
            deletes: [],
            truncated: false,
            skipped: [],
          }),
        });
      } else if (action === 'finish') {
        finishCalls += 1;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'done', txn_duration_seconds: 22, stop_initiated: true }),
        });
      } else {
        await route.fallback();
      }
    });

    await dispatchUnavailableCommandTrigger('e2e-txn-1');

    const txnButton = page.getByTestId(TESTIDS['escalation-toast-txn']);
    await expect(txnButton).toBeVisible({ timeout: 10_000 });
    await expect(txnButton).toHaveText(/Run in cloud container/);

    // The fallbacks stay available alongside the primary action.
    await expect(page.getByRole('button', { name: 'Run as cloud task' })).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: 'Start Full Workspace' })).toBeVisible({ timeout: 5_000 });

    await txnButton.click();

    // The full txn result renders: exit badge, duration, stdout, pulled files.
    const result = page.getByTestId(TESTIDS['escalation-toast-txn-result']);
    await expect(result).toBeVisible({ timeout: 20_000 });
    await expect(result).toHaveText(/exit 0/);
    await expect(result).toHaveText(/21.4s/);
    await expect(result).toHaveText(/built main\.go/);
    await expect(page.getByTestId(TESTIDS['escalation-toast-txn-pulled'])).toHaveText(/1 file pulled back/);

    // Lifecycle order: workspace list → open → push → run → pull → finish.
    const lifecycle = captured.map((c) => `${c.method} ${c.pathname}`).join('\n');
    expect(lifecycle).toContain(`GET /workspace/fly`);
    expect(lifecycle).toContain(`POST /workspace/fly/${WORKSPACE_ID}/txn`);
    expect(lifecycle).toContain(`POST /workspace/fly/${WORKSPACE_ID}/txn/${TXN_ID}/push`);
    expect(lifecycle).toContain(`POST /workspace/fly/${WORKSPACE_ID}/txn/${TXN_ID}/run`);
    expect(lifecycle).toContain(`POST /workspace/fly/${WORKSPACE_ID}/txn/${TXN_ID}/pull`);
    expect(lifecycle).toContain(`POST /workspace/fly/${WORKSPACE_ID}/txn/${TXN_ID}/finish`);

    // The pushed manifest is the pinned delta-manifest shape.
    const push = captured.find((c) => c.pathname.endsWith('/push'));
    expect(push?.body).toEqual({
      base: { git_sha: '', client: 'wasm' },
      files: [],
      deletes: [],
      truncated: false,
      skipped: [],
    });

    // The run carried the triggering command with the escalation timeout.
    const run = captured.find((c) => c.pathname.endsWith('/run'));
    expect(run?.body).toEqual({ command: 'go build ./...', timeout_seconds: 600 });

    // finish is the machine-stop guarantee — exactly once on the happy path.
    await expect
      .poll(() => finishCalls, { timeout: 10_000 })
      .toBe(1);

    await page.unroute(LIST_ROUTE);
    await page.unroute(TXN_OPEN_ROUTE);
    await page.unroute(TXN_ACTION_ROUTE);
  });

  test('409 (txn already open) renders the friendly busy message', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });
    await dismissToastIfOpen();

    await page.route(LIST_ROUTE, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          workspaces: [{ workspace_id: WORKSPACE_ID, repo_url: 'https://github.com/example/repo', status: 'running' }],
        }),
      });
    });

    await page.route(TXN_OPEN_ROUTE, async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'a transaction is already open for this workspace', txn_id: 'other' }),
      });
    });

    await dispatchUnavailableCommandTrigger('e2e-txn-409', 'cargo build');

    const txnButton = page.getByTestId(TESTIDS['escalation-toast-txn']);
    await expect(txnButton).toBeVisible({ timeout: 10_000 });
    await txnButton.click();

    await expect(page.getByTestId(TESTIDS['escalation-toast-txn-error'])).toHaveText(
      'another transaction is running, try again shortly',
      { timeout: 10_000 },
    );

    // Mode B is still the way out when the txn lane is busy.
    await expect(page.getByRole('button', { name: 'Start Full Workspace' })).toBeVisible({ timeout: 5_000 });

    await page.unroute(LIST_ROUTE);
    await page.unroute(TXN_OPEN_ROUTE);
  });
});
