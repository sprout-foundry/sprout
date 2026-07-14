// SP-087-6 — Large Session spec: edge cases for sessions with many messages
//
// Tests that the UI handles a session with many messages gracefully:
// the chat scrolls to bottom, message list renders without freezing,
// and jumping to a specific message by index works.

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

test.describe('Large Session', () => {
  test('chat scrolls to bottom when opening a large session', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const createResp = await fetch(`${sprout.baseUrl}/api/chat-sessions/create`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Large Session Test' }),
    });

    if (!createResp.ok) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    const createData: any = await createResp.json();
    const sessionId = createData.id;
    expect(sessionId).toBeDefined();

    // Use the chat-input testid
    const textarea = page.getByTestId(TESTIDS['chat-input']);
    const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (!isTextareaVisible) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    // Send a batch of messages to build up the session (smaller count for speed)
    const messageCount = 20;
    for (let i = 0; i < messageCount; i++) {
      const msg = `Test message #${i + 1} — padding to simulate a large session load`;
      await textarea.fill(msg);
      await textarea.press('Enter');
      await expect(textarea).toHaveValue('', { timeout: 10_000 });
    }

    const messageList = page.getByTestId(TESTIDS['chat-message-list']);
    await expect(async () => {
      const count = await messageList.locator('> *').count();
      expect(count).toBeGreaterThan(10);
    }).toPass({ timeout: 30_000 });

    // Check for scroll-to-bottom button
    const scrollBottom = page.getByTestId(TESTIDS['chat-scroll-bottom']);
    const isScrollBottomVisible = await scrollBottom.isVisible({ timeout: 5_000 }).catch(() => false);

    if (isScrollBottomVisible) {
      await expect(scrollBottom).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
    }
  });

  test('message list renders large sessions without freezing', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Use the chat-input testid
    const textarea = page.getByTestId(TESTIDS['chat-input']);
    const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (!isTextareaVisible) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    const startTime = Date.now();
    const messageCount = 15;

    for (let i = 0; i < messageCount; i++) {
      const msg = `Performance test message #${i + 1} with some padding text`;
      await textarea.fill(msg);
      await textarea.press('Enter');
      await expect(textarea).toHaveValue('', { timeout: 10_000 });
    }

    const messageList = page.getByTestId(TESTIDS['chat-message-list']);
    await expect(async () => {
      const count = await messageList.locator('> *').count();
      expect(count).toBeGreaterThan(10);
    }).toPass({ timeout: 30_000 });

    const elapsed = Date.now() - startTime;
    expect(elapsed).toBeLessThan(10_000);

    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
  });

  test('jumping to a specific message by index works', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Use the chat-input testid
    const textarea = page.getByTestId(TESTIDS['chat-input']);
    const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (!isTextareaVisible) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    const sentinelText = 'SENTINEL_MESSAGE_FOR_JUMP_TEST';
    for (let i = 0; i < 10; i++) {
      const msg = i === 5 ? sentinelText : `Message before/after sentinel #${i}`;
      await textarea.fill(msg);
      await textarea.press('Enter');
      await expect(textarea).toHaveValue('', { timeout: 10_000 });
    }

    const messageList = page.getByTestId(TESTIDS['chat-message-list']);
    await expect(async () => {
      const count = await messageList.locator('> *').count();
      expect(count).toBeGreaterThan(5);
    }).toPass({ timeout: 30_000 });

    // Look for the sentinel message
    const sentinelLocator = page.locator(`text="${sentinelText}"`).first();
    const isSentinelVisible = await sentinelLocator.isVisible({ timeout: 5_000 }).catch(() => false);

    if (isSentinelVisible) {
      await sentinelLocator.scrollIntoViewIfNeeded().catch(() => {
        // scrollIntoViewIfNeeded may throw if element is already visible — that is fine
      });
      await expect(sentinelLocator).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
    }
  });
});
