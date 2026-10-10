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
- `agent?: HostAgentBackend` — the agent backend this transport selects, when
  the host chooses one explicitly (see "Agent backend" below). Absent means the
  host did not choose, and Sprout keeps its own default for the build.
- `modelEndpoint?: string` — the endpoint for the in-browser agent's model
  calls; absent means the agent falls back to its default endpoint.

#### Agent backend

The agent behind a chat runs either in a Sprout **daemon** the host has for the
project (the preferred backend: full tools, the daemon is the source of truth)
or in the **browser** (the in-browser WASM agent). A host states which, when it
cares, through `transport.agent`:

```ts
type HostAgentBackend =
  | { kind: 'daemon'; apiBaseURL: string; wsURL: string }
  | { kind: 'wasm'; modelEndpoint: string };
```

- `{ kind: 'daemon', apiBaseURL, wsURL }` — the agent runs in a daemon
  reachable through the host. The daemon's URLs drive Sprout's calls (the same
  `clientFetch` / WebSocket resolution described below), so no extra wiring is
  needed beyond the transport itself.
- `{ kind: 'wasm', modelEndpoint }` — the agent runs in the browser. Sprout
  installs the cloud adapter from the host, routes the in-browser agent's events
  into the same event bus WebSocket events use, and points the agent's model
  calls at `modelEndpoint` (falling back to `transport.modelEndpoint`, then the
  platform's own proxy path). The repository to open comes from the host's
  project (`project.repoUrl`), not the `?repo=` URL parameter.

When the in-browser agent runs against a daemon backend, its model calls go to
the daemon's own model proxy (`<apiBaseURL>/api/proxy/chat`).

A host that mounts a workspace through `SproutWorkspace` gets this wiring for
free: in own mode the composition honours `transport.agent` (a daemon backend is
the no-wiring default; a wasm backend installs the adapter and the dispatcher).
A host that composes its own provider stack (ambient mode) owns the wiring.

**Switching backends.** A host may switch backends when a daemon becomes
available. The supported switch is a **re-mount** with a new transport: the
composition keys its wiring on the backend, so re-mounting `SproutWorkspace`
with a transport whose `agent` differs re-runs the wiring against the new
backend. In-place switching without a re-mount is not supported.

**How the transport fields are consumed.** A component that mounts a
workspace in its own provider stack (`SproutWorkspace` with the default
`providers="own"`) registers its host as the *active* host for the
module-level services while it is mounted, and restores the previous host on
unmount. The non-React services then resolve their URLs against it:

- `clientFetch` prefixes a **relative** API path with `transport.apiBaseURL`
  when it is non-empty. Precedence, most specific first: an installed
  adapter (which handles the request itself), then `transport.apiBaseURL`,
  then the SSH proxy base (`window.SPROUT_PROXY_BASE`), then same-origin (no
  prefix). An absolute URL is never prefixed, and `''` is the same-origin
  sentinel that falls through to the proxy base.
- The WebSocket URL resolves, most specific first: the build-time
  `VITE_WS_URL`, then `transport.wsURL`, then the installed adapter's URL,
  then the same-origin `/ws` path (through the proxy base when set). `''`
  falls through.

In `providers="ambient"` mode the caller has already registered its host and
owns the transport, so the composition neither registers nor connects — see
"one workspace per page" in
[workspace-package.md](workspace-package.md).

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
  `{ tokenName: value }`. Sprout applies them live **inside the workspace
  root** (see the token-name mapping below).

#### Token names

The token names are the `@sprout-foundry/design` package's token names
(`packages/design/tokens.css`), which is also the web UI's single source of
truth for its own tokens: the web UI imports the package's
`@sprout-foundry/design/tokens.css` from its entry stylesheet
(`webui/src/index.css`). A host does not import the package to theme Sprout —
it passes values through `theme.tokens` — but the names it overrides are the
package's, so the set a host can target is exactly what
`packages/design/tokens.css` declares.

| Group | Names (examples) | What they govern |
|-------|------------------|------------------|
| Backgrounds | `--bg-primary`, `--bg-secondary`, `--bg-tertiary`, `--bg-elevated`, `--bg-surface`, `--bg-hover`, `--bg-input` | Page, panel and control surfaces |
| Borders | `--border-subtle`, `--border-default`, `--border-strong`, `--border-focus` | Dividers, outlines, focus rings |
| Text | `--text-primary`, `--text-secondary`, `--text-tertiary`, `--text-muted`, `--text-accent` | Foreground text hierarchy |
| Accents | `--accent-primary`, `--accent-secondary`, `--accent-success`, `--accent-warning`, `--accent-error`, `--accent-cyan`, `--accent-primary-rgb` | Semantic colors (note `--accent-primary-rgb` is the space-separated triplet used inside `rgba()`) |
| Brand | `--brand-teal`, `--brand-frost`, `--brand-sprout`, `--brand-active-cyan`, `--brand-navy` | Sprout brand surfaces |
| Typography | `--font-sans`, `--font-mono`, `--text-xs` … `--text-3xl` | Font families and sizes |
| Spacing | `--space-1` … `--space-12` | Layout rhythm |
| Radius | `--radius-sm`, `--radius-md`, `--radius-lg`, `--radius-xl`, `--radius-pill` | Corner rounding |
| Shadows / gradients | `--shadow-subtle`, `--shadow-elevated`, `--shadow-float`, `--gradient-subtle`, `--gradient-elevated` | Elevation |
| Motion | `--ease-out`, `--ease-in-out`, `--duration-fast`, `--duration-base`, `--duration-slow` | Transitions (durations zeroed under `prefers-reduced-motion`) |

A host supplies the same names, e.g.
`tokens: { '--accent-primary': '#8b5cf6', '--accent-primary-rgb': '139, 92, 246' }`.
A value that aliases another token (`--bg-hover: var(--bg-elevated)`) or mixes
one (`--bg-error: color-mix(in srgb, var(--accent-error) 12%, transparent)`)
follows whatever the host overrides those referenced tokens to.

#### Scoping

The host's `theme.tokens` overrides are applied on the **workspace root
element** (`.sprout-workspace`, the element `SproutWorkspace` renders), as
inline custom properties — never on `document.documentElement`. A host page and
the Sprout workspace it mounts share one document, so confining the overrides
to the root is what keeps a host theme from restyling the host's *own* chrome.
The `mode` is different: it selects the applied theme pack, whose variables are
the app's own theme and land on `documentElement` exactly as Sprout's local
build does today. Only the host's per-token overrides are scoped.

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
| `chatSessions` | The host serves the chat session store (see "Chat session store" below) |

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
- `transport: { apiBaseURL: '', wsURL: '', authMode: 'none', agent: { kind:
  'daemon', apiBaseURL: '', wsURL: '' } }` — the agent is the local daemon,
  reached at the transport's own same-origin URLs.
- `navigation.open` is a no-op and `intentPath` always returns `null` — the
  local build has no outward platform pages.
- `notifications.post` forwards to Sprout's in-app notification bus, so local
  toasts and the NotificationCenter are unchanged.
- Capabilities: `ssh`, `git`, `chat`, `workspaceSwitching`, `export`,
  `localTerminal`, `settings`, `automations`, `agentChanges`, `mcp`,
  `localModels`, `verification`, `serverGit` are **on**; `folderPicker`,
  `instances` and `chatSessions` are **off** (the local build keeps its own
  daemon chat session store).

### `cloudHost`

The hosted build's contract (`webui/src/host/cloudHost.ts`). It declares the
*shape* of the cloud host and the platform's navigation surface; the platform
resolves the concrete identity, chrome and theme values at runtime through the
bootstrap adapter.

- `user` and `chrome`/`theme` are absent here (host-provided at runtime).
- `entitlements` resolves live from the platform's billing status;
  `resolve()` refreshes the summary in place.
- `transport: { apiBaseURL: '', wsURL: '', authMode: 'bearer', agent: { kind:
  'wasm', modelEndpoint: '' } }` — `''` is the "resolved at runtime" sentinel
  (the bootstrap adapter resolves the concrete URL), `authMode: 'bearer'` is
  what marks the transport as hosted, and the agent backend is the in-browser
  WASM agent (its model endpoint resolved at runtime from the platform).
- `navigation` resolves intents to platform SPA paths and lists the platform's
  account exits (`accountItems`) and Home work places (`workItems`).
- `notifications.post` still forwards to the in-app bus until the platform's
  own sink lands.
- Capabilities mirror what a hosted build actually exposes:
  `git`, `chat`, `instances`, `settings` are **on**; `ssh`,
  `workspaceSwitching`, `folderPicker`, `export`, `localTerminal`,
  `automations`, `agentChanges`, `mcp`, `localModels`, `verification`,
  `serverGit`, `chatSessions` are **off** (the hosted platform keeps no chat
  session store; the chat list lives in the browser).

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
    chatSessions: false,
  },
};
```

### Chat session store

By default Sprout keeps its own chat session store: the local daemon's
`/api/chat-sessions*` endpoints, or — for the in-browser agent, which has no
daemon — a browser-local store. A host that advertises
`capabilities.chatSessions: true` takes over that store: the chat session
list / create / rename / delete / delete-all / switch / messages calls go to
the host, and finished turns are appended to it, for both the daemon and the
in-browser agent backends. With the capability off (the default) behaviour is
unchanged.

When the capability is on, the calls are made at
`transport.apiBaseURL` with the **same request and response shapes as the
daemon's `/api/chat-sessions*` endpoints** — a host implements the endpoints
below and the chat UI needs no host-specific branch. The base URL is the
transport's `apiBaseURL` (`''` is the same-origin sentinel); the paths below
are appended to it.

#### `GET /api/chat-sessions`

List the chats.

```
200 {
  "message": string,
  "chat_sessions": ChatSession[],
  "active_chat_id": string,
  "total_sessions": number
}
```

`ChatSession` is the daemon's wire shape: `id`, `name`, `created_at`,
`last_active_at`, `message_count`, `current_session_id`, `active_query`
(boolean — a run is in progress), `is_pinned`, `mode` (`"code"` | `"design"`),
plus the computed `is_default` / `is_active` flags the UI reads.

#### `POST /api/chat-sessions/create`

Create a chat. Does **not** switch the active chat — the UI switches to the
new chat itself, as it does against the daemon.

```
body: { "name"?: string, "mode"?: "code" | "design" }
200  { "message": string, "chat_session": ChatSession }
```

#### `POST /api/chat-sessions/switch`

Make a chat active and return its transcript.

```
body: { "id": string, "mode"?: "code" | "design" }
200 {
  "message": string,
  "active_chat_id": string,
  "chat_session": ChatSession & {
    "messages": Array<{ "role": string, "content": string,
                        "reasoning_content"?: string, "timestamp"?: string }>,
    "run_events"?: WsEvent[]
  }
}
404 when the chat is unknown
```

#### `GET /api/chat-sessions/messages?chat_id=<id>`

Read a chat's transcript **without** changing the active chat (background
panes poll this). Same response shape as `switch`.

```
200 { "message": string, "active_chat_id": string, "chat_session": { …, "messages": [...] } }
404 when the chat is unknown
```

#### `POST /api/chat-sessions/rename`

```
body: { "id": string, "name": string }
200  { "message": string, "chat_session": ChatSession }
400 on an empty name; 404 when the chat is unknown
```

#### `POST /api/chat-sessions/delete`

```
body: { "id": string, "remove_worktree"?: boolean }
200  { "message": string, "worktree_removed"?: boolean, "worktree_error"?: string }
404 when the chat is unknown
```

#### `POST /api/chat-sessions/delete-all`

Delete every chat, keeping one fresh empty chat active.

```
200 { "message": string, "deleted_count": number, "active_chat_id": string }
```

#### `POST /api/chat-sessions/turn`

Append a finished turn to a chat's transcript. Sprout calls this when a turn
starts (the question) and again when it finishes (the question and the
answer), for both agent backends — the daemon and the in-browser agent — and
for a chat answering off screen. The host owns persistence and idempotency: a
repeated call for the same question/answer pair must not duplicate the turn.

```
body: { "chat_id": string, "query": string, "response"?: string }
200  { "message": string }
404 when the chat is unknown
```

The call is best-effort — a failure is swallowed, because the turn has
already rendered in the UI. A host that does not implement it still gets the
list/create/rename/delete/switch behaviour.

On reload, the active chat's transcript is restored through `GET
/api/chat-sessions` + `POST /api/chat-sessions/switch` (the same calls the
chat unit makes at boot), so the host store is the source of truth for the
conversation across reloads.

## The agent-event stream

A daemon exposes its live agent-turn events as a Server-Sent Events stream at
`GET /api/agent/events`. It is the HTTP-shaped twin of the WebSocket bridge at
`/ws`: the same events, the same filtering, but carried over a plain HTTP
response so a host that reaches the daemon through a request/response transport
(the runner relay tunnel, `pkg/runner`) can consume it without a protocol
change. The runner's host server reverse-proxies `/daemon/{workspaceID}/...`
with streaming enabled, so the SSE bytes flow through the relay's existing
`data` frames unchanged.

```
GET /api/agent/events?chat_id=<id>&client_id=<id>&after_seq=<n>
200  Content-Type: text/event-stream
```

- `chat_id` scopes the stream to one chat; empty means the client's active
  chat. `client_id` (also accepted as the `X-Sprout-Client-ID` header) selects
  the client/window. `after_seq` replays buffered events past that sequence
  before the live stream resumes, so a reconnecting consumer does not miss the
  turn.
- Each event is one `data:` frame carrying the daemon's `UIEvent` JSON
  (`{ "id", "type", "timestamp", "data" }`). A periodic comment frame
  (`: keep-alive`) keeps an idle stream from looking dead. When `after_seq`
  is supplied, a leading control frame (`event: restored`, payload
  `{ "chat_id", "after_seq", "last_seq", "gap" }`) precedes the replayed
  events so the consumer knows where the live stream resumes and whether its
  position predates the oldest retained event (`gap`); replayed frames come
  from the run buffer, which stores only `type` and `data`, so their `id` and
  `timestamp` may be empty.
- The stream is filtered by the same per-connection policy the WebSocket path
  uses, so a subscriber scoped to chat A never receives chat B's events.
- **Authentication.** Unlike an ordinary GET, this endpoint is not readable
  unauthenticated: when the daemon has `SPROUT_AUTH_TOKEN` configured, the
  request must present `Authorization: Bearer <token>` (a constant-time
  compare) or it is answered `401`. The stream carries prompts and model
  output, so a bare unauthenticated GET must not reach it. Over the relay the
  path is also gated by the runner's per-workspace secret before the daemon is
  reached.

## The agent audit trail

The agent records a facts-only audit trail of everything it did: one event per
model call and one per tool execution. The events carry digests and metadata —
provider, model, endpoint host, request/response SHA-256 and byte counts, token
usage, outcome, and the trigger — and **never** prompt text, response text,
message content, or raw tool arguments. A host that runs the agent for its
users (a workspace daemon, `sprout runner`, the in-browser agent) can consume
the trail to answer "what did the agent send to which provider, and what did it
run?" without storing what the agent said.

The events are always written to the local JSONL audit log (the file `sprout
audit tail` reads). A host that wants them delivered also configures an audit
endpoint, and the agent POSTs them there in batches.

### Configuring the audit endpoint

The endpoint is a config field, `audit.endpoint`:

```json
{
  "audit": {
    "endpoint": "https://host.example.com/api/agent-audit",
    "batch_size": 32,
    "flush_interval_seconds": 5
  }
}
```

- `endpoint` — the URL the agent POSTs batches to. Empty means local-only.
- `batch_size` — events buffered before a batch is sent (default 32).
- `flush_interval_seconds` — how long an event may wait before its batch is
  sent (default 5).

The transport is **best-effort and never blocks a turn**: events are enqueued
on a bounded background queue (a full queue drops the oldest event), a batch is
POSTed from a single background goroutine, and a failed batch is retried a
bounded number of times with backoff before being dropped. A slow or
unreachable endpoint can never stall or fail a turn.

### The batch request

```
POST <audit.endpoint>
Content-Type: application/json

