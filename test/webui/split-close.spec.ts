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
  try {
    browser = await chromium.launch({ channel: 'chrome' });
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

test.describe.configure({ mode: 'serial' });
test.setTimeout(60_000);

test('closing a split keeps the pane the control was invoked from', async () => {
  await page.goto(vite.url, { waitUntil: 'networkidle' });

  // Split once: 2 panes.
  await page.locator('.split-controls button[title="Split vertically"]').first().click();
  const panes = page.locator('.panes-container > .pane-wrapper');
  await expect(panes).toHaveCount(2, { timeout: 10_000 });

  // Focus the SECOND pane (click into it) — its tab bar becomes the active one.
  await panes.nth(1).click();
  await page.waitForTimeout(200);

  // Invoke "Close split panes" from the second pane's controls.
  await panes.nth(1).locator('.split-controls button[title="Close split panes"]').click();

  // One pane remains — and it must be the pane we invoked the close from
  // (the second one), not the leftmost original.
  await expect(panes).toHaveCount(1, { timeout: 10_000 });
  const remaining = panes.nth(0);
  await expect(remaining.locator('.split-controls')).not.toHaveClass(/split-controls--inactive/);
  // The remaining pane is active (no inactive overlay on its editor area).
  await expect(remaining.locator('.editor-pane-wrapper')).toHaveClass(/active/);
});
