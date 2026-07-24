# SP-121-7: Repo Click → GitHub Content Flow

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
