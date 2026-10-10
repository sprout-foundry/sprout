# Using `@sprout-foundry/workspace`

`@sprout-foundry/workspace` is how another application hosts Sprout's
workspace: it mounts one project's workspace (a space such as code or
design) inside its own product and provides a `SproutHost` (see
[host-contract.md](host-contract.md)). It is the only hosted artifact; hosts
do not build Sprout's web UI from source.

## Install

The package is published to GitHub Packages under the `@sprout-foundry`
scope. GitHub Packages requires a token for every install, including public
packages.

`.npmrc` in the host project:

```ini
@sprout-foundry:registry=https://npm.pkg.github.com
```

Token, locally (a GitHub token with `read:packages`):

```bash
npm config set //npm.pkg.github.com/:_authToken "$(gh auth token)"
```

Token in GitHub Actions (the package must grant the host repository read
access under its package settings, "Manage Actions access"):

```yaml
permissions:
  packages: read
steps:
  - uses: actions/setup-node@v4
    with:
      node-version: "22"
      registry-url: "https://npm.pkg.github.com"
      scope: "@sprout-foundry"
  - run: npm ci
    env:
      NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

In a Docker build, pass the token as a BuildKit secret
(`--secret id=npm_token,...`), never as a build argument, so it does not
remain in an image layer.

Pin an exact version. Local, CI, end-to-end and production builds should
all read the same pinned version.

## Mount

```tsx
import { SproutWorkspace } from "@sprout-foundry/workspace/views";
import { SproutProviders } from "@sprout-foundry/workspace/providers";
import type { SproutHost } from "@sprout-foundry/workspace";
import { myHost } from "./myHost";

export function ProjectSpace({
  project,
  space,
}: {
  project: string;
  space: string;
}) {
  return (
    <SproutProviders>
      <SproutWorkspace project={project} space={space} host={myHost} />
    </SproutProviders>
  );
}
```

The package exposes three entry points:

- `.` — the **host contract** (`SproutHost` and the host types, the
  provider and hooks, `localHost`). It is deliberately thin: importing it
  pulls no view, editor or WASM code.
- `./views` — the **composition API**: `SproutWorkspace` (mounts one
  project's workspace), the registered spaces through the registry
  (`WORKSPACE_MODES`, `availableModes`, `registerWorkspaceMode`,
  `resolveWorkspaceMode`, `useWorkspaceMode`), the individual views
  (`ChatView`, `AgentChangesPanel`, editor, files, preview, `ViewsLayout`),
  the WASM asset seam, and `SproutProviders` (so a host that already
  imports the views gets the wrapper from the same surface).
- `./providers` — just `SproutProviders` and its props, for a host that
  wraps the views it composes itself without importing the whole views
  graph.

`SproutProviders` sets up the provider stack the views need (optionally
with an `eventsProvider` for the event transport); `SproutWorkspace` takes
the host. `myHost` implements `SproutHost`: identity, entitlements, transport,
navigation intents, notifications, chrome slots, theme and capabilities.
React and React DOM are peer dependencies; the host provides them. Heavy
parts (the editor, the in-browser agent, space-specific code) load when a
space opens, not when the package is imported.

## The chat unit

The chat surface is the most stateful part of a workspace, so it ships as one
reusable unit rather than a view a host has to feed by hand. `./views` exports
`WorkspaceChatProvider`, `useWorkspaceChat()` and `useWorkspaceChatProps()`:

```tsx
import {
  WorkspaceChatProvider,
  useWorkspaceChatProps,
} from "@sprout-foundry/workspace/views";

<WorkspaceChatProvider
  initialState={initialState}
  eventsProvider={events}
  fetchFn={myFetch}
>
  <MyChatSurface />
