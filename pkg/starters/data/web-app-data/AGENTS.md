# AGENTS.md

A React + Vite app with an API: a Hono Worker on Cloudflare, storing data in
D1 through Drizzle ORM, in TypeScript. The Worker serves the app and the API
under `/api/*`. See README.md for the full layout.

## Check your work

Run these before you finish; all must pass:

```
npm run build   # type-check, build the app into dist/ and bundle the worker as dist/worker.js
npm test        # UI tests (jsdom), then API tests in the Workers runtime
npm run lint
```

Run `npm ci` first if `node_modules/` is missing.

## Where things go

- Routes: `src/App.tsx`; one component per route in `src/pages/`.
- Shared shell: `src/layouts/Layout.tsx`; hooks in `src/hooks/`.
- API routes: `src/worker/index.ts`; tables: `src/worker/schema.ts`. Keep
  API routes under `/api/`: the hosted app runs the worker first for those
  paths and answers every other path from `dist/`.
- The hosted app runs `dist/worker.js`, the bundle of `src/worker/index.ts`
  that `npm run build` makes: code the worker uses must be imported from it.
- A schema change needs a new SQL migration in `drizzle/migrations/` — never
  edit an applied one.
- New UI behaviour gets a test in `test/app.test.tsx`; new API behaviour in
  `test/api.test.ts`.
