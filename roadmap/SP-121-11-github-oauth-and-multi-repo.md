# SP-121-11: GitHub OAuth + Multi-Repo Workspace

**Status:** 🟡 Draft | **Priority:** High | **Effort Estimate:** Multi-week | **Depends on:** SP-121-7, SP-121-8, SP-121-9

## Problem

Two foundational gaps block the rest of the platform:

1. **OAuth is missing.** Users can authenticate with a manually-entered PAT, but there is no OAuth flow. This means no refresh tokens, no per-user credential storage, no scope management, and a fragile UX where the PAT is a magic string with no revocation or expiry handling.

2. **Single-repo-at-a-time.** The current architecture supports exactly one active repo per workspace. Users with polyrepos or who need to work across multiple repos simultaneously must maintain multiple browser tabs. The agent also lacks a "current repo" context, so it can't answer "which repo am I in?" reliably.

## Proposed Solution

### 6c (full): GitHub OAuth flow

**OAuth app registration:**
- Register a GitHub OAuth app (client ID + secret).
- Recommended: register in the `sprout-foundry` organization or a dedicated `sprout-github-oauth` org so it's not per-developer.
- Redirect URI: `https://app.sprout.dev/auth/github/callback` (or `http://localhost:PORT/auth/github/callback` for local dev).

**Flow:**
```
User clicks "Connect GitHub"
  └─► redirect to https://github.com/login/oauth/authorize
            ?client_id=...
            &scope=repo,read:user
            &redirect_uri=...
            &state=<CSRF token>
  └─► GitHub redirects back with ?code=...
  └─► Backend exchanges code for access_token + refresh_token
  └─► Store tokens per-user (encrypted at rest)
  └─► Set "GitHub connected" state in app
```

**Scope management:**
- Request `repo` (full repo access) — needed for push, create, private repo operations.
- Request `read:user` — needed to fetch the user's profile and avatar.
- Future: `delete_repo`, `admin:org` if those features are added.

**Token refresh:**
- Access tokens expire in 8 hours. Use the refresh token to obtain a new access token proactively (refresh before expiry) or on-demand (when a 401 is received).
- Store refresh token securely; rotate on each refresh.

