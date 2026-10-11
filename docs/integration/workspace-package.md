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

The minimal mount is one component. In its default `providers="own"` mode
`SproutWorkspace` builds everything below it — the `HostProvider` for the
`host` you pass, the `SproutProviders` stack, the events transport, and the
chat and editor units — so a host renders it directly, with no provider of its
own around it:

```tsx
import { useMemo } from "react";
import "@sprout-foundry/workspace/styles.css";
import { SproutWorkspace } from "@sprout-foundry/workspace/views";
import type { SproutProject } from "@sprout-foundry/workspace/views";
import { myHost } from "./myHost"; // a module-level SproutHost

export function ProjectSpace({ repoUrl }: { repoUrl: string }) {
  const project = useMemo<SproutProject>(
    () => ({ id: repoUrl, repoUrl }),
    [repoUrl],
  );
  return (
    <SproutWorkspace
      project={project}
      space="code"
      host={myHost}
      wasmBase="/assets/sprout-wasm"
    />
  );
}
```

- `project` is an object (`SproutProject`: `id`, and optionally `name`,
  `root`, `repoUrl`), not a string.
- `space` is a registered space id (`"code"`, `"design"`); an unknown or
  unavailable id resolves to the registry's fallback.
- `host` should be referentially stable (a module-level object or a memoized
  one): the composition registers it as the active host and opens the events
  transport when it changes.
- Without `layout` or `shellProps`, the space's own shell renders and the
  composition feeds it from its chat and editor units: Code gets its chat,
  editor tabs and context panel; Design gets its agent chat. The app chrome
  the composition does not own — the project sidebar, git status in the
  status bar, deploys — renders inert. A host that owns that chrome passes
  `shellProps` (the full `WorkspaceShellProps`) instead, and then supplies the
  chat itself.
