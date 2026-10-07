# The Sprout Host Contract — `SproutHost`

This document describes the host contract, the single channel between Sprout
and whatever hosts it. A host supplies one typed `SproutHost` object; Sprout
never infers its host from a build flag, an `appMode` field or a URL. The
interface lives in `webui/src/host/types.ts` and is exported from the host
entry point (`webui/src/host/index.ts`).

The contract is the part of the integration API that a host *provides*
(SP-160 §160b). What a host *mounts* — the spaces and views — is the
composition API (SP-160 §160a); those exports come from
`webui/src/views/index.ts`.

```ts
import { HostProvider, useHost } from '../../webui/src/host/index';
import type { SproutHost } from '../../webui/src/host/index';
import { ViewsLayout, availableModes } from '../../webui/src/views/index';
```

A host imports only the two public entry points above. Nothing else in the
host tree (`webui/src/host/`) or the views tree is part of the contract.

## Shape

`SproutHost` has eight areas. Only `transport`, `navigation`,
`notifications` and `capabilities` are required; the rest are optional and
Sprout renders nothing (or its own local default) when they are absent.

| Area | Field | Required | The host provides |
|------|-------|----------|-------------------|
| Identity | `user` | no | The account (`id`, optional `displayName`/`avatarUrl`), or `null`/absent for no account |
| Entitlements | `entitlements` | no | A generic usage summary and what running out of usage does |
| Transport | `transport` | yes | Where backend calls go, and how they authenticate |
| Navigation | `navigation` | yes | Outward intents Sprout can request, and deep links back into a project/space |
| Notifications | `notifications` | yes | A sink Sprout posts to, and an optional unread count |
| Chrome slots | `chrome` | no | React nodes for Sprout's header areas, or an instruction to render the host's own chrome |
| Theme | `theme` | no | Token values or a mode Sprout follows live |
| Capabilities | `capabilities` | yes | Explicit flags for what this host supports |

## Fields, field by field

### `user?: HostUser | null`

The host account, when one exists. Absent or `null` means the host has no
account concept and no user is signed in; Sprout then renders no account
chrome of its own. `HostUser` is `{ id: string; displayName?: string;
avatarUrl?: string }` — the host supplies as much identity as it wants to
expose.

### `entitlements?: HostEntitlements`

A generic usage summary, with no platform-specific concept baked in. The host
decides what "usage" means and what running out of it does; Sprout only
renders the label, follows the link, and invokes the out-of-usage action.

- `usageSummary?: { remaining: number | string; label: string; linkTarget:
  string; onOutOfUsage?: () => void }` — the summary to display. `remaining`
  is the remaining share (a number or a host-formatted string), `label`
  describes what it is, `linkTarget` is where following it goes, and
  `onOutOfUsage` is invoked by Sprout when usage runs out. Absent means Sprout
  renders nothing.
- `resolve?: () => Promise<void>` — re-resolve the summary with the host.
  Sprout calls it on mount, on focus, while the tab is visible, and when it
  leaves the host's Home surface. The host owns the fetch and updates its own
  summary (the hosted `CreditsChip` re-reads after each `resolve`). Absent
  means the summary is static and Sprout just renders the supplied value.

### `transport: HostTransport`

Where Sprout's backend calls go. Transport is host-provided so Sprout never
infers its backend from build flags or URLs.

- `apiBaseURL: string` — the base URL for HTTP API calls, or `''` for
  same-origin.
- `wsURL: string` — the WebSocket URL for the agent event stream.
- `authMode: 'none' | 'bearer'` — how the transport authenticates requests.
- `modelEndpoint?: string` — the endpoint for the in-browser agent's model
  calls; absent means the agent falls back to its default endpoint.

### `navigation: HostNavigation`

Outward navigation. Sprout requests an intent; the host resolves it however it
wants (open a page, focus an existing one, no-op). The optional item lists let
the host hand Sprout its account-area exits and Home "Work" places as data
(labels + intents), so no Sprout component hard-codes a host page name.

