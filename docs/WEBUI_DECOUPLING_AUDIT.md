# WebUI Decoupling Audit — Track R Selective Native Replacement

**Date:** 2026-08-29
**Status:** Delivered (Track R, R-0)
**Companion ADR:** [adr-0008-webui-native-seams.md](./adr-0008-webui-native-seams.md)
**Governing roadmap:** `sprout-studio/roadmap/Track-R-selective-replacement.md` (parity-gated, seam-first swap protocol)

This is the R-0 deliverable for the sprout repo: it maps the webui's
swappable subsystems (fs, terminal, chat, git) to concrete module
boundaries, defines the build-flag set and capability-manifest format that
`scripts/build-webui-dist.mjs` implements, states the runtime capability
handshake direction, and lays out the extraction order. It is a code-grounded
audit — every claim is tied to real files under `webui/src/`.

---

## 1. Subsystem inventory

The webui has two deployment postures that both matter here:

- **Cloud mode** (`VITE_SPROUT_MODE=cloud`): the `CloudAdapter`
  (`webui/src/services/cloudAdapter.ts`) intercepts `fetch` and routes
  endpoints either to the Foundry backend or to **WASM-local** handlers
  (`cloudWasmHandlers.ts`), where the full Go→WASM shell runs in-browser and
  provides the POSIX shell, VFS, agent loop, and browser-git.
- **Local mode**: the platform daemon serves `/api/*`; the webui talks to it
  over fetch + WebSocket.

The four swap candidates below are the portions Track R names in its seam
inventory. "Backing" is the concrete technology each portion rides on today.

### 1.1 File system / workspace ops

**Backing:** Go→WASM VFS (`sprout.wasm`), IndexedDB (`lightning-fs`,
`@isomorphic-git/lightning-fs`), OPFS (Origin Private File System), and a
consent handshake with the daemon over `/api/file`.

**Module boundaries** (exact paths):

| Layer | File | Role |
|---|---|---|
| WASM shell loader | `webui/src/services/wasmShell.ts` | Loads `sprout.wasm` + `wasm_exec.js`, exposes the Go `SproutWasm` global (VFS read/write/listDir, shell exec, agent loop, ONNX bridges, IndexedDB file store). |
| External file consent | `webui/src/services/fileAccess.ts` | `readFileWithConsent` / `writeFileWithConsent` — 403 `external_path_consent_required` handshake + `X-Sprout-Consent-Token` retry against `/api/file`. |
| Repo↔VFS sync | `webui/src/services/repoVfsBridge.ts` | Copies cloned-repo working-tree files between lightning-fs and the WASM VFS (`syncRepoToWasmVfs`, `syncWasmFileToRepo`). |
| OPFS replica | `webui/src/services/opfsReplica.ts` | Origin-Private-File-System-backed workspace replica + `.opfs-meta/index.json` sequence counters. |
| WASM-local handlers | `webui/src/services/cloudWasmHandlers.ts` | `handleWasmLocal` serves `/api/file`, `/api/file/consent`, `/api/create`, search, terminal, etc. from the shell. |
| Build-time seam | `webui/vite.config.ts` + `webui/src/services/nativeFsStubs/` | The R-1 flag seam (see §2). |

**Entry points & call sites** (real importers found by grep):

- `wasmShell.ts` ← `hooks/useWasmShell.ts`, `hooks/useWasmTerminalInput.ts`,
  `services/cloudAdapter.ts` (`preloadWasmShell` / `ensureWasmShell`), the
  standalone shells `components/standalone/editorMain.ts` and
  `terminalMain.ts`, plus `services/cloudWasmHandlers.ts` (type-only
  `import type { WasmShell }` — erased at compile time).
- `fileAccess.ts` ← `components/ImageViewer.tsx`, `MediaViewer.tsx`,
  `DiffWorkspaceTab.tsx`, `hooks/useBufferPersistence.ts`,
  `useEditorFileIO.ts`, `useAutoReloadCleanBuffers.ts`.
- `opfsReplica.ts` / `repoVfsBridge.ts` ← **zero production importers.** A
  grep across `webui/src/` (excluding tests and `nativeFsStubs/`) finds no
  importers of either module — the only references are each module's own
  definition and its unit tests. They are still hard-stubbed by the
  `--native-fs` aliases, but no production call site reaches them today.

