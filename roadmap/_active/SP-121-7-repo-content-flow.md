# SP-121-7: Repo Click → GitHub Content Flow

> **Status (2026-09-22):** Shipped — browser-side GitHub clone
> (isomorphic-git + lightning-fs), multi-repo gitClient service, VFS
> bridge to the WASM shell, Phase-7 agent integration
> (`agentGitTools.ts` + `agentGitToolBridge.ts`, 15 git tools).
> Remaining: Playwright E2E pass (shared open item with
> SP-GIT-CLIENT Phase 7).

## Architecture

### Data Flow

```
DashboardPage (repo card onClick)
  └─► setSelectedRepo({owner, name})
        └─► setCurrentView('repodetail')
              └─► RepoDetailPage mount (props: repoOwner, repoName)
                    └─► RepoFileTree onMount
                          └─► GitClientService.clone({owner, name, url, token})
                                └─► isomorphic-git.clone({dir, url, onAuth, ...})
                                      └─► lightning-fs.promises.writeFile(/repos/owner/name/...)
                                            └─► repoVfsBridge.cloneRepoToVfs(owner, name)
                                                  └─► shell.writeFile() [WASM VFS]
                                                        └─► Editor reads file tree (via ?repo= URL)
```

The flow has three boundaries: (1) **React state** (selectedRepo → currentView), (2) **browser-side git** (lightning-fs in IndexedDB), (3) **WASM VFS** (xterm shell served from Sprout). Files cross the second boundary only when the user explicitly opens the editor (Phase 3 bridge).

### In-Memory State Dependencies

```
┌─────────────────────────────────────────────────────────────┐
│  IndexedDB                                                  │
│  ┌──────────────────────┐   read/write (promises API)       │
│  │  lightning-fs        │◄──────────────────────────────┐   │
│  │  store: "sprout-git" │                               │   │
│  │  /repos/owner/name/  │                               │   │
│  └──────────────────────┘                               │   │
│           ▲                                             │   │
│           │ .repos[owner/name] (Promise-cached)         │   │
│  ┌────────┴───────────────┐         ┌──────────────────┐│   │
│  │  GitClientService      │◄────────│  RepoFileTree     ││   │
│  │  (singleton)          │  clone, │  (per-repo state) ││   │
│  │  clone/read/status/   │  read   │  branch, expanded ││   │
│  │  log/branch/checkout  │  tree   │  nodes, clone %   ││   │
│  └───────────────────────┘         └──────────────────┘│   │
│           ▲                                             │   │
│           │ cloneRepoToVfs, openRepoInEditor            │   │
│  ┌────────┴───────────────┐                              │   │
│  │  repoVfsBridge         │──────────────────────────────┘   │
│  │  (VFS sync layer)      │  iterates lightning-fs files,   │
│  │                        │  writes into WASM shell         │
│  └────────────────────────┘                                  │
└──────────────────────────────────────────────────────────────┘
```

**Key invariants:**
- `GitClientService` is a singleton; repo handles are cached by `owner/name` so repeat clones are O(1) lookup.
- `RepoFileTree` holds UI-only state (expanded paths, selected branch) — it does not cache file contents.
- `repoVfsBridge` is the only writer to the WASM VFS in this flow; it reads from lightning-fs, not from GitClientService directly.

### Event Sequence

1. **User clicks repo card** on `DashboardPage`
   - `onClick` handler fires: `setSelectedRepo({owner, name})` and `setCurrentView('repodetail')`.
2. **`RepoDetailPage` mounts**
   - Receives `repoOwner` and `repoName` from app state (set by `bootstrapAdapter` from URL params or selection).
   - Renders `RepoFileTree` with these props.
3. **`RepoFileTree.onMount` triggers clone**
   - Calls `gitClient.clone({owner, name, url, token})`.
   - `GitClientService` checks `this.repos[owner/name]` — if present, returns the cached promise (cache hit, no network call).
4. **isomorphic-git clone progresses**
   - `onProgress` callback emits phase + loaded/total bytes → UI updates percentage bar.
   - Objects stream into lightning-fs under `/repos/owner/name/`.
5. **Clone completes**
   - `gitClient.readTree(owner, name)` enumerates the working tree.
   - `RepoFileTree` renders the directory tree, branch selector populates from `gitClient.listBranches()`.
6. **User clicks "Open in Editor" / "Browser IDE"**
   - `repoVfsBridge.cloneRepoToVfs(owner, name)` runs:
     - Reads every file from lightning-fs `/repos/owner/name/`.
     - Calls `shell.writeFile(vfsPath, content)` for each file inside the WASM VFS.
   - On bridge completion, navigates to `?repo=<html_url>` (deep link).
   - `bootstrapAdapter` parses `?repo=` and sets `currentRepo` in app state.
7. **Editor mounts**
   - Reads `currentRepo` from state, hydrates the file tree from the WASM VFS (which was populated by the bridge).
   - User sees the file tree and can open files.

### Critical Edge Cases

