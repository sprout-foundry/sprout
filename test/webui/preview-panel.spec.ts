// Code-mode preview panel e2e spec.
//
// The preview panel is driven by the dev-server lifecycle API. This spec pins
// the wiring that only a real stack can: the HeaderBar toggle opens the panel
// in the Code surface, the pane renders from the (fresh) backend's lifecycle
// state, and the close affordance collapses the panel.
//
// Why this stack: `startSprout()` boots a real sprout backend with a FRESH
// temp workspace (no dev command, no running dev server), so
// `/api/preview/status` deterministically reports `stopped` — the pane's
// "Preview is stopped." affordance is a stable assertion that the panel is
// wired to the live lifecycle API (not a static mock).

import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import {
  startSprout,
  pickFreePort,
  type SproutHandle,
} from "./fixtures/sprout";
import { startViteDevServer, type ViteHandle } from "./fixtures/vite";
import { newWebuiPage, type WebUIPageHandle } from "./fixtures/page";
import TESTIDS from "./testids";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

test.beforeAll(async () => {
  // System Chrome fallback for dev machines without the Playwright download
  // (AGENTS.md e2e convention).
  try {
    browser = await chromium.launch({ channel: "chrome" });
  } catch {
    browser = await chromium.launch();
  }
  sprout = await startSprout();
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  handle = await newWebuiPage({ browser, url: vite.url });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: "serial" });
test.setTimeout(90_000);

const toggle = () => page.getByTestId(TESTIDS["preview-panel-toggle"]);
const panel = () => page.getByTestId(TESTIDS["preview-panel"]);
const pane = () => page.getByTestId(TESTIDS["preview-pane"]);

/** Click the toggle only if the panel is not already open (serial tests share the page). */
async function openPanel(): Promise<void> {
  if (!(await pane().isVisible())) {
    await toggle().click();
  }
  await expect(pane()).toBeVisible();
}

test.describe("Preview panel", () => {
  test("the Code-mode HeaderBar exposes the preview toggle, with the panel closed by default", async () => {
    await page.goto(vite.url, { waitUntil: "networkidle" });

    // A fresh workspace has no design/ tree, so Code is the mode; its
    // HeaderBar carries the preview toggle.
    await expect(page.getByTestId(TESTIDS["chat-shell"])).toBeVisible({
      timeout: 30_000,
    });
    await expect(toggle()).toBeVisible({ timeout: 30_000 });

    // Closed by default: neither the panel nor its pane is rendered.
    await expect(panel()).toHaveCount(0);
    await expect(pane()).toHaveCount(0);
    await expect(toggle()).toHaveAttribute("aria-pressed", "false");
  });

  test("clicking the toggle opens the preview panel (the pane renders in the Code surface)", async () => {
    await openPanel();
    await expect(panel()).toBeVisible();
    await expect(toggle()).toHaveAttribute("aria-pressed", "true");
  });

  test("a fresh workspace settles the pane in its stopped state (wired to the live API)", async () => {
    await openPanel();
    // The pane polls /api/preview/status; with no dev server the backend
    // reports `stopped`, so the stopped affordance and status label show.
    await expect(page.getByTestId(TESTIDS["preview-pane-stopped"])).toBeVisible(
      { timeout: 30_000 },
    );
    await expect(page.getByTestId(TESTIDS["preview-pane-status"])).toHaveText(
      "Stopped",
    );
  });

  test("the close affordance collapses the panel", async () => {
    await openPanel();
    await page.getByTestId(TESTIDS["preview-pane-close"]).click();
    await expect(pane()).toHaveCount(0);
    await expect(panel()).toHaveCount(0);
    await expect(toggle()).toHaveAttribute("aria-pressed", "false");
  });
});