[ <event>, <event>, ... ]
```

The body is a JSON array of event objects. The host answers `2xx` when it has
accepted the batch; any other status (or a transport error) triggers a retry.
The host owns idempotency — a retried batch may repeat events, so a host that
must not double-count should key on the event's `time` plus its digests.

### Event shape

Every event is a single JSON object with a `kind` discriminator.

#### `kind: "model_call"`

One per model call the agent made, including calls that failed over or errored.

```json
{
  "time": "2026-01-02T15:04:05Z",
  "kind": "model_call",
  "chat_id": "chat-1",
  "session_id": "sess-1",
  "provider": "openai",
  "model": "gpt-5",
  "endpoint_host": "api.openai.com",
  "request_sha256": "…",
  "request_bytes": 12345,
  "response_sha256": "…",
  "response_bytes": 678,
  "prompt_tokens": 1024,
  "completion_tokens": 128,
  "outcome": "ok",
  "trigger": "user_turn",
  "streaming": false
}
```

| Field | Meaning |
|-------|---------|
| `time` | When the call completed (RFC 3339) |
| `kind` | `"model_call"` |
| `chat_id` | The chat this call belongs to (empty when none) |
| `session_id` | The agent session id (empty when none) |
| `provider` | The provider name (`openai`, `anthropic`, `sprout-local`, …) |
| `model` | The model id sent to the provider |
| `endpoint_host` | The host (host:port) of the provider endpoint — never a path or credentials |
| `request_sha256` | SHA-256 of the request body the agent sent (after egress redaction) |
| `request_bytes` | Byte length of that request body |
| `response_sha256` | SHA-256 of the response body (for a streamed call, the bytes read from the SSE stream — a partial-body digest when the stream errored early) |
| `response_bytes` | Byte length of that response |
| `prompt_tokens` | Prompt tokens reported by the provider |
| `completion_tokens` | Completion tokens reported by the provider |
| `outcome` | `"ok"`, `"error"`, or `"failover"` (a retry/failover path was used) |
| `trigger` | `"user_turn"`, `"tool_call_follow_up"`, or `"subagent"` |
| `streaming` | Whether the call used the streaming path |
| `failover` | Present and true when a failover path was used |

#### `kind: "tool_call"`

One per tool execution.

```json
{
  "time": "2026-01-02T15:04:06Z",
  "kind": "tool_call",
  "chat_id": "chat-1",
  "session_id": "sess-1",
  "tool": "write_file",
  "args_sha256": "…",
  "args_bytes": 210,
  "status": "ok",
  "files_touched": ["src/a.go"],
  "trigger": "user_turn"
}
```

| Field | Meaning |
|-------|---------|
| `time` | When the tool finished (RFC 3339) |
| `kind` | `"tool_call"` |
| `chat_id` / `session_id` | Same as the model-call event |
| `tool` | The tool name |
| `args_sha256` | SHA-256 of the tool's arguments (never the arguments) |
| `args_bytes` | Byte length of the serialized arguments |
| `status` | `"ok"` or `"error"` |
| `files_touched` | The file paths the call's arguments declared (deduped, sorted) |
| `trigger` | `"user_turn"` or `"subagent"` |

Because the events are facts-only, a host can retain them under a stricter
policy than the conversation transcript itself; nothing in an event needs the
redaction a prompt would.

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
