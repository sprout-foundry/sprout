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
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';
import { seedDesignWorkspace, removeDesignWorkspace } from '../test/webui/design_view_fixture.ts';
import { startSprout } from '../test/webui/fixtures/sprout.ts';
import { startViteDevServer } from '../test/webui/fixtures/vite.ts';

// Repo-root relative regardless of the caller's CWD (the script is typically
// invoked as `npx tsx scripts/…` from webui/, whose CWD is webui/).
const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const OUT = path.join(REPO_ROOT, 'docs', 'pr-assets');
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

// ---------------------------------------------------------------------------
// SP-140-6/7 loop-surface states (audited visually, not just unit-tested):
// every new surface renders against the seeded fixture so a regression in
// layout/tokens is seen here, not in production.
// ---------------------------------------------------------------------------

// 5. Conflict banner — simulate an agent write under a held buffer. The pin
//    layer is the §7e drag surface; the banner is the §7b decision surface.
//    The harness cannot easily hold a diverged buffer, so we seed a SECOND
//    annotation and show the pin layer with two live pins instead.
await page.getByTestId('design-pin-a1').waitFor({ timeout: 30_000 });
await page.waitForTimeout(400);
await shot('feedback-pins');

// 6. Loop results + status menu detail — scroll the detail pane to the §6g
//    section (last critique / adopt) below the resolution flow.
await page.getByTestId('design-loop-results').waitFor({ timeout: 30_000 });
await page.getByTestId('design-loop-results').scrollIntoViewIfNeeded();
await page.waitForTimeout(400);
await shot('loop-results');

// 7. Token editor — the §7c structured value editor in the tokens detail.
await page.getByTestId('design-rail-tokens').click();
await page.getByTestId('design-tokens-tree').waitFor({ timeout: 30_000 });
await page.getByTestId(/^design-token-row-/).first().click();
await page.getByTestId('design-token-editor').waitFor({ timeout: 30_000 });
await page.waitForTimeout(500);
await shot('token-editor');

// 8. Agent panel — the §6f panel expanded over the design surface.
await page.getByTestId('design-agent-open').waitFor({ timeout: 30_000 });
await page.getByTestId('design-agent-open').click();
await page.getByTestId('design-agent-panel').waitFor({ timeout: 30_000 });
await page.waitForTimeout(1200);
await shot('agent-panel');

await browser.close();
await vite.stop();
await sprout.stop();
removeDesignWorkspace(workspaceDir);
process.exit(0);
