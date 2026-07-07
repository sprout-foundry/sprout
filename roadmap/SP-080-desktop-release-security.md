# SP-080: Desktop Release — Security, Compliance, and Distribution Readiness

**Status:** 🔴 Suspended — Desktop builds and CI are disabled. Desktop is lowest-value distribution channel at current product stage. This spec defines the complete requirements to re-enable and ship a secure desktop release.
**Created:** 2026-07-07
**Suspended:** 2026-07-07 (desktop CI disabled, electron dependencies removed from package.json)

---

## 1. Purpose & Status

The Sprout desktop application (Electron-based, per-workspace Go backend) has been built to a functional prototype stage. It works for local development use but has significant security gaps, compliance deficiencies, and distribution blockers that prevent shipping to end users.

**Decision:** Desktop is disabled until this spec is implemented. The `desktop/` directory is preserved, CI workflows are renamed to `.disabled`, and electron-related dependencies are removed from `package.json`.

When ready to re-engage, implement Phase 1 through 4 in order. Phase 1 is the gate — nothing ships without it.

---

## 2. Current Architecture Summary

### Core Design
- **Electron main process** (`desktop/main.js`) orchestrates app lifecycle, IPC, window management
- **One Go backend per workspace window** — each runs `sprout agent --daemon --isolated-config` on a unique port
- **Auth via bearer token** — Electron generates a 256-bit secret, injects via `session.webRequest.onBeforeSendHeaders`
- **Context isolation enabled**, Node integration disabled, **sandbox disabled** (`sandbox: false`)
- **Three backend modes:** native (macOS/Linux via Unix socket proxy, Windows via TCP), WSL (Windows→WSL), SSH (remote hosts)

### File Inventory (20 files in `desktop/`)
| File | Role | Lines |
|------|------|-------|
| `main.js` | Entry point, IPC handlers, lifecycle | ~260 |
| `preload.js` | Context bridge (23 exposed APIs) | ~60 |
| `windows.js` | BrowserWindow creation, menu, auth injection | ~400 |
| `backend.js` | Backend spawn, port alloc, health, Unix socket proxy | ~350 |
| `updater.js` | Auto-update via electron-updater | ~270 |
| `ssh.js` | SSH remote backend: inspect, upload, tunnel | ~320 |
| `launcher.js` | Renderer logic for launcher UI | ~250 |
| `launcher.html` | Launcher UI markup | ~100 |
| `launcher.css` | Launcher styles | ~200 |
| `state-manager.js` | Persistent state, window bounds, log streams | ~150 |
| `workspace.js` | Git worktree creation, directory resolution | ~150 |
| `protocol.js` | `sprout://` URL scheme registration | ~40 |
| `wsl.js` | WSL distro discovery, binary staging | ~100 |
| `error-pages.js` | Loading/error page HTML generators | ~170 |
| `context.js` | Shared mutable state | ~20 |
| `utils.js` | Pure helpers (shellEscape, normalize, key gen) | ~30 |
| `backend_test.js` | Unit tests for backend module | ~270 |
| `windows_test.js` | Unit tests for window creation | ~280 |
| `updater.test.js` | Jest tests for updater | ~540 |

### Backend Security (already implemented)
- `pkg/webui/auth_middleware.go` — Bearer token auth on POST/PUT/PATCH/DELETE to `/api/*`
- `pkg/webui/server.go` — Refuses to start on non-localhost bind without auth token
- `pkg/webui/security_headers.go` — CSP, X-Frame-Options, Permissions-Policy headers

---

## 3. Phase 1 — Showstoppers (Cannot Ship Without)

### 1.1 Code Signing

**Problem:** CI falls back to ad-hoc signing (`-c.mac.identity=-`). macOS Gatekeeper blocks the app for every user. Windows has no Authenticode — SmartScreen shows "Unknown Publisher."

**Remediation:**
- [ ] Acquire Apple Developer ID certificate
- [ ] Acquire Windows Authenticode certificate (e.g., DigiCert Code Signing)
- [ ] Configure CI secrets: `CSC_LINK` (base64 .p12), `CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_SPECIFIC_PASSWORD`
- [ ] Remove ad-hoc signing fallback from `.github/workflows/desktop-release.yml` — require signing cert or fail the build
- [ ] Verify `scripts/electron-after-sign.cjs` notarization flow works end-to-end with real cert

