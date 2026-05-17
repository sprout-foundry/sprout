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
};

module.exports = config;