**Coupling notes (what must be seamed first):**
1. `hooks/useWasmShell.ts` calls `initWasmShell()` directly on mount and is the
   app-level shell boot. The `CloudAdapter` also boots it lazily via
   `ensureWasmShell()` (a singleton promise cached with a failure short-circuit).
   The native swap must provide the *shell interface* (`WasmShell`) — not just
   the FS — because the same shell object is what `cloudWasmHandlers` and the
   agent loop call.
2. `repoVfsBridge.ts` hard-couples lightning-fs (git storage) to the WASM VFS
   via the `/workspace/repo/...` path convention. With the FS native there is
   no WASM VFS to sync into, so the bridge must be excluded (done). It has no
   production importers today (see the grep note above), so the exclusion is
   inert for the live call graph; when a native repo store lands, any future
   caller that triggers the sync must target the native store directly.
3. `opfsReplica` degrades to "unavailable" cleanly (its `isAvailable()`
   static), and — per the grep note above — has *zero* production importers
   today. That makes it the safest of the four to stub first: there is no
   call site to re-point, no caller branches on it being live.

### 1.2 Terminal

**Backing:** a real PTY session over a **WebSocket** to the daemon
(`/terminal`), xterm.js for rendering, and IndexedDB/localStorage for
session-id persistence + scrollback. (In cloud mode the terminal rides the
same WASM shell's shell tier instead of a PTY — see `useWasmTerminalInput.ts`.)

**Module boundaries:**

| Layer | File | Role |
|---|---|---|
| PTY transport | `webui/src/services/terminalWebSocket.ts` | `TerminalWebSocketService` (singleton + per-pane instances), ping/pong watchdog, reattach, session persistence, freeze/resume registry. |
| xterm integration | `webui/src/hooks/useTerminalXTerm.ts` | xterm.js instance lifecycle wired to the transport. |
| Session glue | `webui/src/hooks/useTerminalSession.ts` | Ties the transport to the pane. |
| Pane UI | `webui/src/components/TerminalPane.tsx` | Renders xterm; watches `isConnected`. |
| Cloud/WASM tier | `webui/src/hooks/useWasmTerminalInput.ts` | Feeds the WASM shell's `executeCommand` when no PTY is available. |

**Entry points & call sites:** `TerminalPane.tsx` ← main layout; the
`usePageVisibility` hook drives `TerminalWebSocketService.freezeAll/
resumeAll` on tab pause. `useReverseSearch.ts` and `useTerminalSearch.ts`
consume the pane buffer.

**Coupling notes:**
- The PTY session model (session id, reattach, scrollback ring buffer) is a
  server-side concept; a native swap must offer a *PTY-grade session model*,
  not just an input pump. That is exactly the parity gap the roadmap flags for
  the terminal portion.
- The local-vs-cloud split means there are **two** terminal backings (PTY WS in
  local mode, WASM shell tier in cloud). A single native terminal must
  displace both, and `useWasmTerminalInput` must be re-pointed.

### 1.3 Chat loop (agent engine)

**Backing:** the full agent loop runs in the Go→WASM shell
(`runAgent` → `ProcessQuery`), driven through `POST /api/query` in cloud mode
(`cloudWasmHandlers.ts:725`, `runAgent` at `:944`). Events stream back to the
UI as JSON via an `onEvent` callback and the WebSocket event handler.

**Module boundaries:**

| Layer | File | Role |
|---|---|---|
| Chat API surface | `webui/src/services/api/chatApi.ts` | `POST /api/query`, `/api/query/steer`, `/api/query/steer/retract` (via `useSproutFetch`). |
| Session manager | `webui/src/hooks/useChatSessionManager.ts` | The app-level chat session state machine (imported by `App.tsx:34`, `AppContent.tsx`, `ChatView.tsx`). |
| Session sync | `webui/src/hooks/useChatSessionsSync.ts`, `useChatPersistence.ts`, `useChatSessionSync.ts` | Cross-tab / persistence sync. |
| Event handling | `webui/src/services/websocket.ts`, `hooks/useWebSocketEventHandler.ts` | Streams `query_started` / tool / streaming events into React state. |
| WASM-local query | `webui/src/services/cloudWasmHandlers.ts` (cases `/api/query`, `/api/query/stop`, `/api/query/steer` at `:75-84`) | Runs `runAgent` in-browser. |
| Endpoint registry | `webui/src/services/cloudEndpointRegistry/endpoints/wasm-local.ts` | Declares `/api/query*` as wasm-local. |

**Entry points & call sites:** `App.tsx:236` installs
`useChatSessionManager`; `components/useCommandSubmit.ts` submits over
`/api/query`; `services/api/apiService.ts` + `index.ts` re-export the chat
surface.

**Coupling notes:**
- The chat loop is the *tightest* coupling: it is not one module but the
  interaction of `runAgent` (WASM) + the streaming event protocol +
  `useChatSessionManager`'s state machine. A native chat swap (R-4) needs a
  full parity audit of **tools, compaction, and streaming events** before
  replacing the webui loop — the roadmap explicitly calls this out for iOS.
- Steer/stop are wasm-local because they touch the running WASM loop;
  `/api/query/status` is the *only* query endpoint that proxies to the
  platform backend. A native chat loop must re-home steer/stop/status.

### 1.4 Git

**Backing:** in-browser **isomorphic-git** over **lightning-fs** (IndexedDB).
Two parallel implementations coexist: a per-repo `gitClient` singleton
(cloned repos at `/repos/<owner>/<name>`) and a single-repo `browserGit`
(`/repo`) that syncs the WASM VFS as its working tree.

**Module boundaries:**

| Layer | File | Role |
|---|---|---|
| Per-repo git | `webui/src/services/gitClient.ts` | `GitClient` — clone/pull/push/status/add/commit/log/branch/checkout/diff over `@isomorphic-git/lightning-fs`. |
| Single-repo git | `webui/src/services/browserGit.ts` | `configureBrowserGit` + `executeGitOp`; `syncVfsToGitFs`/`syncGitFsToVfs` bridge the WASM VFS; `BROWSER_GIT_UNSUPPORTED_OPS` lists what browser mode cannot do. |
| HTTP handler | `webui/src/services/browserGitHandler.ts` | Serves `/api/git/*` from `executeGitOp`; unsupported ops return HTTP 500. |
| WASM shell `git` | `webui/src/services/shellGitAdapter.ts` | Backs the WASM shell's `git` command (read-only subcommands) via `globalThis.__sproutShellGit`, avoiding a container txn. |
| Agent git tools | `webui/src/services/agentGitTools.ts`, `agentGitToolBridge.ts` | `AGENT_GIT_TOOLS` (git_status/git_commit/…) exposed to the WASM agent through a tool-execution hook. |
| Git API surface | `webui/src/services/api/gitApi.ts` | `getGitStatus`, `getGitBranches`, `checkoutGitBranch`, … all over `fetchFn('/api/git/...')`. |
| Endpoint registry | `webui/src/services/cloudEndpointRegistry/endpoints/foundry-backend-git.ts` | Declares `/api/git/*` as "browser-git" (in-browser, not proxied). |
| UI | `webui/src/components/git/*` (`GitCommitBox`, `GitContextMenu`, `GitPRDialog`, …), `GitSidebarPanel.tsx`, `GitHistoryPanel.tsx`, `SidebarGitSection.tsx`; hooks `useGitHandlers.ts`, `useGitWorkspace.ts`. | |

**Entry points & call sites (boot-time wiring):**
`hooks/useAppInitialization.ts:105-165` is the coupling hub — after the WASM
shell preloads it (a) `configureBrowserGit` with `readVfsFiles`/`writeVfsFiles`
that read/write *through the WASM shell's VFS* (so isomorphic-git's working
tree **is** the WASM VFS), (b) `registerGitToolGlobal` + `installGitToolBridge`
on the shell, and (c) installs `shellGitAdapter` to back the shell's `git`
command. `config/mode.ts` also references the browser-git capability.

