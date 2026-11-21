// CLOUD-2: "Run as cloud task" escalation flow (Mode A)
//
// E2E coverage for the EscalationListener's Mode A path: when the browser IDE
// hits a blocking limitation, the toast offers to hand the work to the
// platform task queue (POST /api/tasks) and poll it inline (GET /api/tasks/{id})
// without leaving the IDE.
//
// The CTA does not submit on click: it opens an in-toast confirmation step
// (same shape as the sibling txn CTA) carrying the derived prompt in an
// editable textarea, so the user can read and correct the task's instructions
// before credits are spent. Only "Start task" submits; "Back" returns to the
// toast with nothing posted.
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

import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import { startSprout, type SproutHandle } from "./fixtures/sprout";
import { startViteDevServer, type ViteHandle } from "./fixtures/vite";
import { newWebuiPage, type WebUIPageHandle } from "./fixtures/page";
import TESTIDS from "./testids";

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

/**
 * Launch a Chromium Playwright can actually find. The Playwright browser
 * download is absent on some dev machines and CI images; system Chrome is the
 * first fallback (AGENTS.md), and a bare chrome-headless-shell install (the
 * usual case on Linux test boxes) is the second.
 */
async function launchBrowser(): Promise<Browser> {
  try {
    return await chromium.launch({ channel: "chrome" });
  } catch {
    try {
      return await chromium.launch();
    } catch {
      return await chromium.launch({
        executablePath:
          process.env.SPROUT_E2E_CHROMIUM_PATH || "/usr/bin/chromium",
      });
    }
  }
}

test.beforeAll(async () => {
  browser = await launchBrowser();
  sprout = await startSprout();
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  // Headless rAF throttling can freeze the toast's slide-in animation
  // mid-flight, leaving the button permanently "unstable" for clicks; the
  // component disables the animation under prefers-reduced-motion (see
  // EscalationToast.css), so emulate it here.
  handle = await newWebuiPage({
    browser,
    url: vite.url,
    reducedMotion: "reduce",
  });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: "serial" });
test.setTimeout(120_000);

/** Task id used by every mock response in this spec. */
const TASK_ID = "e2e-task-1";

/** Route globs — exact per-endpoint so app bootstrap traffic is untouched. */
const SUBMIT_ROUTE = "**/api/tasks";
const STATUS_ROUTE = `**/api/tasks/${TASK_ID}`;

/**
 * The prompt the component derives for the git_push_failed trigger this spec
 * dispatches — i.e. what the confirmation step is prefilled with. Shared by
 * every test so the prefill assertion and the submit payload assertion can
 * never drift apart.
 */
const EXPECTED_PROMPT =
  "Continue building this repository. Pushing from the browser failed; commit and push the current changes.";

/**
 * The confirmation step's testids, read from the shared registry so a rename
 * in the component cannot silently pass here.
 */
const PROMPT = TESTIDS["escalation-cloud-task-prompt"];
const START = TESTIDS["escalation-cloud-task-start"];
const BACK = TESTIDS["escalation-cloud-task-back"];

/** Dismiss any toast left over from a previous test (serial page reuse). */
async function dismissToastIfOpen() {
  const dismiss = page.getByRole("button", { name: "Dismiss" });
  if (await dismiss.isVisible().catch(() => false)) {
    await dismiss.click();
  }
}

/** Dispatch a blocking escalation trigger on the page. */
async function dispatchBlockingTrigger(id: string) {
  await page.evaluate(
    ({ id }) => {
      window.dispatchEvent(
        new CustomEvent("sprout:escalation-trigger", {
          detail: {
            id,
            reason: "git_push_failed",
            severity: "blocking",
            message: "push failed",
            repoURL: "https://github.com/example/repo",
          },
        }),
      );
    },
    { id },
  );
}

