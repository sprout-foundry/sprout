// @ts-check
/** @type {import('@playwright/test').PlaywrightTestConfig} */
const config = {
  testDir: './test',
  testMatch: ['desktop-smoke.spec.js'],
  timeout: 60000,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'test-results/desktop-html' }]],
  use: {
    actionTimeout: 15000,
  },
};

module.exports = config;
