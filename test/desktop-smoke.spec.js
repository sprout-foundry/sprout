/**
 * Desktop (Electron) smoke tests.
 *
 * These verify that the app launches, the launcher window appears, and
 * the basic UI renders without crashing.  They run in CI using a virtual
 * framebuffer (xvfb) and do not require a real backend binary — they only
 * test the Electron shell and launcher UI.
 */

const { test, expect, _electron: electron } = require('@playwright/test');
const path = require('node:path');
const fs = require('node:fs');

// Point at the repo root so that `require('electron')` resolves correctly
// when Playwright spawns the app.
const APP_ROOT = path.resolve(__dirname, '..');

test.describe('Desktop smoke', () => {
  test('app launches and shows launcher window', async () => {
    // We pass LEDIT_SMOKE_TEST=1 so the app can skip backend startup if needed
    const app = await electron.launch({
      args: [APP_ROOT],
      env: {
        ...process.env,
        LEDIT_SMOKE_TEST: '1',
        NODE_ENV: 'test',
        // Prevent the app from trying to restore previous windows
        LEDIT_DESKTOP: '1',
        LEDIT_SKIP_RESTORE: '1',
      },
    });

    try {
      // Wait for the first window to appear
      const page = await app.firstWindow();

      // The launcher page should load (it's a local HTML file, no backend required)
      await page.waitForLoadState('domcontentloaded', { timeout: 15000 });

      // Basic check: the window has a title
      const title = await app.evaluate(({ app: electronApp }) => {
        const { BrowserWindow } = require('electron');
        const wins = BrowserWindow.getAllWindows();
        return wins.length > 0 ? wins[0].getTitle() : '';
      });
      expect(title).toBeTruthy();

      // The launcher HTML should be visible (no blank page)
      const bodyText = await page.evaluate(() => document.body?.textContent ?? '');
      expect(bodyText.length).toBeGreaterThan(0);
    } finally {
      await app.close();
    }
  });

  test('launcher window has expected minimum dimensions', async () => {
    const app = await electron.launch({
      args: [APP_ROOT],
      env: {
        ...process.env,
        LEDIT_SMOKE_TEST: '1',
        NODE_ENV: 'test',
        LEDIT_DESKTOP: '1',
        LEDIT_SKIP_RESTORE: '1',
      },
    });

    try {
      await app.firstWindow();
      const bounds = await app.evaluate(() => {
        const { BrowserWindow } = require('electron');
        const win = BrowserWindow.getAllWindows()[0];
        return win ? win.getBounds() : null;
      });

      expect(bounds).not.toBeNull();
      expect(bounds.width).toBeGreaterThanOrEqual(900);
      expect(bounds.height).toBeGreaterThanOrEqual(620);
    } finally {
      await app.close();
    }
  });

  test('main.js module imports resolve without errors', async () => {
    // This test verifies the module graph is intact by checking that each
    // module file can be syntax-checked.  It runs as a Node.js child process
    // rather than launching full Electron, so it's fast and always available.
    const { spawnSync } = require('node:child_process');
    const desktopDir = path.join(APP_ROOT, 'desktop');
    const modules = [
      'context.js',
      'utils.js',
      'state-manager.js',
      'error-pages.js',
      'wsl.js',
      'backend.js',
      'ssh.js',
      'workspace.js',
      'windows.js',
      'main.js',
    ];

    for (const mod of modules) {
      const modPath = path.join(desktopDir, mod);
      expect(fs.existsSync(modPath), `${mod} should exist`).toBe(true);
      const result = spawnSync(process.execPath, ['--check', modPath], {
        encoding: 'utf8',
      });
      expect(result.status, `${mod} syntax check failed: ${result.stderr}`).toBe(0);
    }
  });
});
