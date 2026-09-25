// SP-143 143.7 — the screen kit end-to-end.
//
// The repo's own design tree IS the fixture: the dogfood phone pair
// (mobile-sessions <-> mobile-session) authored per the 143.6 kit contract
// with the runtime, tokens.css, and chrome.css all referenced relatively and
// rewritten through the /api/file proxy by the workbench preview (143.4).
//
// What this spec pins:
//  - iPhone chrome from runtime/chrome.css renders at the declared `mobile`
//    frame (390x844) with the phone screen selected in the workbench.
//  - Theming flows from generated/tokens.css through the proxy: a themed
//    element's computed color equals the token's resolved value (not the
//    browser default).
//  - data-nav anchors swap screens IN PLACE inside the preview iframe (the
//    stem marker moves; the iframe document is not reloaded) and back works.
//  - The preview-gated state switcher toggles declared states.
//  - A token edit + export restyles the screen with ZERO screen-file writes
//    (bytes and mtime unchanged) — the kit's core promise.
//
// Like design_view.spec.ts, this spec starts its own stack: the shared
// Playwright webServer boots a temp workspace without this repo's design
// tree, so the backend is pointed at the repo root itself (a caller-supplied
// workspace is never deleted by the fixture; the token-edit scenario
// restores the token file in afterAll so the tree is left clean).

import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync, statSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { newWebuiPage, type WebUIPageHandle } from "./fixtures/page";
import { startSprout, type SproutHandle } from "./fixtures/sprout";
import { startViteDevServer, type ViteHandle } from "./fixtures/vite";

const REPO_ROOT = resolve(__dirname, "..", "..");
const SCREEN = "design/screens/mobile-sessions.html";
const SCREEN_DETAIL = "design/screens/mobile-session.html";
const TOKEN_FILE = "design/tokens/color.tokens.json";
const GENERATED_CSS = "design/generated/tokens.css";

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

/** The preview iframe inside the workbench render facet (scoped — the grid
 *  thumbnails are srcdoc iframes too). */
function workbenchFrameLocator() {
  return page
    .getByTestId("design-workbench-render")
    .locator(".live-preview-iframe")
    .first();
}

async function workbenchFrame(): Promise<Page["frames"][number] | null> {
  return (await workbenchFrameLocator().contentFrame()) ?? null;
}

/** The preview iframe, throwing when absent (non-poll contexts). */
async function previewFrame(): Promise<Page["frames"][number]> {
  // elementHandle().contentFrame(): a live Frame handle for the workbench's
  // iframe (Locator.contentFrame() yields a FrameLocator without evaluate).
  const handle = await workbenchFrameLocator().elementHandle();
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error("workbench preview iframe not found");
  return frame;
}

/**
 * The current boot-stem marker, or '' while the preview iframe is remounting
 * (a history pop can briefly replace the frame object). Poll-tolerant.
 */
async function stemNow(): Promise<string> {
  try {
    const frame = await previewFrame();
    return (
      (await frame.locator("html").getAttribute("data-sprout-screen-stem")) ??
      ""
    );
  } catch {
    return "";
  }
}

/** The wireframe stem of a screen file path. */
function stemOf(screenFile: string): string {
  return screenFile
    .split("/")
    .pop()!
    .replace(/\.html$/, "");
}

/** Read the resolved token values the css target derived (tokens.json). */
function resolvedTokens(): Record<string, string> {
  const parsed = JSON.parse(
    readFileSync(resolve(REPO_ROOT, "design/generated/tokens.json"), "utf8"),
  );
  return parsed.tokens as Record<string, string>;
}

