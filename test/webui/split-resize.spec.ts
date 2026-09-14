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
  // System Chrome fallback for dev machines without the Playwright download.
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

test('split resize handle changes pane widths', async () => {
  await page.goto(vite.url, { waitUntil: 'networkidle' });

  const splitBtn = page.locator('.split-controls button[title="Split vertically"]').first();
  await expect(splitBtn).toBeVisible({ timeout: 30_000 });
  await splitBtn.click();

  const panes = page.locator('.panes-container > .pane-wrapper');
  await expect(panes).toHaveCount(2, { timeout: 10_000 });
  const handleEl = page.locator('.panes-container > .resize-handle').first();
  await expect(handleEl).toBeVisible();

  const widthBefore = await panes.nth(0).boundingBox();
  const handleBox = await handleEl.boundingBox();
  expect(widthBefore).not.toBeNull();
  expect(handleBox).not.toBeNull();

  const startX = handleBox!.x + handleBox!.width / 2;
  const startY = handleBox!.y + handleBox!.height / 2;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + 60, startY, { steps: 4 });
  await page.mouse.move(startX + 120, startY, { steps: 4 });
  await page.mouse.up();

  const widthAfter = await panes.nth(0).boundingBox();
  expect(widthAfter).not.toBeNull();
  const delta = widthAfter!.width - widthBefore!.width;
  expect(Math.abs(delta)).toBeGreaterThan(50);
});
