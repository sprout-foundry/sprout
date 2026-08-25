# SP-121-11: Git Provider OAuth + Multi-Repo Workspace

**Status:** 🟡 Draft | **Priority:** High | **Effort Estimate:** Multi-week | **Depends on:** SP-121-7, SP-121-8, SP-121-9

## Problem

Two foundational gaps block the rest of the platform:

1. **OAuth is missing.** Users can authenticate with a manually-entered PAT, but there is no OAuth flow. This means no refresh tokens, no per-user credential storage, no scope management, and a fragile UX where the PAT is a magic string with no revocation or expiry handling. This applies to GitHub, GitLab, and Bitbucket — users on any major git provider need a first-class auth experience.

2. **Single-repo-at-a-time.** The current architecture supports exactly one active repo per workspace. Users with polyrepos or who need to work across multiple repos simultaneously must maintain multiple browser tabs. The agent also lacks a "current repo" context, so it can't answer "which repo am I in?" reliably.

## Provider Abstraction

All three providers (GitHub, GitLab, Bitbucket) share the same OAuth 2.0 authorization code flow pattern. The differences are captured in a provider config:

| Parameter | GitHub | GitLab | Bitbucket |
|---|---|---|---|
| **Auth URL** | `https://github.com/login/oauth/authorize` | `https://gitlab.com/oauth/authorize` | `https://bitbucket.org/site/oauth2/authorize` |
| **Token URL** | `https://github.com/login/oauth/access_token` | `https://gitlab.com/oauth/token` | `https://bitbucket.org/site/oauth2/access_token` |
| **Refresh URL** | Same as token URL | Same as token URL | Same as token URL |
| **User API** | `https://api.github.com/user` | `https://gitlab.com/api/v4/user` | `https://api.bitbucket.org/2.0/user` |
| **Repos API** | `https://api.github.com/user/repos` | `https://gitlab.com/api/v4/projects?membership=true` | `https://api.bitbucket.org/2.0/repositories?role=member` |
| **Scope (read)** | `repo,read:user` | `api,read_user,read_repository` | `repository:read,account:read` |
| **Scope (write)** | + `repo` (already included) | + `write_repository` | + `repository:write` |
| **Token expiry** | None (static, but refresh token supported) | 2 hours (refresh token) | 1 hour (refresh token) |
| **OAuth app setup** | GitHub OAuth App (org-level) | GitLab Application (group-level) | Bitbucket OAuth Consumer (workspace-level) |
| **CORS friendly** | api.github.com (yes) | gitlab.com/api/v4 (yes) | api.bitbucket.org (yes) |
| **Sign in with** | GitHub | GitLab | Bitbucket Cloud |

**Provider routing:**
The `auth/github/login` → `auth/github/callback` pattern is replicated for each provider. A shared middleware parses the provider name from the URL path and dispatches to the correct OAuth provider configuration. The frontend shows a provider picker ("Connect GitHub / GitLab / Bitbucket"), and each provider's OAuth flow is independent — the user can connect multiple providers simultaneously.

## Proposed Solution

### 6c (full): Multi-provider OAuth flow

**Shared OAuth abstraction:**
- `internal/oauth/provider.go` — defines `Provider` interface with `AuthURL()`, `Exchange(code)`, `Refresh(token)`, `Revoke(token)`, `UserInfo(token)`, `ListRepos(token)` methods
- One implementation per provider: `oauth/github.go`, `oauth/gitlab.go`, `oauth/bitbucket.go`
- Provider configs injected at startup via env vars (`GITHUB_CLIENT_ID`, `GITLAB_CLIENT_ID`, `BITBUCKET_CLIENT_ID`, etc.)

**OAuth app registration (per provider):**
- **GitHub:** Register a GitHub OAuth App in the `sprout-foundry` organization. Redirect URI: `https://app.sprout.dev/auth/github/callback`.
- **GitLab:** Register a GitLab Application under the `sprout-foundry` group. Redirect URI: `https://app.sprout.dev/auth/gitlab/callback`.
- **Bitbucket:** Register a Bitbucket OAuth Consumer under the `sprout-foundry` workspace. Redirect URI: `https://app.sprout.dev/auth/bitbucket/callback`.

