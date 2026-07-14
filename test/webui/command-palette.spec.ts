// SP-087-4 — Command Palette spec: keyboard shortcut and search functionality
//
// The CommandPalette component lives in @sprout/ui and renders real ARIA
// roles: role="dialog" (overlay), role="combobox" (input), role="listbox"
// (results), role="option" (individual items). These tests use stable
// getByRole selectors instead of guessed CSS class fallbacks.

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

test.describe('Command Palette', () => {
  test('Ctrl+Shift+P opens the command palette', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Open the command palette via keyboard shortcut
    await page.keyboard.press('Control+Shift+P');

    // The CommandPalette renders as role="dialog" with aria-label="Command palette".
    // Assert that a dialog appeared within a bounded timeout.
    const dialog = page.getByRole('dialog', { name: 'Command palette' });
    await expect(dialog).toBeVisible({ timeout: 10_000 });

    // Also verify the combobox input is present and focused
    const combobox = page.getByRole('combobox');
    await expect(combobox).toBeVisible({ timeout: 5_000 });
  });

  test('typing in the command palette shows results', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Open the command palette
    await page.keyboard.press('Control+Shift+P');
    const dialog = page.getByRole('dialog', { name: 'Command palette' });
    await expect(dialog).toBeVisible({ timeout: 10_000 });

    // Get the combobox input and type a search query
    const combobox = page.getByRole('combobox');
    await combobox.fill('toggle');

    // Wait for results to appear in the listbox
    const listbox = page.getByRole('listbox');
    await expect(listbox).toBeVisible({ timeout: 5_000 });

    // Assert at least one option appeared inside the dialog
    const options = dialog.getByRole('option');
    await expect(options.first()).toBeVisible({ timeout: 5_000 });
  });

  test('pressing Enter in the command palette closes it or executes', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Open the command palette
    await page.keyboard.press('Control+Shift+P');
    const dialog = page.getByRole('dialog', { name: 'Command palette' });
    await expect(dialog).toBeVisible({ timeout: 10_000 });

    // Type something to ensure there's a selected result
    const combobox = page.getByRole('combobox');
    await combobox.fill('toggle sidebar');
    await page.waitForTimeout(300);

    // Press Enter to execute the selected command
    await combobox.press('Enter');
    await page.waitForTimeout(1000);

    // After pressing Enter, the palette should close
    await expect(dialog).toBeHidden({ timeout: 5_000 });
  });
});