| Scenario | Behavior |
|---|---|
| **Repo already cloned** | `GitClientService.repos[owner/name]` returns the cached Promise; no re-clone. `RepoFileTree` re-renders directly from existing lightning-fs state. Subsequent `cloneRepoToVfs` is a no-op if destination VFS paths already exist. |
| **Clone fails (network)** | isomorphic-git rejects the clone Promise. `RepoFileTree` shows error state with retry button. lightning-fs may contain a partial `/repos/owner/name/` directory — not auto-cleaned; next attempt will either resume or overwrite depending on `force` flag. |
| **Clone fails (auth)** | `onAuth` callback receives a 401/403 from GitHub. Currently surfaces as a generic clone error; UI should distinguish "token required" from "token rejected" for private vs. public repos. (See remaining item 6c.) |
| **Clone fails (large repo)** | No size limit enforced in `GitClientService`; relies on `onProgress` to surface partial state. Browser memory + IndexedDB quota are the only hard limits. |
| **VFS out of space (quota exceeded)** | `shell.writeFile()` throws when WASM VFS quota is exceeded. `repoVfsBridge` should surface this as a top-level error — current implementation does not batch or paginate writes. |
| **User switches repos** | `setSelectedRepo` replaces the previous owner/name; `RepoFileTree` unmounts/remounts with new props. Each repo persists independently in lightning-fs under `/repos/owner/name/`. No cross-repo state is leaked. The WASM VFS destination is not cleared automatically — opening a new repo will overwrite prior files only if the bridge runs to completion. |
| **Token missing for private repo** | `onAuth` callback returns `undefined` when no token is stored. isomorphic-git receives an empty username and the request fails as 401. The error surfaces in `RepoFileTree`; user is not redirected to a token-entry flow (gap: no token prompt UI exists yet). |

## Status: 🟡 Phases 1-2 Shipped | Phase 3-4 Ready

### Phase 1: Core GitClientService ✅
- `webui/src/services/gitClient.ts` (457 lines) — clone, status, log, branch, checkout, read, diff, add, commit, push
- Uses isomorphic-git + lightning-fs
- Multi-repo: `/repos/owner/name` directory structure
- GitHub token support via `onAuth` callback

### Phase 2: RepoFileTree + RepoDetailPage wiring ✅
- `webui/src/components/platform/RepoFileTree.tsx` (267 lines) — collapsible tree, branch selector, clone button
- `webui/src/components/platform/RepoFileTree.css` — styles
- `RepoDetailPage.tsx` — mounts RepoFileTree with `repoOwner`/`repoName` params
- `PlatformPages.css` — clone section styles

### Phase 3: Bridge to WASM VFS ✅
- `webui/src/services/repoVfsBridge.ts` — copies cloned files into WASM VFS
- `cloneRepoToVfs(owner, name)` syncs lightning-fs → VFS
- `openRepoInEditor(owner, name)` navigates editor with VFS files

### Phase 4: Deep linking via `?repo=` ✅
- `bootstrapAdapter.ts` already handles `?repo=` URL param
- Stores repo URL in app state for downstream consumers

### Phase 5: UI navigation flow ✅
- DashboardPage repo click → navigate to `repodetail` view with `selectedRepo`
- RepoDetailPage's "Browser IDE" button links to `?repo=html_url`

### Completed Items
- [x] **1a** GitClientService with clone/read/status ✅
- [x] **1b** Token management via onAuth callback ✅
- [x] **1c** Single repo per session (path-based isolation) ✅
- [x] **2a** RepoFileTree component (collapsible, branch-aware) ✅
- [x] **2b** Wire RepoFileTree into RepoDetailPage ✅
- [x] **3a** repoVfsBridge to copy cloned files into WASM VFS ✅
- [x] **4a** Deep link handling via `?repo=` URL param (already existed) ✅
- [x] **5a** DashboardPage → RepoDetailPage navigation ✅
- [x] **5b** RepoDetailPage → Browser IDE with `?repo=` ✅

### Remaining Items (Future Phases)
- [ ] **2c** File content preview in RepoFileTree (click → view file in editor)
- [ ] **3b** Auto-bridge on clone completion (clone completion → VFS sync)
- [ ] **6a** Auto-trigger clone on `?repo=` deep link (currently just stores URL)
- [ ] **6b** `push` operations (may need GitHub OAuth token)
- [ ] **6c** Private repo support (requires GitHub OAuth flow)
- [ ] **6d** Branch switch checkout + file tree refresh
- [ ] **6e** Commit history view in RepoDetailPage
- [ ] **6f** Conflict resolution UI for push failures

### Browser Test Verified
- Cloud IDE loads at http://localhost:8080/webui/
- WASM shell initialized
- Browser Workspace label visible (cloud mode)
- Git sidebar active
- API endpoints work: `/user/me/repos`, `/user/me/repos/owner/name/branches` return real GitHub data
- Chat works end-to-end through WASM (tested earlier with streaming)

### Commits
- `sprout:ee11409f` — feat(SP-121-7): browser-side GitHub repo clone, file tree, and VFS bridge
- `platform:bdbe1e5` — build: update embedded webui with SP-121-7 repo clone + file tree