</WorkspaceChatProvider>;
```

The provider takes only what a chat needs to reach a backend — a `fetch`
function and an events provider — and owns everything else: the chat store
(the transcript, `isProcessing`, `lastError`, tool executions, query progress,
file edits, stats, subagent activities, output verbosity, the per-chat cache,
the session list and the queue), the WebSocket event reducer, the chat session
manager (list/create/switch/delete/rename) and the send/queue operations. It
subscribes the events provider to the reducer itself, so a host hands over its
transport and nothing else.

- `useWorkspaceChat()` reads the unit: `state`, `setState`, `handleEvent`,
  `handleReconnect` and the `chat` session manager. A host composing its own
  chat surface uses these directly.
- `useWorkspaceChatProps(overrides)` assembles the `chatProps`, `reviewProps`
  and `diffState` the `ChatView` renders from, mapping the queue to the active
  chat. The `overrides` carry the app-specific callbacks that are not chat
  state (opening a review buffer, the model picker, session restore, fork).

The standalone app mounts the provider with its own `clientFetch` and
`LocalEventsProvider`, so its behaviour is unchanged; a host mounts it with
its host's transport.

`WorkspaceChatProvider` must be mounted under a `HostProvider`: it reads the
host to decide whether the host serves the chat session store (see
`capabilities.chatSessions` in the host contract) and to resolve that store's
API base. `SproutWorkspace` mounts one in its own mode.

**The composed workspace mounts the chat for you.** A `SproutWorkspace` that
renders through a `layout` arrangement (SP-155) mounts the chat unit itself:
the arranged `chat` view gets props assembled from the unit, so a host's
layout shows a working chat — the transcript, sending, queueing and the
streaming reducer — with no extra wiring. The composition owns the events
transport and hands the same instance to both `SproutProviders` and the chat
unit, so the two share one subscription. The `changes` view self-fetches its
session changes and is left with no assembled props.

The app-specific callbacks that are not chat state (the model picker, session
restore, fork, opening a review buffer) are not assembled by the composition —
a host supplies them through `viewProps.chat`, which replaces the assembled
chat props for that kind. `chatInitialState` seeds the chat state (an empty
chat otherwise; it must be referentially stable) and `chatFetch` routes chat
calls through the host's transport. Ambient mode (`providers="ambient"`)
builds none of this: the caller owns the provider stack and its chat, so
`viewProps` is passed to the layout as-is.

The `./views` and `./providers` subpaths each ship their own self-contained
type declarations (`dist/views.d.ts`, `dist/providers.d.ts`), so importing
`SproutWorkspace` or `SproutProviders` type-checks from `dist/` alone —
no web UI source tree required. They reference only `react` as an external
(the registry's mode icons and the editor's CodeMirror-typed props are
inlined).

## Escalation (run it where it can run)

When an agent command cannot run in the browser (exit 127 — no compilers in
the WASM shell), a git push the browser cannot perform, or the VFS hitting its
quota, the work can run instead in the user's cloud workspace or on one of
their runners: the browser's changed files are pushed, the command runs, the
resulting deltas come back (`/workspace/txn`, see the host contract). `./views`
exports the pieces a composing host mounts — the same two the Sprout app root
mounts:

```tsx
import {
  AgentEscalationBridge,
  EscalationListener,
  useEscalationTriggers,
} from "@sprout-foundry/workspace/views";

// Once, at the app root — the detector for the trigger events, and the
// "Browser limitation reached" affordance for the user's own actions:
useEscalationTriggers({ repoURL });
<EscalationListener />;