**Coupling notes (surprising / most-tangled):**
- **git is not independent of FS.** The single-repo `browserGit` uses the WASM
  VFS as its git working tree (via `configureBrowserGit` callbacks in
  `useAppInitialization`). So a native-FS swap (R-2) *changes the git working
  tree backing* without removing git — a hard git swap (R-5) must come **after**
  R-2 or it will fight over the same VFS.
- `gitClient` re-exports/reaches `browserGit` through `lightning-fs`; both
  share the same IndexedDB namespace (`sprout-git`). A native git swap must
  displace *both* the lightning-fs store and the agent tool bridge.
- `shellGitAdapter` intentionally returns exit code **127** for unsupported
  subcommands — that 127 is the trigger for the transactional container
  escalation (ETH-2). Removing it (native git) changes the escalation path, so
  the parity audit must prove the native git answers every subcommand the agent
  actually issues.

---

## 2. Proposed flag set & manifest format (the R-1 contract)

Implemented in `scripts/build-webui-dist.mjs` (`parseArgs`, `validateArgs`,
`buildCapabilityManifest`, `writeCapabilityManifest`) and wired through
`webui/vite.config.ts`.

### 2.1 Flags

Cross-checked against the roadmap's queue (R-2/R-3/R-4/R-5). Names match the
roadmap's proposed `--native-fs` exactly; the reserved flags follow the same
`--native-<portion>` pattern.

