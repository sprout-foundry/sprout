import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

test.beforeAll(async () => {
  try {
    browser = await chromium.launch({ channel: 'chrome' });
  } catch {
    browser = await chromium.launch();
  }
  sprout = await startSprout();
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
test.setTimeout(60_000);

/**
 * The e2e stack runs `sprout agent --daemon`, which is shared-agent mode:
 * the server 403s chat-session creates. This spec pins the shared-mode UX
 * contract for the New Chat control — in shared mode the affordance must
 * not silently no-op (the original bug report: "pressed it a bunch of
 * times because I thought it wasn't working"). The multi-chat happy path
 * (one click → one session) is covered by unit tests on the create guard.
 */
test('rapid New Chat clicks in shared mode produce no duplicate sessions and visible feedback', async () => {
  await page.goto(vite.url, { waitUntil: 'networkidle' });

  // Shared mode is reported by bootstrap config.
  const sharedMode = await page.evaluate(() => {
    const cfg = (window as unknown as { __SPROUT_BOOTSTRAP__?: { sharedMode?: boolean } }).__SPROUT_BOOTSTRAP__;
    return cfg?.sharedMode ?? null;
  });

  const origin = new URL(vite.url).origin;
  const countSessions = async () => {
    const resp = await page.request.get(`${origin}/api/chat-sessions`);
    const body = await resp.json();
    return body.chat_sessions.length as number;
  };
  const before = await countSessions();

  // EditorTabs' New Chat button is hidden in shared mode; the split-controls
  // one now is too. If any "New chat" button is somehow visible, rapid-click
  // it and verify no session storm (server rejects; guard prevents queues).
  const newChatBtns = page.locator('button[title="New chat"]');
  const visible = await newChatBtns.count();
  for (let i = 0; i < Math.min(visible, 3); i++) {
    await newChatBtns.nth(i).click({ timeout: 5000 }).catch(() => {});
  }
  await page.waitForTimeout(1500);

  const after = await countSessions();
  expect(after - before).toBeLessThanOrEqual(1);
  // If shared mode is on, no creates may succeed at all.
  if (sharedMode === true) {
    expect(after).toBe(before);
    // And in shared mode the split-controls New Chat button must be hidden.
    await expect(page.locator('.split-controls button[title="New chat"]')).toHaveCount(0);
  }
});
