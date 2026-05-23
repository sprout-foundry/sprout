/**
 * Desktop E2E smoke tests.
 *
 * These verify that the full app launches with a real Go backend, opens a workspace,
 * the backend health endpoint responds, and the webui loads.  They run in CI using
 * a virtual framebuffer (xvfb) and require a pre-built Go backend binary.
 *
 * The test spawns Electron manually (like desktop-smoke.spec.js) rather than using
 * Playwright's _electron integration, because the workspace opening flow involves
 * a loading page → backend health poll → navigation sequence that is more reliably
 * tested by monitoring a status file written by the main process.
 */

const { test, expect } = require('@playwright/test');
const path = require('node:path');
const fs = require('node:fs');
const os = require('node:os');
const { spawn } = require('node:child_process');
const http = require('node:http');

const APP_ROOT = path.resolve(__dirname, '..');
const ELECTRON_BIN = path.join(
  APP_ROOT,
  'node_modules',
  'electron',
  'dist',
  'electron'
);

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Check if the Go backend binary exists. Throws if not found.
 */
function assertBackendBinary() {
  const platform = process.platform === 'win32' ? 'windows' : process.platform;
  const arch = process.arch === 'x64' ? 'amd64' : process.arch;
  const binaryName = platform === 'windows' ? 'sprout.exe' : 'sprout';
  const binaryPath = path.join(APP_ROOT, 'desktop', 'dist', 'backend', `${platform}-${arch}`, binaryName);
  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Backend binary not found at ${binaryPath}. Run "npm run build:desktop:backend".`);
  }
  return binaryPath;
}

/**
 * Make a single HTTP GET to the backend health endpoint.
 */
function checkHealth(port) {
  return new Promise((resolve, reject) => {
    const req = http.get({ hostname: '127.0.0.1', port, path: '/health', timeout: 5000 }, (res) => {
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`Health returned HTTP ${res.statusCode}`));
        return;
      }
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        const body = Buffer.concat(chunks).toString('utf8');
        try {
          resolve(JSON.parse(body));
        } catch {
          reject(new Error(`Invalid JSON from /health: ${body}`));
        }
      });
    });
    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy();
      reject(new Error('Health check timed out'));
    });
  });
}

/**
 * Poll /health until it responds successfully.
 */
async function pollHealth(port, timeoutMs = 30000) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    try {
      return await checkHealth(port);
    } catch {
      await wait(500);
    }
  }
  throw new Error(`Backend health endpoint did not respond within ${timeoutMs}ms on port ${port}`);
}

/**
 * Check if a string is valid JSON.
 */
function isJson(str) {
  try {
    JSON.parse(str);
    return true;
  } catch {
    return false;
  }
}

/**
 * Wait for the smoke status file to be written with workspace-opened event.
 */
async function waitForWorkspaceReady(statusFile, timeoutMs = 45000) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    if (fs.existsSync(statusFile)) {
      try {
        const raw = fs.readFileSync(statusFile, 'utf8').trim();
        if (raw && isJson(raw)) {
          const data = JSON.parse(raw);
          if (data.event === 'workspace-opened' && data.port) {
            return data;
          }
        }
      } catch {
        // File may be partially written; retry
      }
    }
    await wait(300);
  }
  throw new Error(`Timed out waiting for workspace-ready status file (${timeoutMs}ms). Check Electron logs.`);
}

/**
 * Verify the webui loads by making an HTTP request to the backend root.
 */
function checkWebuiLoads(port) {
  return new Promise((resolve, reject) => {
    const req = http.get({ hostname: '127.0.0.1', port, path: '/', timeout: 10000 }, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        const body = Buffer.concat(chunks).toString('utf8');
        if (res.statusCode === 200 && body.includes('id="root"') && body.includes('<!doctype html>')) {
          resolve(body);
        } else {
          reject(new Error(`Webui did not load properly (HTTP ${res.statusCode}, body length ${body.length})`));
        }
      });
    });
    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy();
      reject(new Error('Webui load check timed out'));
    });
  });
}

/**
 * Make an HTTP GET request and parse the response as JSON.
 */
function httpGetJson(port, apiPath, timeoutMs = 10000) {
  return new Promise((resolve, reject) => {
    const req = http.get({ hostname: '127.0.0.1', port, path: apiPath, timeout: timeoutMs }, (res) => {
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`${apiPath} returned HTTP ${res.statusCode}`));
        return;
      }
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        const body = Buffer.concat(chunks).toString('utf8');
        try {
          resolve(JSON.parse(body));
        } catch {
          reject(new Error(`Invalid JSON from ${apiPath}: ${body}`));
        }
      });
    });
    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy();
      reject(new Error(`${apiPath} request timed out`));
    });
  });
}

/**
 * Launch Electron in smoke test mode.
 */
function launchElectron(workspaceDir, statusFile) {
  const child = spawn(ELECTRON_BIN, [APP_ROOT], {
    cwd: APP_ROOT,
    env: {
      ...process.env,
      NODE_ENV: 'test',
      ELECTRON_DISABLE_SECURITY_WARNINGS: '1',
      SPROUT_SMOKE_TEST: '1',
      SPROUT_SKIP_RESTORE: '1',
      SPROUT_SMOKE_WORKSPACE: workspaceDir,
      SPROUT_SMOKE_STATUS_FILE: statusFile,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  let stdout = '';
  let stderr = '';
  child.stdout.on('data', (chunk) => (stdout += chunk.toString()));
  child.stderr.on('data', (chunk) => (stderr += chunk.toString()));

  return { child, getLogs: () => ({ stdout, stderr }) };
}

/**
 * Gracefully close the Electron process.
 */
async function closeElectron(child) {
  if (child.exitCode === null && !child.killed) {
    child.kill('SIGTERM');
    await Promise.race([new Promise((resolve) => child.once('exit', resolve)), wait(5000)]);
    if (child.exitCode === null && !child.killed) {
      child.kill('SIGKILL');
      await Promise.race([new Promise((resolve) => child.once('exit', resolve)), wait(3000)]);
    }
  }
}

/**
 * Log diagnostic information for debugging.
 */
function logDiagnostics(app, statusDir, statusFile) {
  if (!app) return;
  const logOutput = app.getLogs();
  console.error('[E2E] Electron exit code:', app.child.exitCode, 'killed:', app.child.killed);
  console.error('[E2E] Status file exists:', fs.existsSync(statusFile));
  if (fs.existsSync(statusFile)) {
    console.error('[E2E] Status file contents:', fs.readFileSync(statusFile, 'utf8'));
  }
  const logsDir = path.join(statusDir, 'user-data', 'logs');
  if (fs.existsSync(logsDir)) {
    const files = fs.readdirSync(logsDir).filter(f => f.startsWith('backend'));
    if (files.length > 0) {
      const backendLog = path.join(logsDir, files[files.length - 1]);
      const logContent = fs.readFileSync(backendLog, 'utf8');
      console.error('[E2E] Backend log (last 1000 chars):', logContent.substring(Math.max(0, logContent.length - 1000)));
    }
  }
  console.error('[E2E] stderr (first 500 chars):', (logOutput.stderr || '').substring(0, 500));
  console.error('[E2E] stdout (first 500 chars):', (logOutput.stdout || '').substring(0, 500));
}

/**
 * Create a temp workspace with a dummy file and launch Electron.
 * Returns { app, workspaceDir, statusDir, statusFile, port }.
 */
async function setupTestWorkspace() {
  assertBackendBinary();
  const statusDir = fs.mkdtempSync(path.join(os.tmpdir(), 'sprout-e2e-'));
  const statusFile = path.join(statusDir, 'status.json');
  const workspaceDir = path.join(statusDir, 'workspace');
  fs.mkdirSync(workspaceDir, { recursive: true });
  fs.writeFileSync(path.join(workspaceDir, 'hello.txt'), 'Hello from E2E test workspace\n');

  const app = launchElectron(workspaceDir, statusFile);
  const status = await waitForWorkspaceReady(statusFile, 45000);
  return { app, workspaceDir, statusDir, statusFile, port: status.port };
}

/**
 * Cleanup after a test.
 */
async function cleanupTest(app, statusDir, statusFile) {
  if (app) {
    logDiagnostics(app, statusDir, statusFile);
    await closeElectron(app.child);
  }
  try {
    fs.rmSync(statusDir, { recursive: true, force: true });
  } catch {
    // Ignore cleanup failures
  }
}

// ============================================================================
// TEST CASES
// ============================================================================

test.describe('Desktop E2E smoke', () => {
  test('launches workspace, backend health responds, and UI loads', async () => {
    test.setTimeout(90000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      // Get health and verify fields
      const health = await pollHealth(setup.port, 15000);
      expect(health.status).toBe('ok');
      expect(health.port).toBe(setup.port);
      expect(typeof health.port).toBe('number');
      expect(health.port).toBeGreaterThan(0);
      expect(typeof health.uptime).toBe('string');
      expect(health.uptime.length).toBeGreaterThan(0);
      expect(/\d/.test(health.uptime)).toBe(true);

      // Verify webui loads
      await checkWebuiLoads(setup.port);

      // Verify onboarding status endpoint responds
      const onboarding = await httpGetJson(setup.port, '/api/onboarding/status');
      expect(onboarding).toBeTruthy();
    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('workspace API responds with correct paths', async () => {
    test.setTimeout(90000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      await pollHealth(setup.port, 15000);
      const workspaceInfo = await httpGetJson(setup.port, '/api/workspace');

      expect(workspaceInfo).toBeTruthy();
      expect(typeof workspaceInfo.workspace_root).toBe('string');
      expect(typeof workspaceInfo.daemon_root).toBe('string');
      expect(workspaceInfo.workspace_root).toBe(path.resolve(setup.workspaceDir));
    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('files API returns workspace files', async () => {
    test.setTimeout(90000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      await pollHealth(setup.port, 15000);
      const filesResponse = await httpGetJson(setup.port, '/api/files');

      expect(filesResponse).toBeTruthy();
      // API response shape varies (tree structure), so we check for file presence flexibly
      const responseStr = JSON.stringify(filesResponse).toLowerCase();
      expect(responseStr).toContain('hello.txt');
    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('status file contains all expected fields', async () => {
    test.setTimeout(90000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      // waitForWorkspaceReady already verified it exists
      expect(fs.existsSync(setup.statusFile)).toBe(true);
      const rawStatus = fs.readFileSync(setup.statusFile, 'utf8').trim();
      expect(rawStatus).toBeTruthy();

      const statusData = JSON.parse(rawStatus);
      expect(statusData.event).toBe('workspace-opened');
      expect(typeof statusData.port).toBe('number');
      expect(statusData.port).toBeGreaterThan(0);
      expect(typeof statusData.workspacePath).toBe('string');
      expect(statusData.workspacePath.length).toBeGreaterThan(0);
      expect(typeof statusData.url).toBe('string');
      expect(statusData.url).toContain('http://');
      expect(typeof statusData.timestamp).toBe('string');
      expect(statusData.timestamp).toMatch(/\d{4}-\d{2}-\d{2}/);
    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });
});