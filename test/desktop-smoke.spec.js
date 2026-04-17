/**
 * Desktop (Electron) smoke tests.
 *
 * These verify that the app launches, the launcher window appears, and
 * the basic UI renders without crashing.  They run in CI using a virtual
 * framebuffer (xvfb) and do not require a real backend binary — they only
 * test the Electron shell and launcher UI.
 */

const { test, expect } = require('@playwright/test');
const path = require('node:path');
const fs = require('node:fs');
const os = require('node:os');
const { spawn } = require('node:child_process');

// Point at the repo root so that `require('electron')` resolves correctly
// when Playwright spawns the app.
const APP_ROOT = path.resolve(__dirname, '..');
const ELECTRON_BIN = path.join(APP_ROOT, 'node_modules', 'electron', 'dist', 'electron');

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForSmokeStatus(statusFile, timeoutMs = 15000) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    if (fs.existsSync(statusFile)) {
      const raw = fs.readFileSync(statusFile, 'utf8').trim();
      if (raw) {
        return JSON.parse(raw);
      }
    }
    await wait(200);
  }
  throw new Error(`Timed out waiting for smoke status file: ${statusFile}`);
}

async function launchSmokeApp() {
  const statusDir = fs.mkdtempSync(path.join(os.tmpdir(), 'ledit-desktop-smoke-'));
  const statusFile = path.join(statusDir, 'status.json');
  const child = spawn(ELECTRON_BIN, [APP_ROOT], {
    cwd: APP_ROOT,
    env: {
      ...process.env,
      LEDIT_SMOKE_TEST: '1',
      NODE_ENV: 'test',
      LEDIT_DESKTOP: '1',
      LEDIT_SKIP_RESTORE: '1',
      LEDIT_SMOKE_STATUS_FILE: statusFile,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  let stderr = '';
  let stdout = '';
  child.stderr.on('data', (chunk) => {
    stderr += chunk.toString();
  });
  child.stdout.on('data', (chunk) => {
    stdout += chunk.toString();
  });

  return {
    child,
    statusFile,
    statusDir,
    getLogs: () => ({ stdout, stderr }),
  };
}

async function closeSmokeApp(child, statusDir) {
  if (child.exitCode === null && !child.killed) {
    child.kill('SIGTERM');
    await Promise.race([
      new Promise((resolve) => child.once('exit', resolve)),
      wait(3000),
    ]);
    if (child.exitCode === null && !child.killed) {
      child.kill('SIGKILL');
      await new Promise((resolve) => child.once('exit', resolve));
    }
  }
  fs.rmSync(statusDir, { recursive: true, force: true });
}

test.describe('Desktop smoke', () => {
  test('app launches and shows launcher window', async () => {
    const app = await launchSmokeApp();

    try {
      const status = await waitForSmokeStatus(app.statusFile, 15000);
      expect(status.event).toBe('launcher-loaded');
      expect(status.title).toBeTruthy();
      expect(status.title).toContain('Ledit');
      expect(status.visible).toBe(true);
    } finally {
      await closeSmokeApp(app.child, app.statusDir);
    }
  });

  test('launcher window has expected minimum dimensions', async () => {
    const app = await launchSmokeApp();

    try {
      const status = await waitForSmokeStatus(app.statusFile, 15000);
      const bounds = status.bounds;

      expect(bounds).not.toBeNull();
      expect(bounds.width).toBeGreaterThanOrEqual(900);
      expect(bounds.height).toBeGreaterThanOrEqual(620);
    } finally {
      await closeSmokeApp(app.child, app.statusDir);
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
