// Playwright fixture: browser launch with the AGENTS.md fallback chain.
//
// System Chrome is present on dev machines where the Playwright browser
// download is absent, so the chain is: system Chrome → bundled Chromium →
// a named local build (SPROUT_E2E_CHROMIUM, else /usr/bin/chromium).
// Callers own the context/page setup and closing the returned Browser.

import { chromium, type Browser } from '@playwright/test';
import { existsSync } from 'node:fs';

/** Where the machine has no Playwright browser download: SPROUT_E2E_CHROMIUM names a system build. */
function systemChromiumPath(): string | undefined {
  const fromEnv = process.env.SPROUT_E2E_CHROMIUM;
  if (fromEnv) return fromEnv;
  return existsSync('/usr/bin/chromium') ? '/usr/bin/chromium' : undefined;
}

export async function launchBrowser(): Promise<Browser> {
  const localChromium = systemChromiumPath();
  return await chromium
    .launch({ channel: 'chrome' })
    .catch(() => chromium.launch())
    .catch(() => {
      if (!localChromium) {
        throw new Error('no browser: no system Chrome, no Playwright download, and no SPROUT_E2E_CHROMIUM path');
      }
      return chromium.launch({ channel: 'chromium', executablePath: localChromium });
    });
}