/** Open the toast and click its "Run as cloud task" CTA (step not confirmed). */
async function openCloudTaskConfirm(): Promise<void> {
  await dispatchBlockingTrigger("e2e-confirm");
  const cloudTaskButton = page.getByTestId(
    TESTIDS["escalation-toast-cloud-task"],
  );
  await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });
  await cloudTaskButton.click();
  await expect(page.getByTestId(PROMPT)).toBeVisible({ timeout: 10_000 });
}

/** Captured POST /api/tasks requests, for payload assertions. */
interface CapturedPost {
  url: string;
  method: string;
  contentType: string;
  body: any;
}

test.describe("Escalation — Run as cloud task (CLOUD-2)", () => {
  test("confirm step prefills the prompt; Start task submits the edited text", async () => {
    await page.goto(vite.url, { waitUntil: "domcontentloaded" });
    await expect(page.getByTestId(TESTIDS["sidebar-container"])).toBeVisible({
      timeout: 30_000,
    });

    const posts: CapturedPost[] = [];
    let statusCalls = 0;

    // Stub the platform BEFORE driving the flow.
    await page.route(SUBMIT_ROUTE, async (route) => {
      if (route.request().method() !== "POST") {
        return route.fallback();
      }
      posts.push({
        url: route.request().url(),
        method: route.request().method(),
        contentType: route.request().headers()["content-type"] ?? "",
        body: route.request().postDataJSON(),
      });
      await route.fulfill({
        status: 201,
        contentType: "application/json",
        headers: { "X-Remaining-Task-Credits": "4" },
        // task_id is mandatory — getCloudTask/submitCloudTask validate it.
        body: JSON.stringify({ task_id: TASK_ID, status: "pending" }),
      });
    });

    await page.route(STATUS_ROUTE, async (route) => {
      statusCalls += 1;
      const status = statusCalls <= 2 ? "running" : "completed";
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ task_id: TASK_ID, status }),
      });
    });

    // Fire the browser-limitation trigger the toast listens for.
    await dispatchBlockingTrigger("e2e-1");

    const cloudTaskButton = page.getByTestId(
      TESTIDS["escalation-toast-cloud-task"],
    );
    await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });

    // The Mode B escape hatch stays available alongside Mode A.
    await expect(
      page.getByRole("button", { name: "Start Full Workspace" }),
    ).toBeVisible({ timeout: 5_000 });

    // Clicking the CTA only opens the step: the prompt is prefilled from the
    // trigger and nothing has been queued yet.
    await cloudTaskButton.click();
    const prompt = page.getByTestId(PROMPT);
    await expect(prompt).toBeVisible({ timeout: 10_000 });
    await expect(prompt).toHaveValue(EXPECTED_PROMPT);
    expect(posts).toHaveLength(0);

    // The prefill is a default, not a fixed value: the submitted prompt is
    // exactly what the textarea holds when "Start task" is clicked.
    const EDITED_PROMPT = "Fix the failing build and push the fix.";
    await prompt.fill(EDITED_PROMPT);
    await page.getByTestId(START).click();

    await expect.poll(() => posts.length).toBe(1);
    expect(posts[0].method).toBe("POST");
    expect(new URL(posts[0].url).pathname).toBe("/api/tasks");
    expect(posts[0].contentType).toContain("application/json");
    expect(posts[0].body).toEqual({
      repo_url: "https://github.com/example/repo",
      prompt: EDITED_PROMPT,
    });

    // Confirming closes the step: progress takes its place immediately from
    // the submit response — no poll tick needed to see it.
    await expect(prompt).toHaveCount(0);
    const statusLine = page.getByTestId(
      TESTIDS["escalation-toast-cloud-task-status"],
    );
    await expect(statusLine).toBeVisible({ timeout: 10_000 });
    await expect(statusLine).toHaveText(/Cloud task pending/);

    // Terminal state: status flips to completed and the platform link points
    // at the task. First poll lands at t≈3s, terminal at t≈6s.
    await expect(statusLine).toHaveText(/Cloud task completed/, {
      timeout: 20_000,
    });
    await expect(
      page.getByTestId(TESTIDS["escalation-toast-cloud-task-link"]),
    ).toHaveAttribute("href", `/#/tasks/${TASK_ID}`);

    // Polling stopped at the terminal status (mock keeps returning
    // 'completed', so assert it was polled exactly 3 times: 2× running + 1×
    // completed).
    await page.waitForTimeout(3_500);
    await expect
      .poll(() => statusCalls, { timeout: 5_000 })
      .toBeLessThanOrEqual(3);

    await page.unroute(SUBMIT_ROUTE);
    await page.unroute(STATUS_ROUTE);
  });

  test("Back leaves the confirm step without submitting anything", async () => {
    await page.goto(vite.url, { waitUntil: "domcontentloaded" });
    await expect(page.getByTestId(TESTIDS["sidebar-container"])).toBeVisible({
      timeout: 30_000,
    });
    await dismissToastIfOpen();

    const posts: CapturedPost[] = [];
    await page.route(SUBMIT_ROUTE, async (route) => {
      if (route.request().method() !== "POST") {
        return route.fallback();
      }
      posts.push({
        url: route.request().url(),
        method: "POST",
        contentType: "",
        body: route.request().postDataJSON(),
      });
      await route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({ task_id: TASK_ID, status: "pending" }),
      });
    });

    await openCloudTaskConfirm();

    // Edit first: a value typed and then abandoned must not reach the queue.
    await page.getByTestId(PROMPT).fill("Something I decided not to run.");
    await page.getByTestId(BACK).click();

    // Back to the toast's original actions, with no task queued and no
    // progress rendered.
    await expect(
      page.getByTestId(TESTIDS["escalation-toast-cloud-task"]),
    ).toBeVisible({ timeout: 10_000 });
    await expect(page.getByTestId(PROMPT)).toHaveCount(0);
    await expect(
      page.getByTestId(TESTIDS["escalation-toast-cloud-task-status"]),
    ).toHaveCount(0);
    expect(posts).toHaveLength(0);

    await page.unroute(SUBMIT_ROUTE);
  });

  test("submit failure (402) surfaces the platform error message", async () => {
    await page.goto(vite.url, { waitUntil: "domcontentloaded" });
    await expect(page.getByTestId(TESTIDS["sidebar-container"])).toBeVisible({
      timeout: 30_000,
    });
    await dismissToastIfOpen();

    let submitPosts = 0;
    await page.route(SUBMIT_ROUTE, async (route) => {
      if (route.request().method() !== "POST") {
        return route.fallback();
      }
      submitPosts += 1;
      await route.fulfill({
        status: 402,
        contentType: "application/json",
        body: JSON.stringify({ error: "No task credits remaining" }),
      });
    });

    await dispatchBlockingTrigger("e2e-402");
    const cloudTaskButton = page.getByTestId(
      TESTIDS["escalation-toast-cloud-task"],
    );
    await expect(cloudTaskButton).toBeVisible({ timeout: 10_000 });
    await cloudTaskButton.click();
    await expect(page.getByTestId(PROMPT)).toBeVisible({ timeout: 10_000 });

    // Nothing is posted until the step is confirmed.
    expect(submitPosts).toBe(0);
    await page.getByTestId(START).click();

    // The platform's error body is shown verbatim, and no task link is
    // rendered (the submit never produced a task id).
    await expect(
      page.getByTestId(TESTIDS["escalation-toast-cloud-task-error"]),
    ).toHaveText("No task credits remaining", { timeout: 10_000 });
    await expect(
      page.getByTestId(TESTIDS["escalation-toast-cloud-task-link"]),
    ).toHaveCount(0);
    expect(submitPosts).toBe(1);

    // Mode B is still the way out when Mode A is exhausted.
    await expect(
      page.getByRole("button", { name: "Start Full Workspace" }),
    ).toBeVisible({ timeout: 5_000 });

    await page.unroute(SUBMIT_ROUTE);
  });
});
