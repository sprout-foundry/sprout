# @sprout-foundry/workspace

The composition API for Sprout. A host application mounts Sprout's workspace —
one project, one of the registered spaces — inside its own product through
this package instead of reaching into web UI internals.

Two parts are exported:

- **The host contract** — a typed interface the host passes in: identity,
  entitlements, transport (where backend calls go), navigation intents,
  a notification sink, chrome slots, theme and explicit capability flags.
  Sprout never infers its host from a build flag or the current URL.
- **The views** — the layout, the individual views (chat, agent changes,
  files, editor, preview), the space registry, and the provider requirements
  each view carries.

## Status

Scaffold. The package builds today (ESM, code-split, type declarations) and
re-exports the two public entry points of the web UI
(`webui/src/views/index.ts` and `webui/src/host/index.ts`). The
`SproutWorkspace` component, `SproutProviders` and the stylesheet land in the
following steps of the composition work.

## Build

```bash
npm run build -w @sprout-foundry/workspace
```

The build emits `dist/index.js` plus its lazily loaded chunks and
`dist/index.d.ts`. The `files` and `exports` fields in `package.json` are the
publish allowlist: `dist/` (entry, chunks, declarations) and this README.

## Installation

```bash
npm install @sprout-foundry/workspace
```

This package is published to GitHub Packages. Configure your `.npmrc`:

```
@sprout-foundry:registry=https://npm.pkg.github.com
```

The version tracks the Sprout release — the package and the `sprout` binary
that serves its backend share one version number, so a host cannot load a
package that disagrees with the daemon behind it.
