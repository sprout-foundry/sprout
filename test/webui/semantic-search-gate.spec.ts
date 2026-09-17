// SP-137 (webui surface) — Semantic search stays hidden unless the
// experimental embedding index is opted in.
//
// The Brain toggle in the file-search panel (and its index-status /
// threshold controls) must not render unless the workspace config opts in
// with BOTH embedding_index.enabled AND embedding_index.experimental.
// Embeddings are experimental and off by default (SP-137 backend gate), so
// the default e2e stack — which never opts in — must show no Brain toggle.
//
// Unit-level coverage: webui/src/components/search/useSearchState.test.ts
// (gate truth table + note-instead-of-API-call). This spec pins the
// user-visible half: no toggle in the DOM when the gate is off.

import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

test.beforeAll(async () => {
  // AGENTS.md: prefer system Chrome; the Playwright browser download is
  // absent on some dev machines.
  browser = await chromium.launch({ channel: 'chrome' }).catch(() => chromium.launch());
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

test.describe.configure({ mode: 'serial' });
test.setTimeout(60_000);

async function openSearchPanel() {
  await page.goto(vite.url, { waitUntil: 'networkidle' });
  await expect(page.getByTestId('sidebar-container')).toBeVisible({ timeout: 30_000 });
  const searchTab = page.getByTestId('sidebar-search-tab');
  await expect(searchTab).toBeVisible({ timeout: 15_000 });
  await searchTab.click();
  await expect(page.locator('.search-view')).toBeVisible({ timeout: 15_000 });
}

test.describe('Semantic search gating', () => {
  test('brain toggle is hidden when the embedding gate is off (default)', async () => {
    await openSearchPanel();

    // Text search options render; the semantic toggle must not.
    await expect(page.locator('.search-option-btn').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.locator('.search-semantic-status')).toHaveCount(0);
    await expect(page.locator('.search-semantic-threshold')).toHaveCount(0);
  });

  test('text search still works with the gate off', async () => {
    await openSearchPanel();

    const input = page.locator('.search-text-input');
    await input.fill('SearchView');
    await expect(page.locator('.search-stats').first()).toBeVisible({ timeout: 15_000 });
  });
});