- `open(intent: HostNavigationIntent): void` — resolve an intent, or follow a
  deep link into a project/space.
- `accountItems?: HostNavItem[]` — the account-area exit items (Dashboard,
  Usage & billing, Team, Runners, Settings, …), each `{ label, intent }`.
- `workItems?: HostNavItem[]` — the Home "Work" section items.
- `intentPath?(intent): string | null` — the host's own resolution of an
  intent to a page path, or `null` when the host has no page for it. Sprout
  reads this only to give a link an `href`; the path string stays on the host
  side.

The intent union `HostNavigationIntent` has seven members:

| Intent | Meaning |
|--------|---------|
| `{ type: 'account' }` | The host's account surface |
| `{ type: 'usage' }` | Usage / billing |
| `{ type: 'help' }` | Help |
| `{ type: 'signOut' }` | Perform sign-out |
| `{ type: 'project'; project: string }` | Deep link to a project |
| `{ type: 'space'; project: string; space: string }` | Deep link to a project space |
| `{ type: 'nav'; id: string }` | A host-defined destination by the host's own id (Team, Runners, …); Sprout never interprets the id |

### `notifications: HostNotifications`

A sink Sprout posts notifications to.

- `post(notification: HostNotification): void` — post a notification. The
  shape mirrors Sprout's in-app bus: `{ level: 'info' | 'success' | 'warning'
  | 'error'; title: string; message: string; duration?: number; action?:
  HostNotificationAction }`, where an action is `{ label, onClick, keepOpen? }`.
- `count?: number` — an optional unread count the host surfaces in its own
  chrome. Sprout renders the number it is given and never infers one.

### `chrome?: HostChrome`

Chrome slots the host fills in Sprout's own header, or an instruction to
render the chrome itself.

- `headerLeft?: ReactNode` / `headerRight?: ReactNode` — nodes Sprout renders
  in its left/right header areas (e.g. an account menu).
- `renderOwnChrome?: boolean` — when true the host renders its own chrome and
  Sprout hides its header.

### `theme?: HostTheme`

Theming the host applies. Sprout follows it live, replacing any DOM
observation.

- `mode?: 'light' | 'dark' | 'system'` — the preferred color mode; `'system'`
  follows the OS/browser setting live.
- `tokens?: Record<string, string>` — design-token overrides as
  `{ tokenName: value }`. Sprout applies them live inside the workspace root.

### `capabilities: HostCapabilities`

Explicit, flat flags for what this host supports. These are the switches that
replace the build-time mode flags; every former mode flag maps to one of them.
Components read them through `useHostCapabilities()` (React) or the
non-React accessor `getActiveHost()?.capabilities` (services, module-scope
helpers).

| Flag | Governs |
|------|---------|
| `ssh` | Shell-over-SSH transport for remote/terminal sessions |
| `git` | Git operations (in-browser or host-provided) |
| `chat` | Agent chat |
| `workspaceSwitching` | The workspace switcher UI |
| `folderPicker` | A native folder picker (host shells only) |
| `export` | Exporting the workspace to a local filesystem |
| `instances` | Instance management (the instance list and its actions) |
| `localTerminal` | A local PTY terminal (the host provides the terminal transport) |
| `settings` | The settings panel |
| `automations` | Automation workflows and their scheduling UI |
| `agentChanges` | Agent change history (the change history tab) |
| `mcp` | Model Context Protocol tool configuration |
| `localModels` | Locally-hosted model selection and management |
| `verification` | Verification flows and their results UI |
| `serverGit` | Server-side git, distinct from in-browser git |

## The hosts Sprout ships

Two host objects are built in, and the app entry point (`webui/src/index.tsx`)
picks one once at startup:

```ts
const host = (import.meta.env.VITE_SPROUT_MODE as string) === 'cloud' ? cloudHost : localHost;
setActiveHost(host);
root.render(
  <HostProvider host={host}>
    <App />
  </HostProvider>,
);
```

The entry is the single place that reads the build flag; everything downstream
reads the host through `useHost()` or `getActiveHost()`.

A third host — the headless default — is exported from the same entry point as
`headlessHost()` and the pre-built `defaultHost`. `HostProvider` falls back to
it when no host is supplied (`host ?? headlessHost()`): same-origin transport,
no-op navigation and notifications, and every capability **off**. It is what a
tree that has not been wired to a real host renders with, so nothing has to
assume a host is present.

### `localHost`

The standalone local build's contract (`webui/src/host/localHost.ts`): no
account, no entitlements, the local backend at the current origin, and every
local capability on. The page and the local daemon share an origin, so the
transport records the same-origin sentinel `''` rather than a fixed host.

- `user: null`, `entitlements: undefined`.
- `transport: { apiBaseURL: '', wsURL: '', authMode: 'none' }`.
- `navigation.open` is a no-op and `intentPath` always returns `null` — the
  local build has no outward platform pages.
- `notifications.post` forwards to Sprout's in-app notification bus, so local
  toasts and the NotificationCenter are unchanged.
- Capabilities: `ssh`, `git`, `chat`, `workspaceSwitching`, `export`,
  `localTerminal`, `settings`, `automations`, `agentChanges`, `mcp`,
  `localModels`, `verification`, `serverGit` are **on**; `folderPicker` and
  `instances` are **off**.

### `cloudHost`

The hosted build's contract (`webui/src/host/cloudHost.ts`). It declares the
*shape* of the cloud host and the platform's navigation surface; the platform
resolves the concrete identity, chrome and theme values at runtime through the
bootstrap adapter.

- `user` and `chrome`/`theme` are absent here (host-provided at runtime).
- `entitlements` resolves live from the platform's billing status;
  `resolve()` refreshes the summary in place.
- `transport: { apiBaseURL: '', wsURL: '', authMode: 'bearer' }` — `''` is the
  "resolved at runtime" sentinel (the bootstrap adapter resolves the concrete
  URL), and `authMode: 'bearer'` is what marks the transport as hosted.
- `navigation` resolves intents to platform SPA paths and lists the platform's
  account exits (`accountItems`) and Home work places (`workItems`).
- `notifications.post` still forwards to the in-app bus until the platform's
  own sink lands.
- Capabilities mirror what a hosted build actually exposes:
  `git`, `chat`, `instances`, `settings` are **on**; `ssh`,
  `workspaceSwitching`, `folderPicker`, `export`, `localTerminal`,
  `automations`, `agentChanges`, `mcp`, `localModels`, `verification`,
  `serverGit` are **off**.

The legacy hosted program also had a `supportsSSH` flag and an
`appMode`/`isCloud` pair; today `ssh` is the flag and `authMode === 'bearer'`
is the "am I hosted" test (see the conversion table below).

## How a host supplies each area

Each area is a field on the object a host passes to `HostProvider`. A host
reads the contract back through `useHost()`.

### Entitlements

Supply a usage summary (and, optionally, a resolver the host owns):

```tsx
const host: SproutHost = {
  // …
  entitlements: {
    usageSummary: {
      remaining: 42,
      label: '42 example credits remaining',
      linkTarget: 'account',
    },
  },
};
```

A host whose balance moves at runtime supplies `resolve` instead of a static
value, so Sprout re-reads after it calls `resolve()` on mount, focus,
visibility and Home-close.

### Navigation intents

Supply `open`, and, when the host has an account area, its items as data.
Sprout renders the items and dispatches `open(item.intent)`:

```tsx
const host: SproutHost = {
  // …
  navigation: {
    open(intent) {
      // The host resolves the intent: open its own page, focus an existing
      // one, or no-op. Sprout never interprets the intent.
      myRouter.go(resolvePath(intent));
    },
    accountItems: [
      { label: 'Usage & billing', intent: { type: 'usage' } },
      { label: 'Team', intent: { type: 'nav', id: 'team' } },
    ],
    workItems: [{ label: 'Dashboard', intent: { type: 'nav', id: 'dashboard' } }],
    intentPath(intent) {
      return myRouter.pathFor(intent); // or null when there is no page
    },
  },
};
```

### Chrome slots

Supply React nodes for Sprout's header areas, or ask Sprout to stand down:

```tsx
const host: SproutHost = {
  // …
  chrome: {
    headerLeft: <MyBrand />,
    headerRight: <MyAccountMenu />,
    // renderOwnChrome: true,   // hide Sprout's header and draw your own
  },
};
```

### Theme

Supply a mode, token overrides, or both. Sprout applies them live, so a host
never copies CSS or observes the DOM:

```tsx
const host: SproutHost = {
  // …
  theme: {
    mode: 'dark',
    tokens: { '--accent-primary': '#8b5cf6' },
  },
};
```

### Capabilities

Supply the flat flag set. A flag left `false` hides the surface that reads it:

```tsx
const host: SproutHost = {
  // …
  capabilities: {
    ssh: false,
    git: true,
    chat: true,
    workspaceSwitching: false,
    folderPicker: false,
    export: false,
    instances: false,
    localTerminal: false,
    settings: true,
    automations: false,
    agentChanges: true,
    mcp: false,
    localModels: false,
    verification: false,
    serverGit: false,
  },
};
```

## The example host

`webui/src/host/example/ExampleHost.tsx` is the worked example: it builds a
real `SproutHost` with its own chrome, theme, capabilities, transport,
navigation, notifications and entitlements, mounts two spaces through
`ViewsLayout`, and records what it receives when Sprout posts a notification
or asks it to navigate. It is test-only — nothing in the app renders it — but
it is the shortest complete reference for a host. A host application follows
the same shape:

```tsx
import { HostProvider, useHost } from '../../webui/src/host/index';
import type { HostNotification, HostNavigationIntent, SproutHost } from '../../webui/src/host/index';
import { ViewsLayout, resolveWorkspaceMode } from '../../webui/src/views/index';

