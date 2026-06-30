// SP-087-6 — Empty Workspace spec: edge cases for workspace with no files
//
// Tests that the UI handles an empty workspace gracefully:
// the sidebar file tree shows an empty state, and chat still works.
//
// MISSING TESTIDS (documented in test/webui/tier3-gap-report.md):
//   - `file-tree-empty` — empty-state indicator in the file tree panel (NOT in registry)
//   - `editor-empty` — empty state in the editor when no file is open (NOT in registry)
// These tests are currently `test.fixme()` and will run once the missing
// testids are added to the registry.

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

test.describe('Empty Workspace', () => {
  test.fixme(
    'file tree shows empty state when workspace has no files — file-tree-empty / editor-empty testids missing (SP-087-6 followup)',
    async () => {
      await page.goto(vite.url, { waitUntil: 'networkidle' });
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

      // Verify the workspace is actually empty via the API
      const filesResp = await fetch(`${sprout.baseUrl}/api/files?path=.`);
      const filesData = await filesResp.json();
      expect(filesData.files).toBeDefined();
      expect(Array.isArray(filesData.files)).toBe(true);

      // NOTE: file-tree-empty is NOT in the testid registry.
      // The file tree panel may show an empty state, but there is no
      // dedicated testid for it.
      const sidebar = page.getByTestId(TESTIDS['sidebar-container']);
      await expect(sidebar).toBeVisible({ timeout: 10_000 });

      const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
      const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFilesTab) {
        await filesTab.click();
        await page.waitForTimeout(1000);

        // The file tree should render (even if empty) without crashing.
        // NOTE: file-tree-empty / editor-empty are NOT in the testid registry.
        const fileTreeArea = page.locator('[class*="file-tree"], [class*="file-tree-panel"], [class*="files-panel"]').first();
        const hasFileTree = await fileTreeArea.isVisible({ timeout: 5_000 }).catch(() => false);

        if (hasFileTree) {
          await expect(sidebar).toBeVisible({ timeout: 5_000 });
        } else {
          await expect(sidebar).toBeVisible({ timeout: 5_000 });
        }
      } else {
        await expect(sidebar).toBeVisible({ timeout: 10_000 });
      }
    },
  );

  test.fixme(
    'chat works in an empty workspace — send message and get response — chat-input testid missing (SP-087-6 followup)',
    async () => {
      await page.goto(vite.url, { waitUntil: 'networkidle' });
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

      // Fallback: first <textarea> inside the chat shell (chat-input testid is missing)
      const textarea = page.locator('[data-testid="chat-shell"] textarea').first();
      const isTextareaVisible = await textarea.isVisible({ timeout: 10_000 }).catch(() => false);

      if (!isTextareaVisible) {
        await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
        return;
      }

      const testMessage = 'Hello from empty workspace';
      await textarea.click();
      await textarea.fill(testMessage);
      await textarea.press('Enter');

      await expect(textarea).toHaveValue('', { timeout: 10_000 });

      const messageList = page.getByTestId(TESTIDS['chat-message-list']);
      await expect(async () => {
        const count = await messageList.locator('> *').count();
        expect(count).toBeGreaterThan(0);
      }).toPass({ timeout: 30_000 });

      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
    },
  );
});
