// @ts-check
/** @type {import('@playwright/test').PlaywrightTestConfig} */
const config = {
  testDir: './test',
  testMatch: ['**/desktop-smoke.spec.js', '**/desktop-e2e-smoke.spec.js', '**/full-user-flow.spec.js'],
  timeout: 60000,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'test-results-html' }]],
  use: {
    actionTimeout: 15000,
  },

  // SP-087-2: Project separation for desktop vs webui e2e tests
  projects: [
    {
      name: 'desktop',
      testMatch: ['**/desktop-smoke.spec.js', '**/desktop-e2e-smoke.spec.js', '**/full-user-flow.spec.js'],
    },
    {
      name: 'webui',
      testDir: './test/webui',
      testMatch: ['**/*.spec.ts'],
      timeout: 120000,
    },
  ],

  // SP-087-2: Auto-start the full e2e stack (sprout backend + Vite dev server)
  // Skip with SPROUT_SKIP_WEBSERVER=1 when running manually or in headed mode.
  webServer: process.env.SPROUT_SKIP_WEBSERVER
    ? undefined
    : {
        command: 'npx tsx test/webui/start-stack.mjs',
        timeout: 120 * 1000,
        reuseExistingServer: !process.env.CI,
        stdout: 'pipe',
        stderr: 'pipe',
      },
};

module.exports = config;
