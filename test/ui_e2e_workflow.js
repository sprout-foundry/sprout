const { chromium } = require('playwright');
const http = require('http');
const https = require('https');

const DEFAULT_BASE_URLS = ['http://localhost:54000', 'http://localhost:54331'];

function now() {
  return new Date().toISOString();
}

function getConfiguredUrls() {
  const env = process.env.LEDIT_WEBUI_URL;
  if (!env || !env.trim()) {
    return DEFAULT_BASE_URLS;
  }
  return env
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
}

function checkUrlReachable(url) {
  return new Promise((resolve) => {
    try {
      const parsed = new URL(url);
      const client = parsed.protocol === 'https:' ? https : http;
      const req = client.request(
        {
          hostname: parsed.hostname,
          port: parsed.port,
          path: '/',
          method: 'GET',
          timeout: 3000,
        },
        (res) => {
          resolve(res.statusCode && res.statusCode < 500);
          res.resume();
        }
      );
      req.on('error', () => resolve(false));
      req.on('timeout', () => {
        req.destroy();
        resolve(false);
      });
      req.end();
    } catch (_e) {
      resolve(false);
    }
  });
}

async function resolveBaseUrl() {
  const candidates = getConfiguredUrls();
  for (const url of candidates) {
    // eslint-disable-next-line no-await-in-loop
    const ok = await checkUrlReachable(url);
    if (ok) {
      return url;
    }
  }
  return candidates[0];
}

