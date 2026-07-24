# SP-121-9: First-Run UX + Onboarding

**Status:** 🟡 Draft | **Priority:** High | **Effort Estimate:** ~3-5 days | **Depends on:** SP-121-7, SP-121-8

## Problem

A user who has never loaded a repo lands in an empty workspace with no narrative — no prompt to import, clone, or create. The empty `RepoDetailPage` state is a blank panel. The onboarding experience needs to guide users to their first repo quickly, and the empty states across the app need to be informative rather than silent.

## Proposed Solution

### 1. First-run onboarding screen

When no repo is loaded (no `selectedRepo` in app state, and `?repo=` URL param is absent), show an onboarding screen as the initial landing state instead of a blank `RepoDetailPage`.

The onboarding screen offers two entry points:

**Option A: Import from GitHub URL**
- A large text input: "Paste a GitHub repository URL"
- Placeholder: `https://github.com/owner/repo`
- On submit: parse the URL (`owner/repo`), extract the owner/name, trigger `gitClient.clone()` with the stored PAT (if available) or prompt for token entry
- Support both `https://github.com/` and `git@github.com:` formats
- Show clone progress inline

**Option B: Clone from PAT-authenticated list**
- If a GitHub PAT is stored, fetch the user's repos via `/user/me/repos` and show a searchable list
- Clicking a repo card triggers the clone flow (same as DashboardPage today)
- If no PAT is stored, show a "Connect GitHub" prompt instead of an empty list

**Option C: Create new repo** (see item 7b below)

The onboarding screen renders as the default state of `RepoDetailPage` when `repoOwner`/`repoName` props are absent.

### 7b. Create new repo from UI

- Add a "Create new repo" button in the onboarding screen and/or the `RepoDetailPage` toolbar.
- If a backend endpoint `POST /api/repo/create` exists: use it to create a GitHub repo (requires OAuth — see SP-121-11).
- If no backend endpoint: fall back to `git init` locally in lightning-fs (`/repos/owner/name/`), optionally set up a remote later.
- Show a dialog with: repo name, description (optional), public/private toggle, and "Initialize with README" checkbox.
- After creation, enter the repo detail view as if it had been cloned.

### 7c. "Open in file browser" affordance

- In the `RepoDetailPage` or toolbar, add a "Open in File Browser" / "Reveal in OS" button.
- **Desktop builds:** Call `repoVfsBridge.openRepoInFileBrowser(owner, name)` — this shells out to `open` (macOS), `xdg-open` (Linux), or `start` (Windows) on the lightning-fs mount point or a temp directory.
  - Note: lightning-fs does not expose a real filesystem path. The bridge should first copy the repo to a temp directory (e.g., `os.TempDir() + "/sprout-" + owner + "-" + name`), then open that path.
- **Web builds:** Show an info modal: "Files are stored in IndexedDB. Download a ZIP to access them locally." Include a "Download as ZIP" button that zips the lightning-fs `/repos/owner/name/` tree.

### Empty state for RepoDetailPage

- When `RepoDetailPage` mounts without `repoOwner`/`repoName`, render the onboarding screen (item 1 above).
- When a repo is selected but the clone is in progress, render a progress state (already handled in SP-121-7).
- When a repo is loaded but the tree is empty (just-initialized repo with no files), show a "This repo is empty. Add a file to get started." prompt with a "New file" button.

## Open Questions

- **Q1:** Should the onboarding screen be its own page/route (e.g., `/onboarding`) or a conditional render inside `RepoDetailPage`? (Default: conditional render inside `RepoDetailPage` — fewer routes, simpler nav.)

- **Q2:** Should we support importing a repo without cloning it (i.e., just track it by URL, no local files)? (Default: out of scope for now; all imports clone.)

- **Q3:** For the "Create new repo" fallback (no backend), should we offer a "Connect to GitHub to create a hosted repo" prompt that routes to the OAuth flow? (Default: yes — show the button that triggers SP-121-11 OAuth, with "Create locally only" as a secondary option.)

- **Q4:** Should the ZIP download be streamed (for large repos) or assembled in memory? (Default: streamed via a `ReadableStream` + JSZip, to avoid OOM on large repos.)

## Done Means

- [ ] First-run onboarding screen renders when no repo is loaded
- [ ] GitHub URL import parses `owner/repo` and triggers clone
- [ ] PAT-authenticated repo list renders and triggers clone on click
- [ ] "Create new repo" dialog creates a local git repo in lightning-fs
- [ ] "Open in File Browser" copies repo to temp dir and opens OS file manager on desktop
- [ ] "Download as ZIP" option available on web builds
- [ ] Empty `RepoDetailPage` shows informative onboarding, not a blank panel
- [ ] Empty freshly-created repo shows "This repo is empty" prompt
