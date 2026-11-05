# AGENTS.md

A static site built with Astro, deployed to Cloudflare Pages. See README.md
for the full layout.

## Check your work

Run these before you finish; all must pass:

```
npm run build   # static build into dist/
npm test        # vitest checks the built pages (needs the build first)
npm run lint
```

Run `npm ci` first if `node_modules/` is missing.

## Where things go

- Pages: `src/pages/` — the file path is the route (`about.astro` → `/about/`).
- Shared shell (header, nav, footer): `src/layouts/BaseLayout.astro`.
- Reusable pieces: `src/components/`, one component per file.
- Site-wide settings: `src/config.ts` — the form's endpoint is set only there.
- Static files copied as-is: `public/`.
- A new page or feature gets a test in `test/`.
