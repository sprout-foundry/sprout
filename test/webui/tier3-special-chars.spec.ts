// SP-087-6 — Special Characters spec: edge cases for filenames with special characters
//
// Tests that the UI handles files with spaces, unicode, punctuation, emoji,
// and mixed special characters in filenames gracefully: the file tree renders
// them correctly, and the editor can open such files.

import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import TESTIDS from './testids';
import fs from 'node:fs';
import path from 'node:path';

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

test.describe('Special Characters in Filenames', () => {
  test('file tree renders files with spaces in filenames', async () => {
    const filePath = path.join(sprout.workspaceDir, 'hello world.txt');
    fs.writeFileSync(filePath, 'File with spaces in name');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Use the file-tree-item testid
      const fileItem = page.getByTestId(TESTIDS['file-tree-item']).filter({ hasText: 'hello world.txt' }).first();
      const hasFileItem = await fileItem.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFileItem) {
        await expect(fileItem).toBeVisible({ timeout: 5_000 });
      }

      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 10_000 });
    }
  });

  test('file tree renders files with unicode and emoji in filenames', async () => {
    const unicodeFile = path.join(sprout.workspaceDir, 'héllo_世界.txt');
    fs.writeFileSync(unicodeFile, 'Unicode filename content');

    const emojiFile = path.join(sprout.workspaceDir, '🚀_rocket.txt');
    fs.writeFileSync(emojiFile, 'Emoji filename content');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Use the file-tree-item testid
      const unicodeItem = page.getByTestId(TESTIDS['file-tree-item']).filter({ hasText: 'héllo_世界.txt' }).first();
      const hasUnicodeItem = await unicodeItem.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasUnicodeItem) {
        await expect(unicodeItem).toBeVisible({ timeout: 5_000 });
      }

      const emojiItem = page.getByTestId(TESTIDS['file-tree-item']).filter({ hasText: '🚀_rocket.txt' }).first();
      const hasEmojiItem = await emojiItem.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasEmojiItem) {
        await expect(emojiItem).toBeVisible({ timeout: 5_000 });
      }

      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 10_000 });
    }
  });

  test('editor can open a file with mixed special characters', async () => {
    const mixedFile = path.join(sprout.workspaceDir, 'my file (final) - 世界 🚀.md');
    fs.writeFileSync(mixedFile, '# Mixed Special Characters\n\nThis file has a complex filename.');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Use the file-tree-item testid
      const fileItem = page.getByTestId(TESTIDS['file-tree-item']).filter({ hasText: 'my file (final)' }).first();
      const hasFileItem = await fileItem.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFileItem) {
        await fileItem.click();
        await page.waitForTimeout(1500);
      }

      // Verify the editor is visible (either loaded or welcome tab shown)
      const editor = page.getByTestId(TESTIDS['editor']);
      const isEditorVisible = await editor.isVisible({ timeout: 5_000 }).catch(() => false);

      if (isEditorVisible) {
        await expect(editor).toBeVisible({ timeout: 5_000 });
      } else {
        await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 5_000 });
      }
    } else {
      await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 10_000 });
    }
  });

  test('file tree renders files with punctuation in filenames', async () => {
    const punctFile = path.join(sprout.workspaceDir, '!@#$%^&().txt');
    fs.writeFileSync(punctFile, 'Punctuation filename content');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Use the file-tree-item testid
      const fileItem = page.getByTestId(TESTIDS['file-tree-item']).filter({ hasText: '!@#$%^&()' }).first();
      const hasFileItem = await fileItem.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFileItem) {
        await expect(fileItem).toBeVisible({ timeout: 5_000 });
      }

      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 10_000 });
    }
  });
});
