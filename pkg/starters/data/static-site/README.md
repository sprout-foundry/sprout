# Static site starter

A minimal [Astro](https://astro.build/) static site: a home page, an
`/about/` content page, a shared layout, and a contact form posting to an
external endpoint.

## Requirements

- Node.js 20 or newer

## Commands

```
npm ci        # install pinned dependencies
npm run dev   # dev server on http://localhost:4321
npm run build # static build into dist/
npm test      # vitest checks the built home page (run after npm run build)
npm run format
npm run lint
```

## Layout

- `src/pages/` — one file per route (`index.astro` is `/`, `about.astro`
  is `/about/`).
- `src/layouts/BaseLayout.astro` — the shared HTML shell (header, nav,
  footer).
- `src/components/ContactForm.astro` — the contact form.
- `src/config.ts` — site-wide config; `CONTACT_ENDPOINT` is the single
  place the form's external endpoint is set. **Replace the placeholder
  before deploying** so the form posts somewhere real.
- `public/` — files copied verbatim into the build (`favicon.svg`,
  `404.html`).

## Adding a page

Create `src/pages/<name>.astro` and import `BaseLayout`; the file path
becomes the route.

## Deploying

`npm run build` produces a static site in `dist/`, deployable to any
static host (the starter targets Cloudflare Pages).

## Dependency security

`npm audit --audit-level=high` is clean — no high or critical advisory
remains, and no advisory is left unfixed. The stack is on Astro 7, Vite 8
and Vitest 4; the bump to `eslint-plugin-astro` 2 also dropped the
`fast-glob`/`micromatch`/`braces` chain that used to carry a high advisory,
so the tree is clean end to end.
