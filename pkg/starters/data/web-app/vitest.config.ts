import { defineConfig } from 'vitest/config';

// Tests run against jsdom with Testing Library's matchers installed from
// test/setup.ts. This is a separate config from vite.config.ts so the app
// build and the test run each get an exact config object (and the vitest
// types never leak into the app build).
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['test/setup.ts'],
    include: ['test/**/*.test.{ts,tsx}'],
  },
});