**PAT as fallback:**
- Users who prefer PAT-only auth can still enter a PAT in settings. PAT and OAuth should coexist — if a PAT is present, use it in preference to the OAuth token for git operations (PAT is more reliable for git until GitHub's OAuth token integration with git is stable).
- Clearly indicate which auth method is active in the UI.

**Backend changes needed:**
- New endpoint: `GET /auth/github` — initiates OAuth redirect
- New endpoint: `GET /auth/github/callback` — exchanges code for tokens, stores per-user
- New endpoint: `POST /auth/refresh` — refreshes access token
- New endpoint: `DELETE /auth/github` — revokes tokens, disconnects GitHub
- Token storage: encrypted in the user's credential store (same backend used for provider API keys)

**Frontend changes needed:**
- "Connect GitHub" button in Settings → Integrations page
- OAuth status indicator (connected/disconnected, which account)
- "Disconnect" action
- Wire OAuth token into `gitClient` auth callbacks in place of PAT when OAuth is active

### 8. Multi-repo workspace

**Concept:**
A workspace can hold N repos simultaneously. Each repo is independently cloned, managed, and bridged. The user works in one "active" repo at a time (shown in the sidebar/tab bar), but all attached repos are accessible.

**Architecture:**

```
┌──────────────────────────────────────────────────────────────┐
│  WorkspaceState                                              │
│  {                                                          │
│    repos: [                                                 │
│      { owner, name, url, localDir, vfsRoot, isActive },     │
│      ...                                                    │
│    ]                                                        │
│  }                                                          │
└──────────────────────────────────────────────────────────────┘
     │
     ├─► RepoTabBar (top of RepoDetailPage)
     │      [repo-A] [repo-B] [repo-C] [+]
     │
     ├─► RepoFileTree (per active repo)
     │
     └─► AgentContext: { currentRepo: owner/name }
              Agent tools append `cwd` / `repo` context to calls
              so the agent knows which repo it's operating in.
```

**VFS path structure per repo:**
- Repo A: `/workspace/repo-A/` (or `/workspace/owner-name-A/`)
- Repo B: `/workspace/repo-B/`
- Workspace-level files (if any): `/workspace/.sprout/`

**Repo tab bar:**
- Shows one tab per attached repo.
- Active tab is highlighted.
- Clicking a tab sets that repo as active: `setCurrentRepo(owner/name)`, which updates agent context and switches the file tree + VFS bridge.
- "+" button opens the onboarding screen to attach a new repo.

**Sidebar changes:**
- The sidebar shows all attached repos with their status (clean/dirty/cloning).
- Collapsing a repo minimizes its tab (does not detach it).
- "Detach repo" removes it from the workspace (does not delete local files).

**Agent context:**
- Add `currentRepo: string | null` to the agent's session context.
- When the agent calls file-read, file-write, or shell tools, append the active repo's VFS root as the effective working directory if the user hasn't explicitly set a path.
- If the user says "edit file X" and X is ambiguous across repos, the agent should ask: "File X exists in both repo-A and repo-B. Which would you like to edit?" (See Q3 below.)

**Persistence:**
- Workspace state (list of attached repos, active repo, per-repo git state) is stored in `localStorage` or `IndexedDB`.
- On page reload, restore the workspace — re-attach all repos (checking for local lightning-fs copies before re-cloning).

## Architectural Decision Points

These must be resolved before or during implementation:

- **Q1:** Should the GitHub OAuth app live in the `sprout-foundry` platform repo or here? (Default: platform repo — auth is a platform concern and should be centralized, not duplicated per feature. The OAuth app credentials should be injected via environment/config into the webui at build time.)

- **Q2:** Should multi-repo workspaces be per-user persistent (saved in the user's profile) or per-session? (Default: per-session in browser storage, with a "Save workspace" action that exports the workspace as a portable config. Per-user persistence adds complexity and edge cases around stale clones. Revisit if there's demand for shared workspaces.)

- **Q3:** How does the agent handle "I'm in repo A, user says edit file X" — does it ask which repo, or use the active one? (Default: use the active one. If the file doesn't exist in the active repo, surface a "File not found in active repo. Try another repo?" prompt. Provide a `/repo repo-owner/repo-name` slash command to switch context mid-conversation.)

- **Q4:** Should we limit the number of repos in a workspace? (Default: no hard limit for now; IndexedDB quota and browser memory are the practical limits. Add a warning at 5+ repos.)

- **Q5:** How does the VFS handle namespace collisions (e.g., two repos with the same `package.json` at the VFS root)? (Default: repos are namespaced by `owner-name` prefix in VFS paths — `/workspace/owner-repo/` vs `/workspace/other-repo/`.)

- **Q6:** When a new repo is attached, should we auto-bridge it to VFS immediately or lazily (on first access)? (Default: lazily — clone to lightning-fs, bridge to VFS only when the user opens the repo tab or navigates to it.)

## Open Questions

- **Q7:** Should we support OAuth device flow (for CLI, where redirect is not available)? GitHub supports `github.com/login/device/code`. (Default: yes for the CLI, no for the webui. This is a CLI-only concern.)

- **Q8:** What happens if the OAuth token is revoked from the GitHub web UI? The refresh will fail silently. Should we detect this and prompt re-authentication? (Default: on 401 from git operations, check if the token is still valid via `/user` endpoint; if not, show a "GitHub access expired — reconnect" banner.)

- **Q9:** For multi-repo, should the agent be able to operate on multiple repos in a single turn (e.g., "copy the auth handler from repo A to repo B")? (Default: out of scope for MVP. Each turn operates in one active repo.)

## Done Means

### OAuth
- [ ] GitHub OAuth app registered and credentials injected via config
- [ ] OAuth redirect flow works end-to-end (connect → authorize → callback → stored tokens)
- [ ] Token refresh works (proactive refresh before expiry)
- [ ] OAuth token wired into `gitClient` auth callbacks
- [ ] PAT remains functional as a fallback; UI clearly shows active auth method
- [ ] "Disconnect GitHub" revokes tokens and clears stored credentials
- [ ] GitHub connected/disconnected status visible in Settings

### Multi-repo
- [ ] Workspace holds multiple repos simultaneously in lightning-fs
- [ ] Repo tab bar shows all attached repos; clicking switches active repo
- [ ] Agent context carries `currentRepo`; tools operate on the active repo's VFS root
- [ ] Workspace state persists across page reloads
- [ ] "+" button attaches a new repo via the onboarding screen
- [ ] "Detach repo" removes a repo from the workspace without deleting local files
- [ ] Sidebar shows all repos with status indicators
- [ ] `/repo owner/name` slash command switches agent context to a different repo
- [ ] Cross-repo ambiguity ("file X in multiple repos") surfaces a disambiguation prompt
