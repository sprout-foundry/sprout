# Web app starter

A minimal client-side app: [React](https://react.dev/) + [Vite](https://vite.dev/)

- [React Router](https://reactrouter.com/), with two routes, a shared
  layout, and one piece of client state persisted to `localStorage`.

## Requirements

- Node.js 20 or newer

## Commands

```
npm ci         # install pinned dependencies
npm run dev    # dev server on http://localhost:5173
npm run build  # type-check and build into dist/
npm run preview
npm test       # vitest + Testing Library
npm run format
npm run lint
```

## Layout

- `src/main.tsx` — the entry point; mounts the app into `#root`.
- `src/App.tsx` — the router: the `/` and `/about` routes, both under the
  shared layout.
- `src/layouts/Layout.tsx` — the shared shell (header, nav, footer) that
  renders around every route via `<Outlet />`.
- `src/pages/` — one component per route (`Home.tsx` is `/`, `About.tsx`
  is `/about`).
- `src/hooks/useLocalStorage.ts` — the persisted-state hook: reads once on
  mount, writes through to `localStorage` on every change. The home page
  uses it to count visits across reloads.
- `src/styles/global.css` — global styles.
- `test/app.test.tsx` — vitest + Testing Library: renders the routes in an
  in-memory router and checks the `localStorage` persistence.
- `public/` — files copied verbatim into the build (`favicon.svg`,
  `_redirects`).

## Adding a route

Add a component under `src/pages/` and an entry to the `children` array in
`src/App.tsx`; the shared layout renders around it automatically. Add a nav
link in `src/layouts/Layout.tsx`.

## Adding a test

Create `test/<name>.test.tsx`. `test/setup.ts` loads Testing Library's
matchers into vitest and clears `localStorage` between tests; render a
route with `createMemoryRouter` (see `test/app.test.tsx`) rather than a
browser.

## Deploying

`npm run build` produces a static bundle in `dist/` (the starter targets
Cloudflare Pages).

This is a single-page app: only `/index.html` exists on disk, so the host
must serve `index.html` for any path that is not a real file, or a direct
visit to a route such as `/about` returns a 404. The starter ships
`public/_redirects` with the Cloudflare Pages fallback:

```
/*    /index.html   200
```

On other static hosts configure the equivalent SPA fallback (for example a
`try_files` rule on nginx, or a rewrite to `/index.html`).
