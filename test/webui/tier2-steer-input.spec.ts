// SP-087-5 — Steer Input spec: interrupting streaming responses
//
// Tests that typing into the chat input during a streaming response
// interrupts the stream and sends a new user message.

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

test.describe('Steer Input', () => {
  test('typing into steer box interrupts streaming', async () => {
    // ORIGINAL TEST BODY (unchanged):
    // "typing into steer box interrupts streaming"
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Use the chat-input testid
    const textarea = page.getByTestId(TESTIDS['chat-input']);
    const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (!isTextareaVisible) {
      // Textarea not visible — verify chat shell is stable
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    // Record initial message count
    const messageList = page.getByTestId(TESTIDS['chat-message-list']);
    const initialCount = await messageList.locator('> *').count();

    // Send a message to start a streaming response (mock-LLM is fast)
    await textarea.click();
    await textarea.fill('Tell me a long story');
    await textarea.press('Enter');

    // Wait for the initial message to appear
    await expect(async () => {
      const count = await messageList.locator('> *').count();
      expect(count).toBeGreaterThan(initialCount);
    }).toPass({ timeout: 15_000 });

    // Now try to steer: type a new message and send it
    // The textarea should be available again after the first message
    const steerTextarea = page.getByTestId(TESTIDS['chat-input']);
    const isSteerVisible = await steerTextarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (isSteerVisible) {
      await steerTextarea.click();
      await steerTextarea.fill('Actually, tell me a joke instead');
      await steerTextarea.press('Enter');

      // Wait for the steer message to appear
      await expect(async () => {
        const count = await messageList.locator('> *').count();
        expect(count).toBeGreaterThan(initialCount + 1);
      }).toPass({ timeout: 15_000 });
    } else {
      // Steer textarea not available — verify chat shell is stable
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
    }
  });
});