// Once per page — installs globalThis.__sproutEscalate, the hook the WASM
// agent calls on exit 127, and asks the consent question:
<AgentEscalationBridge repoURL={repoURL} />;
```

`AgentEscalationBridge` takes the repository the workspace was imported from —
the same value `SproutWorkspace`'s `project.repoUrl` carries, so a host threads
one source of truth to both. The consent question, the host picker (runners or
cloud) and the progress line are rendered by the bridge; a host that wants its
own consent UI calls `installEscalationBridge` directly with a
`requestConsent` callback (its `EscalationBridgeOptions` and `EscalationResult`
types are exported alongside), and reads or sets the user's policy through
`getEscalationPolicy` / `setEscalationPolicy`.

The seam is WASM-side only — no daemon and no local terminal involved. The
transactional runs go through relative paths, so they ride the host's
transport: a host that serves the cloud transactional surface gets working
escalation; without a backend the triggers still fire and the consent answer
"deny" (the default-safe behavior) keeps every command in the browser.

## One workspace per page

Sprout's non-React services reach a few **module singletons** — process-wide
values shared by every module in the page, not per-provider instances:

- the **active host** (`getActiveHost()`), the transport the client-session
  fetch and the WebSocket URL resolve against;
- the **client id** (per browsing context, resolved once at boot);
- the **events transport** (the WebSocket service behind
  `LocalEventsProvider`).

A page therefore hosts **one workspace at a time**. `SproutWorkspace` in its
default `providers="own"` mode registers its `host` as the active host while
it is mounted and restores the previous host on unmount (or keeps its own host
when there was none, so the services always have a resolvable host), and it
opens the events transport — so the mounted workspace's
`transport.apiBaseURL` / `transport.wsURL` drive Sprout's calls. Mounting two
own-mode workspaces on one page is not supported: the second registration
would win the singleton and both would share one event stream. A host that
needs two workspaces side by side mounts one provider stack (its own
`HostProvider` + `SproutProviders`) and renders the second workspace with
`providers="ambient"`, so exactly one registration and one transport exist.

The standalone app is the same model: its entry point (`webui/src/index.tsx`)
registers the active host once at startup and the app root owns the events
transport, so the app renders its workspace in ambient mode.

## Styles

The workspace's styles are one stylesheet the package ships and declares:

```tsx
import "@sprout-foundry/workspace/styles.css";
```

It is emitted at `dist/workspace.css` and reachable through the package's
`exports` map (`"./styles.css": "./dist/workspace.css"`); no other `.css`
file is emitted, so a host imports exactly one stylesheet. Import it once,
before mounting; it is static CSS with no runtime dependency.

**Scoped to the workspace root.** The stylesheet is scoped to the
`.sprout-workspace` class that `SproutWorkspace`'s root element carries, so
it cannot style the host page outside the mounted workspace: there is no
top-level `:root`, `html`, `body` or `*` selector in the artifact. The
workspace's token declarations are re-scoped onto that root class rather
than `:root`, so a host's own token block is never overwritten by loading
this stylesheet; the light-theme guards are re-scoped to
`.sprout-workspace[data-theme="light"]`, and the workspace root carries the
resolved `data-theme` so those guards stay live.

**Consuming the design tokens.** The stylesheet styles every surface through
`var(--token)` custom properties (the color, space, type, radius and shadow
tokens). The token set is owned by `@sprout-foundry/design`; a host imports
it itself:

```tsx
import "@sprout-foundry/design/tokens.css"; // and, optionally, /reset.css
import "@sprout-foundry/workspace/styles.css";
```

The package deliberately does **not** `@import` `@sprout-foundry/design`
from its stylesheet, and declares no dependency on it. A host that imports
the design tokens (above) gets the token values on the document root as
usual — that is the intended path, and the workspace's own token
declarations are scoped to the workspace root precisely so they do not
override the host's. A host that does _not_ import the design package can
still load the workspace's stylesheet, but every `var(--token)` then resolves
to the component CSS's own literal fallbacks rather than the designed values,
so importing the tokens is recommended. A host that themes Sprout live does
so through `SproutHost.theme`, whose values land on the workspace root —
importing a stylesheet could not observe that anyway. `@sprout-foundry/design`
is therefore an optional, host-supplied companion (not a dependency of this
package); the token _consumption_ is the `var(--token)` references, which is
the contract. Moving the web UI's own token declarations into
`@sprout-foundry/design` is separate work.

## WASM assets

Sprout's in-browser agent (the browser-local fallback for a host that serves
no daemon) runs on a Go→WASM binary. The package ships it content-hashed so a
host can cache the package as immutable and an upgrade can never pair an old
binary with new JS:

- `dist/wasm/sprout.<hash>.wasm` — the compiled agent.
- `dist/wasm/wasm_exec.<hash>.js` — the Go WASM runtime.
- `dist/wasm/wasm-manifest.json` — the small manifest the loader reads, mapping
  each logical name to its content-hashed filename.

The package ships only these three files. The bare `sprout.wasm` /
`wasm_exec.js` are **not** emitted alongside: the binary is ~62 MB, and a
fixed-name duplicate would double the installed payload for no benefit, since a
host serving `dist/wasm/` always has the manifest and the loader resolves the
hashed name from it. The standalone local/cloud build
(`scripts/build-webui-dist.mjs`) still writes fixed-name copies where the embed
(`pkg/webui/static`, `webui/public/wasm`) is served without a manifest; the
package is the only artifact that drops them.
The hash is the first 10 hex characters of the assets' sha256, produced by the same
pure helpers the cloud/standalone build uses
(`scripts/build-webui-dist.mjs`), so a changed binary always yields a new URL.
The `./wasm/` subpath export points at the directory:

```js
import { createRequire } from "node:module";
const wasmDir = new URL("@sprout-foundry/workspace/wasm/", import.meta.url);
```

**Building.** The package's build copies the WASM from
`webui/public/wasm/{sprout.wasm,wasm_exec.js}` — the sources `make build-wasm`
produces — and fails if they are absent (a package that ships no WASM/manifest
is a broken artifact, so a mis-ordered build is loud, not silently green).
`make build-all` runs `build-wasm` before the package build; a standalone
`npm run build -w @sprout-foundry/workspace` uses whatever `make build-wasm`
last emitted, so run it after changing the Go WASM.

**Where the loader looks.** A host serves `dist/wasm/` at a path it owns — its
bundler copies the directory somewhere. Tell Sprout that base so the loader
resolves the manifest and the hashed assets against it:

```tsx
<SproutWorkspace
  project={project}
  space={space}
  host={myHost}
  wasmBase="/assets/sprout-wasm"
/>
```

`wasmBase` is also a prop of `SproutProviders` (for a host that composes the
views itself) and available as `WasmAssetsProvider` /
`setActiveWasmBase` for a host that loads the WASM shell directly. When no base
is supplied Sprout keeps its default: it probes `/webui/wasm` (the daemon mount)
versus `/wasm` (a root-served bundle), which is what the standalone local build
and the platform's cloud build have always done. A host that serves the assets
at neither path sets `wasmBase`.

## Publishing (maintainers)

Publishing runs in GitHub Actions on a version tag that matches
`packages/workspace/package.json`:

```bash
git tag workspace-v1.0.0 -m "workspace v1.0.0"
git push origin workspace-v1.0.0
```

`.github/workflows/publish-workspace.yml` builds the package in production
mode, validates the artifact (`docs/__tests__/workspace-package.test.js`)
and publishes it. `@sprout-foundry/design` publishes the same way from
`.github/workflows/publish-design.yml` on `design-v*` tags. A published
version can never be overwritten; bump the version for every release.

After a package's first publish, in its GitHub package settings: set the
visibility to public, and under "Manage Actions access" give each host
repository read access.
