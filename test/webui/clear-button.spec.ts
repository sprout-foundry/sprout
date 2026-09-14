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

test('New Session button clears the transcript promptly', async () => {
  await page.goto(vite.url, { waitUntil: 'networkidle' });

  // Send a message so the transcript has content.
  const textarea = page.getByTestId('chat-input');
  await textarea.fill('hello before clear');
  await textarea.press('Enter');
  await expect(
    page.locator('.message-content, [data-testid="chat-message-list"] > *').filter({ hasText: 'hello before clear' }).first(),
  ).toBeVisible({ timeout: 20_000 });

  // Click the New Session button and measure how long until the transcript
  // no longer shows the message.
  const newSessionBtn = page.getByTestId('chat-new-button');
  await expect(newSessionBtn).toBeVisible();
  const t0 = Date.now();
  await newSessionBtn.click();

  await expect(
    page.locator('.message-content, [data-testid="chat-message-list"] > *').filter({ hasText: 'hello before clear' }),
  ).toHaveCount(0, { timeout: 15_000 });
  const elapsed = Date.now() - t0;
  console.log('CLEAR_ELAPSED_MS:', elapsed);
  // Clear should feel instant — well under 2s even on a slow machine.
  expect(elapsed).toBeLessThan(2000);

  // The cleared chat must still be usable: send another message.
  await textarea.fill('after clear message');
  await textarea.press('Enter');
  await expect(
    page.locator('.message-content, [data-testid="chat-message-list"] > *').filter({ hasText: 'after clear message' }).first(),
  ).toBeVisible({ timeout: 20_000 });

  // Typed /clear (via the input) also clears, not just the button.
  await textarea.fill('/clear');
  await textarea.press('Enter');
  await expect(
    page.locator('.message-content, [data-testid="chat-message-list"] > *').filter({ hasText: 'after clear message' }),
  ).toHaveCount(0, { timeout: 15_000 });
});
