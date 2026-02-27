const { chromium } = require('playwright');

const BASE_URL = process.env.LEDIT_WEBUI_URL || 'http://localhost:54331';

function now() {
  return new Date().toISOString();
}

async function run() {
  const failures = [];
  const pageErrors = [];
  const consoleErrors = [];

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
    const response = await page.goto(BASE_URL, { waitUntil: 'domcontentloaded', timeout: 15000 });
    if (!response || !response.ok()) {
      throw new Error(`Failed to load ${BASE_URL}`);
    }
    await page.locator('.navigation-bar').waitFor({ state: 'visible', timeout: 10000 });
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
  console.log(`Checks: ${8 - failures.length}/8 passed`);

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
