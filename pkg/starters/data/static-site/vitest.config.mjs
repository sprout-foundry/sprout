// @ts-check
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['test/**/*.test.ts'],
    // The home-page test reads dist/, which `astro build` produces. Build
    // the site before running the tests (npm run build && npm test).
    environment: 'node',
  },
});
