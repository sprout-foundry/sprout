import { defineConfig } from 'vitest/config';

// Tests run against jsdom with Testing Library's matchers installed from
// test/setup.ts. This is a separate config from vite.config.ts so the app
// build and the test run each get an exact config object (and the vitest
// types never leak into the app build). The API test needs the Workers
// runtime, not jsdom, so it is excluded here and run by
// vitest.workers.config.ts instead.
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['test/setup.ts'],
    include: ['test/**/*.test.{ts,tsx}'],
    exclude: ['test/api.test.ts', 'node_modules/**'],
  },
});
