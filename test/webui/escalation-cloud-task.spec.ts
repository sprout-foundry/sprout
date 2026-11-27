// CLOUD-2: "Run as cloud task" escalation flow (Mode A)
//
// E2E coverage for the EscalationListener's Mode A path: when the browser IDE
// hits a blocking limitation, the toast offers to hand the work to the
// platform task queue (POST /api/tasks) and poll it inline (GET /api/tasks/{id})
// without leaving the IDE.
//
// CONFIRM STEP: clicking "Run as cloud task" opens a confirm step instead of
// submitting. The step holds a prefilled prompt box (testid
// `escalation-cloud-task-prompt`, holding `deriveEscalationPrompt`), a
// "Start task" button (`escalation-cloud-task-start`) that posts exactly the
// text in the box, and a "Back" button (`escalation-cloud-task-back`) that
// returns to the toast without posting anything. The three cases below cover
// all three guarantees: nothing posts before "Start task", the edit is what
// gets posted, and "Back" posts nothing at all.
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
import { existsSync } from 'node:fs';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import TESTIDS from './testids';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

// Where the machine has no Playwright browser download (the repo's dev
// machines, and this sandbox, which ships only /usr/bin/chromium), the
// two-step AGENTS.md chain has nothing left to fall back to. SPROUT_E2E_CHROMIUM
// names that system browser; without it the chain stands as mandated.
function systemChromiumPath(): string | undefined {
  const fromEnv = process.env.SPROUT_E2E_CHROMIUM;
  if (fromEnv) return fromEnv;
  return existsSync('/usr/bin/chromium') ? '/usr/bin/chromium' : undefined;
}

test.beforeAll(async () => {
  const localChromium = systemChromiumPath();
  browser = await chromium
    .launch({ channel: 'chrome' })
    .catch(() => chromium.launch())
    .catch(() => {
      if (!localChromium) {
        throw new Error('no browser: no system Chrome, no Playwright download, and no SPROUT_E2E_CHROMIUM path');
      }
      return chromium.launch({ channel: 'chromium', executablePath: localChromium });
    });
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
const LIST_ROUTE = '**/api/tasks?*';

/** The prompt `deriveEscalationPrompt` builds for this spec's trigger. */
const DERIVED_PROMPT =
  'Continue building this repository. Pushing from the browser failed; commit and push the current changes.';

/** What the user types into the confirm step before starting the task. */
const EDITED_PROMPT = 'Continue building this repository, but run the full test suite first.';

/** The confirm step's own testids (see the header note). */
const PROMPT_TESTID = TESTIDS['escalation-cloud-task-prompt'];
const START_TESTID = TESTIDS['escalation-cloud-task-start'];
const BACK_TESTID = TESTIDS['escalation-cloud-task-back'];

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

/**
 * Stub POST /api/tasks, capturing every submit. Returns a promise-free handle
 * to the captured array so callers can assert on it as requests land.
 */
async function stubTaskSubmit(posts: CapturedPost[]) {
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
}

/** Stub a failing submit with the given status and error body. */
async function stubTaskSubmitFailure(status: number, error: string) {
  await page.route(SUBMIT_ROUTE, async (route) => {
    if (route.request().method() !== 'POST') {
      return route.fallback();
    }
    await route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify({ error }),
    });
  });
}

/** Rows seen by the task-list endpoint. The flow never reads it, so an empty
 *  list is the honest answer for a task that was never submitted. */
async function stubTaskList() {
  await page.route(LIST_ROUTE, async (route) => {
    if (route.request().method() !== 'GET') {
      return route.fallback();
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tasks: [] }) });
  });
}

/**
 * The confirm step as a unit: opening it posts nothing, "Start task" posts
 * exactly what the prompt box holds, and "Back" posts nothing. Split out so
 * each case reads as the one guarantee it checks.
 */
async function openConfirmStep() {
  const cloudTaskButton = page.getByTestId(TESTIDS['escalation-toast-cloud-task']);
  await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });
  await cloudTaskButton.click();

  const prompt = page.getByTestId(PROMPT_TESTID);
  await expect(prompt).toBeVisible({ timeout: 10_000 });
  return prompt;
}