async function run() {
  const failures = [];
  const pageErrors = [];
  const consoleErrors = [];
  const checks = [];

  const baseUrl = await resolveBaseUrl();
  console.log(`[INFO] Using WebUI URL: ${baseUrl}`);

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();

  page.on('pageerror', (error) => {
    pageErrors.push(error.message || String(error));
  });

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      const text = msg.text();
      if (!text.includes('favicon.ico')) {
        consoleErrors.push(text);
      }
    }
  });

  const check = async (name, fn) => {
    checks.push(name);
    try {
      await fn();
      console.log(`[PASS] ${name}`);
    } catch (error) {
      const message = error && error.message ? error.message : String(error);
      console.log(`[FAIL] ${name}: ${message}`);
      failures.push({ name, message });
    }
  };

  await check('Load WebUI shell', async () => {
    const response = await page.goto(baseUrl, { waitUntil: 'domcontentloaded', timeout: 15000 });
    if (!response || !response.ok()) {
      throw new Error(`Failed to load ${baseUrl}`);
    }
    await page.locator('.navigation-bar').waitFor({ state: 'visible', timeout: 10000 });
  });

  await check('Provider/model change emits atomic model_change payload', async () => {
    await page.evaluate(() => {
      if (window.__leditWsCaptureInstalled) {
        window.__leditWsSends = [];
        return;
      }

      window.__leditWsSends = [];
      window.__leditWsCaptureInstalled = true;
      const originalSend = window.WebSocket.prototype.send;
      window.WebSocket.prototype.send = function patchedSend(data) {
        try {
          window.__leditWsSends.push(typeof data === 'string' ? data : String(data));
        } catch (_e) {}
        return originalSend.call(this, data);
      };
    });

    // Open settings area where provider/model selectors live.
    await page.getByRole('button', { name: 'Settings' }).click();
    const providerSelect = page.locator('#provider-select');
    const modelSelect = page.locator('#model-select');
    await providerSelect.waitFor({ state: 'visible', timeout: 8000 });
    await modelSelect.waitFor({ state: 'visible', timeout: 8000 });

    const selectedProvider = await providerSelect.inputValue();
    const providerOptions = await providerSelect.locator('option').allTextContents();
    if (providerOptions.length === 0) {
      throw new Error('No provider options available');
    }

    // Pick a different provider if available, otherwise keep current.
    const providerValues = await providerSelect.locator('option').evaluateAll((opts) =>
      opts.map((o) => o.getAttribute('value')).filter(Boolean)
    );
    const targetProvider = providerValues.find((v) => v !== selectedProvider) || selectedProvider;
    if (!targetProvider) {
      throw new Error('No valid provider value available');
    }
    await providerSelect.selectOption(targetProvider);
    await page.waitForTimeout(150);

    // Force an explicit model change action.
    const currentModel = await modelSelect.inputValue();
    const modelValues = await modelSelect.locator('option').evaluateAll((opts) =>
      opts.map((o) => o.getAttribute('value')).filter(Boolean)
    );
    const targetModel = modelValues.find((v) => v !== currentModel) || currentModel;
    if (!targetModel) {
      throw new Error('No model options available');
    }
    await modelSelect.selectOption(targetModel);
    await page.waitForTimeout(200);

    const capture = await page.evaluate(() => {
      const raw = Array.isArray(window.__leditWsSends) ? window.__leditWsSends : [];
      const parsed = raw
        .map((entry) => {
          try {
            return JSON.parse(entry);
          } catch {
            return null;
          }
        })
        .filter(Boolean);
      const modelChange = parsed.reverse().find((evt) => evt.type === 'model_change');
      return modelChange || null;
    });

    if (!capture) {
      throw new Error('No model_change websocket event captured');
    }

    if (!capture.data || typeof capture.data.provider !== 'string' || !capture.data.provider.trim()) {
      throw new Error('model_change event missing provider field');
    }
    if (!capture.data.model || capture.data.model !== targetModel) {
      throw new Error(`model_change event model mismatch (expected ${targetModel})`);
    }
    if (capture.data.provider !== targetProvider) {
      throw new Error(`model_change provider mismatch (expected ${targetProvider}, got ${capture.data.provider})`);
    }
  });

  await check('WebSocket connected status', async () => {
    await page.getByText('Connected to ledit server', { exact: false }).waitFor({ timeout: 12000 });
  });

  await check('Chat view and input render', async () => {
    await page.getByRole('button', { name: 'Chat view' }).click();
    await page.getByRole('heading', { name: /AI Assistant/i }).waitFor({ timeout: 5000 });
    await page.getByTestId('command-input').waitFor({ state: 'visible', timeout: 5000 });
  });

  await check('Chat send flow', async () => {
    const chatInput = page.getByTestId('command-input');
    const sendButton = page.getByRole('button', { name: /Send message/i });
    const prompt = `ui-e2e-chat-${Date.now()}`;

    await chatInput.fill(prompt);
    await sendButton.waitFor({ state: 'visible', timeout: 3000 });
    await sendButton.click();

    await page.waitForFunction(() => {
      const el = document.querySelector('[data-testid="command-input"]');
      return !!el && el.value === '';
    }, { timeout: 15000 });

    await page.getByLabel('user message').filter({ hasText: prompt }).first().waitFor({ timeout: 15000 });
  });

  await check('New Session clears current chat content', async () => {
    await page.getByRole('button', { name: 'Chat view' }).click();
    const chatInput = page.getByTestId('command-input');
    const sendButton = page.getByRole('button', { name: /Send message/i });
    const prompt = `ui-e2e-clear-${Date.now()}`;

    await chatInput.fill(prompt);
    await sendButton.click();
    await page.getByLabel('user message').filter({ hasText: prompt }).first().waitFor({ timeout: 15000 });

    await page.getByRole('button', { name: /New Session/i }).click();

    // Wait for /clear roundtrip to remove prior messages from rendered chat.
    await page.waitForFunction((txt) => {
      return !Array.from(document.querySelectorAll('[aria-label="user message"]')).some(
        (el) => (el.textContent || '').includes(txt)
      );
    }, prompt, { timeout: 20000 });
  });

  await check('Instance-scoped state key is present', async () => {
    const topInstance = page.locator('#top-instance-select');
    await topInstance.waitFor({ state: 'visible', timeout: 8000 });
    const pid = await topInstance.inputValue();
    if (!pid || pid === '0') {
      throw new Error('No active instance pid found in selector');
    }

    const keyCheck = await page.evaluate((instancePid) => {
      const stateKey = `ledit:webui:state:v1:${instancePid}`;
      return {
        hasStateKey: window.localStorage.getItem(stateKey) !== null,
        storedPid: window.localStorage.getItem('ledit:webui:instancePid'),
      };
    }, pid);

    if (!keyCheck.hasStateKey) {
      throw new Error(`Missing instance-scoped app state key for pid ${pid}`);
    }
    if (keyCheck.storedPid !== pid) {
      throw new Error(`instancePid storage mismatch (expected ${pid}, got ${keyCheck.storedPid})`);
    }
  });

  await check('View switching: Logs', async () => {
    await page.getByRole('button', { name: 'Logs view' }).click();
    await page.getByRole('heading', { name: /Event Logs/i }).waitFor({ timeout: 5000 });
    await page.getByText(/Total:/).first().waitFor({ timeout: 5000 });
  });

  await check('View switching: Git', async () => {
    await page.getByRole('button', { name: 'Git view' }).click();
    await page.locator('.git-view').waitFor({ state: 'visible', timeout: 8000 });
  });

  await check('Terminal command execution via WebUI', async () => {
    const toggle = page.locator('.terminal-btn.toggle-btn');
    await toggle.waitFor({ state: 'visible', timeout: 5000 });

    const title = await toggle.getAttribute('title');
    if (title && title.toLowerCase().includes('expand')) {
      await toggle.click();
    }

    const terminalInput = page.locator('.terminal-input');
    await terminalInput.waitFor({ state: 'visible', timeout: 10000 });
    await page.waitForFunction(() => {
      const input = document.querySelector('.terminal-input');
      return !!input && !input.disabled;
    }, { timeout: 15000 });

    const marker = `LEDIT_E2E_TERMINAL_${Date.now()}`;
    await terminalInput.fill(`echo ${marker}`);
    await terminalInput.press('Enter');

    await page.getByText(marker, { exact: false }).first().waitFor({ timeout: 15000 });

    const terminalNotConnected = page.getByText('Terminal not connected', { exact: false });
    if (await terminalNotConnected.count()) {
      throw new Error('Terminal showed not connected state after command submission');
    }
  });

  await check('No runtime errors in page', async () => {
    if (pageErrors.length > 0) {
      throw new Error(`pageerror: ${pageErrors[0]}`);
    }
    if (consoleErrors.length > 0) {
      throw new Error(`console.error: ${consoleErrors[0]}`);
    }
  });

  await browser.close();

  console.log(`\n[${now()}] UI E2E workflow complete`);
  console.log(`Checks: ${checks.length - failures.length}/${checks.length} passed`);

  if (failures.length > 0) {
    console.log('Failures:');
    for (const f of failures) {
      console.log(`- ${f.name}: ${f.message}`);
    }
    process.exit(1);
  }
}

run().catch((error) => {
  console.error(`Fatal UI E2E failure: ${error && error.stack ? error.stack : error}`);
  process.exit(1);
});