// ---------------------------------------------------------------------------
// Fixture starter project + local dev server (acceptance:
// "Preview pane shows a local dev server for a starter project (e2e)").
//
// A second, independent stack boots the real backend over a fixture starter
// project: a .sprout/starter.json whose `dev` command is a
// plain node dev server on a known `dev_port`. The pane's action starts the
// dev server through the backend's preview manager, and the pane
// settles into `running` with the iframe embedding the localhost URL.
// ---------------------------------------------------------------------------
test.describe("Preview pane with a fixture starter project (local dev server)", () => {
  let fixtureBrowser: Browser;
  let sproutFixture: SproutHandle;
  let viteFixture: ViteHandle;
  let handleFixture: WebUIPageHandle;
  let pageFixture: Page;
  let devPort = 0;
  let fixtureDir = "";

  const toggleFixture = () =>
    pageFixture.getByTestId(TESTIDS["preview-panel-toggle"]);
  const paneFixture = () => pageFixture.getByTestId(TESTIDS["preview-pane"]);
  const statusFixture = () =>
    pageFixture.getByTestId(TESTIDS["preview-pane-status"]);
  const iframeFixture = () =>
    pageFixture.getByTestId(TESTIDS["preview-pane-iframe"]);

  async function openFixturePanel(): Promise<void> {
    if (!(await paneFixture().isVisible())) {
      await toggleFixture().click();
    }
    await expect(paneFixture()).toBeVisible();
  }

  test.beforeAll(async () => {
    // System Chrome fallback for dev machines without the Playwright download
    // (AGENTS.md e2e convention).
    try {
      fixtureBrowser = await chromium.launch({ channel: "chrome" });
    } catch {
      fixtureBrowser = await chromium.launch();
    }

    // The fixture starter project: a free dev port, a node dev server
    // serving a marker page, and a manifest naming that command + port.
    devPort = await pickFreePort();
    fixtureDir = fs.mkdtempSync(
      path.join(os.tmpdir(), "sprout-preview-fixture-"),
    );
    fs.mkdirSync(path.join(fixtureDir, ".sprout"), { recursive: true });
    fs.writeFileSync(
      path.join(fixtureDir, ".sprout", "starter.json"),
      JSON.stringify(
        {
          starter: { id: "fixture-e2e", version: "1.0.0" },
          dev: "node dev-server.mjs",
          dev_port: devPort,
          routes: ["/"],
        },
        null,
        2,
      ),
      "utf8",
    );
    fs.writeFileSync(
      path.join(fixtureDir, "dev-server.mjs"),
      [
        `import { createServer } from 'node:http';`,
        `const html = '<!doctype html><html><head><title>Fixture E2E</title></head><body><h1 id="fixture-marker">fixture dev server is up</h1></body></html>';`,
        `createServer((req, res) => { res.writeHead(200, { 'content-type': 'text/html' }); res.end(html); }).listen(${devPort});`,
        ``,
      ].join("\n"),
      "utf8",
    );
    // One project source file so the workspace is a project, not a shell.
    fs.writeFileSync(
      path.join(fixtureDir, "index.html"),
      "<!doctype html><html><body>fixture</body></html>\n",
      "utf8",
    );

    sproutFixture = await startSprout({ workspaceDir: fixtureDir });
    viteFixture = await startViteDevServer({
      sproutBackendUrl: sproutFixture.baseUrl,
    });
    handleFixture = await newWebuiPage({
      browser: fixtureBrowser,
      url: viteFixture.url,
    });
    pageFixture = handleFixture.page;
  });

  test.afterAll(async () => {
    // Stop the dev server the backend started; its own Shutdown would also
    // drain the preview managers, but the explicit stop keeps teardown fast.
    try {
      if (sproutFixture) {
        await fetch(`${sproutFixture.baseUrl}/api/preview/stop`, {
          method: "POST",
        });
      }
    } catch {
      // best-effort teardown
    }
    await handleFixture?.cleanup();
    await fixtureBrowser?.close();
    await viteFixture?.stop();
    await sproutFixture?.stop();
    // startSprout keeps a caller-provided workspace dir; remove ours.
    if (fixtureDir) {
      fs.rmSync(fixtureDir, { recursive: true, force: true });
      fixtureDir = "";
    }
  });

  test("starts the fixture dev server and embeds it in the pane", async () => {
    await expect(pageFixture.getByTestId(TESTIDS["chat-shell"])).toBeVisible({
      timeout: 30_000,
    });
    await openFixturePanel();
    await expect(statusFixture()).toHaveText("Stopped", { timeout: 30_000 });

    // The pane's action starts (or restarts) the dev server; the manager
    // spawns the manifest's dev command and the port settles within the
    // ready timeout.
    await pageFixture.getByTestId(TESTIDS["preview-pane-restart"]).click();
    await expect(statusFixture()).toHaveText("Running", { timeout: 60_000 });
    await expect(iframeFixture()).toHaveAttribute(
      "src",
      `http://localhost:${devPort}`,
    );

    // The fixture page is actually served on the embedded origin.
    const res = await fetch(`http://127.0.0.1:${devPort}/`, {
      signal: AbortSignal.timeout(5_000),
    });
    expect(res.status).toBe(200);
    expect(await res.text()).toContain("fixture-marker");
  });

  test("stopping the dev server settles the pane back to stopped", async () => {
    const res = await fetch(`${sproutFixture.baseUrl}/api/preview/stop`, {
      method: "POST",
    });
    expect(res.status).toBe(200);
    // The pane's heartbeat (5s while running) picks up the settled state.
    await expect(statusFixture()).toHaveText("Stopped", { timeout: 30_000 });
  });
});