function MyRoot() {
  const host = useHost();
  const space = resolveWorkspaceMode('code', { hasDesignTree: false });
  return (
    <div data-theme={host.theme?.mode}>
      <MyChrome />
      <ViewsLayout arrangement={{ left: [], center: ['editor'], right: [], overlay: [] }} />
      <button onClick={() => host.navigation.open({ type: 'account' })}>Account</button>
    </div>
  );
}

export function MyHost({ host }: { host: SproutHost }) {
  return (
    <HostProvider host={host}>
      <MyRoot />
    </HostProvider>
  );
}
```

## What the former `isCloud` branches became

Before the host contract, `webui/src` branched on a build-time `isCloud` flag
(and its runtime twin `appMode`) in the web UI's workspace UI, startup and
transport layers. Each branch became a capability read or a host/transport
field. The conversions landed in the host.3–host.8 commits; the sites that
also carry a source comment say "the former isCloud branch", and the original
branch is recoverable from the conversion commits (`git show 536a191c2`,
`e5443ab53`, `5d7bc6616`, `daa442b0f`, `dab372189`).

| Former `isCloud` branch (module / what it did) | What it became (capability or host field) |
|---|---|
| `config/mode.ts` — `supportsSSH`, local on / cloud off | `capabilities.ssh` |
| `config/mode.ts` — `supportsGit`, local on / cloud on | `capabilities.git` |
| `config/mode.ts` — `supportsChat`, local on / cloud on | `capabilities.chat` |
| `config/mode.ts` — `supportsWorkspaceSwitching`, local on / cloud off | `capabilities.workspaceSwitching` |
| `config/mode.ts` — `supportsFolderPicker`, local off / cloud off | `capabilities.folderPicker` |
| `config/mode.ts` — `supportsExport`, local on / cloud off | `capabilities.export` |
| `config/mode.ts` — `supportsInstances`, local off / cloud on | `capabilities.instances` |
| `config/mode.ts` — `supportsLocalTerminal`, local on / cloud off | `capabilities.localTerminal` |
| `config/mode.ts` — `supportsSettings`, local on / cloud on | `capabilities.settings` |
| `config/mode.ts` — `supportsAutomations` (`!isCloud`) | `capabilities.automations` |
| `config/mode.ts` — `supportsAgentChanges` (`!isCloud`) | `capabilities.agentChanges` |
| `components/StatusBar.tsx` — workspace label/name shown when not cloud | `capabilities.workspaceSwitching` |
| `components/StatusBar.tsx` — git branch placeholder when not cloud | `capabilities.localTerminal` |
| `components/StatusBar.tsx` — account-managed model badge | `transport.authMode === 'bearer'` |
| `components/HeaderBar.tsx` — platform header affordances (back-to-dashboard, Start Building, credits) vs Sprout's MenuBar | `transport.authMode === 'bearer'` |
| `components/AppContent.tsx` — platform notification polling + phone tab bar | `transport.authMode === 'bearer'` |
| `workspaces/CodeShell.tsx` — project named by repo slug | `transport.authMode === 'bearer'` |
| `components/chat/EmptyChatPanel.tsx` — offer the "open a repository" panel | `transport.authMode === 'bearer'` |
| `components/chat/ChatMetricsStrip.tsx` — hide the dollar cost (credits are metered) | `transport.authMode === 'bearer'` (inverted) |
| `components/GitSidebarPanel.tsx` — browser git, and the "unsupported in browser mode" ops | `transport.authMode === 'bearer'` |
| `components/GitHistoryPanel.tsx` — browser git cannot revert | `transport.authMode === 'bearer'` |
| `hooks/useAppInitialization.ts` — auth check / login redirect and browser boot wiring | `transport.authMode === 'bearer'` |
| `bootstrapAdapter.ts` — decide whether to install the `CloudAdapter` (the `appMode === 'cloud'` branch) | `getActiveHost()?.transport.authMode === 'bearer'` |
| `bootstrapAdapter.ts` — hosted-vs-local fallback URL | `getActiveHost()?.transport.authMode === 'bearer'` |
| `services/backendHealth.ts` — adapter fetch vs `clientFetch` for the health probe | `transport.authMode === 'bearer'` |
| `services/gitCorsProxy.ts` — the `/git-proxy` CORS proxy for browser git | `transport.authMode === 'bearer'` |
| `host/PlatformGitHubAccountCard.tsx` — open account settings inside the editor when hosted | `transport.authMode === 'bearer'` |
| `services/websocket.ts` — send the pause dead-letter on tab freeze | `capabilities.localTerminal` |
| `services/serviceWorkerRegistration.ts` — register the service worker | `capabilities.localTerminal` |
| `components/WasmLoadingOverlay.tsx` — show the in-browser WASM download overlay | `capabilities.localTerminal` (inverted) |
| `hooks/useCloudSessionPersistence.ts` — mirror conversations in a hosted shell | `capabilities.localTerminal` (inverted) |
| `components/SettingsPanel.tsx` — hide local-only subsections | `capabilities.localTerminal` (inverted) |
| `components/SidebarSettingsSection.tsx` — local-only subsection; BYOK vs platform model picker | `capabilities.localTerminal` |
| `components/SidebarGitSection.tsx` — two local-only panes (worktrees) | `capabilities.localTerminal` |
| `components/SidebarFilesSection.tsx` — clone-repo action | `capabilities.localTerminal` |
| `components/Sidebar.tsx` — the Automations tab (`supportsAutomations`, i.e. `!isCloud`) | `capabilities.automations` |
| `components/WorkspaceBar.tsx` — hidden host name | `capabilities.workspaceSwitching` |
| `components/Terminal.tsx` — the WASM terminal notice | `capabilities.localTerminal` (inverted) |
| `hooks/useSecurityHandlers.ts` — the ask-user POST path | `capabilities.localTerminal` (inverted) |
| `hooks/useEscalationTriggers.ts` + `components/AgentEscalationBridge.tsx` — hosted-only escalation | `capabilities.localTerminal` (inverted) |
| `components/locationSwitcher/SSHPanel.tsx` — the SSH panel | `capabilities.ssh` |
| `components/locationSwitcher/WorkspacePopover.tsx` + `hooks/useInstances.ts` — the instance list | `capabilities.instances` |
| `components/chat/ChatHistorySwitcher.tsx` + `components/ChatView.tsx` — session export | `capabilities.export` |
| `components/layered/LayeredSidebar.tsx` — hosted layout title, project rail, terminal section | `capabilities.instances` (`hosted` = `instances`) |

Two former reads moved to the host object itself rather than to a capability:
the platform URL helpers (`host/platformUrl.ts`), `host/platformGitHub.ts`,
`host/PlatformGitHubAccountCard.tsx` and the platform page names in
`layered/HomeNav.tsx`, `ProjectRail.tsx`, `LayeredSidebar.tsx` and
`UserMenu.tsx` now resolve through `host.navigation` intents/items and
`host.entitlements`, so no module outside `webui/src/host/` names the
platform's billing, team, runner or account pages. (Those modules also read
capabilities for their layout, hence their rows above.)

## The boundary guard

The contract is enforced by a test: `webui/src/host/hostBoundary.test.ts`
scans `webui/src` and fails if any module **outside** `webui/src/host/` reads
the build mode, names `appMode` or `isCloud`, or imports a host
implementation by name (`localHost`, `cloudHost`, `headlessHost`,
`defaultHost`, `setActiveHost`). It flags every route to the build mode:

- `import.meta.env.VITE_SPROUT_MODE` (the mode var),
- `import.meta.env.MODE` (Vite's built-in mode string),
- any bracket read of the env object (`import.meta.env[…]`), which can dodge
  a property-name rule,
- aliasing the env object (`const env = import.meta.env`, a destructure, or
  passing it to a function).

The rule is scoped to what can resolve to the **build mode**, not a blanket
ban on `import.meta.env`: a direct `.PROP` read of a non-mode property (a URL,
the `PROD`/dev flag, a `VITE_SPROUT_NATIVE_*` seam flag) is legitimate and is
not flagged.

Exactly one module is allowed to select a host — the app entry
`webui/src/index.tsx` — and it must import the host from the host tree. A new
file naming a host is a failure, not a silent second entry point. The entry's
env carve-out is limited to what it actually does: a single
`import.meta.env.VITE_SPROUT_MODE` read. It may not use `.MODE`, bracket access
or aliasing.

### Restoring a shell's declared capabilities

A host shell advertises what it provides by installing an adapter (a studio
`--native-fs` dist ships a `capabilities.json` declaring
`supportsFolderPicker` / `supportsWorkspaceSwitching`, which its shell reports
through the installed adapter). The host shell is the one that ships the
native operations, so its declaration wins over the host's local default.

The entry (`webui/src/index.tsx`) applies the adapter's declared capability
flags onto the selected host's `capabilities` (`host/applyHostCapabilities.ts`)
after the adapter installs, then calls `upsertActiveHostCapabilities()`
(`host/accessor.ts`) to re-dispatch `HOST_UPDATED_EVENT` and run the
capability hooks, so `config/mode`'s `supports*` bindings re-derive from the
merged host. The merge is generic — every capability key the adapter declares,
whatever its value — with no platform- or studio-specific branch. A plain
local build (no declared flags) keeps the host's own capabilities, and the
cloud host is unaffected.
