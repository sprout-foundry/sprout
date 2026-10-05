// Layered layout: the project rail and project sidebar (L1/L2), drilling
// into Files, opening a file (the conversation moves to the context panel),
// search, and the phone drawer. The suites' build pins classic; the page
// switches with ?layout=layered, which the app honours at runtime.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import { startSprout, type SproutHandle } from "./fixtures/sprout";
import { startViteDevServer, type ViteHandle } from "./fixtures/vite";
import TESTIDS from "./testids";

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;

function seededWorkspace(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "sprout-e2e-layered-"));
  fs.writeFileSync(path.join(dir, "README.md"), "# Layered\n");
  fs.mkdirSync(path.join(dir, "src"));
  fs.writeFileSync(
    path.join(dir, "src", "main.go"),
    "package main\n\nfunc main() {}\n",
  );
  return dir;
}

test.beforeAll(async () => {
  browser = await chromium
    .launch({ channel: "chrome" })
    .catch(() => chromium.launch());
  sprout = await startSprout({ workspaceDir: seededWorkspace() });
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
});

test.afterAll(async () => {
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: "serial" });
test.setTimeout(90_000);

async function openLayered(viewport: {
  width: number;
  height: number;
}): Promise<Page> {
  const context = await browser.newContext({ viewport });
  const page = await context.newPage();
  await page.goto(`${vite.url}/?layout=layered`, { waitUntil: "networkidle" });
  return page;
}

test.describe("Layered layout", () => {
  test("shows the project rail and the project sidebar sections", async () => {
    const page = await openLayered({ width: 1440, height: 900 });
    await expect(page.getByTestId(TESTIDS["project-rail"])).toBeVisible({
      timeout: 30_000,
    });
    const nav = page.getByTestId(TESTIDS["project-nav"]);
    await expect(nav).toBeVisible();
    for (const heading of ["Conversations", "Code"]) {
      await expect(
        nav.locator(".project-nav-heading", { hasText: heading }),
      ).toBeVisible();
    }
    for (const entry of ["Files", "Search", "Settings"]) {
      await expect(
        nav.locator(".project-nav-item", { hasText: entry }),
      ).toBeVisible();
    }
    await page.context().close();
  });

  test("drills into Files, opens a file, and keeps the conversation beside it", async () => {
    const page = await openLayered({ width: 1440, height: 900 });
    const nav = page.getByTestId(TESTIDS["project-nav"]);
    await expect(nav).toBeVisible({ timeout: 30_000 });

    await nav.locator(".project-nav-item", { hasText: "Files" }).click();
    const readme = page
      .locator(".file-tree-item", { hasText: "README.md" })
      .first();
    await expect(readme).toBeVisible({ timeout: 15_000 });
    await readme.click();

    await expect(page.getByTestId("editor")).toBeVisible({ timeout: 15_000 });
    const panel = page.getByTestId(TESTIDS["context-panel"]);
    await expect(panel).toBeVisible();
    // The context panel shows only the Agent Changes tab (the Conversation
    // tab was dropped in the repo-scoped storage change).
    const tab = panel.getByTestId("context-panel-tab").first();
    await expect(tab).toHaveAttribute("aria-label", "Agent Changes");

    await nav.locator(".project-nav-back").first().click();
    await expect(
      nav.locator(".project-nav-item", { hasText: "Files" }),
    ).toBeVisible();
    await page.context().close();
  });

  test("opens search from the top bar", async () => {
    const page = await openLayered({ width: 1440, height: 900 });
    const search = page.getByTestId(TESTIDS["header-search"]);
    await expect(search).toBeVisible({ timeout: 30_000 });
    await search.click();
    await expect(page.locator(".command-palette")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.locator(".command-palette")).toHaveCount(0);
    await page.context().close();
  });

  test("on a phone, the sidebar is a drawer that closes on navigation", async () => {
    const page = await openLayered({ width: 390, height: 844 });
    const menu = page.locator(".top-mobile-menu-btn");
    await expect(menu).toBeVisible({ timeout: 30_000 });
    await expect(page.locator(".sidebar.mobile.open")).toHaveCount(0);

    await menu.click();
    await expect(page.locator(".sidebar.mobile.open")).toHaveCount(1);
    // The standalone daemon has no Home or account, so no phone tab bar.
    await expect(page.getByTestId(TESTIDS["phone-tab-bar"])).toHaveCount(0);

    const conversations = page
      .getByTestId(TESTIDS["project-nav"])
      .locator(".project-nav-section")
      .first();
    await conversations.locator(".project-nav-item").first().click();
    await expect(page.locator(".sidebar.mobile.open")).toHaveCount(0);
    await page.context().close();
  });
});
