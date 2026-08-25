# SP-121-8: Git UI Polish

**Status:** 🟡 Draft | **Priority:** Medium | **Effort Estimate:** ~1 day | **Depends on:** SP-121-7

## Problem

The core GitHub clone flow (SP-121-7) landed the backend primitives and the basic file tree. Several UI affordances are missing wiring — buttons exist but don't call the underlying `gitClient` methods, and the README has no preview treatment. These are small, high-visibility wins.

## Proposed Solution

Wire the remaining UI affordances in `RepoFileTree` and `RepoDetailPage` that have backend primitives but no UI connection.

### Sub-items

- [ ] **3a** README preview — `RepoFileTree` shows a `README.md` entry at the tree root. On click, render it in a side panel using `react-markdown` (or an existing renderer if one is present in the webui). No edit capability — read-only preview. If no README exists, the panel shows an empty state with "No README found."

- [ ] **6b (UI)** "Push to GitHub" button — in the `RepoDetailPage` toolbar or branch selector area, add a "Push" button. Wire it to `gitClient.push()` using the stored PAT. Show a progress indicator during the push, then refresh the commit status. On auth failure, surface a clear "Token expired — re-authenticate" prompt rather than a generic error.

- [ ] **6c (UI)** "Pull from upstream" button — add a "Pull" button adjacent to Push. Wire `gitClient.pull()`. On completion, call `repoVfsBridge.cloneRepoToVfs(owner, name)` to re-bridge the updated files into the WASM VFS (clearing stale content first). Show a conflict warning if the pull returns a non-trivial conflict count.

- [ ] **6d (UI)** Branch chip click handler — the branch selector already renders branch names. Wire the chip click to `gitClient.checkout({ ref: branchName })`, then call `repoVfsBridge.cloneRepoToVfs(owner, name)` to re-sync the VFS with the new branch's content. Show a loading indicator during the checkout+bridge.

- [ ] **7a** File creation wiring — wire `RepoFileTree.onCreateFile(path, content)` to `gitClient.writeFile(path, content)` and persist into lightning-fs. Wire `onCreateFolder(path)` to `gitClient.mkdir`. Both operations should immediately re-render the tree (no full re-mount required). If the VFS is open, also write to the WASM VFS via `shell.writeFile`.

## Open Questions

- **Q1:** For the README preview, should it live in a collapsible side panel, a modal, or a tab within `RepoDetailPage`? (Default: collapsible side panel — less disruptive than a modal, more visible than a tab.)

- **Q2:** Should the Push/Pull buttons be gated behind an "is this a fork?" check (i.e., only show Push if the remote is the user's own repo or they have write access)? (Default: show the buttons, let the operation fail with a clear message if unauthorized.)

- **Q3:** Should file create use the VFS as the source of truth or lightning-fs? (Default: lightning-fs for persistence; VFS sync via bridge on demand, not on every keystroke.)

## Done Means

- [ ] README preview renders markdown when `README.md` is clicked in `RepoFileTree`
- [ ] "Push" button triggers `gitClient.push()` and shows success/error feedback
- [ ] "Pull" button triggers `gitClient.pull()` and re-bridges VFS on success
- [ ] Branch chip click triggers `gitClient.checkout()` and re-bridges VFS
- [ ] New file/folder creation from tree UI persists to lightning-fs and re-renders the tree
- [ ] No console errors in any of the above flows
