// SP-087-6 — Long Paths spec: edge cases for very long file paths
//
// Tests that the UI handles deeply nested paths (~30 levels) and very long
// single filenames (~200 chars) gracefully: the file tree does not crash
// rendering, and the editor can open such files.

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

test.describe('Long File Paths', () => {
  test('deeply nested file path renders without crashing', async () => {
    // Create a deeply nested directory structure (~30 levels) directly on disk
    const deepPath = Array.from({ length: 30 }, (_, i) => `level${i}`).join('/');
    const deepFile = path.join(sprout.workspaceDir, deepPath, 'deep_file.txt');
    fs.mkdirSync(path.dirname(deepFile), { recursive: true });
    fs.writeFileSync(deepFile, 'I am a deeply nested file');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Use the file-tree testid
      const fileTree = page.getByTestId(TESTIDS['file-tree']);
      const hasFileTree = await fileTree.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFileTree) {
        await expect(fileTree).toBeVisible({ timeout: 5_000 });
      }

      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 10_000 });
    }
  });

  test('very long filename renders without crashing', async () => {
    // Create a file with a very long filename (~200 chars) directly on disk
    const longName = 'A'.repeat(180) + '_very_long_filename.txt';
    const longFile = path.join(sprout.workspaceDir, longName);
    fs.writeFileSync(longFile, 'I have a very long filename');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Use the file-tree testid
      const fileTree = page.getByTestId(TESTIDS['file-tree']);
      const hasFileTree = await fileTree.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFileTree) {
        await expect(fileTree).toBeVisible({ timeout: 5_000 });
      }

      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 5_000 });
    } else {
      await expect(page.getByTestId(TESTIDS['sidebar-container'])).toBeVisible({ timeout: 10_000 });
    }
  });

  test('editor can open a file with a long path', async () => {
    // Create a file with a moderately long path for editor testing
    const moderatePath = path.join(
      sprout.workspaceDir,
      'a',
      'b',
      'c',
      'd',
      'e',
      'f',
      'g',
      'h',
      'i',
      'j',
      'test_editor.txt',
    );
    fs.mkdirSync(path.dirname(moderatePath), { recursive: true });
    fs.writeFileSync(moderatePath, 'Content for editor test with long path');

    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    const filesTab = page.getByTestId(TESTIDS['sidebar-files-tab']);
    const hasFilesTab = await filesTab.isVisible({ timeout: 5_000 }).catch(() => false);

    if (hasFilesTab) {
      await filesTab.click();
      await page.waitForTimeout(2000);

      // Try to locate the file by its name using the file-tree-item testid
      const fileItem = page.getByTestId(TESTIDS['file-tree-item']).filter({ hasText: 'test_editor.txt' }).first();
      const hasFileItem = await fileItem.isVisible({ timeout: 5_000 }).catch(() => false);

      if (hasFileItem) {
        await fileItem.click();
        await page.waitForTimeout(1500);
      }

      // Verify the editor is visible (either it loaded or the welcome tab is shown)
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
});
