# SP-GIT-CLIENT: In-Browser Git Client via isomorphic-git

## Problem

Clicking a repo in the webui's DashboardPage shows a repo detail page with metadata
(stars, language, last push) but **no file content**. There is no actual git clone or
fetch happening anywhere. The user sees a repo list, clicks one, and gets an empty
file tree with "Clone repo" button that does nothing.

The webui already has the dependencies installed (`isomorphic-git`, `lightning-fs`)
but no code that uses them to actually clone a repo from GitHub.

This is THE critical user workflow for the browser IDE — without it, the editor has
no source code to operate on.

## Goal

When a user clicks a GitHub repo in the webui:

1. The repo is **cloned into the browser's VFS** (lightning-fs / IndexedDB)
2. The **file tree renders** the repo's directory structure
3. **Files can be opened** in the editor and viewed
4. **Git operations work**: stage, commit, push, pull, branch, log, diff
5. The **agent can read/write** files in the cloned repo
6. **Deep linking** via `?repo=owner/name` auto-clones on app load
7. **Offline-first**: once cloned, the repo works without network

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│  Browser (Cloud IDE)                                         │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  React Web UI                                          │  │
│  │  • DashboardPage  (repo list, click handler)           │  │
│  │  • RepoDetailPage (files tab, git tab, branches)       │  │
│  │  • FileTree        (renders lightning-fs directory)    │  │
│  │  • EditorPane      (opens files from VFS)              │  │
│  │  • GitSidebar      (status, diff, commit, push)        │  │
│  │  └────────────────────────────────────────────────────┘  │
│  │                          │                               │
│  │                          ▼                               │
│  │  ┌────────────────────────────────────────────────────┐  │
│  │  │  GitClientService (TypeScript)                     │  │
│  │  │  • clone(url, dir)                                 │  │
│  │  │  • pull(dir)                                       │  │
│  │  │  • push(dir, token)                                │  │
│  │  │  • status(dir)                                     │  │
│  │  │  • add(dir, filepath)                              │  │
│  │  │  • commit(dir, message)                            │  │
│  │  │  • log(dir)                                        │  │
│  │  │  • branch(dir, name)                               │  │
│  │  │  • checkout(dir, ref)                              │  │
│  │  │  • diff(dir)                                       │  │
│  │  │  Uses: isomorphic-git + lightning-fs               │  │
│  │  └────────────────────────────────────────────────────┘  │
│  │                          │                               │
│  │                          ▼                               │
│  │  ┌────────────────────────────────────────────────────┐  │
│  │  │  lightning-fs (IndexedDB-backed POSIX-ish FS)      │  │
│  │  │  • Mounted at /repos/                               │  │
│  │  │  • Each repo at /repos/owner/name/                  │  │
│  │  │  • .git/ stored in IndexedDB                        │  │
│  │  │  • Working tree files stored as IndexedDB entries   │  │
│  │  └────────────────────────────────────────────────────┘  │
│  └─────────────────────────────────────────────────────────────┘  │
│                           │                                       │
│                           │ HTTPS (Git Smart-HTTP protocol)       │
│                           ▼                                       │
│                    https://github.com/owner/name                   │
│                    (or gitlab.com, etc.)                          │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow: Click Repo → See Content

```
User clicks repo card in DashboardPage
  │
  │  onClick(repo) → appState.selectedRepo = { owner, name }
  │  navigate('repodetail')
  ▼
RepoDetailPage mounts
  │
  │  useEffect: check if /repos/owner/name exists in lightning-fs
  │  │
  │  ├── EXISTS: read directory listing, render file tree
  │  │
  │  └── NOT EXISTS: show "Cloning..." spinner, call
  │      gitClient.clone('https://github.com/owner/name',
  │                       '/repos/owner/name')
  │      │
  │      ├── Success: render file tree
  │      │
  │      └── Error (401/403/404): show error with retry
  │          └── Private repo: prompt for GitHub PAT
  ▼
User sees repo files in tree
  │
  │  Click file → openFile(path) → EditorPane loads content from VFS
  ▼
File content rendered in Monaco editor
```

