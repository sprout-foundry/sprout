# SP-121-10: Git Power Features

**Status:** 🟡 Draft | **Priority:** Medium | **Effort Estimate:** ~1-2 weeks | **Depends on:** SP-121-7, SP-121-8

## Problem

The basic clone+read flow is complete. Power users need the Git operations they rely on daily: reviewing commit history, comparing diffs, resolving merge conflicts, and switching branches without losing state. The backend primitives exist in `gitClient`; the UI is the gap.

## Proposed Solution

### 6e. Commit history view

**Extend `gitClient.log` signature:**
```ts
log(options: { ref?: string; depth?: number; since?: Date }): Promise<GitLogEntry[]>
// Returns: [{ sha, message, author, date, parentShas, changedFiles }]
```

Expose a "History" tab or panel in `RepoDetailPage` (adjacent to the file tree). The panel shows a paginated list of commits:

```
[avatar] <author> — <relative date>
  <commit message (first line)>
  <sha> · <N files changed>

  [click → expanded view]
```

**Clicking a commit** expands it to show:
- Full commit message (subject + body)
- List of changed files (with `+N / -N` line counts)
- A "View diff" button that opens the diff viewer for this commit's parent→this diff

**Pagination:** Load 30 commits initially; show a "Load more" button at the bottom. Each page fetches the next 30 via `log({ ref, depth: 30, since: lastCommitDate })`.

### 6e.1. Diff view

Build a `<DiffViewer>` component that renders unified diff output.

**Inputs:**
- `diff(a: string, b: string, file: string): Promise<string>` from `gitClient` — returns raw unified diff
- Or `diff.commit(shaA, shaB)` which fetches the two tree SHAs and diffs them

**Rendering:**
- Parse the raw diff into hunk headers and line groups
- Side-by-side or unified toggle (default: unified for narrow viewports)
- Syntax highlighting via the existing `Prism` or `highlight.js` integration
- Line-level additions (green bg), deletions (red bg), and context (default bg)
- Hunk headers preserved: `@@ -N,M +N,M @@`
- Clicking a hunk header scrolls to that section

**Integration points:**
- Accessible from commit history (per-commit diff)
- Accessible from the main toolbar ("Compare with HEAD" / "View uncommitted changes")

### 6f. Conflict resolution UI

When `gitClient.pull()` returns a conflict (detected via isomorphic-git's conflict state), route to a conflict resolution screen.

**MVP approach (acceptable for first pass):**
- Show a modal: "Merge conflict detected in N files."
- List the conflicted files.
- For each file, show the raw conflict markers (`<<<<<<< OURS`, `=======`, `>>>>>>> THEIRS`) in a `<ConflictViewer>` component.
- Offer three actions per conflicted file:
  1. **Accept ours** — discard THEIRS changes
  2. **Accept theirs** — discard OURS changes
  3. **Abort merge** — run `git merge --abort`, discard all uncommitted work

After resolving (or aborting), refresh the tree and the VFS.

**Full three-way merge view (future):**
- BASE / OURS / THEIRS / RESULT panes
- Per-hunk selector (user picks which version for each hunk)
- Live RESULT preview as user makes selections
- Mark conflict as resolved, then `git add <file>` to mark as resolved
- Run `git commit` to complete the merge

This is explicitly out of scope for this spec — track as a follow-on item.

### 6d (full). Branch switch with full VFS re-bridge

The current branch chip click (wired in SP-121-8) calls `gitClient.checkout()` but does not clear the WASM VFS. The VFS retains files from the previous branch, creating a stale state.

**Full branch switch flow:**
1. User clicks a branch chip.
2. Show a "Switching branch..." overlay with a progress indicator.
3. Call `gitClient.checkout({ ref: branchName, force: false })`.
4. If checkout succeeds, clear the WASM VFS paths under `/workspace/` for this repo (via `shell.rmdir` or equivalent).
5. Call `repoVfsBridge.cloneRepoToVfs(owner, name)` to re-bridge the new branch's files.
6. Refresh the file tree to reflect the new branch's content.
7. Dismiss the overlay.

**Edge cases:**
- If checkout fails (uncommitted changes), show a dialog: "You have uncommitted changes. Commit, stash, or discard them before switching."
  - "Commit changes" → run `gitClient.commit()` first, then continue
  - "Stash" → `gitClient.stash()`, then checkout, then `gitClient.stashPop()` after re-bridge
  - "Discard" → `gitClient.checkout({ force: true })`
- If the new branch has no files in common with the old, the VFS clear+re-bridge is the safest path.

## Open Questions

- **Q1:** Should the commit history panel be a side-by-side view (tree left, history right) or a separate tab? (Default: side-by-side with a collapsible history pane — keeps all repo context visible.)

- **Q2:** Should diff viewer support binary files (show "binary file changed" placeholder)? (Default: yes, with a "Binary file" badge and no line-level diff.)

- **Q3:** For the conflict resolution abort, should we also offer "force pull" (discard local changes and accept remote)? (Default: yes — it's a valid escape hatch; label it clearly as destructive.)

- **Q4:** Should branch switching show a diff of what changed between branches before switching? (Default: no for MVP; add as a nice-to-have later.)

## Done Means

- [ ] Commit history panel shows paginated list with author, date, message, SHA, changed-files count
- [ ] Clicking a commit expands to show full message + changed files
- [ ] "View diff" from commit history opens `<DiffViewer>` with that commit's diff
- [ ] `<DiffViewer>` renders additions, deletions, hunk headers, and syntax highlighting
- [ ] Unified / side-by-side toggle works
- [ ] Pull with conflicts shows conflict modal listing conflicted files
- [ ] Accept ours / Accept theirs / Abort merge actions work end-to-end
- [ ] Branch switch clears and re-bridges the WASM VFS with a progress indicator
- [ ] Branch switch with uncommitted changes shows a choice dialog before proceeding