**Files:** `.github/workflows/desktop-release.yml`, `scripts/electron-after-sign.cjs`, `package.json` (build.mac)

### 1.2 Enable Electron Sandbox

**Problem:** All `BrowserWindow` instances in `desktop/windows.js` have `sandbox: false`. Without sandbox, renderer processes have more attack surface even with context isolation.

**Remediation:**
- [ ] Set `sandbox: true` on all BrowserWindow configs in `desktop/windows.js` (launcher, workspace, SSH windows)
- [ ] Verify `desktop/preload.js` works with sandbox — it uses `contextBridge` and `ipcRenderer`, both sandbox-compatible
- [ ] Verify `desktop/launcher.js` doesn't use Node.js APIs (it uses `window.sproutDesktop` via contextBridge — should be fine)
- [ ] Update `desktop_test.js` and `windows_test.js` to reflect sandboxed environment
- [ ] Run full smoke test suite with sandbox enabled

**Files:** `desktop/windows.js` (lines ~80-90 launcher, ~200-210 workspace, ~310-320 SSH)

### 1.3 Per-Workspace Auth Token Isolation

**Problem:** `desktop/backend.js` generates one `authToken` at app startup via `generateSecret()`. All workspace backends share the same token. Compromising one workspace (e.g. XSS) allows lateral movement to any other workspace.

Additionally, `SPROUT_AUTH_TOKEN` is passed as an environment variable to child processes (WSL, Windows native), visible in `/proc/<pid>/environ`.

**Remediation:**
- [ ] Move token generation from module-level (`let authToken` at top of `backend.js`) to per-backend-spawn
- [ ] Change `generateSecret()` to return a new token each call
- [ ] `injectAuthHeaders()` in `windows.js` must use the per-window token, not a global
- [ ] For Windows native: pass token via `--secret` CLI flag (like macOS/Linux already does) instead of `SPROUT_AUTH_TOKEN` env var
- [ ] For WSL: pass via `--secret` CLI flag instead of env var
- [ ] Remove `SPROUT_AUTH_TOKEN` from all child process env — use only `--secret` flag
- [ ] Update `pkg/webui/server.go` to read from `--secret` flag consistently (already supported)

**Files:** `desktop/backend.js` (lines ~30-35 `let authToken`, line ~304 `--secret`, line ~366 `SPROUT_AUTH_TOKEN`), `desktop/windows.js` (line ~220 `injectAuthHeaders`), `desktop/ssh.js` (line ~290 inline script)

### 1.4 macOS Notarization

**Problem:** `scripts/electron-after-sign.cjs` has the notarization flow but gates on env vars being set. Without configured credentials, notarization is skipped silently.

