# ADR-0008: WebUI native seams (Track R selective replacement)

## Status
Accepted (2026-08-29).

## Context
The webui is the default product interface (Track P). Native implementations
are reaching implementation parity with portions of it (file system, terminal,
chat loop, git). We need a *mechanism* — not a side-by-side dual UI — for the
shell to take over a portion of the webui once it reaches **full
implementation parity**, so the product only ever improves and can roll back
instantly.

The seam inventory and parity-gated swap protocol are defined in
`sprout-studio/roadmap/Track-R-selective-replacement.md`. This ADR records the
concrete contract that contract depends on: the build-flag set, the
capability-manifest format, and the runtime handshake. The full code-grounded
rationale lives in [WEBUI_DECOUPLING_AUDIT.md](./WEBUI_DECOUPLING_AUDIT.md).

Two seam layers, used together:
- **Build-time (hard swap):** `scripts/build-webui-dist.mjs` feature flags
  physically exclude a portion's webui modules from the bundle and emit a
  capability manifest.
- **Runtime handshake (soft detect):** the bridge bootstrap carries a
  capabilities map; the webui feature-detects and defers to native when the
  shell declares a capability. Missing capability = the webui implementation,
  exactly as today.

## Decision

### Flag set

Implemented flags on `scripts/build-webui-dist.mjs`
(`parseArgs` / `validateArgs`):

| Flag | Status | Portion | Effect |
|---|---|---|---|
| `--native-fs` | **Implemented** | `fs` | Sets `VITE_SPROUT_NATIVE_FS=1` (enables the Vite `nativeFsStubAliases`) and emits `capabilities.json` with `fs` excluded. |
| `--native-terminal` | **Implemented (R-3)** | `terminal` | Sets `VITE_SPROUT_NATIVE_TERMINAL=1` (enables the Vite `nativeTerminalStubAliases`) and emits `capabilities.json` with `terminal` excluded. |
| `--native-chat` | **Implemented (R-4)** | `chat` | Sets `VITE_SPROUT_NATIVE_CHAT=1` (enables the Vite `nativeChatStubAliases`) and emits `capabilities.json` with `chat` excluded. |
| `--native-git` | **Implemented (R-4)** | `git` | Sets `VITE_SPROUT_NATIVE_GIT=1` (enables the Vite `nativeGitStubAliases`) and emits `capabilities.json` with `git` excluded. |

Semantics:
- Any unknown `--*` token, an invalid `--mode`, or `--native-fs` +
  `--components` / `--native-terminal` + `--components` / `--native-chat` +
  `--components` / `--native-git` + `--components` together → **exit 1
  before any build step** (no `npm ci`, no Vite run).
- `--ratify-fs` requires `--native-fs`; `--ratify-terminal` requires
  `--native-terminal`; `--ratify-chat` requires
  `--native-chat`; `--ratify-git` requires
  `--native-git` (a lone ratify flag fails fast, exit 1, before any
  build step).
- A portion's flag only excludes that portion; flags are additive and each is
  gated on its own parity audit (roadmap: one portion at a time). A
  `--native-fs --native-terminal` build emits both `excluded[]` entries (fs
  first, then terminal) and enables both alias sets. A
  `--native-fs --native-terminal --native-chat` build emits all three
  `excluded[]` entries (fs, then terminal, then chat) and enables all three
  alias sets. A `--native-fs --native-terminal --native-chat --native-git`
  build emits all four `excluded[]` entries (fs, then terminal, then chat,
  then git) and enables all four alias sets.

### Manifest format (`capabilities.json`)

Emitted to the dist output dir **only when at least one `--native-*` flag
excludes a portion**; a default build emits none (absence = nothing excluded).
Core schema (full field descriptions in the audit doc §2.2):

```json
{
  "schemaVersion": 1,
  "generatedAt": "<ISO-8601 UTC>",
  "buildMode": "cloud | local",
  "excluded": [
    {
      "portion": "fs",            // fs | terminal | chat | git | …
      "flag": "--native-fs",
      "replacedBy": "native",
      "hardExclusion": true,     // build-time: module physically stubbed from the bundle
      "status": "seam-only",     // servability: seam-only = do not serve yet; "ratified" = parity-proven swap
      "notes": "<what was excluded, where it's documented>"
    }
  ]
}
```