**Flow (identical for all providers):**
```
User clicks "Connect <Provider>"
  └─► redirect to <Provider Auth URL>
            ?client_id=...
            &scope=...
            &redirect_uri=...
            &state=<CSRF token>
  └─► Provider redirects back with ?code=...
  └─► Backend exchanges code for access_token + refresh_token
        (OAuth2 authorization code flow, provider-specific token URL)
  └─► Store tokens per-provider per-user (encrypted at rest)
  └─► Set "<Provider> connected" state in app
  └─► If first provider, optionally fetch and import user's repos
```

**Scope management per provider:**
- **GitHub:** `repo,read:user` — full repo access, profile read.
- **GitLab:** `api,read_user,read_repository` — API access, profile read, repo read. Write ops add `write_repository`.
- **Bitbucket:** `repository:read,account:read` — repo read, account read. Write ops add `repository:write`.

**Token refresh:**
- GitHub tokens are static (no built-in expiry). GitLab expires in 2h, Bitbucket in 1h.
- Refresh proactively: background timer checks token age every 30 minutes. If within 10 minutes of expiry (for providers with expiry), refresh.
- Refresh on-demand: when a git operation receives a 401, attempt a refresh before surfacing the error to the user.
- Store refresh token securely alongside access token; rotate both on each refresh.

**PAT as fallback (all providers):**
- Users who prefer PAT/token auth can still enter a token in Settings.
- PAT and OAuth coexist — if a PAT is present for a given provider, use it in preference to the OAuth token for git operations (PAT is more reliable for git protocol operations).
- Clearly indicate which auth method is active per provider in the UI.
- Provider-agnostic PAT input: one field per connected provider.

**Backend changes needed:**
- `internal/oauth/provider.go` — shared OAuth interface + factories
- `internal/oauth/github.go` — GitHub OAuth implementation
- `internal/oauth/gitlab.go` — GitLab OAuth implementation
- `internal/oauth/bitbucket.go` — Bitbucket OAuth implementation
- `internal/api/auth_oauth.go` — HTTP handlers for all 3 providers:
  - `GET /auth/{provider}/login` — initiates OAuth redirect (one handler, dispatches by provider param)
  - `GET /auth/{provider}/callback` — exchanges code for tokens (one handler, dispatches by provider param)
  - `POST /auth/refresh` — refreshes access token (provider-agnostic, reads `provider` from body)
  - `DELETE /auth/{provider}` — revokes tokens, disconnects provider
- Token storage: encrypted credential store (same backend used for provider API keys), keyed by `user_id:provider`

**Frontend changes needed:**
- "Connect Provider" section in Settings → Integrations page (list of 3 providers)
- Per-provider status indicator (connected/disconnected, which account)
- "Disconnect" action per provider
- Provider picker on the onboarding screen (shows all connected providers' repos)
- Wire OAuth token into `gitClient` auth callbacks — select the correct provider's token based on the repo URL's domain

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
- [ ] OAuth provider abstraction layer (`Provider` interface + 3 implementations)
- [ ] GitHub OAuth app registered; credentials injected via config
- [ ] GitLab Application registered; credentials injected via config
- [ ] Bitbucket OAuth Consumer registered; credentials injected via config
- [ ] OAuth redirect flow works end-to-end for all 3 providers (connect → authorize → callback → stored tokens)
- [ ] Token refresh works per provider (proactive + on-demand)
- [ ] OAuth token wired into `gitClient` auth callbacks — provider auto-detected from repo URL domain
- [ ] PAT remains functional as a fallback per provider; UI clearly shows active auth method per provider
- [ ] "Disconnect" revokes tokens and clears stored credentials per provider
- [ ] Provider connection status visible in Settings (all 3)
- [ ] Provider picker on onboarding screen shows repos from all connected providers

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