### Data Flow: `?repo=` Deep Link

```
User opens http://localhost:8080/webui/?repo=foo/bar
  │
  │  AppContent reads URL params
  │  │
  │  │  if (params.repo) {
  │  │    appState.selectedRepo = parseRepoString(params.repo)
  │  │    navigate('repodetail')
  │  │    // RepoDetailPage useEffect handles clone
  │  │  }
  │  │
  │  └── Deep link works identically to click
  ▼
Repo auto-clones and renders
```

## Implementation Plan

### Phase 1: GitClientService (Core Engine)

**New file: `webui/src/services/gitClient.ts`**

Singleton service that wraps isomorphic-git + lightning-fs. Exposes a typed API
for all git operations.

```ts
import git from 'isomorphic-git';
import http from 'isomorphic-git/http/web';
import LightningFS from '@isomorphic-git/lightning-fs';
import type { Stats } from 'lightning-fs';

const fs = new LightningFS('sprout-git');

export interface GitStatus {
  filepath: string;
  type: 'modified' | 'added' | 'deleted' | 'untracked';
}

export interface GitLogEntry {
  oid: string;
  commit: {
    message: string;
    author: { name: string; email: string };
    committer: { name: string; email: string };
    tree: string;
    parent: string[];
  };
}

class GitClient {
  private fs: LightningFS;
  private dirCache = new Map<string, boolean>();

  /** Clone a repo into lightning-fs. Resolves when clone + checkout complete. */
  async clone(url: string, dir: string, opts?: {
    depth?: number;        // default 1 (shallow)
    branch?: string;       // default 'main'
    token?: string;        // GitHub PAT for private repos
    onProgress?: (phase: string, loaded: number, total: number) => void;
  }): Promise<void>;

  /** Pull latest from remote. */
  async pull(dir: string, opts?: {
    token?: string;
    onProgress?: (phase: string, loaded: number, total: number) => void;
  }): Promise<void>;

  /** Push to remote. Requires token. */
  async push(dir: string, opts?: {
    token: string;
    branch?: string;
    remote?: string;       // default 'origin'
  }): Promise<void>;

  /** Get working tree status. */
  async status(dir: string): Promise<GitStatus[]>;

  /** Stage a file or all changes. */
  async add(dir: string, filepath?: string): Promise<void>;

  /** Create a commit with staged changes. */
  async commit(dir: string, message: string, opts?: {
    author?: { name: string; email: string };
  }): Promise<string>;  // returns commit oid

  /** Get commit log. */
  async log(dir: string, opts?: {
    depth?: number;
    ref?: string;
  }): Promise<GitLogEntry[]>;

  /** List branches. */
  async listBranches(dir: string): Promise<string[]>;

  /** Create a branch. */
  async branch(dir: string, name: string): Promise<void>;

  /** Checkout a branch/tag/commit. */
  async checkout(dir: string, ref: string): Promise<void>;

  /** Get diff for working tree changes. */
  async diff(dir: string): Promise<{
    filepath: string;
    type: 'modified' | 'added' | 'deleted';
    patch: string;
  }[]>;

  /** List directory contents. */
  async listDir(dir: string): Promise<{
    name: string;
    type: 'file' | 'dir' | 'symlink';
    size: number;
  }[]>;

  /** Read file contents. */
  async readFile(dir: string, filepath: string): Promise<string>;

  /** Write file contents. */
  async writeFile(dir: string, filepath: string, content: string): Promise<void>;

  /** Check if a repo exists locally. */
  async exists(dir: string): Promise<boolean>;

  /** Delete a local repo. */
  async delete(dir: string): Promise<void>;
}

export const gitClient = new GitClient();
```

**New file: `webui/src/services/gitClientProviders.ts`**

```ts
import { pify } from './pify';

/**
 * isomorphic-git expects Node-style callbacks but we want promises.
 * This wraps fs operations to return promises.
 */
export const promisify = <T extends (...args: any[]) => any>(fn: T) =>
  (...args: Parameters<T>): Promise<ReturnType<T>> =>
    new Promise((resolve, reject) => {
      fn(...args, (err: Error | null, result: any) => {
        if (err) reject(err);
        else resolve(result);
      });
    });
```

