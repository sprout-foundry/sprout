# AGENTS.md

A React + Vite app with an API: a Hono Worker on Cloudflare, storing data in
D1 through Drizzle ORM, in TypeScript. The Worker serves the app and the API
under `/api/*`. See README.md for the full layout.

## Check your work

Run these before you finish; all must pass:

```
npm run build   # type-check the app and worker, then build into dist/
npm test        # UI tests (jsdom), then API tests in the Workers runtime
npm run lint
```

Run `npm ci` first if `node_modules/` is missing.

## Where things go

- Routes: `src/App.tsx`; one component per route in `src/pages/`.
- Shared shell: `src/layouts/Layout.tsx`; hooks in `src/hooks/`.
- API routes: `src/worker/index.ts`; tables: `src/worker/schema.ts`.
- A schema change needs a new SQL migration in `drizzle/migrations/` — never
  edit an applied one.
- New UI behaviour gets a test in `test/app.test.tsx`; new API behaviour in
  `test/api.test.ts`.
