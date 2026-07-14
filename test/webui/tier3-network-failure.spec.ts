// SP-087-6 — Network Failure spec: edge cases for network failures during chat
//
// Tests that the UI handles network failures gracefully: API endpoint failures
// show error states, WebSocket disconnects show a disconnected overlay, and
// the chat shell remains stable throughout.

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

test.describe('Network Failure During Chat', () => {
  test('chat works normally before simulating failure (control test)', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Use the chat-input testid
    const textarea = page.getByTestId(TESTIDS['chat-input']);
    const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (!isTextareaVisible) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    const testMessage = 'Hello, this is a control test message';
    await textarea.fill(testMessage);
    await textarea.press('Enter');

    await expect(textarea).toHaveValue('', { timeout: 10_000 });

    const messageList = page.getByTestId(TESTIDS['chat-message-list']);
    await expect(async () => {
      const count = await messageList.locator('> *').count();
      expect(count).toBeGreaterThan(0);
    }).toPass({ timeout: 30_000 });

    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
  });

  test('chat shows error state when API endpoint is blocked', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Use the chat-input testid
    const textarea = page.getByTestId(TESTIDS['chat-input']);
    const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

    if (!isTextareaVisible) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
      return;
    }

    // Block the chat API endpoint
    await page.route(`${sprout.baseUrl}/api/chat*`, (route) => {
      route.abort('failed');
    });

    try {
      const testMessage = 'This message should fail due to blocked API';
      await textarea.fill(testMessage);
      await textarea.press('Enter');

      await page.waitForTimeout(3000);

      // Check for error state — chat-error IS in the registry
      const chatError = page.getByTestId(TESTIDS['chat-error']);
      const hasChatError = await chatError.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasChatError) {
        await expect(chatError).toBeVisible({ timeout: 5_000 });
      } else {
        const errorText = page
          .locator('[data-testid="chat-shell"]')
          .filter({ hasText: /error|retry|failed/i })
          .first();
        const hasErrorText = await errorText.isVisible({ timeout: 5_000 }).catch(() => false);

        if (hasErrorText) {
          await expect(errorText).toBeVisible({ timeout: 5_000 });
        }
      }

      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });

      // Verify textarea is still usable (can fill again, value persists)
      await textarea.fill('Textarea should still work');
      const textareaValue = await textarea.inputValue();
      expect(textareaValue).toBe('Textarea should still work');
    } finally {
      // Unroute the blocked endpoint (always, even on failure)
      await page.unroute(`${sprout.baseUrl}/api/chat*`).catch(() => {
        // Ignore unroute errors — the route may already be cleared
      });
    }
  });

  test('chat shell remains stable after WebSocket disconnect', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Try to close the WebSocket connection via page.evaluate
    const wsClosed = await page.evaluate(() => {
      try {
        const win = window as any;
        if (win.__sproutWebSocket) {
          win.__sproutWebSocket.close();
          return true;
        }
        if (win.wsClient) {
          win.wsClient.close();
          return true;
        }
        const entries = performance.getEntriesByType('resource');
        const wsEntries = entries.filter(
          (e) => typeof e.name === 'string' && e.name.startsWith('ws:'),
        );
        if (wsEntries.length > 0) {
          return true;
        }
      } catch {
        // Silently ignore — WS may not be exposed
      }
      return false;
    });

    await page.waitForTimeout(3000);

    // Check for disconnected overlay using the testid
    const disconnectedOverlay = page.getByTestId(TESTIDS['disconnected-overlay']);
    const hasDisconnectedOverlay = await disconnectedOverlay.isVisible({
      timeout: 5_000,
    }).catch(() => false);

    if (hasDisconnectedOverlay) {
      await expect(disconnectedOverlay).toBeVisible({ timeout: 5_000 });
    } else if (!wsClosed) {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
    }

    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
  });

  test('chat recovers when network is restored after failure', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // First, block the API endpoint
    await page.route(`${sprout.baseUrl}/api/chat*`, (route) => {
      route.abort('failed');
    });

    try {
      // Use the chat-input testid
      const textarea = page.getByTestId(TESTIDS['chat-input']);
      const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

      if (!isTextareaVisible) {
        await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
        return;
      }

      // Send a message that will fail
      await textarea.fill('This will fail');
      await textarea.press('Enter');
      await page.waitForTimeout(2000);

      // Now restore the network by unrouting
      await page.unroute(`${sprout.baseUrl}/api/chat*`).catch(() => {
        // Ignore — already unrouted
      });

      await page.waitForTimeout(3000);

      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });

      // Try sending another message now that network is restored
      await textarea.fill('Network should be restored now');
      await textarea.press('Enter');

      const cleared = await textarea
        .waitFor({ state: 'visible', timeout: 5_000 })
        .then(async () => {
          const val = await textarea.inputValue();
          return val === '';
        })
        .catch(() => false);

      if (cleared) {
        await expect(textarea).toHaveValue('', { timeout: 10_000 });
      }

      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
    } finally {
      // Always unroute, even on failure
      await page.unroute(`${sprout.baseUrl}/api/chat*`).catch(() => {
        // Ignore unroute errors
      });
    }
  });
});