**Remediation:**
- [ ] Configure `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_SPECIFIC_PASSWORD` CI secrets
- [ ] Test `xcrun notarytool submit` flow end-to-end in CI
- [ ] Add CI gate: if signing is enabled but notarization credentials are missing, fail the build (don't silently skip)

**Files:** `scripts/electron-after-sign.cjs`, `.github/workflows/desktop-release.yml`

---

## 4. Phase 2 — Distribution Readiness

### 2.1 Content Security Policy for Launcher

**Problem:** `launcher.html` is loaded via `loadFile()` (local file:// URL). It has no CSP. If any content injection occurs, scripts execute with full `window.sproutDesktop` bridge access.

**Remediation:**
- [ ] Add `<meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self'; style-src 'self';">` to `launcher.html`
- [ ] Ensure all scripts/styles are loaded from same origin (they are — `./launcher.js`, `./launcher.css`)

**Files:** `desktop/launcher.html`

### 2.2 Privacy Policy & Data Disclosure

**Problem:** No privacy policy, no first-run consent dialog, no data handling disclosure anywhere in the app.

**Remediation:**
- [ ] Write a privacy policy document (can be a static HTML file bundled with the app or hosted externally)
- [ ] Add "Privacy Policy" link in Help menu (`desktop/windows.js` `buildMenu()`)
- [ ] Create a first-run dialog explaining:
  - What data is stored locally (workspace paths, window bounds, update state, backend logs)
  - What data is sent to remote hosts (SSH backend binary, workspace paths)
  - That no telemetry/analytics is collected
- [ ] Store first-run consent in `update-state.json` or a new `preferences.json`

**Files:** `desktop/windows.js` (menu), `desktop/launcher.html` (first-run modal), `desktop/state-manager.js` (consent persistence)

### 2.3 EULA / Terms

**Problem:** No end-user license agreement or terms of use.

**Remediation:**
- [ ] Draft a minimal EULA for a developer tool (MIT license is the code license; EULA covers the app distribution, data handling, liability disclaimers)
- [ ] Present at first-run alongside privacy policy
- [ ] Require acceptance before using the app

**Files:** New: `desktop/resources/EULA.md` or similar

### 2.4 Update Rollback Mechanism

**Problem:** `updater.js` calls `autoUpdater.quitAndInstall()` with no backup. If an update fails or breaks, the user has no way to recover.

**Remediation:**
- [ ] Before `quitAndInstall()`, copy the current app bundle to a backup location (e.g., `~/Library/Application Support/sprout/backups/`)
- [ ] On failed startup (backend can't launch, crash on init), detect stale backup and offer rollback
- [ ] Keep last 2 previous versions as rollback targets

**Files:** `desktop/updater.js`, `desktop/main.js` (startup recovery)

### 2.5 macOS App Store Readiness (Optional / Future)

**If targeting MAS (vs. direct download only):**
- [ ] Sandbox entitlements (`com.apple.security.app-sandbox`, etc.)
- [ ] No external network access without explicit `com.apple.security.network.client` entitlement
- [ ] No code execution of external binaries (blocks `spawn(binaryPath, ...)` for backend) — **this is a fundamental blocker for MAS**
- [ ] App review compliance (no third-party code signing, privacy manifests)
- [ ] **Conclusion:** MAS is not feasible with the current architecture (spawning Go binaries). Direct download is the only viable path.

---

## 5. Phase 3 — Hardening

### 3.1 Log Rotation & Redaction

**Problem:** `state-manager.js:openBackendLogStream()` creates a new file per backend spawn with no size limit, no count limit, and no redaction. Logs contain backend stdout/stderr which may include API keys, token values, and conversation content.

**Remediation:**
- [ ] Add max-size rotation: cap each file at 10MB
- [ ] Add max-count: keep last 5 log files per backend label
- [ ] Add sensitive data redaction: regex patterns to mask known token formats (e.g., `sk-...`, `Bearer ...`, API key patterns)
- [ ] Log files should be created with restrictive permissions (0600)

**Files:** `desktop/state-manager.js`

### 3.2 SSH Host Key Verification Hardening

**Problem:** `ssh.js` uses `StrictHostKeyChecking=accept-new` — first connection to any host is automatically trusted. MITM risk on first connect.

**Remediation:**
- [ ] On first connect to a new host, show a warning dialog with the host key fingerprint
- [ ] Allow user to approve or deny
- [ ] Make behavior configurable via desktop settings
- [ ] Use `StrictHostKeyChecking=ask` with batch-mode fallback for known hosts

**Files:** `desktop/ssh.js` (lines ~140-160 `runSSH`, line ~240 `startSSHBackendForHost`)

### 3.3 Accessibility

**Problem:** Zero accessibility support in launcher or workspace UI.

**Remediation:**
- [ ] Add ARIA labels to all interactive elements in `launcher.html`
- [ ] Ensure keyboard navigation works (Tab order, Enter/Space activation)
- [ ] Focus management in modal dialogs (traps focus, returns focus on close)
- [ ] Add `role` attributes to landmark regions
- [ ] Test with VoiceOver/NVDA

**Files:** `desktop/launcher.html`, `desktop/launcher.js`

### 3.4 Input Validation & Path Traversal Protection

**Problem:** `workspace.js` accepts any path string. No validation against path traversal (`../` chains to escape to sensitive directories).

**Remediation:**
- [ ] Validate workspace paths: must be absolute, must exist, must be a directory
- [ ] Resolve symlinks and check resolved path is not in sensitive locations (`/etc`, `/root`, etc.)
- [ ] Sanitize all inputs to child process spawns (the `shellEscape` helper is good but should be applied consistently)
- [ ] Validate branch names and worktree paths in `createWorktree()` against path traversal

**Files:** `desktop/workspace.js`, `desktop/utils.js`

### 3.5 State File Security

**Problem:** `desktop-state.json` stores workspace paths in plaintext in `userData`. On a shared machine, any user can read it.

**Remediation (low priority):**
- [ ] Consider OS-level encryption of the state file (e.g., macOS Keychain for sensitive fields)
- [ ] At minimum, ensure file permissions are 0600
- [ ] Document that state file is unencrypted in privacy policy

**Files:** `desktop/state-manager.js`

---

## 6. Phase 4 — Operational

### 4.1 Crash Reporting

**Current:** No crash reporting. Users see an error page and copy diagnostics manually.

**Remediation:**
- [ ] Implement opt-in crash reporting (e.g., Sentry, Bugsnag, or self-hosted)
- [ ] First-run dialog includes opt-in checkbox
- [ ] Anonymize workspace paths in crash reports (hash or redact)
- [ ] Include version, platform, arch, backend mode in reports

**Files:** `desktop/error-pages.js`, `desktop/main.js`

### 4.2 Telemetry

**Current:** None.

**Decision:** Do not add telemetry without explicit user opt-in. If needed, follow the same consent pattern as crash reporting.

### 4.3 Internationalization

**Current:** All strings hardcoded in English across `launcher.html`, `launcher.js`, `error-pages.js`.

**Remediation:**
- [ ] Extract all user-facing strings to a translation file
- [ ] Implement locale detection and fallback to English
- [ ] Consider whether non-English markets are a priority — may defer to Phase 5

**Files:** `desktop/launcher.html`, `desktop/launcher.js`, `desktop/error-pages.js`, `desktop/windows.js`

### 4.4 Desktop Support Bundle

**Current:** `pkg/webui/api_support_bundle.go` exists for the webui. Desktop has no equivalent.

**Remediation:**
- [ ] Add "Create Support Bundle" option in Help menu
- [ ] Package: desktop-state.json, recent logs, app version, platform info
- [ ] Redact sensitive data before bundling
- [ ] Save to a user-chosen location or copy to clipboard

**Files:** `desktop/windows.js` (menu), new: `desktop/support-bundle.js`

---

## 7. Data Inventory

### Persistent User Data (written to `app.getPath('userData')`)

| File | Data | Sensitivity | Retention |
|------|------|-------------|-----------|
| `desktop-state.json` | Workspace paths, backend modes, WSL distros, window bounds | Low (paths only) | Indefinite (append-only) |
| `update-state.json` | `installOnQuit` (bool), `updateDownloaded` (bool), `downloadedVersion` (string) | None | Indefinite |
| `logs/backend-{label}-{timestamp}.log` | Backend stdout/stderr — may contain API keys, conversation snippets, file paths | **High** | Indefinite (no rotation) |

### In-Memory State (`context.js`)

| Key | Contents | Sensitive? |
|-----|----------|------------|
| `instanceRegistry` | Child process refs, ports, workspace paths, proxy servers, socket paths | Low |
| `workspaceWindowMap` | Workspace key → window ID | None |
| `sshWindowMap` | SSH key → window ID | None |
| `authToken` | 256-bit hex secret | **High** (one per app launch currently) |

### Temporary Files

| Path | Purpose | Cleanup |
|------|---------|---------|
| `/tmp/sprout-desktop-{hex}.sock` | Unix domain socket (macOS/Linux) | OS cleans on reboot; should be cleaned on window close |

### Remote State

| Target | What | Concern |
|--------|------|---------|
| SSH hosts: `~/.cache/sprout-desktop/backend/{version}/sprout` | Go binary staged on remote | Binary integrity verification needed |
| SSH hosts: `~/.cache/sprout-desktop/logs/{alias}.log` | Backend logs on remote | Sensitive data on potentially untrusted hosts |
| WSL: `$HOME/.cache/sprout-desktop/backend/sprout` | Go binary staged in WSL distro | WSL is trusted (same machine) |

---

## 8. IPC Channel Registry

### Renderer → Main (invoke)

| Channel | Purpose | Sensitive Data? |
|---------|---------|-----------------|
| `desktop:listRecentWorktrees` | Get recent workspace list | Paths only |
| `desktop:listSshHosts` | Parse SSH config for hosts | SSH host info |
| `desktop:listWslDistros` | List WSL distros | None |
| `desktop:pickRepository` | Dialog to pick git repo | Path |
| `desktop:pickWorkspace` | Dialog to pick workspace | Path |
| `desktop:pickWorktree` | Dialog to pick worktree | Path |
| `desktop:pickWorktreeParent` | Dialog to pick parent dir | Path |
| `desktop:openWorkspace` | Open workspace in window | Path, backend mode |
| `desktop:openWorktree` | Open worktree in window | Path |
| `desktop:openSshWorkspace` | Open SSH workspace | Host alias, remote path |
| `desktop:createWorktree` | Create git worktree | Repo path, branch, worktree path |
| `desktop:installWsl` | Trigger WSL install | None |
| `desktop:installGitForWindows` | Trigger Git install | None |
| `desktop:appVersion` | Get app version | None |
| `desktop:checkForUpdates` | Check for updates | None |
| `desktop:installUpdate` | Install downloaded update | None |
| `desktop:deferUpdate` | Defer update to quit | None |
| `desktop:isUpdatePending` | Check pending install | None |
| `desktop:cancelPendingInstall` | Cancel pending install | None |

### Main → Renderer (send)

| Channel | Purpose | Sensitive Data? |
|---------|---------|-----------------|
| `desktop:hotkey` | Menu hotkey dispatch | Command ID string |
| `desktop:trigger-update-check` | Menu trigger | None |
| `update:error` | Update error notification | Error message |
| `update:available` | Update available | Version string |
| `update:download-progress` | Download progress | Numbers |
| `update:downloaded` | Update downloaded | Version string |

---

## 9. Security Threat Model

### Threat 1: Lateral Movement via Shared Auth Token
- **Risk:** High (Phase 1.3)
- **Attack:** Compromise one workspace via XSS → use shared auth token to write to another workspace
- **Mitigation:** Per-workspace tokens (Phase 1.3)

### Threat 2: MITM on SSH First Connect
- **Risk:** Medium (Phase 3.2)
- **Attack:** Network attacker intercepts first SSH connection, provides fake host key
- **Mitigation:** First-connect warning dialog, configurable host key checking (Phase 3.2)

### Threat 3: Log File Credential Exposure
- **Risk:** High (Phase 3.1)
- **Attack:** Local attacker reads backend logs to extract API keys or tokens
- **Mitigation:** Log rotation, redaction, file permissions (Phase 3.1)

### Threat 4: Sandbox Escape
- **Risk:** Medium (Phase 1.2)
- **Attack:** Exploit renderer to access Node.js APIs, execute arbitrary code
- **Mitigation:** Enable sandbox (Phase 1.2)

### Threat 5: Unsigned Binary Supply Chain
- **Risk:** Critical (Phase 1.1)
- **Attack:** Tamper with update download, user installs malicious update
- **Mitigation:** Code signing, notarization (Phase 1.1, 1.4)

### Threat 6: Path Traversal in Workspace Selection
- **Risk:** Low (Phase 3.4)
- **Attack:** Craft workspace path to read sensitive system files via backend
- **Mitigation:** Path validation and resolution (Phase 3.4)

---

## 10. File-by-File Remediation Checklist

### `desktop/backend.js`
- [ ] Move `authToken` from module-level to per-spawn
- [ ] `generateSecret()` returns new token each call
- [ ] Remove `SPROUT_AUTH_TOKEN` from child env (Windows, WSL) — use `--secret` flag
- [ ] Add per-backend token to return value of `startBackendForWorkspace()`
- [ ] Ensure socket files are cleaned up on window close

### `desktop/windows.js`
- [ ] `sandbox: true` on all BrowserWindow configs
- [ ] `injectAuthHeaders()` uses per-window token from backend spawn result
- [ ] Add privacy policy link to Help menu
- [ ] Add EULA link to Help menu
- [ ] Add "Create Support Bundle" to Help menu
- [ ] Notification targets should use focused window, not `getAllWindows()[0]`

### `desktop/preload.js`
- [ ] Verify all exposed APIs work with sandbox enabled
- [ ] No changes expected — uses only `contextBridge` and `ipcRenderer`

### `desktop/launcher.html`
- [ ] Add CSP meta tag
- [ ] Add ARIA labels to all interactive elements
- [ ] Add first-run dialog markup (EULA + privacy policy consent)

### `desktop/launcher.js`
- [ ] Wire up first-run dialog flow
- [ ] Add keyboard navigation support
- [ ] Focus management for modal

### `desktop/updater.js`
- [ ] Add pre-install backup of current app bundle
- [ ] Add rollback detection on startup
- [ ] Target focused window for notifications, not `getAllWindows()[0]`

### `desktop/ssh.js`
- [ ] Add first-connect warning dialog for new host keys
- [ ] Add SHA-256 verification after SCP upload
- [ ] Make `StrictHostKeyChecking` behavior configurable

### `desktop/state-manager.js`
- [ ] Add log file size limit (10MB)
- [ ] Add log file count limit (5 per label)
- [ ] Add sensitive data redaction function
- [ ] Set restrictive file permissions (0600) on state files

### `desktop/workspace.js`
- [ ] Add path traversal protection
- [ ] Validate resolved path is not in sensitive system directories
- [ ] Sanitize branch names for use in paths

### `desktop/error-pages.js`
- [ ] Add crash reporting opt-in UI
- [ ] Redact sensitive data from diagnostics JSON

### `desktop/main.js`
- [ ] Add startup recovery: detect failed update, offer rollback
- [ ] Clean up socket files on `will-quit`

### `desktop/utils.js`
- [ ] Add `sanitizePath()` function for path traversal protection
- [ ] Add `redactSensitive()` function for log redaction

### `scripts/electron-after-sign.cjs`
- [ ] Fail build if signing is enabled but notarization creds are missing

### `.github/workflows/desktop-release.yml`
- [ ] Require signing cert or fail (remove ad-hoc fallback for releases)

---

## 11. Acceptance Criteria

### Phase 1 Complete When:
- [ ] macOS builds are signed with Developer ID and notarized
- [ ] Windows builds are signed with Authenticode
- [ ] All BrowserWindow configs have `sandbox: true` and pass tests
- [ ] Each workspace backend has its own auth token (not shared)
- [ ] No `SPROUT_AUTH_TOKEN` in child process environment
- [ ] CI signing/notarization pipeline passes end-to-end

### Phase 2 Complete When:
- [ ] Launcher has CSP meta tag
- [ ] Privacy policy exists and is linked in app
- [ ] EULA exists and is presented at first run
- [ ] Update rollback mechanism works (tested: install update → fail → rollback)
- [ ] First-run consent is persisted and checked on launch

### Phase 3 Complete When:
- [ ] Log files rotate at 10MB, max 5 per label
- [ ] Log redaction masks API key patterns
- [ ] SSH first-connect shows warning dialog
- [ ] All launcher elements pass accessibility audit (ARIA, keyboard nav)
- [ ] Path traversal attempts are rejected
- [ ] State files have 0600 permissions

### Phase 4 Complete When:
- [ ] Crash reporting is opt-in with clear consent
- [ ] Support bundle generation works
- [ ] All user-facing strings are extractable for i18n (deferred actual translations)

---

## Re-engagement Process

When ready to re-engage with desktop:

1. Re-add `electron`, `electron-builder`, `electron-updater` to `package.json` dependencies
2. Restore desktop scripts in `package.json`
3. Rename `.disabled` workflow files back to active names
4. Implement Phase 1 items
5. Get code signing certificates and configure CI secrets
6. Run Phase 1 acceptance criteria
7. Proceed to Phase 2

Do not skip phases. Each phase is a gate for the next.