| Flag | Status | Effect | Exit |
|---|---|---|---|
| `--native-fs` | **Implemented** (R-2) | Sets `VITE_SPROUT_NATIVE_FS=1` for the Vite build (enables the `nativeFsStubAliases` in `vite.config.ts`) and emits `capabilities.json` with the `fs` portion excluded. | 0 |
| `--native-terminal` | **Implemented (R-3)** | Sets `VITE_SPROUT_NATIVE_TERMINAL=1` for the Vite build (enables the `nativeTerminalStubAliases` in `vite.config.ts`) and emits `capabilities.json` with the `terminal` portion excluded. | 0 |
| `--native-chat` | **Reserved** (R-4) | Fails fast before any build step. | 1 |
| `--native-git` | **Reserved** (R-5) | Fails fast before any build step. | 1 |

Validation rules (all fire **before** `npm ci` / Vite run):

- Any reserved `--native-*` flag (`--native-chat` R-4, `--native-git` R-5) →
  `Error: --native-<x> is reserved for future Track R work (R-<n>) and is not
  yet implemented. See docs/...` → **exit 1**.
- Any unrecognized `--*` token → `Error: Unknown option '<x>'. Run with --help.`
  → **exit 1**.
- `--native-fs` **and** `--components` together → `Error: --native-fs cannot be
  combined with --components (standalone component entries are not the app
  bundle).` → **exit 1**.
- `--native-terminal` **and** `--components` together → `Error: --native-terminal
  cannot be combined with --components (standalone component entries are not the
  app bundle).` → **exit 1**.
- `--ratify-fs` without `--native-fs` → `Error: --ratify-fs requires --native-fs.`
  → **exit 1**. `--ratify-terminal` without `--native-terminal` → `Error:
  --ratify-terminal requires --native-terminal.` → **exit 1**.
- Invalid `--mode` (must be `cloud` | `local` | `components`) → **exit 1**.

### 2.2 `capabilities.json` — JSON Schema