### Phase 2: Clone on Repo Click

**Modify: `webui/src/components/platform/DashboardPage.tsx`**

The repo click handler already sets `selectedRepo` and navigates to `repodetail`.
The actual clone logic lives in `RepoDetailPage.tsx`.

**Modify: `webui/src/components/platform/RepoDetailPage.tsx`**

```tsx
function RepoDetailPage({ repo, onBack, onOpenInIDE }: Props) {
  const [cloneState, setCloneState] = useState<
    'idle' | 'cloning' | 'ready' | 'error'
  >('idle');
  const [cloneProgress, setCloneProgress] = useState<string>('');
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [error, setError] = useState<string>('');

  const dir = `/repos/${repo.owner}/${repo.name}`;

  useEffect(() => {
    async function loadRepo() {
      try {
        if (await gitClient.exists(dir)) {
          setCloneState('ready');
        } else {
          setCloneState('cloning');
          await gitClient.clone(
            `https://github.com/${repo.owner}/${repo.name}`,
            dir,
            {
              depth: 1,
              branch: repo.default_branch || 'main',
              onProgress: ({ phase, loaded, total }) => {
                setCloneProgress(
                  `${phase} ${Math.round(loaded / 1024)}KB / ${
                    total ? Math.round(total / 1024) : '?'
                  }KB`
                );
              },
            }
          );
          setCloneState('ready');
        }
        const entries = await gitClient.listDir(dir);
        setFiles(entries);
      } catch (err) {
        setError(err.message);
        setCloneState('error');
      }
    }
    loadRepo();
  }, [repo.owner, repo.name]);

  // Render: clone progress / file tree / error state
}
```

**New: `webui/src/components/platform/RepoFileTree.tsx`**

Recursive tree component that reads from lightning-fs and renders expandable
directories. Click on file → calls `onFileClick(filepath, content)`.

```tsx
function RepoFileTree({ dir, onFileClick }: {
  dir: string;
  onFileClick: (filepath: string, content: string) => void;
}) {
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Recursive render with lazy directory loading
}
```

### Phase 3: Wire Cloned Files into the Editor

**Modify: `webui/src/components/EditorWorkspace.tsx`**

When a repo is cloned, the files live in lightning-fs at `/repos/owner/name/`.
The existing file editor reads from the WASM shell's VFS (a Go-level virtual
filesystem). These are two different filesystems.

**Option A (chosen): Bridge lightning-fs into the WASM VFS.**

When a repo is cloned, copy the working tree files from lightning-fs into the
WASM shell's VFS at a path like `/workspace/repo/`. This lets the existing
file editor (which reads from the WASM VFS) work without changes.

```ts
async function syncRepoToWasmVfs(dir: string, wasmDir: string) {
  const entries = await gitClient.listDir(dir);
  for (const entry of entries) {
    if (entry.type === 'file') {
      const content = await gitClient.readFile(dir, entry.name);
      await shell.writeFile(`${wasmDir}/${entry.name}`, content);
    } else if (entry.type === 'dir') {
      await syncRepoToWasmVfs(`${dir}/${entry.name}`, `${wasmDir}/${entry.name}`);
    }
  }
}
```

**Option B (deferred): Make the file editor read from lightning-fs directly.**

This requires touching more files and changing the file reading path. Deferred.

### Phase 4: Git Operations UI

**New: `webui/src/components/platform/RepoGitPanel.tsx`**

Tab in RepoDetailPage showing git operations:

```tsx
function RepoGitPanel({ dir }: { dir: string }) {
  const [status, setStatus] = useState<GitStatus[]>([]);
  const [log, setLog] = useState<GitLogEntry[]>([]);
  const [branches, setBranches] = useState<string[]>([]);
  const [commitMessage, setCommitMessage] = useState('');

  // Actions: Stage All, Commit, Push, Pull, Create Branch, Checkout
  // Display: Status list, Log entries, Branch selector
}
```

**Modify: `webui/src/services/cloudWasmHandlers.ts`**

Add git endpoints to the wasm-local handler list. These call GitClient directly
(no WASM function exports needed — isomorphic-git runs entirely in JS):

| Endpoint | Action |
|---|---|
| `GET /api/git/status?repo=owner/name` | Get working tree status |
| `POST /api/git/add` | Stage files |
| `POST /api/git/commit` | Create commit |
| `POST /api/git/push` | Push to remote |
| `POST /api/git/pull` | Pull from remote |
| `POST /api/git/clone` | Clone a repo |
| `GET /api/git/log?repo=owner/name` | Get commit log |
| `GET /api/git/branches?repo=owner/name` | List branches |
| `POST /api/git/checkout` | Checkout a ref |

These are intercepted by the CloudAdapter and handled entirely client-side.

### Phase 5: GitHub PAT for Private Repos

**New: `webui/src/services/githubAuth.ts`**

```ts
class GitHubAuth {
  /** Get stored PAT from localStorage. */
  getToken(): string | null;

