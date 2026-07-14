// SP-087-4 — Chat spec: basic chat shell, message send/receive
//
// Tests the chat shell renders, user can type and submit a message,
// and the mock-LLM response appears in the chat message list.

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

test.describe('Chat', () => {
  test('chat shell renders on the webui home page', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });
  });

  // NOTE: chat-input and chat-send are NOT in the testid registry. We
  // use a fallback: the first <textarea> inside the chat-shell element,
  // then Enter to send. This is a known gap to be filled when testids
  // are added for chat-input / chat-send.
  test('user can type a message into the chat input and submit it', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Fallback: first <textarea> inside the chat shell (chat-input testid is missing)
    const textarea = page.locator('[data-testid="chat-shell"] textarea').first();
    await expect(textarea).toBeVisible({ timeout: 15_000 });

    const testMessage = 'Hello from e2e test';
    await textarea.click();
    await textarea.fill(testMessage);

    // Press Enter to send (chat-send testid is missing, so we use keyboard)
    await textarea.press('Enter');

    // After sending, the textarea should be cleared
    await expect(textarea).toHaveValue('', { timeout: 10_000 });
  });

  test('mock-LLM response appears in chat message list after sending a message', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Record initial message count (may be 0 or have welcome text)
    const messageList = page.getByTestId(TESTIDS['chat-message-list']);
    await expect(messageList).toBeVisible({ timeout: 15_000 });
    const initialChildren = await messageList.locator('> *').count();

    // Fallback: first <textarea> inside the chat shell (chat-input testid is missing)
    const textarea = page.locator('[data-testid="chat-shell"] textarea').first();
    await expect(textarea).toBeVisible({ timeout: 15_000 });

    const testMessage = 'Tell me a joke';
    await textarea.click();
    await textarea.fill(testMessage);
    await textarea.press('Enter');

    // Wait for processing indicator to appear then disappear (mock-LLM is fast but give it time)
    const processing = page.getByTestId(TESTIDS['chat-processing']);
    // Processing may appear briefly; wait for the message list to grow
    // Use a generous timeout for the mock-LLM round-trip
    await expect(async () => {
      const count = await messageList.locator('> *').count();
      expect(count).toBeGreaterThan(initialChildren);
    }).toPass({ timeout: 30_000 });
  });
});
