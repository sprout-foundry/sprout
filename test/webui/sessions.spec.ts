// SP-087-4 — Sessions spec: sidebar session search functionality
//
// Tests that the session search input is visible in the sidebar and
// that typing in it reveals the search dropdown.

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

test.describe.configure({ mode: 'serial' });
test.setTimeout(60_000);

test.describe('Sessions', () => {
  test('sessions search input is visible in the sidebar', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });

    // The sidebar container should be visible
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });

    // The search input uses value "sidebar-session-search-input" (key in registry is sidebar-sessions-search-input)
    const searchInput = page.getByTestId(TESTIDS['sidebar-sessions-search-input']);
    await expect(searchInput).toBeVisible({ timeout: 15_000 });
  });

  test('typing in the search input shows the search dropdown', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 30_000 });

    const searchInput = page.getByTestId(TESTIDS['sidebar-sessions-search-input']);
    await expect(searchInput).toBeVisible({ timeout: 15_000 });

    // Type something into the search input
    await searchInput.click();
    await searchInput.fill('test query');

    // After typing, the search dropdown should become visible
    // (it may show no-results or matching results, but the dropdown itself should appear)
    const dropdown = page.getByTestId(TESTIDS['sidebar-sessions-search-dropdown']);
    await expect(dropdown).toBeVisible({ timeout: 15_000 });
  });
});
