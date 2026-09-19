/**
 * Captures DesignView screenshots for the SP-140 PR body.
 *
 * Uses the same e2e fixture harness as the DesignView spec (a real sprout
 * backend with a seeded design/ tree + a Vite dev server), so the shots show
 * the feature as a user sees it rather than a static mock.
 *
 *   npx tsx scripts/capture-design-screenshots.mts
 *
 * Output: docs/pr-assets/designview-{flows,screens,tokens,feedback}.png
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

// Seed a §4d feedback file so the PR screenshot shows the populated resolution
// pane rather than the absent-file state. Shape and path mirror what the webui
// write path produces (design/feedback/<asset stem>.json), which is the
// contract pinned by pkg/design/feedback_contract_test.go.
fs.mkdirSync(path.join(workspaceDir, 'design', 'feedback'), { recursive: true });
fs.writeFileSync(
  path.join(workspaceDir, 'design', 'feedback', 'login.json'),
  `${JSON.stringify(
    {
      target: 'design/screens/login.html',
      status: 'changes-requested',
      resolution: '',
      annotations: [
        {
          id: 'a1',
          at: { x: 0.42, y: 0.18 },
          area: 'hierarchy',
          note: 'Primary CTA reads as secondary; swap the emphasis',
          resolved: false,
          created: '2026-09-15T10:36:47Z',
        },
        {
          id: 'a2',
          at: { x: 0.5, y: 0.72 },
          area: 'spacing',
          note: 'Submit button sits too close to the field above it',
          resolved: true,
          created: '2026-09-15T10:41:02Z',
        },
      ],
    },
    null,
    2,
  )}\n`,
  'utf8',
);

const sprout = await startSprout({ workspaceDir });
const vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });

let browser;
try {
  browser = await chromium.launch({ channel: 'chrome' });
} catch {
  browser = await chromium.launch();
}

const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });

async function openDesign(): Promise<void> {
  await page.goto(vite.url, { waitUntil: 'networkidle' });
  await page.getByTestId('chat-shell').waitFor({ timeout: 30_000 });
  await page.getByTestId('sidebar-brand-trigger').click();
  await page.getByTestId('sidebar-brand-option-design').click();
  await page.getByTestId('design-view').waitFor({ timeout: 30_000 });
}

async function shot(name: string): Promise<void> {
  const file = path.join(OUT, `designview-${name}.png`);
  await page.screenshot({ path: file });
  console.log(`wrote ${file}`);
}

// 1. Flows tab — the graph with wireframe imagery and edge labels.
await openDesign();
await page.getByTestId('design-flow-graph').waitFor({ timeout: 30_000 });
await page.waitForTimeout(1500);
await shot('flows');

// 2. Screens tab — the thumbnail grid with README status chips.
await page.getByTestId('design-rail-screens').click();
await page.getByTestId('design-screens-cards').waitFor({ timeout: 30_000 });
await page.waitForTimeout(1200);
await shot('screens');

// 3. Tokens tab — the grouped DTCG tree with colour swatches.
await page.getByTestId('design-rail-tokens').click();
await page.getByTestId('design-tokens-tree').waitFor({ timeout: 30_000 });
await page.waitForTimeout(1200);
await shot('tokens');

// 4. Feedback pane — annotate a screen, showing the §4d write path.
await page.getByTestId('design-rail-screens').click();
await page.getByTestId('design-screen-card-login').click();
await page.getByTestId('design-feedback-affordance').waitFor({ timeout: 30_000 });
await page.waitForTimeout(800);
await shot('feedback');

await browser.close();
await vite.stop();
await sprout.stop();
removeDesignWorkspace(workspaceDir);
process.exit(0);