/** The live preview: select the screens section, then a screen, if needed. */
async function openWorkbenchOn(screenFile: string): Promise<void> {
  await page.goto(vite.url, { waitUntil: "networkidle" });
  const designView = page.getByTestId("design-view");
  const chatShell = page.getByTestId("chat-shell");
  await expect(designView.or(chatShell).first()).toBeVisible({
    timeout: 30_000,
  });
  if (!(await designView.isVisible())) {
    const trigger = page.getByTestId("sidebar-brand-trigger");
    await expect(trigger).toBeVisible({ timeout: 30_000 });
    await trigger.click();
    const designOption = page.getByTestId("sidebar-brand-option-design");
    await expect(designOption).toBeVisible({ timeout: 30_000 });
    await designOption.click();
  }
  await expect(designView).toBeVisible({ timeout: 30_000 });

  // The rail's per-screen button selects the screen (the wireframe asset;
  // with §8b render-first the workbench renders the HTML screen for the
  // stem). The section entry first ensures the Screens section is active so
  // the sidebar pane and workbench agree.
  const screensEntry = page.getByTestId("design-rail-screens-section");
  if (await screensEntry.count()) {
    await screensEntry.click();
  }
  const screenBtn = page.getByTestId(
    `design-rail-screen-${stemOf(screenFile)}`,
  );
  await expect(screenBtn).toBeVisible({ timeout: 15_000 });
  await screenBtn.click();
  // Fallback for a tree shape without the rail (grid card click).
  if (
    !(await page
      .getByTestId("design-workbench")
      .isVisible()
      .catch(() => false))
  ) {
    await page
      .getByTestId("design-rail-screens-section")
      .click()
      .catch(() => {});
    const card = page.locator(
      `[data-testid="design-screen-card-mobile-sessions"]`,
    );
    if (await card.count()) await card.first().click();
  }

  await expect(page.getByTestId("design-workbench")).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByTestId("design-workbench-render")).toBeVisible({
    timeout: 30_000,
  });
  const frameEl = page.locator(".live-preview-iframe").first();
  await expect(frameEl).toBeVisible({ timeout: 30_000 });
  // The boot stem marker before any interaction (runtime booted).
  await expect.poll(stemNow, { timeout: 15_000 }).toBe(stemOf(screenFile));
}

test.beforeAll(async () => {
  try {
    browser = await chromium.launch({ channel: "chrome" });
  } catch {
    browser = await chromium.launch();
  }
  sprout = await startSprout({ workspaceDir: REPO_ROOT });
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  handle = await newWebuiPage({ browser, url: vite.url });
  page = handle.page;
  // /api/file answers with Cache-Control: no-cache and conditional 304s, but
  // the token-edit scenario must still observe the REGENERATED css on
  // reload. Disabling the cache keeps every proxy fetch honest without
  // weakening the assertion (the theming still flows through /api/file).
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.setCacheDisabled", { cacheDisabled: true });
});

// The token-edit scenario restores the token file (bytes AND regenerated
// css) so the repo tree is byte-clean after the run.
let tokenBackup: string | undefined;
let cssBackup: string | undefined;

test.afterAll(async () => {
  if (tokenBackup !== undefined) {
    writeFileSync(resolve(REPO_ROOT, TOKEN_FILE), tokenBackup);
  }
  if (cssBackup !== undefined) {
    writeFileSync(resolve(REPO_ROOT, GENERATED_CSS), cssBackup);
  }
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: "serial" });
test.setTimeout(120_000);

