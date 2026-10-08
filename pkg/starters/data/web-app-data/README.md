# Web app with data starter

The client-side app from the `web-app` starter — [React](https://react.dev/)

- [Vite](https://vite.dev/) + [React Router](https://reactrouter.com/) — plus a
  data layer: a [Hono](https://hono.dev/) API running on a Cloudflare
  [Worker](https://developers.cloudflare.com/workers/), reading and writing a
  Cloudflare [D1](https://developers.cloudflare.com/d1/) database through
  [Drizzle ORM](https://orm.drizzle.team/).

The Worker serves both the built app (from `dist/`) and the API under
`/api/*`, so the app and its data share one origin and one deploy.

## Requirements

- Node.js 20 or newer
- A Cloudflare account only to deploy; local development and tests need none

## Commands

```
npm ci                # install pinned dependencies
npm run db:migrate    # apply Drizzle migrations to local D1
npm run dev           # wrangler dev on http://localhost:8787 (local state)
npm run build         # type-check the app and worker, then build the app
npm run preview
npm test              # UI tests (jsdom) then API tests (Workers runtime)
npm run format
npm run lint
```

`npm run dev` runs `wrangler dev`: it bundles the Worker, serves the built
assets, and backs D1 with local state under `.wrangler/state/`. Run
`npm run db:migrate` first so the `items` table exists. Because the dev
server serves `./dist`, run `npm run build` once before `npm run dev` (or
point the assets directory at the Vite dev server if you prefer HMR).

## Layout

- `src/main.tsx` — the entry point; mounts the app into `#root`.
- `src/App.tsx` — the router: the `/`, `/about` and `/items` routes, all
  under the shared layout.
- `src/layouts/Layout.tsx` — the shared shell (header, nav, footer) that
  renders around every route via `<Outlet />`.
- `src/pages/` — one component per route; `Items.tsx` is the page that
  calls the API.
- `src/worker/index.ts` — the Hono app: `GET /api/items` lists and
  `POST /api/items` creates one. Exported as the Worker's default export.
- `src/worker/schema.ts` — the Drizzle table definition (`items`).
- `drizzle/migrations/` — the SQL migrations `wrangler d1 migrations`
  applies (`0000_init.sql` creates `items`).
- `test/app.test.tsx` — the UI test: renders the routes with an in-memory
  router and checks the `Items` page against a stubbed `fetch`.
- `test/api.test.ts` — the API test: runs the Worker through
  `@cloudflare/vitest-plugin` against local D1.
- `design/` — the design workspace scaffold.

## Tests

There are two kinds of test, so there are two Vitest configs:

- `vitest.config.ts` runs the UI tests under jsdom (`npm test` runs it).
- `vitest.workers.config.ts` runs `test/api.test.ts` inside the Workers
  runtime via `@cloudflare/vitest-plugin`, with the D1 binding from
  `wrangler.toml` and local D1 state. `npm test` runs it after the jsdom
  run; a single Vitest config cannot serve both environments.

The API test applies `drizzle/migrations/0000_init.sql` to local D1, then
dispatches requests through `SELF.fetch` and asserts the list → create →
list round trip.

## Data layer

One table ships: `items` (`id` integer primary key, `name` text, not null).

**Add a table:**

1. Add it to `src/worker/schema.ts` with `drizzle-orm/sqlite-core`
   (D1 is SQLite).
2. Generate a migration: `npx drizzle-kit generate`. The generated SQL lands
   in `drizzle/migrations/` alongside the hand-written `0000_init.sql`.
3. Apply it locally: `npm run db:migrate`.
4. Add the route in `src/worker/index.ts` and call it from the UI.

**Deploy:** replace `database_id` in `wrangler.toml` with the id
`wrangler d1 create web-app-data-db` prints, run
`npm run db:migrate:remote`, then `npx wrangler deploy`. This starter's
manifest declares `deploy_target: workers`.

## KV and R2

Only D1 is wired by default. Cloudflare
[KV](https://developers.cloudflare.com/kv/) (simple key-value state) and
[R2](https://developers.cloudflare.com/r2/) (object storage) are documented
here for when a feature needs them; adding a binding that nothing uses would
only be dead configuration, so no binding exists until then.

When a feature needs one, create the namespace or bucket and uncomment the
block in `wrangler.toml`:

```toml
# wrangler kv namespace create KV   -> prints the id below
[[kv_namespaces]]
binding = "KV"
id = "REPLACE_WITH_KV_NAMESPACE_ID"

# wrangler r2 bucket create web-app-data-bucket
[[r2_buckets]]
binding = "BUCKET"
bucket_name = "web-app-data-bucket"
```

Add the binding to your `Env` type in `src/worker/index.ts` (`KV: KVNamespace`,
`BUCKET: R2Bucket`) and read it from `c.env`. Local development and the tests
then emulate KV and R2 in `.wrangler/state/` automatically.

## Design tree

`design/` holds the tokens, wireframes, screens and flows scaffold; see
`design/README.md`.

## Dependency security

`npm audit --audit-level=high` is clean — no high or critical advisory
remains. The stack is on React 19, React Router 7.18, Vite 8, Vitest 4,
Wrangler 4 and Drizzle ORM 0.45. Two points worth recording:

- The Workers test pool moved to **`@cloudflare/vitest-plugin`** (the old
  `@cloudflare/vitest-pool-workers` is deprecated and renamed). See
  `vitest.workers.config.ts`.
- `npm audit` reports four **moderate** advisories from `drizzle-kit`'s
  dev-only `@esbuild-kit` chain (an old `esbuild` whose dev server lets a
  website read responses). They are below the high threshold, never ship in
  `dist/`, and the only "fix" is a large `drizzle-kit` downgrade; they are
  left as-is. `npm audit --audit-level=high` (the CI gate) exits clean.
