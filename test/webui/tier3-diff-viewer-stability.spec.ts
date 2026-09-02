// Regression: the diff viewer must not constantly rebuild itself.
//
// Bug (fixed 2026-09-01): `.workspace-tab` sized to its content inside the
// row-flex `.editor-pane-host`, and `.workspace-diff-merge-wrapper` had no
// height bound. The merge wrapper's measured width therefore depended on
// the rendered diff mode (unified content ≈ 604px, side-by-side ≈ 519px),
// straddling NARROW_PANE_PX (560). The ResizeObserver-driven mode degrade
// flipped side-by-side↔unified forever — ~30 view destroy/rebuild cycles
// per second ("constantly re-loads").
//
// Instruments:
// 1. Count of /api/git/diff requests over a 10s idle window after opening.
// 2. MutationObserver: .cm-mergeView rebuilds and narrow-mode toggles.
// 3. Layout sanity: merge container bounded inside the pane host.

import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { execSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import TESTIDS from './testids';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

function seedWorkspace(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sprout-diff-stability-'));
  const run = (cmd: string) => execSync(cmd, { cwd: dir });
  run('git init -q');
  run('git config user.email t@t.t');
  run('git config user.name t');
  // ~700-line file so the diff has real scroll height and wide-enough
  // content that side-by-side vs unified measure different widths.
  const lines = Array.from({ length: 700 }, (_, i) => `line ${i + 1} of the file`);
  fs.writeFileSync(path.join(dir, 'big.txt'), lines.join('\n') + '\n');
  run('git add big.txt');
  run('git commit -qm base');
  const mod = lines.map((l, i) => (i % 80 === 0 ? l.toUpperCase() + ' CHANGED' : l));
  fs.writeFileSync(path.join(dir, 'big.txt'), mod.join('\n') + '\n');
  return dir;
}

test.beforeAll(async () => {
  browser = await chromium.launch();
  sprout = await startSprout({ workspaceDir: seedWorkspace() });
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  handle = await newWebuiPage({ browser, url: vite.url });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: 'serial' });
test.setTimeout(120_000);

test('diff viewer does not constantly reload', async () => {
  await page.goto(vite.url, { waitUntil: 'networkidle' });
  await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

  const diffRequests: number[] = [];
  page.on('request', (req) => {
    if (req.url().includes('/api/git/diff')) diffRequests.push(Date.now());
  });

  await page.getByTestId(TESTIDS['sidebar-git-tab']).click();
  await page.waitForTimeout(1500);

  const fileRow = page.locator('.git-sidebar-file-row').first();
  await expect(fileRow).toBeVisible({ timeout: 5000 });
  await fileRow.click();
  await page.waitForTimeout(3000);

  // Count view rebuilds and narrow-mode toggles over a 10s idle window.
  const stats = await page.evaluate(() => {
    return new Promise<Record<string, number>>((resolve) => {
      const counts = { mergeViewAdded: 0, narrowToggles: 0 };
      let lastNarrow: string | null = null;
      const hasMerge = (el: Element) =>
        (el as HTMLElement).classList?.contains('cm-mergeView') || !!el.querySelector?.('.cm-mergeView');
      const mo = new MutationObserver((muts) => {
        for (const m of muts) {
          for (const n of m.addedNodes) if (n.nodeType === 1 && hasMerge(n)) counts.mergeViewAdded++;
        }
        const hint = document.querySelector('.merge-view-narrow-hint');
        if (hint && lastNarrow !== 'on') {
          counts.narrowToggles++;
          lastNarrow = 'on';
        } else if (!hint && lastNarrow === 'on') {
          counts.narrowToggles++;
          lastNarrow = 'off';
        }
      });
      mo.observe(document.body, { childList: true, subtree: true });
      setTimeout(() => {
        mo.disconnect();
        resolve(counts);
      }, 10000);
    });
  });

  // Layout: the merge container must be bounded by the pane, not by content.
  const layout = await page.evaluate(() => {
    const container = document.querySelector('.merge-view-container') as HTMLElement | null;
    const tab = document.querySelector('.workspace-tab') as HTMLElement | null;
    const paneHost = document.querySelector('.editor-pane-host') as HTMLElement | null;
    return {
      containerH: container?.offsetHeight ?? 0,
      tabH: tab?.offsetHeight ?? 0,
      tabW: tab?.offsetWidth ?? 0,
      paneHostH: paneHost?.offsetHeight ?? 0,
      paneHostW: paneHost?.offsetWidth ?? 0,
    };
  });

  test.info().annotations.push({ type: 'note', description: `stats: ${JSON.stringify(stats)}` });
  test.info().annotations.push({ type: 'note', description: `layout: ${JSON.stringify(layout)}` });
  test.info().annotations.push({ type: 'note', description: `diffRequests: ${diffRequests.length}` });

  // No refetch loop, no rebuild loop, no mode-flip loop while idle.
  expect(diffRequests.length, 'git diff refetches over 10s idle window').toBeLessThan(3);
  expect(stats.mergeViewAdded, 'merge view rebuilds over 10s idle window').toBeLessThan(3);
  expect(stats.narrowToggles, 'narrow-mode toggles over 10s idle window').toBe(0);
  // Tab fills its host; the merge container is bounded inside it.
  expect(layout.tabW).toBeGreaterThan(layout.paneHostW - 4);
  expect(layout.containerH).toBeGreaterThan(0);
  expect(layout.containerH).toBeLessThanOrEqual(layout.tabH);
});