test.describe('Escalation — Run as cloud task (CLOUD-2)', () => {
  test('confirm step posts the edited prompt, then polls to completed', async () => {
    await page.goto(vite.url, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });

    const posts: CapturedPost[] = [];
    let statusCalls = 0;

    // Stub the platform BEFORE driving the flow.
    await stubTaskSubmit(posts);

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
    await expect(page.getByRole('button', { name: 'Start a Dev Space' })).toBeVisible({ timeout: 5_000 });

    // Opening the step is a prompt, not a submission: nothing is posted and
    // the progress view has not started.
    await cloudTaskButton.click();

    const prompt = page.getByTestId(PROMPT_TESTID);
    await expect(prompt).toBeVisible({ timeout: 10_000 });
    expect(posts).toHaveLength(0);
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-status'])).toHaveCount(0);

    // The box arrives prefilled with the prompt derived from the trigger, so
    // the common case is one click.
    await expect(prompt).toHaveValue(DERIVED_PROMPT);

    // The user edits it; the edited text is what the platform receives.
    await prompt.fill(EDITED_PROMPT);
    await page.getByTestId(START_TESTID).click();

    // Progress renders immediately from the submit response — no poll tick
    // needed to see it.
    const statusLine = page.getByTestId(TESTIDS['escalation-toast-cloud-task-status']);
    await expect(statusLine).toBeVisible({ timeout: 10_000 });
    await expect(statusLine).toHaveText(/Cloud task pending/);

    // Submit payload: repo + exactly the edited prompt. One POST only — the
    // click that started it spent itself here.
    await expect.poll(() => posts.length, { timeout: 10_000 }).toBe(1);
    expect(posts[0].method).toBe('POST');
    expect(new URL(posts[0].url).pathname).toBe('/api/tasks');
    expect(posts[0].contentType).toContain('application/json');
    expect(posts[0].body).toEqual({
      repo_url: 'https://github.com/example/repo',
      prompt: EDITED_PROMPT,
    });

    // Terminal state: status flips to completed and the platform link points
    // at the task. First poll lands at t≈3s, terminal at t≈6s.
    await expect(statusLine).toHaveText(/Cloud task completed/, { timeout: 20_000 });
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-link'])).toHaveAttribute(
      'href',
      `/#/tasks/${TASK_ID}`,
    );

    // Polling stopped at the terminal status (mock keeps returning
    // 'completed', so assert it was polled exactly 3 times: 2× running + 1×
    // completed). The same wait settles the submit count: one POST, never a
    // retry.
    await page.waitForTimeout(3_500);
    await expect.poll(() => statusCalls, { timeout: 5_000 }).toBeLessThanOrEqual(3);
    expect(posts).toHaveLength(1);

    await page.unroute(SUBMIT_ROUTE);
    await page.unroute(STATUS_ROUTE);
  });

  test('"Back" closes the confirm step and posts nothing', async () => {
    await page.goto(vite.url, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });
    await dismissToastIfOpen();

    const posts: CapturedPost[] = [];
    await stubTaskSubmit(posts);
    // A submit would come back as a task-list row; an empty list plus the
    // statusline assertion below covers the second route the flow could take.
    await stubTaskList();

    await dispatchBlockingTrigger('e2e-back');

    const prompt = await openConfirmStep();
    await expect(prompt).toHaveValue(DERIVED_PROMPT);

    await page.getByTestId(BACK_TESTID).click();

    // Back undoes the step: it is gone, the cloud-task CTA is back, and the
    // platform was never called.
    await expect(page.getByTestId(PROMPT_TESTID)).toHaveCount(0, { timeout: 10_000 });
    const cloudTaskButton = page.getByTestId(TESTIDS['escalation-toast-cloud-task']);
    await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-status'])).toHaveCount(0);
    expect(posts).toHaveLength(0);

    // Nothing was posted, so there is no task to deep-link to.
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-link'])).toHaveCount(0);

    await page.unroute(SUBMIT_ROUTE);
    await page.unroute(LIST_ROUTE);
  });

  test('submit failure (402) surfaces the platform error message', async () => {
    await page.goto(vite.url, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });
    await dismissToastIfOpen();

    await stubTaskSubmitFailure(402, 'No task credits remaining');

    await dispatchBlockingTrigger('e2e-402');

    const prompt = await openConfirmStep();
    await expect(prompt).toHaveValue(DERIVED_PROMPT);

    // "Start task" is what fires the request (accepted-prompt case above);
    // only the platform's answer differs here.
    await page.getByTestId(START_TESTID).click();

    // The platform's error body is shown verbatim, and no task link is
    // rendered (the submit never produced a task id).
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-error'])).toHaveText(
      'No task credits remaining',
      { timeout: 10_000 },
    );
    await expect(page.getByTestId(TESTIDS['escalation-toast-cloud-task-link'])).toHaveCount(0);

    // Mode B is still the way out when Mode A is exhausted.
    await expect(page.getByRole('button', { name: 'Start a Dev Space' })).toBeVisible({ timeout: 5_000 });

    await page.unroute(SUBMIT_ROUTE);
  });
});