Emitted **only** when at least one `--native-*` flag excludes a portion. A
default build emits **no** `capabilities.json` — absence means "full webui
dist, nothing excluded" (mirrored by the optional-file check in
`verifyDistLayout`). The file is written to the output dir with 2-space indent.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Track R capability manifest",
  "type": "object",
  "additionalProperties": false,
  "required": ["schemaVersion", "generatedAt", "buildMode", "excluded"],
  "properties": {
    "schemaVersion": {
      "const": 1,
      "description": "Manifest format version. Bump on any shape change."
    },
    "generatedAt": {
      "type": "string",
      "format": "date-time",
      "description": "ISO-8601 UTC timestamp when the dist was built."
    },
    "buildMode": {
      "type": "string",
      "enum": ["cloud", "local"],
      "description": "The --mode this dist was built with."
    },
    "excluded": {
      "type": "array",
      "description": "Portions the shell provides natively and the webui hard-excluded from this bundle.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["portion", "flag", "replacedBy", "hardExclusion", "status", "notes"],
        "properties": {
          "portion": {
            "type": "string",
            "description": "Portion key the shell feature-detects on. One of: fs, terminal, chat, git (more to be added)."
          },
          "flag": {
            "type": "string",
            "description": "The build flag that produced this exclusion (e.g. '--native-fs')."
          },
          "replacedBy": {
            "type": "string",
            "const": "native",
            "description": "Currently always 'native'; reserved for a future 'partial' or named-provider value."
          },
          "hardExclusion": {
            "type": "boolean",
            "description": "Build-time only: true = the webui module was physically removed/stubbed out of the bundle. Does NOT assert the dist is servable — see status."
          },
          "status": {
            "type": "string",
            "enum": ["seam-only", "ratified"],
            "description": "Gates servability. 'seam-only' (today's value for every --native-* build): the dist is a build-time artifact — the shell MUST NOT serve it until the portion's parity gate ratifies it. 'ratified': parity-proven, shell-servable swap (R-2 ratification for fs)."
          },
          "notes": {
            "type": "string",
            "description": "Human note on what was excluded and where it is documented."
          }
        }
      }
    }
  }
}
```

**Example — a `--native-fs` cloud build** (as emitted today by
`buildCapabilityManifest`):

```json
{
  "schemaVersion": 1,
  "generatedAt": "2026-08-29T18:48:09.000Z",
  "buildMode": "cloud",
  "excluded": [
    {
      "portion": "fs",
      "flag": "--native-fs",
      "replacedBy": "native",
      "hardExclusion": true,
      "status": "seam-only",
      "notes": "WASM FS / workspace ops provided natively by the shell; FS modules stubbed out of the bundle (see docs/WEBUI_DECOUPLING_AUDIT.md)"
    }
  ]
}
```

### 2.3 Runtime handshake direction (R-1)

The handshake is **shell-declares, webui-defers**:

1. The **shell** (sprout-studio) builds/serves a dist whose
   `capabilities.json` lists the portions it provides natively.
2. The **webui** feature-detects those capabilities and defers to the native
   implementation for any declared portion.
3. **Missing capability = webui implementation, exactly as today.** A dist with
   no `capabilities.json` (or without a given `portion`) runs 100% webui for
   that portion.

This keeps one dist valid on many shells (roadmap principle: "the bridge is the
only bridge") and makes the webui the source of truth for fallback. R-1 extends
the bridge bootstrap metadata with a capabilities map (`fs`, `editor`,
`terminal`, `chat`, `git`, …) so the same detection works at runtime, not just
via the shipped manifest.

---

## 3. Extraction order

The roadmap's per-portion **swap protocol** is:

1. **Parity audit** — enumerate the webui behaviors for the portion; map each to
   the native implementation; write the gap list. Gate: zero gaps.
2. **Seam cut (in sprout)** — extract the portion behind an interface + build
   flag; default build stays byte-identical; webui tests stay green.
3. **Capability handshake** — extend the bridge bootstrap manifest; the webui
   defers when the shell declares the capability.
4. **Native implementation** — the shell serves the portion through its
   existing bridge channel, both platforms.
5. **Swap dist** — build with the flag on; full suites + device checks;
   ratify; make it the default. Rollback: rebuild with the flag off.

Mapped onto the roadmap queue:

| Step | Subsystem | Why this order |
|---|---|---|
| **R-0** | (this audit) | Establish module boundaries, flag set, manifest format, and what must be extracted first. |
| **R-1** | Capability handshake | Extend bootstrap with the capabilities map; webui-side detection once the manifest (this doc §2) is defined. |
| **R-2** | **File system** (first swap) | FS is the most isolated (4 stubbed leaf modules, `opfsReplica` degrades cleanly, `hardExclusion=true` already lands). It also unblocks the terminal and git audits, which both ride the VFS. |
| **R-3** | Terminal | After R-2 stabilizes. Needs a PTY-grade native session model + parity audit vs `terminalWebSocket` (PTY) and `useWasmTerminalInput` (WASM tier). |
| **R-4** | Chat loop (iOS) | Tightly coupled to the WASM agent loop; requires a full tools/compaction/streaming parity audit first. Android already swapped via E-M5/D6. |
| **R-5** | Git | After R-2 (git's working tree is the VFS today, so it cannot precede the FS swap); audit after R-2. |

**Concrete ordering rationale:**

- **FS first** because it is the lowest-coupling swap (leaf modules, clean
  degradation) and it is the shared substrate — the terminal's WASM tier, the
  git working tree, and the agent's file tools all read the VFS.
- **Terminal before chat** because the terminal is a self-contained session
  model, whereas the chat loop is the agent engine and the largest parity
  surface.
- **Chat before git** is *not* required by dependencies; the constraint is
  that **git must come after FS** (R-2) because `configureBrowserGit` uses the
  VFS as its working tree. R-4 (chat) and R-5 (git) are otherwise independent
  and may be reordered once their parity audits are ready.
- **"One portion at a time"** (roadmap principle 4): each portion lands,
  stabilizes on devices, *then* the next begins. No overlapping swaps.

---

## 4. Residual coupling (FS subsystem, after `--native-fs`)

`--native-fs` **hard-excludes** exactly four modules via the
`nativeFsStubAliases` regexes in `webui/vite.config.ts`:

```
@/services/{fileAccess,repoVfsBridge,opfsReplica,wasmShell}
./{fileAccess,repoVfsBridge,opfsReplica,wasmShell}
(?:\.\.\/)+services/{fileAccess,repoVfsBridge,opfsReplica,wasmShell}
```

…to the stand-ins in `webui/src/services/nativeFsStubs/`:

| Stub | Behavior |
|---|---|
| `fileAccess.ts` | `readFileWithConsent` / `writeFileWithConsent` reject with a "provided natively" error. |
| `wasmShell.ts` | `initWasmShell` **throws** (fail-fast); `resetWasmShell` is a no-op. Types re-exported (`export type` — erased, no runtime dep). |
| `opfsReplica.ts` | `OPFSReplicaService` reports `isAvailable()=false`, empty status; safe no-ops. |
| `repoVfsBridge.ts` | `syncRepoToWasmVfs` returns an empty result with one explanatory error; `syncWasmFileToRepo` no-ops. |
| `nativeFsFlag.ts` | Compile-time constant `NATIVE_FS_ENABLED = import.meta.env.VITE_SPROUT_NATIVE_FS === '1'`. |

**Measured effect:** stubbing these four modules dropped the `wasmShell`
chunk from **22.76 kB → 0.26 kB** and eliminated the ONNX chain (the
`onnxruntime` chunk + the 26 MB `ort-wasm-simd-threaded.jsep.wasm`), taking
the total dist from **124 MB → 99 MB**. The **default build is byte-identical
in module set** (no aliases active, `excluded` empty, no `capabilities.json`).

### What is NOT excluded (the app still expects an FS to exist)

The four stubs remove the *leaf* FS modules, but the call sites still reference
the seam and the app still boots expecting a filesystem:

- `hooks/useWasmShell.ts` still calls `initWasmShell()` on mount — in a
  native-fs build it now **throws**. The shell must supply the shell interface
  through the native seam before any `useWasmShell`-driven screen renders.
- `services/cloudAdapter.ts` still lazily inits the shell (`ensureWasmShell`)
  for its wasm-local handlers; with the stub those handlers' shell-backed paths
  (file CRUD, search) are no-ops/errors until the shell provides them.
- Terminal/editor entry paths (`components/standalone/{editorMain,
  terminalMain}.ts`, `useWasmTerminalInput.ts`) still call
  `initWasmShell()` — the WASM *shell tier* (commands, autocomplete) is **not
  part of the FS swap** and must be backed natively too.

**Measured residuals that survive `--native-fs`** (documented, to be addressed
before a *hard* full-FS swap lands in R-2):

| Residual | Why it survives | Work needed before hard FS swap (R-2) |
|---|---|---|
| Terminal scrollback / session via IndexedDB + localStorage | `terminalWebSocket.ts` persists session ids in localStorage; PTY scrollback is server-side | Native terminal must own session persistence + scrollback. |
| `gitClient` / `browserGit` lightning-fs (IndexedDB) usage | Git storage is a separate IndexedDB namespace, not aliased by the FS flag | Native git (R-5) must replace the lightning-fs store; FS swap alone does not remove it. |
| `useAppInitialization` boot wiring | `configureBrowserGit` + `agentGitToolBridge` + `shellGitAdapter` all reach through `shell.writeFile` / `shell.deleteFile` (the WASM shell VFS) | Must be re-pointed at the native repo store + native git before git is hard-swapped. |
| Editor symbol utilities | `utils/symbolUtils.ts` + `extensions/languageRegistry.ts` (`resolveLanguageId`), consumed by `extensions/{codeActions,hoverTooltip,codeLens,renameOverlay,stickyScroll}.ts` and `components/{GoToSymbolOverlay,GoToWorkspaceSymbolOverlay,EditorBreadcrumb,StatusBar}.ts` | Symbol/search is a *later* audit target in the roadmap ("audit after FS swap proves the pattern"); not excluded by `--native-fs`. |
| ONNX embedding chain | Excluded only *as a consequence* of stubbing `wasmShell` (the ONNX bridges live in `wasmShell.ts` init) | If a future native shell keeps in-browser embeddings, this must be re-added intentionally. |

**Bottom line:** today's `--native-fs` is a *hard leaf exclusion* of the four
FS modules. It is **not yet** a full-FS swap: the shell must natively provide
the shell interface (VFS + shell tier + consent), re-point the git working-tree
callbacks, and own terminal persistence before R-2 can be ratified as the
default.