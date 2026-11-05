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

Published and in use. Four entry points: `.` (the host contract),
`./views` (`SproutWorkspace`, the space registry, the individual views, the
chat and editor units), `./providers` (`SproutProviders`) and `./agent` (the
headless agent, below). The minimal
mount is `SproutWorkspace` with a `project`, a `space` and a `host`; in its
default own mode it builds the provider stack itself, so the host does not
wrap it:

```tsx
import "@sprout-foundry/workspace/styles.css";
import { SproutWorkspace } from "@sprout-foundry/workspace/views";

<SproutWorkspace
  project={{ id: repoUrl, repoUrl }}
  space="code"
  host={myHost}
/>;
```

The integration guide (`docs/integration/workspace-package.md` in the Sprout
repository) covers layouts, the chat and editor units, escalation, styles and
the WASM assets; `docs/integration/host-contract.md` covers the `SproutHost`
a host provides.

## Headless agent

`@sprout-foundry/workspace/agent` runs the in-browser agent without Sprout's
interface, for a host that renders its own: `createAgent({ wasmBase,
modelEndpoint, persona, hostTools, files, context })` returns an agent with
`send`, `cancel`, `answer`, typed events (text, tool steps, `ask_user`, file
writes, mockup progress, `done`) and the agent's files. It imports no React
and no web UI module. See `docs/integration/headless-agent.md`.

```ts
import { createAgent } from "@sprout-foundry/workspace/agent";

const agent = await createAgent({
  wasmBase: "/assets/sprout-wasm",
  modelEndpoint: "/proxy/chat",
  persona: "designer",
});
agent.on("text", (e) => render(e.text));
await agent.send("Sketch a landing page");
```

## Styles

The workspace's styles ship as one stylesheet:

```tsx
import "@sprout-foundry/workspace/styles.css";
```

It is emitted at `dist/workspace.css` and scoped to the `.sprout-workspace`
class the workspace root renders, so it cannot style the host page outside
the mounted workspace (no top-level `:root`/`html`/`body`/`*` selector). It
styles surfaces through `var(--token)` design tokens; the host imports
`@sprout-foundry/design` for the token values, or supplies them on the
workspace root through the host theme.

## WASM assets

The package ships Sprout's in-browser agent (Go→WASM) content-hashed under
`dist/wasm/` (`sprout.<hash>.wasm`, `wasm_exec.<hash>.js`, plus
`wasm-manifest.json`), so a host may cache the package as immutable and an
upgrade can never mix an old binary with new JS. `dist/wasm/` is declared as
the `./wasm/` subpath export and in the `files` allowlist.

A host serves that directory at a URL it owns and tells Sprout where it is:

```tsx
<SproutWorkspace
  project={project}
  space={space}
  host={myHost}
  wasmBase="/assets/sprout-wasm"
/>
```

`wasmBase` is also a `SproutProviders` prop, or use `WasmAssetsProvider` /
`setActiveWasmBase` directly. With no base, Sprout probes `/webui/wasm` versus
`/wasm` (the shipped local and cloud builds' behaviour).

## Build

```bash
npm run build -w @sprout-foundry/workspace
npm test -w @sprout-foundry/workspace   # the headless agent's tests
```

The build emits `dist/index.js` plus its lazily loaded chunks, `dist/agent.js`
(self-contained),
`dist/workspace.css`, `dist/index.d.ts` and `dist/wasm/` (the content-hashed
WASM assets and their manifest). The `files` and `exports` fields in
`package.json` are the publish allowlist: `dist/` (entry, chunks, stylesheet,
declarations, WASM) and this README.

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