test.describe("SP-143 screen kit", () => {
  test("phone chrome renders at the declared frame, themed from tokens", async () => {
    await openWorkbenchOn(SCREEN);

    // The declared frame sizes the glass; the bezel is a 12px border around
    // it (runtime/chrome.css), so the body box is frame + 2×bezel.
    const bodyBox = await (await previewFrame()).locator("body").boundingBox();
    expect(bodyBox?.width).toBe(390 + 2 * 12);
    expect(bodyBox?.height).toBe(844 + 2 * 12);

    // Theming: the screen-title's color is the token's resolved value, not
    // the browser default (proves tokens.css loaded through the proxy).
    const tokens = resolvedTokens();
    const expected = tokens["color.dark.text.primary"];
    expect(expected).toBeTruthy();
    const titleColor = await (await previewFrame())
      .locator(".screen-title")
      .evaluate((el) => getComputedStyle(el).color);
    expect(titleColor.toLowerCase()).toContain(rgbOf(expected).toLowerCase());
  });

  test("data-nav swaps the screen in place; back returns", async () => {
    // Mark the booted document so an iframe reload is detectable.
    await (
      await previewFrame()
    ).evaluate(() => {
      (window as unknown as { __bootMarker: number }).__bootMarker = 42;
    });

    await (await previewFrame())
      .locator('[data-nav="to:mobile-session;trigger:tap session row"]')
      .first()
      .click();

    await expect.poll(stemNow, { timeout: 15_000 }).toBe("mobile-session");

    // In place, not a reload: the marker survives the swap.
    const marker = await (
      await previewFrame()
    ).evaluate(
      () => (window as unknown as { __bootMarker?: number }).__bootMarker,
    );
    expect(marker).toBe(42);

    // The nav entry lives in the IFRAME's history (the srcdoc fallback
    // records a same-document fragment there), not the top page's.
    await (await previewFrame()).evaluate(() => history.back());
    await expect.poll(stemNow, { timeout: 15_000 }).toBe("mobile-sessions");
  });

  test("the preview-gated state switcher toggles declared states", async () => {
    // Re-open on the detail screen (declared states ready/loading/error).
    await openWorkbenchOn(SCREEN_DETAIL);

    await expect.poll(stemNow, { timeout: 15_000 }).toBe("mobile-session");

    const readyVisible = async () =>
      (await workbenchFrame())!.locator('[data-state="ready"]').isVisible();
    const errorVisible = async () =>
      (await workbenchFrame())!.locator('[data-state="error"]').isVisible();
    await expect.poll(readyVisible, { timeout: 10_000 }).toBe(true);
    await expect.poll(errorVisible, { timeout: 10_000 }).toBe(false);

    await (await previewFrame())
      .locator("#sprout-state-switcher button", { hasText: "error" })
      .click();

    await expect.poll(errorVisible, { timeout: 10_000 }).toBe(true);
    await expect.poll(readyVisible, { timeout: 10_000 }).toBe(false);
  });

  test("a token edit restyles the screen with zero screen writes", async () => {
    await openWorkbenchOn(SCREEN);

    const screenPath = resolve(REPO_ROOT, SCREEN);
    const before = readFileSync(screenPath);
    const beforeMtime = statSync(screenPath).mtimeMs;

    // Theme color before the edit.
    const tokensBefore = resolvedTokens();
    const originalValue = tokensBefore["color.dark.text.primary"];
    expect(originalValue).toBeTruthy();

    // Swap the token to a clearly different hex, then re-export through the
    // same pipeline the agent tool drives (the fixtures helper).
    tokenBackup = readFileSync(resolve(REPO_ROOT, TOKEN_FILE), "utf8");
    cssBackup = readFileSync(resolve(REPO_ROOT, GENERATED_CSS), "utf8");
    writeFileSync(
      resolve(REPO_ROOT, TOKEN_FILE),
      tokenBackup.replace(originalValue, "#00ff00"),
    );

    execFileSync(
      "go",
      ["run", "./test/webui/fixtures/designtokencss", REPO_ROOT],
      {
        cwd: REPO_ROOT,
        stdio: "pipe",
      },
    );

    // The screen file was not written: same bytes, same mtime.
    expect(readFileSync(screenPath).toString()).toBe(before.toString());
    expect(statSync(screenPath).mtimeMs).toBe(beforeMtime);

    // And the preview, reloaded fresh, now shows the new color.
    await openWorkbenchOn(SCREEN);
    const servedCss = await page.request
      .get(viteProxyFileUrl("design/generated/tokens.css"))
      .then((r) => r.text());
    expect(servedCss).toContain("#00ff00"); // regen visible through the proxy
    const titleColor = await (await previewFrame())
      .locator(".screen-title")
      .evaluate((el) => getComputedStyle(el).color);
    expect(titleColor.toLowerCase()).toContain(rgbOf("#00ff00").toLowerCase());
  });
});

/** The /api/file proxy URL for a workspace path, as the preview rewriter does. */
function viteProxyFileUrl(workspacePath: string): string {
  const u = new URL(vite.url);
  return `${u.origin}/api/file?path=${encodeURIComponent(workspacePath)}`;
}

/** hex → "rgb(r, g, b)" so a token value compares against computed style. */
function rgbOf(hex: string): string {
  const h = hex.replace("#", "");
  const full =
    h.length === 3
      ? h
          .split("")
          .map((c) => c + c)
          .join("")
      : h;
  const r = parseInt(full.slice(0, 2), 16);
  const g = parseInt(full.slice(2, 4), 16);
  const b = parseInt(full.slice(4, 6), 16);
  return `rgb(${r}, ${g}, ${b})`;
}