`excluded` is an empty array (and the file is not written) for a default
build. `schemaVersion` is bumped on any shape change.

**`hardExclusion` gates *module exclusion* (build-time); `status` gates
*servability*.** A `status: "seam-only"` entry means the dist is a build-time
artifact only: the shell MUST NOT serve a dist containing a `seam-only`
exclusion (a `--native-fs` dist served today would throw on mount, since no
shell provides the shell interface natively yet). Only once the R-2 parity
gate ratifies the swap does the entry carry `status: "ratified"`, making the
dist a parity-proven, shell-servable swap.

### Handshake contract (what sprout-studio consumes)

sprout-studio **reads `capabilities.json` from the served dist** and gates
which portions it serves natively:

1. For each entry in `excluded`, the shell provides the named `portion`
   natively and the webui's hard-excluded modules are no-ops/stubs.
2. For any portion **not** in `excluded`, the webui runs its own
   implementation — identical to today's behavior. The shell serves only the
   dist whose module set matches the capabilities it actually provides.
3. The bridge bootstrap (R-1) additionally carries a runtime capabilities map
   so the same defer-to-native logic works on shells that do not ship a
   per-build manifest. The webui is the source of truth for fallback; the
   bridge is the only bridge.

### Rollback story

Rollback = **rebuild the dist with the flag omitted.** The default build
(omitting every `--native-*` flag) is unchanged from before Track R — no
aliases active, no `capabilities.json`, byte-identical module set. Flipping
the flag off restores the portion in a single rebuild; no source changes, no
migrations.

### Invariants

1. **Default build unchanged.** Omitting all `--native-*` flags produces a
   dist byte-identical in module set to the pre-Track-R default (no aliases,
   no manifest). A swap never regresses the default.
2. **The manifest reflects the actual exclusion.** Every `excluded[]` entry
   with `hardExclusion: true` must correspond to a module that is genuinely
   removed/stubbed from the bundle — never to a portion the shell *expects*
   but that is still present. `hardExclusion` is a build-time honesty
   guarantee about the module set, not a wish — and not a servability
   claim: `status: "seam-only"` entries mark dists the shell must not serve
   until the parity gate ratifies them (`status: "ratified"`).

### Deferral wiring (R-2w)

R-2w is the *manifest-driven* half of the FS swap: the webui's workspace FS
ops defer to the shell's native `files` channel when the shell proves it
provides `fs`. (The other half, the hard leaf exclusion of the four FS
modules, is the existing `--native-fs` alias seam.)

**Runtime gate.** Deferral is active IFF all four hold, in precedence order:

1. the compile-time `NATIVE_FS_ENABLED` flag is true (i.e. the dist was
   built with `--native-fs`; in the default build this short-circuits
   before ever touching `window.SproutStudio` — a dead branch, so the
   default build stays byte-identical),
2. the shell bridge is present (`window.SproutStudio` with
   `getCapabilities` / `readWorkspaceFile` / `writeWorkspaceFile` /
   `listWorkspace`),
