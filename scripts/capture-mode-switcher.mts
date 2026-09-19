/**
 * Capture the workspace-mode switcher and the Design surface for review.
 *
 * Run: npx tsx scripts/capture-mode-switcher.mts
 * Writes docs/pr-assets/workspace-*.png
 */

import fs from 'node:fs';
import path from 'node:path';
import { chromium } from '@playwright/test';
import { seedDesignWorkspace, removeDesignWorkspace } from '../test/webui/design_view_fixture.ts';
import { startSprout } from '../test/webui/fixtures/sprout.ts';
import { startViteDevServer } from '../test/webui/fixtures/vite.ts';

const OUT = path.join('docs', 'pr-assets');
fs.mkdirSync(OUT, { recursive: true });

const workspaceDir = seedDesignWorkspace();
const sprout = await startSprout({ workspaceDir });
const vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });

let browser;
try {
  browser = await chromium.launch({ channel: 'chrome' });
} catch {
  browser = await chromium.launch();
}
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });

async function boot(): Promise<void> {
  await page.goto(vite.url, { waitUntil: 'networkidle' });
  // The design-presence probe is a one-shot; let it settle before asserting.
  await page.waitForTimeout(3500);
}

async function shot(name: string): Promise<void> {
  const file = path.join(OUT, `workspace-${name}.png`);
  await page.screenshot({ path: file });
  console.log(`wrote ${file}`);
}

await boot();

// 1. The switcher, open, in Code mode.
await page.getByTestId('sidebar-brand-trigger').click();
await page.waitForTimeout(400);
await shot('switcher-open');
await page.keyboard.press('Escape');
await page.waitForTimeout(300);

// 1b. Code mode after the switcher closes: the Code rail (Git/Files/Search).
await shot('code-rail');

// 2. Switch to Design.
await page.getByTestId('sidebar-brand-trigger').click();
await page.waitForTimeout(400);
await page.getByTestId('sidebar-brand-option-design').click();
await page.waitForTimeout(3000);
await shot('design-flows');

// 3. Screens entry on the Design rail.
await page.getByTestId('design-rail-screens').click();
await page.waitForTimeout(1500);
await shot('design-screens');

// 4. Tokens entry on the Design rail.
await page.getByTestId('design-rail-tokens').click();
await page.waitForTimeout(1500);
await shot('design-tokens');

await browser.close();
await vite.stop();
await sprout.stop();
removeDesignWorkspace(workspaceDir);
process.exit(0);
