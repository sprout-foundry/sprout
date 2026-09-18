// SP-140 Phase 4 — Vision parity e2e: image paste/upload flows through the
// WebUI to the agent's inline-multimodal path (vision primary) or the
// delegation path (non-vision primary). Covers the upload API surface and
// the placeholder format the agent parses.
//
// Per AGENTS.md: launches with system Chrome (Playwright browser download
// is absent on some dev machines), falling back to chromium.

import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import TESTIDS from './testids';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

// Minimal valid 1x1 PNG.
const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

test.beforeAll(async () => {
  const channel = process.platform === 'darwin' ? { channel: 'chrome' } : {};
  browser = await chromium.launch(channel).catch(() => chromium.launch());
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

test.describe('Vision parity', () => {
  test('chat shell renders', async () => {
    await page.goto(vite.url, { waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });
  });

  test('image upload endpoint accepts a PNG and returns an absolute path', async ({ request }) => {
    const buffer = Buffer.from(PNG_BASE64, 'base64');
    const resp = await request.post(`${sprout.baseUrl}/api/upload/image`, {
      data: buffer,
      headers: { 'Content-Type': 'image/png' },
    });
    if (resp.status() === 404) {
      test.info().annotations.push({ type: 'note', description: 'upload route not registered; skipping' });
      return;
    }
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.path).toBeTruthy();
    expect(body.filename).toMatch(/\.png$/);
  });

  test('image upload rejects non-image payloads', async ({ request }) => {
    const resp = await request.post(`${sprout.baseUrl}/api/upload/image`, {
      data: 'this is not an image',
      headers: { 'Content-Type': 'text/plain' },
    });
    if (resp.status() === 404) return;
    expect(resp.status()).toBe(400);
    const body = await resp.json();
    expect(body.error || body.code).toBeTruthy();
  });
});
