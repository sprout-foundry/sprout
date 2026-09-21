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

// Force the full context profile for the capture backend. Without this the
// mock model's 128K window auto-detects Low-Context Mode, whose 13-tool
// allowlist excludes the design_* tools — the fallback parser then refuses
// to recover the scripted design_validate call (unknown tool) and the chat
// shows narration only. The DesignView PR shots must show the design loop,
// so the design tools must be registered. The workspace layer reads
// workspace.json (config.json is the GLOBAL layer's filename).
const wsConfigDir = path.join(workspaceDir, '.sprout');
fs.mkdirSync(wsConfigDir, { recursive: true });
fs.writeFileSync(
  path.join(wsConfigDir, 'workspace.json'),
  JSON.stringify({ context_mode: 'full' }, null, 2) + '\n',
  'utf8',
);

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

// The queue must be in the environment BEFORE startSprout() spawns the
// backend — the mock provider reads SPROUT_MOCK_LLM_QUEUE once at agent
// creation (first chat). Entries are separated by the same marker the Go
// side splits on; each entry is a full assistant message. Entry 1 carries
// an inline fenced-JSON design_validate call that seed's loop
// fallback-parses into a real structured tool call and executes against
// the seeded design/ tree; entry 2 is the synthesis over the real findings.
const DESIGN_VALIDATE_PROMPT = 'Check the design tree for convention issues before I ship the login screen.';

const MOCK_QUEUE = [
  // Turn 1: narration + a real design_validate call.
  'Let me validate the design tree for convention issues.\n\n```json\n{"tool_calls":[{"type":"function","function":{"name":"design_validate","arguments":"{}"}}]}\n```',
  // Turn 2: synthesis over the real tool output the loop fed back.
  'The design tree validates cleanly — zero error-severity findings, and drift reads design-ahead, the healthy state for an active project. The login screen is safe to build against: color.brand, spacing, and the manifest all satisfy the conventions, and the two open feedback items are cosmetic.',
];
process.env.SPROUT_MOCK_LLM_QUEUE = MOCK_QUEUE.join('\n---MOCK-LIGHTWEIGHT-TURN---\n');

const sprout = await startSprout({ workspaceDir });
const vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });

let browser;
try {
  browser = await chromium.launch({ channel: 'chrome' });
} catch {
  browser = await chromium.launch();
}

const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });

// Seed a stable client id BEFORE app code runs. On a fresh profile the app
// can race its own id minting (WS connect uses getWebUIClientId while API
// fetches use resolveWebUIClientId); if they diverge, the server's
// multi-tab consistency filter drops every query event and the chat shows
// "Processing…" forever. A pre-seeded id pins both paths to one identity.
const SEEDED_CLIENT_ID = '11111111-2222-4333-8444-555555555555';
await page.addInitScript(
  ([id]) => {
    window.sessionStorage.setItem('sprout.webuiClientId', id);
    window.name = 'sproutClientId:' + id;
  },
  [SEEDED_CLIENT_ID],
);

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

// 8. Agent tab — the side column's Agent tab is active (SP-140-6 §6f
//    rework: one Details | Agent column instead of a docked panel). The
//    exchange is real: the backend runs --mock-llm with SPROUT_MOCK_LLM_QUEUE
//    seeded above startSprout(); the queue's first entry is an inline
//    fenced-JSON design_validate tool call that seed's loop fallback-parses
//    and executes against the real seeded design/ tree, so the shot shows a
//    genuine tool run — narration, tool badge, streamed synthesis — not a
//    staged transcript.
const panel = page.getByTestId('design-agent-panel');
await page.getByTestId('design-side-tab-agent').click();
await panel.waitFor({ timeout: 30_000 });

// Type the design prompt into the panel's chat and send it, driving a real
// agent turn: mock LLM → fallback-parsed design_validate → tool execution
// against the seeded tree → streamed synthesis.
const chatInput = page.locator('[data-testid="design-agent-chat"] textarea').first();
await chatInput.waitFor({ timeout: 30_000 });
await chatInput.fill(DESIGN_VALIDATE_PROMPT);
await chatInput.press('Enter');

// Wait for the user bubble, then the tool badge (the UI renders
// design_validate as "design validate" — underscores become spaces — via
// getShortToolName), then the final synthesis bubble: the full loop in one
// frame.
await page.getByTestId('design-agent-chat').getByText(DESIGN_VALIDATE_PROMPT).waitFor({ timeout: 30_000 });
await page.getByTestId('design-agent-chat').getByText(/design\s+validate/i).first().waitFor({ timeout: 30_000 });
await page.getByTestId('design-agent-chat').getByText(/validates cleanly/).waitFor({ timeout: 60_000 });
await page.waitForTimeout(800);
await shot('agent-panel');

await browser.close();
await vite.stop();
await sprout.stop();
removeDesignWorkspace(workspaceDir);
process.exit(0);
