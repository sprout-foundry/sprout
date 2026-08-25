// SP-087-2 — Playwright fixture: sprout backend server
//
// Spawns the real `sprout` binary in daemon + mock-LLM mode so that
// e2e tests have a live backend to talk to.  Temp config/workspace
// directories are cleaned up when stop() is called.

import { spawn } from 'node:child_process';
import net from 'node:net';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';

// Resolve the repo root three levels up from this file:
//   test/webui/fixtures/sprout.ts → test/webui → test → <repo-root>
const REPO_ROOT = path.resolve(__dirname, '..', '..', '..');

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface SproutHandle {
  /** TCP port the sprout web server is listening on */
  port: number;
  /** Base URL for HTTP API calls */
  baseUrl: string;
  /** WebSocket URL (ws://) */
  wsUrl: string;
  /** Temporary config directory (HOME / XDG_CONFIG_HOME) */
  configDir: string;
  /** Temporary workspace directory (CWD for the sprout process) */
  workspaceDir: string;
  /** Gracefully shut down the sprout process and remove temp dirs */
  stop(): Promise<void>;
}

export interface StartSproutOptions {
  /** Override the port (default: OS-assigned via pickFreePort) */
  port?: number;
  /** Disable mock-LLM (default: true — always use mock LLM for tests) */
  mockLLM?: boolean;
  /** Pre-populated workspace directory (default: fresh temp dir) */
  workspaceDir?: string;
}

// ---------------------------------------------------------------------------
// Port helper
// ---------------------------------------------------------------------------

export function pickFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(0, () => {
      const addr = srv.address();
      if (typeof addr === 'object' && addr !== null) {
        srv.close(() => resolve(addr.port));
      } else {
        srv.close(() => reject(new Error('Could not determine free port')));
      }
    });
    srv.on('error', reject);
  });
}

// ---------------------------------------------------------------------------
// Health-check helper
// ---------------------------------------------------------------------------

async function waitForHealth(baseUrl: string, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: Error | undefined;

  while (Date.now() < deadline) {
    try {
      const resp = await fetch(`${baseUrl}/health`, { signal: AbortSignal.timeout(2000) });
      if (resp.ok) return;
      lastErr = new Error(`Health check returned ${resp.status}`);
    } catch (err: any) {
      lastErr = err;
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  throw new Error(
    `sprout did not become ready within ${timeoutMs}ms (last error: ${lastErr?.message})`
  );
}

// ---------------------------------------------------------------------------
// Temp directory helpers
// ---------------------------------------------------------------------------

function createTempDir(prefix: string): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

async function removeDir(dir: string) {
  if (!fs.existsSync(dir)) return;
  await fs.promises.rm(dir, { recursive: true, force: true, maxRetries: 3 });
}

// ---------------------------------------------------------------------------
// Main start function
// ---------------------------------------------------------------------------

export async function startSprout(opts: StartSproutOptions = {}): Promise<SproutHandle> {
  const { port: userPort, mockLLM = true, workspaceDir: userWorkspace } = opts;

  // Create temp directories
  const configDir = createTempDir('sprout-e2e-config-');
  const workspaceDir = userWorkspace ?? createTempDir('sprout-e2e-workspace-');

  // Pick a free port
  const port = userPort ?? (await pickFreePort());

  // Build CLI args
  // NOTE: `sprout serve` hardcodes --mock-llm and --daemon but does NOT
  // expose --web-port.  Use `sprout agent --mock-llm --daemon --web-port`
  // directly so we can control the port.
  const args: string[] = ['agent', '--web-port', String(port)];
  if (mockLLM) {
    args.push('--mock-llm');
  }
  args.push('--daemon');

  // Environment overrides for isolation
  // Strip CI env vars — IsCI() in agent_modes.go disables the web UI when
  // CI=true, which would prevent the web server from starting during E2E.
  const env: Record<string, string> = {
    ...process.env,
    HOME: configDir,
    XDG_CONFIG_HOME: path.join(configDir, '.config'),
    XDG_DATA_HOME: path.join(configDir, '.local', 'share'),
    // Ensure sprout does not try to open a browser
    BROWSER: 'none',
    // Skip connection check to speed up startup
    SPROUT_NO_CONNECTION_CHECK: '1',
  };
  delete env.CI;
  delete env.GITHUB_ACTIONS;

  const bin = path.join(REPO_ROOT, 'sprout');
  const child = spawn(bin, args, {
    env,
    cwd: workspaceDir,
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: false,
  });

  // Catch spawn errors (e.g. binary not found) so they don't crash as unhandled exceptions
  let spawnError: Error | null = null;
  child.once('error', (err) => {
    spawnError = err;
  });

  const baseUrl = `http://127.0.0.1:${port}`;
  const wsUrl = `ws://127.0.0.1:${port}`;

  // Log stdout/stderr for debugging
  child.stdout?.on('data', (chunk: Buffer) => {
    // eslint-disable-next-line no-console
    console.log(`[sprout:out] ${chunk.toString().trim()}`);
  });
  child.stderr?.on('data', (chunk: Buffer) => {
    // eslint-disable-next-line no-console
    console.error(`[sprout:err] ${chunk.toString().trim()}`);
  });

  // Wait for readiness — wrap so spawn errors surface immediately
  try {
    await waitForHealth(baseUrl);
  } catch (healthErr) {
    if (spawnError) {
      try { child.kill('SIGKILL'); } catch {}
      throw spawnError;
    }
    throw healthErr;
  }

  return {
    port,
    baseUrl,
    wsUrl,
    configDir,
    workspaceDir,
    async stop() {
      try { child.kill('SIGTERM'); } catch {}
      // Wait up to 3 s for graceful exit
      await new Promise<void>((resolve) => {
        if (child.exitCode !== null) return resolve();
        child.once('exit', () => resolve());
        setTimeout(resolve, 3000);
      });
      // SIGKILL only if still alive
      if (child.exitCode === null && !child.killed) {
        try { child.kill('SIGKILL'); } catch {}
      }
      // Cleanup temp dirs
      await removeDir(configDir);
      if (!userWorkspace) {
        await removeDir(workspaceDir);
      }
    },
  };
}
