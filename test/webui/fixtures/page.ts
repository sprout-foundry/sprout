// SP-087-2 — Playwright fixture: WebUI page
//
// Creates a browser context + page, navigates to the Vite dev server,
// and provides a cleanup() method to close everything.

import type { Browser, BrowserContext, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface WebUIPageHandle {
  /** The Playwright Page instance */
  page: Page;
  /** The Playwright BrowserContext (for cookies, storage, etc.) */
  context: BrowserContext;
  /** Close the page and browser context */
  cleanup(): Promise<void>;
}

export interface NewWebUIPageOptions {
  /** Playwright Browser instance to use */
  browser: Browser;
  /** URL to navigate to (e.g. Vite dev server URL) */
  url?: string;
}

// ---------------------------------------------------------------------------
// Main factory
// ---------------------------------------------------------------------------

export async function newWebuiPage({
  browser,
  url,
}: NewWebUIPageOptions): Promise<WebUIPageHandle> {
  const context = await browser.newContext({
    // Give the context a reasonable viewport for web UI testing
    viewport: { width: 1280, height: 720 },
  });

  const page = await context.newPage();

  if (url) {
    await page.goto(url, { waitUntil: 'networkidle' });
  }

  return {
    page,
    context,
    async cleanup() {
      await context.close();
    },
  };
}
