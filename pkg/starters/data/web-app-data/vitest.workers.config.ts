import { defineWorkersConfig } from '@cloudflare/vitest-pool-workers/config';

// The API test runs inside the Workers runtime (workerd) through
// @cloudflare/vitest-pool-workers, so it exercises the real Hono app, the D1
// binding and local D1 state — no separate server and no network. The pool
// reads wrangler.toml (bindings, D1 database) and applies the Drizzle
// migration into the local database before the tests run. It is a separate
// config from vitest.config.ts because jsdom and the Workers pool cannot
// share one config; `npm test` runs both, one after the other.
export default defineWorkersConfig({
  test: {
    include: ['test/api.test.ts'],
    poolOptions: {
      workers: {
        wrangler: { configPath: './wrangler.toml' },
        miniflare: {
          d1Databases: ['DB'],
        },
      },
    },
  },
});