- `layout` (an SP-155 `ViewsArrangement`) replaces the space shell with the
  host's own placement of the individual views — see
  [The chat unit](#the-chat-unit) and [Files and editor](#files-and-editor).

Do not wrap an own-mode `SproutWorkspace` in `SproutProviders` or
`HostProvider`: it builds its own stack, and a second one would open a second
events transport. A host that needs the stack above the workspace (to render
its own Sprout views beside it) mounts `HostProvider` + `SproutProviders`
itself and passes `providers="ambient"` — see
[One workspace per page](#one-workspace-per-page).

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

`SproutProviders` is the provider stack the views need (optionally with an
`eventsProvider` for the event transport), for a host that composes views
itself under its own `HostProvider`; an own-mode `SproutWorkspace` mounts it
for you. `myHost` implements `SproutHost`: identity, entitlements, transport,
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
unit, so the two share one subscription. The files, editor and changes views
are assembled too — see [Files and editor](#files-and-editor).

The app-specific callbacks that are not chat state (the model picker, session
restore, fork, opening a review buffer) are not assembled by the composition —
a host adds them through `chatOverrides`, a `Partial<ChatProps>` merged over
the assembled chat props, so the live transcript and send/queue wiring stay:

```tsx
<SproutWorkspace
  /* … */
  chatOverrides={useMemo(
    () => ({
      onModelClick: openModelPicker,
      onRestoreSession: restoreSession, // enables the header history switcher
      onForkAtBreakpoint: forkChat,
    }),
    [openModelPicker, restoreSession, forkChat],
  )}
/>
```

`chatOverrides` applies to the arranged `chat` view and to the composed space
shell's chat. `viewProps.chat` instead replaces the assembled chat props
wholesale (and `chatOverrides` is then ignored), for a host rendering its own
chat state; so does `shellProps`. `chatInitialState` seeds the chat state (an empty
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

## Files and editor

An arranged workspace is an IDE, not just a chat: in own mode a
`SproutWorkspace` with a `layout` also assembles the file tree, the editor and
the changes panel, so a host that arranges them gets working surfaces with no
extra wiring.

```tsx
import { SproutWorkspace } from "@sprout-foundry/workspace/views";

<SproutWorkspace
  project={{ id: repoUrl, repoUrl }}
  space="code"
  host={myHost}
  layout={{
    left: ["fileTree"],
    center: ["editor"],
    right: ["chat"],
    overlay: [],
  }}
/>;
```

- **`fileTree`** gets its callbacks from `useWorkspaceFileTree()`: listing
  through `/api/files`, create/delete/rename through `/api/create`,
  `/api/delete` and `/api/rename`, all on the adapter-aware `clientFetch` —
  so they reach the daemon, the in-browser workspace's VFS (WASM hosts) or the
  host's `transport.apiBaseURL`, whichever the host's transport names. It is
  the same hook the standalone app's Files sidebar uses. Selecting a file
  opens it in the editor; the active file is the tree's selection.
- **`editor`** renders `WorkspaceEditorView`: a tab per open file over the
  editor pane, with the app's buffer plumbing (loading, edits, Cmd/Ctrl+S
  and auto-save through `/api/file`, external-change detection).
- **`changes`** gets an `onFileClick` that opens the file in the editor.
- File links in the chat and tool output (the `sprout:open-in-editor` window
  event) open in the editor too.

The editor is a unit like the chat. `./views` exports
`WorkspaceEditorProvider`, `useWorkspaceEditor()` and
`useWorkspaceEditorProps()`; `SproutWorkspace` mounts the provider in own
mode, and the standalone app mounts the same provider at its root. It must sit
under `SproutProviders` (it reads the buffer manager).

- `useWorkspaceEditor()` reads the unit: `openFiles`, `activeFile`,
  `openFile(path, { line? })`, `closeFile(path)` and `saveFile(path?)` (the
  active file when `path` is omitted).
- `useWorkspaceEditorProps()` returns the wiring for the views around the
  editor: `fileTree` (`onFileSelect`, `selectedFile`) and `onFileClick(path,
line?)`.

**Overriding.** A kind in `viewProps` replaces the assembled props for that
kind, as for `chat`. A host that wants its own tree data passes
`viewProps.fileTree` (a `FileTreeProps`); `useWorkspaceFileTree({ fetchFn,
rootPath, onFileSelect, selectedFile })` is the starting point and needs no
provider, so it can be called in the host component that renders the
workspace. The host's tree sits outside the editor unit, so to open a file in
the editor it dispatches the open request the workspace listens for:

```tsx
const fileTree = useWorkspaceFileTree({
  fetchFn: myFetch,
  onFileSelect: (file) =>
    window.dispatchEvent(
      new CustomEvent("sprout:open-in-editor", { detail: { path: file.path } }),
    ),
});

<SproutWorkspace /* … */ layout={layout} viewProps={{ fileTree }} />;
```

A host-supplied `viewProps.editor` drops the assembled editor: the `editor`
kind then renders the bare `Editor` primitive with the host's props.

**The changes panel needs the daemon.** `AgentChangesPanel` reads the
daemon's change tracker (`/api/changes/summary`, `/api/changes/session`). A
host whose backend does not serve them — the in-browser agent, or a
self-hosted backend — sets `capabilities.agentChanges: false` in its
`SproutHost`; the panel then shows "Agent changes aren't available in this
workspace" and makes no request. `localHost` (the daemon) declares it `true`
and keeps the panel as it was; `headlessHost()` declares it `false`. Better
still, leave `changes` out of the arrangement on such a host.

## Preview

Two preview kinds can be arranged:

- **`previewPanel`** — the Code preview panel, driven by the dev-server
  lifecycle API (`/api/preview/*` through the adapter, polled while open). An
  own-mode composition assembles its `open` state: it starts open (the host
  placed it, and the arrangement has no toggle of its own) and its close
  button closes it. `viewProps.previewPanel` is merged over these, so a host
  that wants its own toggle passes `{ open, onClose }`. In ambient mode pass
  both yourself — without `open: true` the panel renders nothing. The default
  arrangement places `previewPanel` in the `overlay` slot, so an arrangement
  that omits `overlay` gets it; pass `overlay: []` to leave it out.
- **`previewPane`** — the presentational pane with no data source; the host
  supplies everything through `viewProps.previewPane` (`PreviewPaneProps`).
  Required: `status` (`"starting" | "running" | "stopped" | "failed"`) and
  `onRestart`. Optional: `url` (the iframe source, shown when `running`),
  `error` (shown when `failed`), `reloadKey` (bump to reload the app),
  `title` (header label, default "Preview"), `restartDisabled`, and `onClose`
  (omitted, no close button renders). Arranged without these props the pane
  renders an empty header and body, and its Restart button calls an
  undefined `onRestart`.

## Terminal

An arranged **`terminal`** is a working terminal on the host's backend — the
same terminal surface the standalone app docks at the bottom of the page
(session tabs, splits, the shell picker, background agent sessions), rendered
in place in its slot and always expanded:

```tsx
<SproutWorkspace
  project={{ id: repoUrl, repoUrl }}
  space="code"
  host={myHost}
  layout={{
    left: ["fileTree"],
    center: ["editor", "terminal"],
    right: ["chat"],
  }}
/>
```

What it attaches to follows the host (`resolveWorkspaceTerminalBackend`,
exported from `./views`):

- **In-browser agent** (`transport.agent.kind: "wasm"`): a terminal on the
  in-browser WASM shell — the shell the agent runs in, over the same
  workspace files. Commands the browser cannot run (compilers, runtimes) exit
  127 and feed the escalation flow, as in the hosted app.
- **Daemon terminal** (`capabilities.localTerminal: true`, and no wasm
  agent): the PTY terminal. Its WebSocket goes to `/terminal` under
  `transport.apiBaseURL` (`https://…` becomes `wss://…`; an empty base is the
  page origin), and its `/api/terminal/*` calls go through the same transport
  — see the host contract's [backend paths](./host-contract.md#backend-paths-for-an-in-browser-agent-host).
- **Neither**: the view renders "Terminal not available in this workspace."
  (`data-testid="workspace-terminal-unavailable"`) instead of a dead surface.

`viewProps.terminal` (`WorkspaceTerminalViewProps`) overrides: `backend`
(`"auto"` by default; `"wasm"`, `"pty"` or `"none"` pins one — `"none"` shows
the unavailable state), `unavailableMessage` (replaces the default text) and
`className`. The view fills its slot (with a 240px floor); its size beyond
that is the host's CSS. `WorkspaceTerminalView` is exported for hosts that
place it themselves under the provider stack.

## Command palette

The app's command palette (files, symbols, commands) is a workspace unit too.
**`commandPalette`** is an overlay kind: it renders nothing until opened. It is
**on by default** in own mode — the default arrangement's `overlay` is
`["previewPanel", "commandPalette"]`, and the composed space shell (no
`layout`) always mounts it. An arrangement that supplies its own `overlay`
without it opts out:

```tsx
layout={{ center: ["chat"], overlay: ["previewPanel"] }} // no palette
layout={{ center: ["chat"], overlay: ["commandPalette"] }} // palette, no preview
```

- **Opening.** Its shortcuts — `command_palette` (Ctrl/Cmd+Shift+P),
  `quick_open` and `editor_goto_symbol`, from the hotkey config — open it
  when the key is pressed **inside the workspace**; the same key pressed in
  the host page around it is left to the host. Dispatching
  `OPEN_COMMAND_PALETTE_EVENT` (exported from `./views`) on `window` opens it
  from host chrome. Mounting it registers for that event, which is what makes
  the workspace header's search control appear; without the palette there is
  no dead control.
- **What it lists.** Workspace files (the `/api/file-index` walk through the
  host transport) and the recently opened editors; symbols in the active file
  and across the workspace; and only the built-in commands something mounted
  in the embed handles. A host makes a built-in command id available by
  handling its `sprout:hotkey` event and calling
  `registerCommandHandler([id])` (exported; returns the unregister
  function), or adds its own commands through `viewProps.commandPalette`:
  `{ commands: CommandDef[], onExecuteCommand(id) }` (return `false` to keep
  the palette open). `onOpenChange(open)` reports the palette opening and
  closing.
- **Opening files.** A picked file opens through the `sprout:open-in-editor`
  request, so it lands in the arranged editor (or the composed shell's).

The standalone app keeps its own palette (the same component, fed by the same
`useCommandPaletteSources` hook); inside the app shell the workspace unit
renders nothing. In ambient mode, leave `commandPalette` out of the overlay if
the caller mounts its own palette.

## Escalation (run it where it can run)

When an agent command cannot run in the browser (exit 127 — no compilers in
the WASM shell), a git push the browser cannot perform, or the VFS hitting its
quota, the work can run instead in the user's cloud workspace or on one of
their runners: the browser's changed files are pushed, the command runs, the
resulting deltas come back (`/workspace/txn`, see the host contract). `./views`
exports the pieces a composing host mounts — the same two the Sprout app root
mounts:

These read the host through `HostProvider`, so they must render **inside** the
workspace's provider. With `SproutWorkspace` in its default `providers="own"`
mode, pass them as its `children` (rendered inside the provider it creates);
mounted beside `SproutWorkspace` they throw "useHost must be used within a
HostProvider". In `providers="ambient"` mode, mount them anywhere under your
own `HostProvider`.

```tsx
import {
  AgentEscalationBridge,
  EscalationListener,
  SproutWorkspace,
  useEscalationTriggers,
} from "@sprout-foundry/workspace/views";

// The detector for the trigger events is a hook, so it lives in a small
// component of the host's own.
function EscalationTriggers({ repoURL }: { repoURL: string }) {
  useEscalationTriggers({ repoURL });
  return null;
}

<SproutWorkspace host={host} project={{ repoUrl: repoURL }} /* … */>
  {/* The detector, and the "Browser limitation reached" affordance for
      the user's own actions (git push, VFS quota, timeouts): */}
  <EscalationTriggers repoURL={repoURL} />
  <EscalationListener />
  {/* Installs globalThis.__sproutEscalate, the hook the WASM agent calls
      on exit 127, and asks the consent question: */}
  <AgentEscalationBridge repoURL={repoURL} />
</SproutWorkspace>;
```

Mount each piece once per page. If a second `AgentEscalationBridge` mounts,
the newest one handles escalations, and unmounting it hands the hook back to
the one it replaced. A bridge that unmounts while a consent question is open
answers it "deny", so the agent is told the command did not run.

`AgentEscalationBridge` takes the repository the workspace was imported from —
the same value `SproutWorkspace`'s `project.repoUrl` carries, so a host threads
one source of truth to both. The consent question, the host picker (runners or
cloud) and the progress line are rendered by the bridge; a host that wants its
own consent UI calls `installEscalationBridge` directly with a
`requestConsent` callback (its `EscalationBridgeOptions` and `EscalationResult`
types are exported alongside), and reads or sets the user's policy through
`getEscalationPolicy` / `setEscalationPolicy`.

**Placement.** By default the consent dialog is a page-level overlay and the
progress line sits at the bottom-left of the page. To keep both inside the
host's own chrome — scoped to the embedded workspace's area, say — pass an
element as `container`: the bridge renders them into it through a React
portal, positioned within it (absolute instead of fixed). Give the element a
positioned layout (`position: relative`) and the size the dialog should cover.
A contained dialog does not mark the rest of the page inert (`aria-modal` is
not set); Escape still declines. Omitted or `null`, the bridge renders
page-level as before.

```tsx
const [pane, setPane] = useState<HTMLElement | null>(null);

<div ref={setPane} style={{ position: "relative" }}>
  <SproutWorkspace host={host} project={{ repoUrl: repoURL }} /* … */>
    <AgentEscalationBridge repoURL={repoURL} container={pane} />
  </SproutWorkspace>
</div>;
```

**Consent.** The policy is per browser: `ask` (the default) asks before every
command; `always` (the prompt's "Always allow") runs without asking on the host
remembered for the repository; `never` keeps every command in the browser. Even
under `always`, the bridge asks before every run on a bare-metal runner, which
runs commands as the user with no sandbox; a host driving
`installEscalationBridge` itself gets the same behavior by passing
`requiresConsent`.

The seam is WASM-side only — no daemon and no local terminal involved. The
transactional runs go through relative paths, so they ride the host's
transport: a host that serves the cloud transactional surface gets working
escalation. Without that backend the triggers still fire and the user is still
asked; an approved run fails and the agent is told it did not run.

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