3. the `bridge.capabilities` op's `capabilities.fs === true`, AND
4. the op's `excluded[]` contains an entry `{ portion: 'fs', status:
   'ratified' }`.

Gate-fail on any step (no bridge, `getCapabilities()` rejecting or
malformed, `seam-only`/absent manifest, shell not declaring `fs`) → the
webui keeps its existing behavior (the `--native-fs` stubs throw). The
gate is resolved once, cached for the app's lifetime, and never throws.
(Leaf module: `webui/src/services/nativeFs/`; stubs:
`webui/src/services/nativeFsStubs/fileAccess.ts`.)

**Routing surface.** When the gate passes:
- `readFileWithConsent` / `writeFileWithConsent` → `readWorkspaceFile` /
  `writeWorkspaceFile`, with the result synthesized into a standard
  `Response` (no call-site changes — consumers already use `.ok`, `.text()`,
  `.blob()`).
- file-tree open/browse (Sidebar `onFetchFiles`) → `listWorkspace(maxDepth)`.

Path normalization: webui paths are converted to workspace-relative (strip
a leading `/` or `./`; backslash → `/`); `..` segments and empty paths are
rejected client-side before the bridge.

**Error → status mapping** (bridge `{ok:false, error}` → synthesized
Response status): `notFound` → 404; `invalidParams` / `notInWorkspace` /
`isDirectory` → 400; `userCancelled` → 409; `workspaceNotSet` → 503;
`ioFailed` / unknown → 500. (Exported as a pure table in
`services/nativeFs`.)

**Build flag.** `--ratify-fs` (on `scripts/build-webui-dist.mjs`) emits the
`fs` portion of `capabilities.json` with `status: "ratified"` instead of the
default `"seam-only"`. It **requires** `--native-fs` (a lone `--ratify-fs`
fails fast, exit 1, before any build step) and inherits the
`--native-fs` + `--components` prohibition. The default build (no
`--native-fs`) still emits **no** `capabilities.json`.

**Known limitation.** The `files` channel has no create / delete / rename
ops, so `filesApi.createItem` / `deleteItem` / `renameItem` (and therefore
file/folder create, delete, and rename in the file tree) stay on the
existing path even in a `--native-fs` dist. They are a known limitation of
`--native-fs` dists, to be closed when the channel gains those ops.

**Operational caveats:**

- The gate resolution is cached for the app's lifetime; if the shell-injected
  bridge arrives after the first resolution, deferral stays off until reload.
- `listWorkspace` caps results at 5000 entries, so very wide/deep listings
  can be silently truncated (the daemon path was complete).

#### Boot sequence (R-2f)

R-2f closes the boot-path gap R-2w left: R-2w defers FS *operations*, but
boot still eagerly preloaded the WASM shell, whose artifacts a ratified
`--native-fs` dist excludes by design — so a shell-served ratified dist
booted into the "Failed to load browser runtime" error screen. When
`NATIVE_FS_ENABLED` (the compile-time `--native-fs` flag) the boot path
performs **no** wasmShell fetch/instantiate (and therefore no
ONNX/embedding chain, which hangs off the same module):

- **No boot-time preload** (`useAppInitialization`): the cloud-mode
  `preloadWasmShell()` call, its `wasmLoading`/`wasmError` state, and the
  `wasmReady`-gated git/bridge wiring are skipped entirely. The rest of the
  boot (stats, files, sessions, startup restore) proceeds unchanged.
- **Chat/API over normal HTTP** (`CloudAdapter.fetch`): wasm-local
  endpoints — plus the two dynamic decision endpoints
  (`/api/edits/{id}/decision`, `/api/shell-approvals/{id}/decision`) —
  skip the wasm-shell interception and route straight to the standard
  Foundry proxy (the server safety-net); request bodies are left
  untouched for that path.
- **Local terminal tab**: the WASM terminal input hook never inits the
  shell and surfaces `wasmProvidedByShell`, so `TerminalPane` renders a
  clear "Terminal provided by the native shell" placeholder instead of a
  loading/error line.
- **Known limitation — `?repo=` auto-import**: `CloudAdapter.importRepo`
  still calls `ensureWasmShell()` (it is a repo-write path, not a boot
  path). In a `--native-fs` dist the stub fail-fasts and bootstrap
  surfaces `sprout:repo-import-failed` — no crash, but the import is
  unavailable: repo files are provided natively by the shell. (Routing
  import through the R-2w bridge is future Track R work.)

The default build (flag off) remains byte-identical: every R-2f check is a
compile-time constant that is the first thing evaluated in its block, so
it compiles out as a dead branch and the exact pre-R-2f call sequence and
console output run. (Files: `useAppInitialization.ts`,
`cloudAdapter.ts`, `useWasmTerminalInput.ts`, `TerminalPane.tsx`.)

#### Terminal seam (R-3)

R-3 is the terminal analogue of the FS seam: the WASM/PTY terminal transport
is provided natively by the shell, so the webui's terminal module is excluded
from the bundle. Everything below is a compile-time constant that is the
first thing evaluated in its block, so the default build (flag off) is
byte-identical — every R-3 check compiles out as a dead branch.

- **Excluded module.** `services/terminalWebSocket` is aliased to a no-op
  stand-in in `webui/src/services/nativeTerminalStubs/terminalWebSocket.ts`
  (via `nativeTerminalStubAliases` in `webui/vite.config.ts`, active only
  when `VITE_SPROUT_NATIVE_TERMINAL === '1'`). The stand-in keeps the full
  public `TerminalWebSocketService` signature (instance + statics) but never
  opens a WebSocket; the `@`-alias regexes cover all three import forms
  (`@/services/terminalWebSocket`, `./terminalWebSocket`, `../services/…`)
  with optional `.js` suffixes, mirroring `nativeFsStubAliases` exactly.
- **Compile-time short-circuits.** `useTerminalSession` skips the
  WS-lifecycle effect (no `TerminalWebSocketService.createInstance()`, no WS
  connect) and exposes `terminalProvidedByShell`; `usePageVisibility` skips
  the PTY/WASM freeze/resume visibility wiring; `TerminalPane` gates the
  "Loading terminal..." line on `!terminalProvidedByShell` and renders the
  SAME "Terminal provided by the native shell" placeholder for
  `terminalProvidedByShell` that it renders for `wasmProvidedByShell` (one
  shared block when both hold).
- **Runtime gate leaf.** `webui/src/services/nativeTerminal/index.ts`
  mirrors `services/nativeFs/index.ts` (narrow structural bridge type
  `SproutStudioTerminalBridge` + detector, PURE `resolveNativeTerminalGate`
  with the nativeFs reason set, cached `nativeTerminalGate()` resolver +
  `__resetNativeTerminalGateForTests()`). It is the SHELL-side deferral
  decision (terminal sessions route to the shell); the webui placeholder UI
  is unconditional in `--native-terminal` builds. The leaf is
  resolved-but-unused until ratification (imported by nothing yet except
  tests) and inert in default builds.
- **`--ratify-terminal`** (requires `--native-terminal`; a lone ratify flag
  fails fast) emits the `terminal` entry of `capabilities.json` with
  `status: "ratified"` instead of the default `"seam-only"` (the
  ratification records the parity-proven swap).
- **Additive with fs.** A `--native-fs --native-terminal` build emits BOTH
  `excluded[]` entries (fs first, then terminal — order follows the build
  script's code shape) and enables BOTH alias sets.
- **Rollback.** Rebuild without `--native-terminal`: no aliases, no
  `terminal` manifest entry, the real terminal module returns to the bundle.
  No source changes, no migrations.

#### Chat seam (R-4)

R-4 is the chat analogue of the FS/terminal seams: the fetch/SSE agent-turn
chat transport is provided natively by the shell, so the webui's chat
transport module is excluded from the bundle. Everything below is a
compile-time constant that is the first thing evaluated in its block, so the
default build (flag off) is byte-identical — every R-4 check compiles out as
a dead branch.

- **Excluded module.** `services/api/chatApi` is aliased to a no-op stand-in
  in `webui/src/services/nativeChatStubs/chatApi.ts` (via
  `nativeChatStubAliases` in `webui/vite.config.ts`, active only when
  `VITE_SPROUT_NATIVE_CHAT === '1'`). The stand-in keeps the full public
  `chatApi` signature (`sendQuery` / `uploadImage` / `steerQuery` /
  `retractSteer` / `executeCommand` / `stopQuery` / `rewindQuery` + the
  `RetractSteerResponse` / `ExecuteCommandResponse` / `RewindResponse` types)
  but never issues a fetch; because the module lives at `services/api/` (not
  the services root), the alias regexes cover all import forms
  (`@/services/api/chatApi`, `./api/chatApi` from within `services/`, and
  `(?:../)+api/chatApi` / `(?:../)+services/api/chatApi` from
  `components/` / `hooks/`) with optional `.js` suffixes, mirroring
  `nativeTerminalStubAliases`.
- **Compile-time short-circuits.** `useCommandSubmit` no-ops the
  `/api/query` submission paths (`handleSend` / `commandRef`) — no fetch, no
  network, "chat provided by the native shell"; `useChatSessionManager`
  short-circuits `handleSendMessage` (the webui chat session loop is never
  wired); `useWebSocketEventHandler` short-circuits the chat-event streaming
  entry (`handleEvent` — `query_started` / `stream_chunk` / `query_completed`
  / tool / agent events are never processed into React state);
  `cloudWasmHandlers` short-circuits the wasm-local `/api/query`,
  `/api/query/stop`, and `/api/query/steer` cases (each returns a 501
  "Chat provided by the native shell"); `ChatView` renders the
  "Chat provided by the native shell" placeholder (mirroring the terminal
  placeholder in `TerminalPane`) instead of the local-runtime chat UI. The
  session-manager / state-machine hooks stay real — the seam is the transport
  only.
- **Runtime gate leaf.** `webui/src/services/nativeChat/index.ts`
  mirrors `services/nativeFs/index.ts` (narrow structural bridge type
  `SproutStudioChatBridge` + detector, PURE `resolveNativeChatGate` with the
  `native-chat-disabled` / `no-bridge` / `malformed-capabilities` /
  `chat-not-declared` / `chat-not-ratified` / `getCapabilities-rejected` /
  `unexpected-error` / `active` reason set, cached `nativeChatGate()`
  resolver + `__resetNativeChatGateForTests()`, type-only re-export of
  `SproutStudioCapabilities`). It is the SHELL-side deferral decision (chat
  sessions route to the shell); the webui placeholder UI is unconditional in
  `--native-chat` builds. The leaf is resolved-but-unused until ratification
  (imported by nothing yet except tests) and inert in default builds.
- **`--ratify-chat`** (requires `--native-chat`; a lone ratify flag fails
  fast) emits the `chat` entry of `capabilities.json` with
  `status: "ratified"` instead of the default `"seam-only"` (the
  ratification records the parity-proven swap).
- **Additive with fs + terminal.** A `--native-fs --native-terminal
  --native-chat` build emits all three `excluded[]` entries (fs first, then
  terminal, then chat — order follows the build script's code shape) and
  enables all three alias sets.
- **Rollback.** Rebuild without `--native-chat`: no aliases, no `chat`
  manifest entry, the real chat transport module returns to the bundle.
  No source changes, no migrations.

#### Git seam (R-4)

R-4 is the git analogue of the FS/terminal/chat seams: the git client API and
its boot wiring are provided natively by the shell, so the webui's git client
API module is excluded from the bundle. Everything below is a compile-time
constant that is the first thing evaluated in its block, so the default build
(flag off) is byte-identical — every R-4 git check compiles out as a dead
branch.

- **Excluded module.** `services/api/gitApi` is aliased to a no-op stand-in
  in `webui/src/services/nativeGitStubs/gitApi.ts` (via
  `nativeGitStubAliases` in `webui/vite.config.ts`, active only when
  `VITE_SPROUT_NATIVE_GIT === '1'`). The stand-in keeps the full public
  `gitApi` signature (`getGitStatus` / `getGitBranches` / `checkoutGitBranch`
  / `createGitBranch` / `pullGit` / `pushGit` / `stageFile` / `unstageFile` /
  `discardChanges` / `stageAll` / `unstageAll` / `createCommit` /
  `generateCommitMessage` / `getGitLog` / `getGitCommitDetail` /
  `getGitCommitFileDiff` / `checkoutGitCommit` / `revertGitCommit` /
  `getGitDiff` / `createPullRequest`) but never issues a fetch; because the
  module lives at `services/api/` (not the services root), the alias regexes
  cover all import forms (`@/services/api/gitApi`, `./api/gitApi` from within
  `services/`, and `(?:../)+api/gitApi` / `(?:../)+services/api/gitApi` from
  `components/` / `hooks/`) with optional `.js` suffixes, mirroring
  `nativeChatStubAliases`.
- **Client-vs-deeper-alias DECISION.** Only the client API surface
  (`services/api/gitApi.ts`) is aliased at this layer. `services/gitClient.ts`
  and `services/browserGit.ts` are **not** aliased here. Per the decoupling
  audit §1.4, `browserGit`'s working tree IS the WASM VFS (via the
  `configureBrowserGit` `readVfsFiles` / `writeVfsFiles` callbacks), and
  `gitClient` shares the lightning-fs IndexedDB namespace with `browserGit`.
  A deeper alias into those VFS-backed, shared modules is unsafe for a *seam*
  (it would fight the native-FS backing the working tree). The seam therefore
  lives at the client API surface + the compile-time short-circuits of the
  boot wiring, matching the audit's intent for a seam (not a full swap). This
  is documented in `webui/vite.config.ts` (the `nativeGitStubAliases` block)
  and in the `nativeGitStubs/gitApi.ts` header.
- **Compile-time short-circuits.** `useAppInitialization` skips its three git
  boot blocks — (a) `configureBrowserGit` (the browser-git VFS wiring), (b)
  `registerGitToolGlobal` + `installGitToolBridge` (the agent git tool
  bridge), and (c) `registerShellGitGlobal` (the shell git adapter) — each
  guarded on `NATIVE_GIT_ENABLED` (imported from
  `../services/nativeGitStubs/nativeGitFlag`), so none of the git boot wiring
  runs in a `--native-git` dist; `config/mode.ts` documents (as a comment
  only — `supportsGit` is a runtime, mode-based capability, not a branch)
  that browser git is not advertised as functional when the shell provides
  git natively; the git UI surface renders the "Git provided by the native
  shell" placeholder at the `Sidebar` call site (mirroring the
  `ChatView.tsx:384` "Chat provided by the native shell" placeholder) instead
  of `SidebarGitSection` / `GitSidebarPanel` / `GitHistoryPanel`. The panels'
  own logic is untouched — the seam is the surface + boot wiring only.
- **Runtime gate leaf.** `webui/src/services/nativeGit/index.ts`
  mirrors `services/nativeChat/index.ts` (narrow structural bridge type
  `SproutStudioGitBridge` + detector, PURE `resolveNativeGitGate` with the
  `native-git-disabled` / `no-bridge` / `malformed-capabilities` /
  `git-not-declared` / `git-not-ratified` / `getCapabilities-rejected` /
  `unexpected-error` / `active` reason set, cached `nativeGitGate()`
  resolver + `__resetNativeGitGateForTests()`, type-only re-export of
  `SproutStudioCapabilities`). It is the SHELL-side deferral decision (git
  operations route to the shell); the webui placeholder UI is unconditional in
  `--native-git` builds. The leaf is resolved-but-unused until ratification
  (imported by nothing yet except tests) and inert in default builds.
- **`--ratify-git`** (requires `--native-git`; a lone ratify flag fails
  fast) emits the `git` entry of `capabilities.json` with
  `status: "ratified"` instead of the default `"seam-only"` (the
  ratification records the parity-proven swap).
- **Additive with fs + terminal + chat.** A `--native-fs --native-terminal
  --native-chat --native-git` build emits all four `excluded[]` entries (fs
  first, then terminal, then chat, then git — order follows the build
  script's code shape) and enables all four alias sets.
- **Rollback.** Rebuild without `--native-git`: no aliases, no `git`
  manifest entry, the real git client API module returns to the bundle.
  No source changes, no migrations.

## Consequences
- Future portions (e.g. a future `--native-<x>`) add a flag + a `portion`
  value + a parity audit; the manifest shape is stable.
- Reviewers reject a swap that (a) changes the default build, or (b) marks a
  portion `hardExclusion` without the module actually being excluded.
- Each swap ratifies only after: full test suites green on both platforms, a
  device check, and a tested rollback (rebuild with the flag off).
- The webui remains the fallback source of truth; the shell never ships a
  module set that omits a portion it does not provide.

## References
- `sprout-studio/roadmap/Track-R-selective-replacement.md` — swap protocol & queue.
- [docs/WEBUI_DECOUPLING_AUDIT.md](./WEBUI_DECOUPLING_AUDIT.md) — subsystem
  inventory, full manifest schema, extraction order, FS residual coupling.
- [docs/DIST_BUNDLE_LAYOUT.md](./DIST_BUNDLE_LAYOUT.md) — canonical dist layout
  (`capabilities.json` is an optional file in the verified layout).
- [ADR-0007](./adr-0007-locking-strategy.md) — house ADR format.