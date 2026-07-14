#!/usr/bin/env node
// SP-087-2 — Start the full e2e test stack (sprout backend + Vite dev server)
//
// Usage:
//   npx tsx test/webui/start-stack.mjs
//
// Writes .ports.json to process.cwd() with discovered ports.
// Gracefully shuts down on SIGINT / SIGTERM.

// NOTE: We import .ts files here — this script MUST be run via `npx tsx`
// because Node ESM cannot natively load TypeScript modules.
import { startSprout, pickFreePort } from './fixtures/sprout.ts';
import { startViteDevServer } from './fixtures/vite.ts';
import fs from 'node:fs';
import path from 'node:path';

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const SPROUT_PORT = parseInt(process.env.SPROUT_PORT ?? '', 10) || undefined;
const VITE_PORT = parseInt(process.env.VITE_PORT ?? '', 10) || undefined;

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  // Pick free ports (or use env overrides)
  const sproutPort = SPROUT_PORT ?? (await pickFreePort());
  const vitePort = VITE_PORT ?? (await pickFreePort());

  console.log(`Starting sprout backend on port ${sproutPort} ...`);
  const sprout = await startSprout({ port: sproutPort });
  console.log(`✓ sprout ready at ${sprout.baseUrl}`);

  console.log(`Starting Vite dev server on port ${vitePort} ...`);
  const vite = await startViteDevServer({ port: vitePort, sproutBackendUrl: sprout.baseUrl });
  console.log(`✓ Vite ready at ${vite.url}`);

  // Write .ports.json so Playwright / other tooling can discover the ports
  const portsFile = path.join(process.cwd(), '.ports.json');
  fs.writeFileSync(
    portsFile,
    JSON.stringify(
      {
        sprout: sproutPort,
        vite: vitePort,
        sproutBaseUrl: sprout.baseUrl,
        viteUrl: vite.url,
      },
      null,
      2
    ),
    'utf8'
  );
  console.log(`Ports written to ${portsFile}`);

  // Signal handlers for graceful shutdown
  const cleanup = async (signal) => {
    console.log(`\nReceived ${signal}, shutting down ...`);
    await vite.stop();
    await sprout.stop();
    // Remove the ports file
    try {
      fs.unlinkSync(portsFile);
    } catch {
      // ignore
    }
    process.exit(0);
  };

  const handleSignal = (signal) => {
    cleanup(signal).catch((err) => {
      console.error(`Cleanup error during ${signal}:`, err);
      process.exit(1);
    });
  };
  process.on('SIGINT', () => handleSignal('SIGINT'));
  process.on('SIGTERM', () => handleSignal('SIGTERM'));

  // Keep the process alive indefinitely
  await new Promise(() => {});
}

main().catch((err) => {
  console.error('Failed to start stack:', err);
  process.exit(1);
});
