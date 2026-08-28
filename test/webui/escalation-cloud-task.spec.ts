// CLOUD-2: "Run as cloud task" escalation flow (Mode A)
//
// E2E coverage for the EscalationListener's Mode A path: when the browser IDE
// hits a blocking limitation, the toast offers to hand the work to the
// platform task queue (POST /api/tasks) and poll it inline (GET /api/tasks/{id})
// without leaving the IDE.
//
// The platform endpoints are stubbed with page.route (the Vite dev E2E
// environment has no Foundry platform). The escalation is driven by
// dispatching the `sprout:escalation-trigger` window event directly — the
// listener mounts unconditionally in App.tsx and reacts to blocking triggers
// regardless of cloud mode.
//
// NOTE on timing: the component's poll interval is a hardcoded module const
// (CLOUD_TASK_POLL_INTERVAL_MS = 3000) with the first poll delayed by one
// interval, and there is no override hook — so the "completed" assertions use
// generous timeouts instead (~6s of real polling: pending → running →
// completed). Do not shrink them.
//
// Run: npx playwright test --project=webui test/webui/escalation-cloud-task.spec.ts

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
  // mid-flight, leaving the button permanently "unstable" for clicks; the
  // component disables the animation under prefers-reduced-motion (see
  // EscalationToast.css), so emulate it here.
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

/** Task id used by every mock response in this spec. */
const TASK_ID = 'e2e-task-1';

/** Route globs — exact per-endpoint so app bootstrap traffic is untouched. */
const SUBMIT_ROUTE = '**/api/tasks';
const STATUS_ROUTE = `**/api/tasks/${TASK_ID}`;

/** Dismiss any toast left over from a previous test (serial page reuse). */
async function dismissToastIfOpen() {
  const dismiss = page.getByRole('button', { name: 'Dismiss' });
  if (await dismiss.isVisible().catch(() => false)) {
    await dismiss.click();
  }
}

/** Dispatch a blocking escalation trigger on the page. */
async function dispatchBlockingTrigger(id: string) {
  await page.evaluate(
    ({ id }) => {
      window.dispatchEvent(
        new CustomEvent('sprout:escalation-trigger', {
          detail: {
            id,
            reason: 'git_push_failed',
            severity: 'blocking',
            message: 'push failed',
            repoURL: 'https://github.com/example/repo',
          },
        }),
      );
    },
    { id },
  );
}

/** Captured POST /api/tasks requests, for payload assertions. */
interface CapturedPost {
  url: string;
  method: string;
  contentType: string;
  body: any;
}

test.describe('Escalation — Run as cloud task (CLOUD-2)', () => {
  test('submit → poll → completed renders inline progress and task link', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });

    const posts: CapturedPost[] = [];
    let statusCalls = 0;

    // Stub the platform BEFORE driving the flow.
    await page.route(SUBMIT_ROUTE, async (route) => {
      if (route.request().method() !== 'POST') {
        return route.fallback();
      }
      posts.push({
        url: route.request().url(),
        method: route.request().method(),
        contentType: route.request().headers()['content-type'] ?? '',
        body: route.request().postDataJSON(),
      });
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        headers: { 'X-Remaining-Task-Credits': '4' },
        // task_id is mandatory — getCloudTask/submitCloudTask validate it.
        body: JSON.stringify({ task_id: TASK_ID, status: 'pending' }),
      });
    });

    await page.route(STATUS_ROUTE, async (route) => {
      statusCalls += 1;
      const status = statusCalls <= 2 ? 'running' : 'completed';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ task_id: TASK_ID, status }),
      });
    });

    // Fire the browser-limitation trigger the toast listens for.
    await dispatchBlockingTrigger('e2e-1');

    const cloudTaskButton = page.getByTestId(TESTIDS['escalation-toast-cloud-task']);
    await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });

    // The Mode B escape hatch stays available alongside Mode A.
    await expect(page.getByRole('button', { name: 'Start Full Workspace' })).toBeVisible({ timeout: 5_000 });

    await cloudTaskButton.click();

    // Progress renders immediately from the submit response — no poll tick
    // needed to see it.
    const statusLine = page.getByTestId(TESTIDS['escalation-toast-cloud-task-status']);
    await expect(statusLine).toBeVisible({ timeout: 10_000 });
    await expect(statusLine).toHaveText(/Cloud task pending/);

    // Submit payload: repo + derived prompt carrying the escalation reason.
    await expect.poll(() => posts.length).toBe(1);
    expect(posts[0].method).toBe('POST');
    expect(new URL(posts[0].url).pathname).toBe('/api/tasks');
    expect(posts[0].contentType).toContain('application/json');
    expect(posts[0].body).toEqual({
      repo_url: 'https://github.com/example/repo',
      prompt: 'Continue building this repository. Escalation reason: git_push_failed.',
    });

    // Terminal state: status flips to completed and the platform link points
    // at the task. First poll lands at t≈3s, terminal at t≈6s.
    await expect(statusLine).toHaveText(/Cloud task completed/, { timeout: 20_000 });
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-link'])).toHaveAttribute(
      'href',
      `/tasks/${TASK_ID}`,
    );

    // Polling stopped at the terminal status (mock keeps returning
    // 'completed', so assert it was polled exactly 3 times: 2× running + 1×
    // completed).
    await page.waitForTimeout(3_500);
    await expect.poll(() => statusCalls, { timeout: 5_000 }).toBeLessThanOrEqual(3);

    await page.unroute(SUBMIT_ROUTE);
    await page.unroute(STATUS_ROUTE);
  });

  test('submit failure (402) surfaces the platform error message', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });
    await dismissToastIfOpen();

    await page.route(SUBMIT_ROUTE, async (route) => {
      if (route.request().method() !== 'POST') {
        return route.fallback();
      }
      await route.fulfill({
        status: 402,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'No task credits remaining' }),
      });
    });

    await dispatchBlockingTrigger('e2e-402');

    const cloudTaskButton = page.getByTestId(TESTIDS['escalation-toast-cloud-task']);
    await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });

    await cloudTaskButton.click();

    // The platform's error body is shown verbatim, and no task link is
    // rendered (the submit never produced a task id).
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-error'])).toHaveText(
      'No task credits remaining',
      { timeout: 10_000 },
    );
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-link'])).toHaveCount(0);

    // Mode B is still the way out when Mode A is exhausted.
    await expect(page.getByRole('button', { name: 'Start Full Workspace' })).toBeVisible({ timeout: 5_000 });

    await page.unroute(SUBMIT_ROUTE);
  });
});
