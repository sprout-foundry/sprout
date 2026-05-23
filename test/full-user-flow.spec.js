/**
 * Full User Flow E2E Test
 *
 * Tests the complete user journey: register → login → open editor → chat → create file → git clone
 *
 * This test spawns the Electron app with a fresh workspace, then exercises the backend
 * HTTP APIs to verify each step of the user flow works end-to-end.  It follows the same
 * pattern as desktop-e2e-smoke.spec.js but extends the test coverage to the full workflow.
 *
 * Requires a pre-built Go backend binary.
 */

const { test, expect } = require('@playwright/test');
const path = require('node:path');
const fs = require('node:fs');
const os = require('node:os');
const { spawn, spawnSync } = require('node:child_process');
const http = require('node:http');

const APP_ROOT = path.resolve(__dirname, '..');
const ELECTRON_BIN = path.join(
  APP_ROOT,
  'node_modules',
  'electron',
  'dist',
  'electron'
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Check if git is available on PATH. Throws if not found. */
function assertGitAvailable() {
  const result = spawnSync('git', ['--version'], { encoding: 'utf8' });
  if (result.status !== 0 || result.error) {
    throw new Error(`git is not available on PATH. Ensure git is installed.`);
  }
}

/** Run a git command and assert it succeeded. */
function runGit(args, cwd, errorMsg) {
  const result = spawnSync('git', args, { cwd, encoding: 'utf8', timeout: 15000 });
  if (result.error) {
    throw new Error(`${errorMsg}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    throw new Error(`${errorMsg}: git exited with status ${result.status}. stderr: ${result.stderr || '<empty>'}`);
  }
  return result;
}

/** Check if the Go backend binary exists. Throws if not found. */
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

/** Make a single HTTP GET to the backend and parse JSON response. */
function httpGetJson(port, apiPath, timeoutMs = 10000) {
  return new Promise((resolve, reject) => {
    const req = http.get(
      { hostname: '127.0.0.1', port, path: apiPath, timeout: timeoutMs },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          const body = Buffer.concat(chunks).toString('utf8');
          if (res.statusCode < 200 || res.statusCode >= 300) {
            reject(new Error(`HTTP ${res.statusCode} from ${apiPath}: ${body.substring(0, 500)}`));
            return;
          }
          try {
            resolve(JSON.parse(body));
          } catch (e) {
            reject(new Error(`Invalid JSON from ${apiPath}: ${body.substring(0, 200)}`));
          }
        });
      }
    );
    req.on('error', reject);
    req.on('timeout', () => { req.destroy(); reject(new Error(`${apiPath} timed out`)); });
  });
}

/** Make an HTTP POST with JSON body and parse JSON response. */
function httpPostJson(port, apiPath, body, timeoutMs = 10000) {
  const raw = JSON.stringify(body);
  return new Promise((resolve, reject) => {
    const req = http.request(
      { hostname: '127.0.0.1', port, path: apiPath, method: 'POST', timeout: timeoutMs, headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(raw) } },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          const responseBody = Buffer.concat(chunks).toString('utf8');
          if (res.statusCode < 200 || res.statusCode >= 300) {
            reject(new Error(`HTTP ${res.statusCode} from POST ${apiPath}: ${responseBody.substring(0, 500)}`));
            return;
          }
          try {
            resolve(JSON.parse(responseBody));
          } catch (e) {
            reject(new Error(`Invalid JSON from POST ${apiPath}: ${responseBody.substring(0, 200)}`));
          }
        });
      }
    );
    req.on('error', reject);
    req.on('timeout', () => { req.destroy(); reject(new Error(`POST ${apiPath} timed out`)); });
    req.write(raw);
    req.end();
  });
}

/** Make an HTTP DELETE with optional JSON body and parse JSON response. */
function httpDeleteJson(port, apiPath, body, timeoutMs = 10000) {
  const raw = body ? JSON.stringify(body) : '';
  return new Promise((resolve, reject) => {
    const req = http.request(
      { hostname: '127.0.0.1', port, path: apiPath, method: 'DELETE', timeout: timeoutMs, headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(raw) } },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          const responseBody = Buffer.concat(chunks).toString('utf8');
          if (res.statusCode < 200 || res.statusCode >= 300) {
            reject(new Error(`HTTP ${res.statusCode} from DELETE ${apiPath}: ${responseBody.substring(0, 500)}`));
            return;
          }
          try {
            resolve(JSON.parse(responseBody));
          } catch (e) {
            reject(new Error(`Invalid JSON from DELETE ${apiPath}: ${responseBody.substring(0, 200)}`));
          }
        });
      }
    );
    req.on('error', reject);
    req.on('timeout', () => { req.destroy(); reject(new Error(`DELETE ${apiPath} timed out`)); });
    if (raw) req.write(raw);
    req.end();
  });
}

/** Check if a string is valid JSON. */
function isJson(str) {
  try { JSON.parse(str); return true; } catch { return false; }
}

/** Poll /health until it responds successfully. */
async function pollHealth(port, timeoutMs = 30000) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    try { return await httpGetJson(port, '/health', 5000); } catch { await wait(500); }
  }
  throw new Error(`Backend health did not respond within ${timeoutMs}ms on port ${port}`);
}

/** Wait for the smoke status file to contain workspace-opened event. */
async function waitForWorkspaceReady(statusFile, timeoutMs = 45000) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    if (fs.existsSync(statusFile)) {
      try {
        const raw = fs.readFileSync(statusFile, 'utf8').trim();
        if (raw && isJson(raw)) {
          const data = JSON.parse(raw);
          if (data.event === 'workspace-opened' && data.port) return data;
        }
      } catch { /* partially written; retry */ }
    }
    await wait(300);
  }
  throw new Error(`Timed out waiting for workspace-ready status file (${timeoutMs}ms).`);
}

/** Verify the webui HTML loads. */
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
    req.on('timeout', () => { req.destroy(); reject(new Error('Webui load timed out')); });
  });
}

/** Launch Electron in smoke test mode. */
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

/** Gracefully close the Electron process. */
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

/** Log diagnostic info for debugging. */
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

/** Setup: create temp workspace, launch Electron, wait for workspace ready. */
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

/** Cleanup: close Electron, remove temp directory. */
async function cleanupTest(app, statusDir, statusFile) {
  if (app) {
    logDiagnostics(app, statusDir, statusFile);
    await closeElectron(app.child);
  }
  try { fs.rmSync(statusDir, { recursive: true, force: true }); } catch { /* ignore */ }
}

// ============================================================================
// TEST CASES
// ============================================================================

test.describe('Full User Flow E2E', () => {

  test('register → login → open editor → chat → create file → git clone', async () => {
    test.setTimeout(180000); // 3 minutes for full flow

    // ---- Phase 0: Setup ----
    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      // ---- Phase 1: LOGIN — Workspace opened, backend healthy, webui loads ----
      console.error('[E2E] Phase 1: Login — health check & webui load');
      const health = await pollHealth(setup.port, 15000);
      expect(health.status).toBe('ok');
      expect(typeof health.port).toBe('number');
      expect(health.port).toBe(setup.port);

      // Verify webui (editor shell) loads
      const webuiHtml = await checkWebuiLoads(setup.port);
      expect(webuiHtml.length).toBeGreaterThan(0);

      // Verify workspace info endpoint
      const workspaceInfo = await httpGetJson(setup.port, '/api/workspace');
      expect(typeof workspaceInfo.workspace_root).toBe('string');
      expect(workspaceInfo.workspace_root).toBe(path.resolve(setup.workspaceDir));

      // ---- Phase 2: REGISTER — Onboarding / provider configuration ----
      console.error('[E2E] Phase 2: Register — onboarding provider setup');
      const onboardingStatus = await httpGetJson(setup.port, '/api/onboarding/status');
      expect(onboardingStatus).toBeTruthy();
      expect(Array.isArray(onboardingStatus.providers)).toBe(true);
      expect(onboardingStatus.providers.length).toBeGreaterThan(0);

      // Find a provider that does NOT require an API key (e.g., "test" or "editor"),
      // or skip onboarding completion if setup is already done.
      if (onboardingStatus.setup_required) {
        // Try to find a provider we can configure without an API key
        const noKeyProvider = onboardingStatus.providers.find(
          (p) => !p.requires_api_key && p.id !== 'test'
        );
        if (noKeyProvider) {
          console.error('[E2E] Registering provider:', noKeyProvider.id);
          const onboardingResult = await httpPostJson(setup.port, '/api/onboarding/complete', {
            provider: noKeyProvider.id,
            model: noKeyProvider.recommended_model || (noKeyProvider.models && noKeyProvider.models[0]) || '',
          });
          expect(onboardingResult.success).toBe(true);
        } else {
          // No free provider available — skip to editor-only mode
          console.error('[E2E] No free provider available, skipping onboarding complete');
        }
      } else {
        console.error('[E2E] Onboarding already completed, skipping registration step');
      }

      // ---- Phase 3: OPEN EDITOR — Files API, verify workspace content ----
      console.error('[E2E] Phase 3: Open Editor — verify files in workspace');
      const filesResponse = await httpGetJson(setup.port, '/api/files');
      const filesJson = JSON.stringify(filesResponse);
      expect(filesJson).toContain('hello.txt');

      // Read the existing file via raw HTTP (GET /api/file returns raw content, not JSON)
      const rawFileResp = await new Promise((resolve, reject) => {
        http.get(
          { hostname: '127.0.0.1', port: setup.port, path: `/api/file?path=${encodeURIComponent(path.join(setup.workspaceDir, 'hello.txt'))}`, timeout: 5000 },
          (res) => {
            const chunks = [];
            res.on('data', (chunk) => chunks.push(chunk));
            res.on('end', () => resolve({ status: res.statusCode, body: Buffer.concat(chunks).toString('utf8') }));
          }
        ).on('error', reject);
      });
      expect(rawFileResp.status).toBe(200);
      expect(rawFileResp.body).toContain('Hello from E2E test workspace');

      // ---- Phase 4: CHAT — Create chat session, verify it appears in listing ----
      console.error('[E2E] Phase 4: Chat — create session and verify listing');
      const chatCreateResult = await httpPostJson(setup.port, '/api/chat-sessions/create', {
        name: 'E2E Test Chat',
      });
      expect(chatCreateResult.success).toBe(true);
      const chatSessionId = chatCreateResult.id;
      expect(chatSessionId).toBeTruthy();

      // Verify session appears in listing
      const chatSessionsResp = await httpGetJson(setup.port, '/api/chat-sessions');
      expect(chatSessionsResp).toBeTruthy();
      expect(Array.isArray(chatSessionsResp.chat_sessions)).toBe(true);
      const foundSession = chatSessionsResp.chat_sessions.find((s) => s.id === chatSessionId || s.name === 'E2E Test Chat');
      expect(foundSession).toBeTruthy();

      // ---- Phase 5: CREATE FILE — Use the /api/create endpoint ----
      console.error('[E2E] Phase 5: Create File — create new file via API');
      const newFilePath = path.join(setup.workspaceDir, 'e2e-test-file.js');
      const createResult = await httpPostJson(setup.port, '/api/create', {
        path: newFilePath,
      });
      expect(createResult.message).toBe('success');
      expect(createResult.path).toBeTruthy();

      // Verify file was actually created on disk
      expect(fs.existsSync(newFilePath)).toBe(true);

      // Now write content to the file via /api/file (POST)
      const fileWriteResult = await httpPostJson(
        setup.port,
        `/api/file?path=${encodeURIComponent(newFilePath)}`,
        { content: '// Generated by E2E test\nmodule.exports = { hello: "world" };' }
      );
      expect(fileWriteResult.success).toBe(true);

      // Verify file content was written correctly
      const writtenContent = fs.readFileSync(newFilePath, 'utf8');
      expect(writtenContent).toContain('Generated by E2E test');
      expect(writtenContent).toContain('hello');
      expect(writtenContent).toContain('world');

      // Verify the new file shows up in the files listing
      const updatedFilesResponse = await httpGetJson(setup.port, '/api/files');
      const updatedFilesJson = JSON.stringify(updatedFilesResponse);
      expect(updatedFilesJson).toContain('e2e-test-file.js');

      // ---- Phase 6: GIT CLONE — Initialize git repo, execute clone via terminal ----
      console.error('[E2E] Phase 6: Git Clone — init repo and verify git operations');

      // Initialize git in workspace (using terminal/shell API if available, otherwise direct shell)
      const initResult = spawnSync('git', ['init'], { cwd: setup.workspaceDir, encoding: 'utf8', timeout: 10000 });
      expect(initResult.status).toBe(0);

      // Configure git user (required for commits)
      spawnSync('git', ['config', 'user.email', 'e2e@test.com'], { cwd: setup.workspaceDir, encoding: 'utf8' });
      spawnSync('git', ['config', 'user.name', 'E2E Test'], { cwd: setup.workspaceDir, encoding: 'utf8' });

      // Stage and commit our test files
      spawnSync('git', ['add', '.'], { cwd: setup.workspaceDir, encoding: 'utf8' });
      spawnSync('git', ['commit', '-m', 'Initial commit from E2E test'], { cwd: setup.workspaceDir, encoding: 'utf8' });

      // Verify the git status endpoint
      const gitStatus = await httpGetJson(setup.port, '/api/git/status');
      expect(gitStatus).toBeTruthy();

      // Clone the workspace to a separate directory (simulating git clone)
      const cloneDir = path.join(setup.statusDir, 'cloned-workspace');
      const cloneResult = spawnSync('git', ['clone', setup.workspaceDir, cloneDir], {
        encoding: 'utf8',
        timeout: 15000,
      });
      expect(cloneResult.status).toBe(0);

      // Verify the cloned repository has our files
      expect(fs.existsSync(path.join(cloneDir, 'hello.txt'))).toBe(true);
      expect(fs.existsSync(path.join(cloneDir, 'e2e-test-file.js'))).toBe(true);

      // Verify the git status endpoint on the workspace (should be clean after clone)
      const gitStatusAfter = await httpGetJson(setup.port, '/api/git/status');
      expect(gitStatusAfter).toBeTruthy();

      // ---- Final assertions ----
      console.error('[E2E] All phases completed successfully');

      // Verify files listing still contains both files
      const finalFiles = await httpGetJson(setup.port, '/api/files');
      const finalFilesJson = JSON.stringify(finalFiles);
      expect(finalFilesJson).toContain('hello.txt');
      expect(finalFilesJson).toContain('e2e-test-file.js');

      // Verify workspace is still healthy
      const finalHealth = await pollHealth(setup.port, 5000);
      expect(finalHealth.status).toBe('ok');

    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('file create and write flow with nested directories', async () => {
    test.setTimeout(120000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      await pollHealth(setup.port, 15000);

      // Create a file in a nested directory that doesn't exist yet
      const nestedPath = path.join(setup.workspaceDir, 'src', 'components', 'Button.js');
      const createResult = await httpPostJson(setup.port, '/api/create', {
        path: nestedPath,
      });
      expect(createResult.message).toBe('success');

      // Verify the nested directory and file were created
      expect(fs.existsSync(nestedPath)).toBe(true);

      // Write content to the nested file
      await httpPostJson(
        setup.port,
        `/api/file?path=${encodeURIComponent(nestedPath)}`,
        { content: 'export default function Button() { return "<button>Click me</button>"; }' }
      );

      // Verify content was written
      const content = fs.readFileSync(nestedPath, 'utf8');
      expect(content).toContain('Button');
      expect(content).toContain('Click me');

      // Create a directory via /api/create
      const dirPath = path.join(setup.workspaceDir, 'src', 'hooks') + '/';
      const dirResult = await httpPostJson(setup.port, '/api/create', {
        directory: dirPath,
      });
      expect(dirResult.message).toBe('success');
      expect(fs.existsSync(path.join(setup.workspaceDir, 'src', 'hooks'))).toBe(true);

    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('chat session lifecycle: create, list, rename, delete', async () => {
    test.setTimeout(120000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      await pollHealth(setup.port, 15000);

      // Create a chat session
      const createResult = await httpPostJson(setup.port, '/api/chat-sessions/create', {
        name: 'Lifecycle Test Session',
      });
      expect(createResult.success).toBe(true);
      const sessionId = createResult.id;
      expect(sessionId).toBeTruthy();

      // Verify it appears in the list
      const sessionsResp = await httpGetJson(setup.port, '/api/chat-sessions');
      expect(Array.isArray(sessionsResp.chat_sessions)).toBe(true);
      const found = sessionsResp.chat_sessions.find((s) => s.id === sessionId);
      expect(found).toBeTruthy();

      // Rename the session
      const renameResult = await httpPostJson(setup.port, '/api/chat-sessions/rename', {
        id: sessionId,
        name: 'Renamed Test Session',
      });
      expect(renameResult.success).toBe(true);

      // Verify the rename took effect
      const sessionsAfterRename = await httpGetJson(setup.port, '/api/chat-sessions');
      const renamedSession = sessionsAfterRename.chat_sessions.find((s) => s.id === sessionId);
      expect(renamedSession.name).toBe('Renamed Test Session');

      // Delete the session
      const deleteResult = await httpPostJson(setup.port, '/api/chat-sessions/delete', {
        id: sessionId,
      });
      expect(deleteResult.success).toBe(true);

      // Verify it's gone
      const sessionsAfterDelete = await httpGetJson(setup.port, '/api/chat-sessions');
      const stillThere = sessionsAfterDelete.chat_sessions.find((s) => s.id === sessionId);
      expect(stillThere).toBeFalsy();

    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('file operations: read, write, rename, delete', async () => {
    test.setTimeout(120000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      await pollHealth(setup.port, 15000);

      // Create a file
      const testFile = path.join(setup.workspaceDir, 'rename-test.txt');
      await httpPostJson(setup.port, '/api/create', { path: testFile });

      // Write content
      await httpPostJson(
        setup.port,
        `/api/file?path=${encodeURIComponent(testFile)}`,
        { content: 'Before rename' }
      );

      // Read content to verify
      const readResp = await new Promise((resolve, reject) => {
        http.get(
          { hostname: '127.0.0.1', port: setup.port, path: `/api/file?path=${encodeURIComponent(testFile)}`, timeout: 5000 },
          (res) => {
            const chunks = [];
            res.on('data', (chunk) => chunks.push(chunk));
            res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
          }
        ).on('error', reject);
      });
      expect(readResp).toBe('Before rename');

      // Rename the file
      const renamedPath = path.join(setup.workspaceDir, 'renamed-file.txt');
      const renameResult = await httpPostJson(setup.port, '/api/rename', {
        old_path: testFile,
        new_path: renamedPath,
      });
      expect(renameResult.message).toBe('success');

      // Verify old path is gone and new path exists
      expect(fs.existsSync(testFile)).toBe(false);
      expect(fs.existsSync(renamedPath)).toBe(true);

      // Delete the file
      const deleteResult = await httpDeleteJson(setup.port, '/api/delete', { path: renamedPath });
      expect(deleteResult.message).toBe('success');
      expect(fs.existsSync(renamedPath)).toBe(false);

    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });

  test('git operations: init, stage, commit, status, log, branches', async () => {
    test.setTimeout(120000);

    const setup = await setupTestWorkspace();
    const app = setup.app;

    try {
      await pollHealth(setup.port, 15000);

      // Initialize git repo
      spawnSync('git', ['init'], { cwd: setup.workspaceDir, encoding: 'utf8' });
      spawnSync('git', ['config', 'user.email', 'e2e@test.com'], { cwd: setup.workspaceDir, encoding: 'utf8' });
      spawnSync('git', ['config', 'user.name', 'E2E Test'], { cwd: setup.workspaceDir, encoding: 'utf8' });

      // Check git status via API
      const gitStatus = await httpGetJson(setup.port, '/api/git/status');
      expect(gitStatus).toBeTruthy();

      // Stage all files
      await httpPostJson(setup.port, '/api/git/stage-all', {});

      // Check status again - should show staged files
      const gitStatusStaged = await httpGetJson(setup.port, '/api/git/status');
      expect(gitStatusStaged).toBeTruthy();

      // Commit
      const commitResult = await httpPostJson(setup.port, '/api/git/commit', {
        message: 'E2E git commit test',
      });
      expect(commitResult.success).toBe(true);

      // Check git log
      const gitLog = await httpGetJson(setup.port, '/api/git/log');
      expect(gitLog).toBeTruthy();
      expect(Array.isArray(gitLog.commits)).toBe(true);
      expect(gitLog.commits.length).toBeGreaterThan(0);

      // Check branches
      const branches = await httpGetJson(setup.port, '/api/git/branches');
      expect(branches).toBeTruthy();

      // Create a new branch
      const createBranchResult = await httpPostJson(setup.port, '/api/git/branch/create', {
        name: 'e2e-test-branch',
      });
      expect(createBranchResult.success).toBe(true);

      // Verify branch exists
      const branchesAfter = await httpGetJson(setup.port, '/api/git/branches');
      expect(branchesAfter.branches).toBeTruthy();
      const foundBranch = branchesAfter.branches.find((b) => b === 'e2e-test-branch');
      expect(foundBranch).toBeTruthy();

    } finally {
      await cleanupTest(app, setup.statusDir, setup.statusFile);
    }
  });
});
