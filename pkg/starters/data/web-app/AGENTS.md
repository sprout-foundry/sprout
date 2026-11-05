# AGENTS.md

A client-side app: React, Vite and React Router, in TypeScript, deployed to
Cloudflare Pages. See README.md for the full layout.

## Check your work

Run these before you finish; all must pass:

```
npm run build   # type-check, then build into dist/
npm test        # vitest + Testing Library
npm run lint
```

Run `npm ci` first if `node_modules/` is missing.

## Where things go

- Routes: `src/App.tsx`; one component per route in `src/pages/`.
- Shared shell (header, nav, footer): `src/layouts/Layout.tsx`.
- Reusable hooks: `src/hooks/`, one per file.
- Styles: `src/styles/`.
- Static files copied as-is: `public/`.
- A new page or behaviour gets a test in `test/`.
