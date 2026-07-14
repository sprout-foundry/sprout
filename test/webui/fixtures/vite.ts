// SP-087-2 — Playwright fixture: Vite dev server
//
// Spawns `npx vite` in the webui/ directory so that e2e tests can
// load the React web UI.  The server is stopped (SIGTERM → SIGKILL)
// when stop() is called.

import { spawn } from 'node:child_process';
import path from 'node:path';

// Resolve the webui directory: test/webui/fixtures/vite.ts → webui/
const WEBUI_DIR = path.resolve(__dirname, '..', '..', '..', 'webui');

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface ViteHandle {
  /** TCP port the Vite dev server is listening on */
  port: number;
  /** Base URL for the Vite dev server */
  url: string;
  /** Gracefully shut down the Vite process */
  stop(): Promise<void>;
}

export interface StartViteOptions {
  /** Override the port (default: 3000 from vite.config.ts) */
  port?: number;
}

// ---------------------------------------------------------------------------
// Health-check helper
// ---------------------------------------------------------------------------

async function waitForVite(url: string, timeoutMs = 60_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: Error | undefined;

  while (Date.now() < deadline) {
    try {
      const resp = await fetch(url, { signal: AbortSignal.timeout(3000) });
      if (resp.ok) return;
      lastErr = new Error(`Vite returned ${resp.status}`);
    } catch (err: any) {
      lastErr = err;
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  throw new Error(
    `Vite dev server did not become ready within ${timeoutMs}ms (last error: ${lastErr?.message})`
  );
}

// ---------------------------------------------------------------------------
// Main start function
// ---------------------------------------------------------------------------

export async function startViteDevServer(opts: StartViteOptions = {}): Promise<ViteHandle> {
  const port = opts.port ?? 3000;

  const child = spawn('npx', ['vite', '--host', '127.0.0.1', '--port', String(port), '--strictPort'], {
    cwd: WEBUI_DIR,
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: false,
    env: {
      ...process.env,
      // Prevent vite from opening a browser
      BROWSER: 'none',
    },
  });

  // Catch spawn errors (e.g. vite not found) so they don't crash as unhandled exceptions
  let spawnError: Error | null = null;
  child.once('error', (err) => {
    spawnError = err;
  });

  const url = `http://127.0.0.1:${port}`;

  // Log stdout/stderr for debugging
  child.stdout?.on('data', (chunk: Buffer) => {
    // eslint-disable-next-line no-console
    console.log(`[vite:out] ${chunk.toString().trim()}`);
  });
  child.stderr?.on('data', (chunk: Buffer) => {
    // eslint-disable-next-line no-console
    console.error(`[vite:err] ${chunk.toString().trim()}`);
  });

  // Wait for readiness — wrap so spawn errors surface immediately
  try {
    await waitForVite(url);
  } catch (healthErr) {
    if (spawnError) {
      try { child.kill('SIGKILL'); } catch {}
      throw spawnError;
    }
    throw healthErr;
  }

  return {
    port,
    url,
    async stop() {
      try { child.kill('SIGTERM'); } catch {}
      // Wait up to 5 s for graceful exit
      await new Promise<void>((resolve) => {
        if (child.exitCode !== null) return resolve();
        child.once('exit', () => resolve());
        setTimeout(resolve, 5000);
      });
      // SIGKILL only if still alive
      if (child.exitCode === null && !child.killed) {
        try { child.kill('SIGKILL'); } catch {}
      }
    },
  };
}