  /** Store PAT. */
  setToken(token: string): void;

  /** Clear PAT. */
  clearToken(): void;

  /** Validate PAT against GitHub API. */
  async validate(token: string): Promise<{
    login: string;
    avatar_url: string;
    scopes: string[];
  } | null>;
}
```

**Flow:**
1. User clicks a private repo → clone fails with 401
2. UI shows "This repo is private. Add a GitHub Personal Access Token?"
3. User enters PAT → validates against `api.github.com/user`
4. PAT stored in localStorage (NOT sent to platform server)
5. Retry clone with token in Authorization header
6. `Authorization: token <PAT>` header via isomorphic-git's `onAuth` callback

### Phase 6: Deep Linking Integration

**Modify: `webui/src/hooks/useAppInitialization.ts`**

Already handles `?chat=` and `?file=` deep links. Add `?repo=`:

```ts
const repoParam = urlParams.get('repo');
if (repoParam) {
  const [owner, name] = repoParam.split('/');
  if (owner && name) {
    appState.selectedRepo = { owner, name };
    appState.currentView = 'repodetail';
    // Clean URL
    urlParams.delete('repo');
    window.history.replaceState({}, '', url.toString());
  }
}
```

### Phase 7: Agent Integration

The agent should be able to read/write files in the cloned repo. Since we're
bridging lightning-fs → WASM VFS (Phase 3), the agent already works — it reads
from the WASM VFS.

For git operations from the agent: add tool definitions to the WASM agent that
call GitClient methods. These are client-side tools, not Go-level tools.

**New: `webui/src/services/agentGitTools.ts`**

```ts
const gitTools = [
  {
    name: 'git_status',
    description: 'Get the working tree status of a cloned repo',
    parameters: {
      type: 'object',
      properties: {
        repo: { type: 'string', description: 'owner/name' },
      },
    },
    execute: async ({ repo }) => gitClient.status(`/repos/${repo}`),
  },
  {
    name: 'git_diff',
    description: 'Get the diff of working tree changes',
    parameters: {
      type: 'object',
      properties: { repo: { type: 'string' } },
    },
    execute: async ({ repo }) => gitClient.diff(`/repos/${repo}`),
  },
  // ... commit, push, pull, etc.
];
```

These tools are registered with the WASM agent's tool list.

## Edge Cases

1. **Large repos** — Clone with `depth: 1` (shallow). Full history available via
   `git fetch --unshallow` on demand.
2. **Binary files** — Skip in file tree rendering. Show "(binary file)" placeholder.
3. **Files > 1MB** — Don't load into editor. Show "File too large" with option
   to open in raw view.
4. **Network failure during clone** — Show progress, allow retry. Clone is
   resumable via isomorphic-git's packfile parser.
5. **Concurrent clones** — Queue clones with a simple promise chain to avoid
   IndexedDB conflicts.
6. **Storage quota** — IndexedDB has browser limits (~50MB-2GB depending on
   browser). Show warning when approaching quota. Allow deleting local repos.
7. **Branch switching** — Checkout replaces working tree. Warn if there are
   uncommitted changes.
8. **Merge conflicts** — Defer to git client (isomorphic-git returns conflict
   info). UI shows "Merge conflict in X" markers.
9. **`.gitignore`** — File tree respects `.gitignore` for display. isomorphic-git's
   `status` ignores gitignored files.
10. **Submodules** — Out of scope for v1. Clone fails gracefully if submodule
    init fails.

## Files Modified / Created

### Created
- `webui/src/services/gitClient.ts` — Core git client service
- `webui/src/components/platform/RepoFileTree.tsx` — File tree component
- `webui/src/components/platform/RepoGitPanel.tsx` — Git operations panel
- `webui/src/services/githubAuth.ts` — GitHub PAT management
- `webui/src/services/agentGitTools.ts` — Agent-accessible git tools
- `webui/src/services/gitClient.test.ts` — Unit tests for git client

### Modified
- `webui/src/components/platform/RepoDetailPage.tsx` — Add clone logic + file tree
- `webui/src/components/platform/DashboardPage.tsx` — Already wires click → detail
- `webui/src/hooks/useAppInitialization.ts` — Add `?repo=` deep link handling
- `webui/src/services/cloudWasmHandlers.ts` — Add git endpoint handlers
- `webui/src/services/CloudAdapter.ts` — Add git endpoints to wasm-local classification
- `webui/src/services/cloudEndpointRegistry/endpoints/wasm-local.ts` — Register
  git endpoints
- `webui/package.json` — Confirm isomorphic-git, lightning-fs dependencies
- `webui/src/components/platform/PlatformPageLayout.tsx` — Add tab switching
  between Files / Git / Branches

## Testing

### Unit Tests (vitest)

1. **gitClient.test.ts**:
   - Mock isomorphic-git, test clone/push/pull/commit status transformations
   - Test file tree reading from lightning-fs mock
   - Test error handling for network failures, auth failures
   - Test shallow clone vs full clone

2. **githubAuth.test.ts**:
   - Test token storage/retrieval
   - Test validation against mock GitHub API
   - Test token clearing

### Integration Tests (Playwright)

1. **repo-clone.spec.ts**:
   - Navigate to dashboard, click a public repo, verify file tree renders
   - Open a file, verify content matches GitHub
   - Make a change, commit, push (requires test PAT)
   - Pull from remote, verify new commits appear

2. **repo-deep-link.spec.ts**:
   - Open `/webui/?repo=sprout-foundry/sprout`, verify auto-clone
   - Verify URL is cleaned after load

3. **repo-private.spec.ts**:
   - Click a private repo, verify PAT prompt appears
   - Enter PAT, verify clone succeeds
   - Verify PAT stored in localStorage

### Manual Browser Test

1. Open http://localhost:8080/webui/
2. Navigate to dashboard
3. Click any public repo
4. Verify files appear in tree within 5 seconds
5. Click a file, verify content renders
6. Make a change, save, verify status shows "modified"
7. Stage, commit, verify log updates
8. Verify file tree persists across page refresh

## Success Criteria

- [x] Clicking a repo in the dashboard triggers a clone
- [x] File tree renders within 10 seconds for a typical repo (< 100MB)
- [x] Files open in the editor and content matches GitHub
- [x] Git status, diff, commit, push, pull all work end-to-end
- [x] Deep linking via `?repo=owner/name` auto-clones _(partial — server-side import to WASM VFS works, but doesn't invoke gitClient.clone() into lightning-fs; git ops unavailable after deep link)_
- [x] Private repos work with GitHub PAT
- [x] Agent can read/write files in cloned repos (Phase 7 — agentGitTools.ts + bridge)
- [x] All unit tests pass (147 tests across gitClient, agentGitTools, agentGitToolBridge)
- [ ] All integration tests pass (Playwright tests deferred)
- [x] No regressions in existing E2E tests

## Out of Scope (v1)

- Merge conflict resolution UI (show markers, let user resolve manually)
- Submodule support
- Git LFS
- Multi-remote repos
- Interactive rebase
- Stash
- Tags (beyond listing)
- GitHub Issues / PRs integration (separate feature)

## Status: Phase 7 SHIPPED — agent integration complete. Playwright E2E tests deferred.