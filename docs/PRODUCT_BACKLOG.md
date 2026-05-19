# Product Backlog

This backlog is the current productization plan for `ledit` as a desktop-first application that still preserves the existing backend-driven architecture.

The goal is not to rewrite working subsystems. The goal is to close the gap between "works for development" and "ships as a dependable product".

## Priority Model

- `P0`: blocks reliable product use or release readiness
- `P1`: high-value product work that should follow immediately after `P0`
- `P2`: important polish and scale work
- `P3`: opportunistic improvements

---

## P0: Release-Critical

### 1. Fix desktop CI build matrix (all 6 platforms)

Status: `done`

Completed changes:
- Generated `icon.png` (512x512) and `icon.iconset` (10 standard sizes) from existing source
- Created `desktop/resources/icons/png/` with Linux-required icon sizes (was missing entirely)
- Added `generateMacIcon()` in `electron-before-pack.cjs` to produce `icon.icns` via `iconutil` on macOS
- Made `verify-electron-package.mjs` platform-aware (accepts `--platform` flag, `PLATFORM`/`DESKTOP_PLATFORM` env, or auto-detects from dir name)
- Split CI into `build-backends` job (runs once, all cross-compiles) + `build-desktop` matrix job (downloads prebuilt backends)
- Added `SKIP_BACKEND_BUILD` support to `electron-before-pack.cjs` for CI matrix jobs
- Added Windows targets (`windows-amd64`, `windows-arm64`) to the backend build step

### 2. Desktop Validation Matrix

Status: `not started`

Scope:
- Validate packaged desktop startup on `windows`, `macOS`, and `linux`
- Validate multi-window behavior
- Validate workspace restore behavior
- Validate packaged backend launch and health check behavior

Acceptance criteria:
- Each supported platform has a documented smoke test
- Packaged app can open a folder and load the UI
- Restored windows reopen successfully after relaunch
- Failures are surfaced with actionable errors instead of silent startup failure

### 3. Windows + WSL End-to-End Validation

Status: `partially implemented`

**Current state:** The Electron main process has substantial WSL support:
- Distro discovery (`listWslDistros`)
- WSL backend binary resolution (`ensureWslBackendBinary`)
- Path conversion (`toWslPath`)
- WSL launch command with env vars (`LEDIT_DESKTOP_BACKEND_MODE=wsl`)
- Git resolution via `git.exe` on Windows (`getGitResolutionPath`)

**Open validation gaps:**
- WSL distro discovery and backend staging not tested on a real Windows host
- Linux path handling in `recentWorkspaces` and `restorableWorkspaces` during restore
- WSL terminal behavior (xterm.js connecting to WSL-backed backend)
- Git behavior across the Windows/Linux boundary

Acceptance criteria:
- WSL-backed workspace launches successfully from a packaged Windows app
- Existing WSL-backed workspace can be reopened from Recent
- WSL-backed window restore works after relaunch
- Known limitations are documented if any remain

### 4. Signing, Notarization, and Installer Readiness

Status: `partially implemented`

**Current state:**
- macOS signing certificate setup exists in CI (base64-encoded P12 from `CSC_LINK` secret)
- `entitlements.mac.plist` and `entitlements.mac.inherit.plist` exist
- `electron-after-sign.cjs` hook exists for post-signing
- Windows has `icon.ico` and NSIS config
- Linux has PNG icon set and desktop entry
- `icon.icns` for macOS auto-generated from iconset on macOS CI

**Open gaps:**
- Windows code signing: no certificate/secrets configured in CI
- macOS notarization: `APPLE_ID`/`APPLE_APP_SPECIFIC_PASSWORD` secrets referenced but not validated
- Linux installer quality (AppImage, deb, rpm) not verified

Acceptance criteria:
- Release CI produces signed Windows installers
- Release CI produces notarized macOS artifacts
- Installer metadata and icons are correct on each platform
- Desktop release documentation matches the real release flow

### 5. Crash and Diagnostics Baseline

Status: `partially implemented`

**Current state:**
- `renderErrorPage()` shows basic exit code/signal info when backend dies
- Exit handler (`registerExitHandler`) detects backend crashes and offers reload
- Retry logic in `createWorkspaceWindow` with `maxRetries`
- Backend stdout/stderr now piped to log files in `userData/logs/`

**Open gaps:**
- No diagnostic bundle export
- No renderer crash capture
- No frontend-level error boundary

Acceptance criteria:
- Desktop app shows a clear failure screen when backend launch fails
- Users can export a diagnostics snapshot
- Crash/failure logs are written to a stable app data location

---

## P1: Product-Quality UX

### 6. First-Run Onboarding

Status: `not started`

Scope:
- Provider/model selection
- API credential setup
- Initial workspace selection
- Optional WSL selection on Windows

Note: The Go backend has `LEDIT_DESKTOP_BACKEND_MODE` handling in `pkg/webui/onboarding_api.go` and `DesktopOnboardingHandler` — verify what's already wired up before scoping.

### 7. Workspace Model UX Cleanup

Status: `partially implemented`

### 8. Error Handling and Recovery UX

Status: `not started`

### 9. Session Management as a Product Feature

Status: `partially implemented`

## P1: Test and Quality Gates

### 10. Desktop E2E Coverage

Status: `not started`

### 11. Workspace Switching Coverage

Status: `partially implemented`

---

## P2: Performance and Scale

### 12. Large Repository Performance

Status: `not started`

### 13. State Persistence and Migration

Status: `not started`

### 14. Native OS Integration

Status: `partially implemented`

### 15. Auto-Update

Status: `not started`

---

## P3: Commercial/Product Operations

### 16. Release Operations

Status: `partially implemented`

### 17. Product Analytics and Feedback Loop

Status: `not started`

---

## Technical Debt: Desktop Architecture

| Item | Location | Description |
|------|----------|-------------|
| `main.js` is ~1780 lines | `desktop/main.js` | Single file contains protocol handler, state management, WSL logic, SSH logic, window management, backend spawning, error pages, worktree support. Should be split into modules. |
| No desktop package.json | `desktop/` | Desktop metadata lives in root `package.json`. Consider extracting `desktop/package.json`. |
| `preload.js` is minimal (24 lines) | `desktop/preload.js` | Will need expansion as more native capabilities are exposed to renderer. |
| `child.unref()` on backend processes | `desktop/main.js` | Backend processes are unref'd from the event loop — intentional but means orphans on crash. |

---

## Execution Order

### Phase 0: CI Green (DONE)
1. ~~Fix desktop CI build matrix~~
2. ~~Generate icon assets and fix icon paths~~
3. ~~Fix verify script to be platform-aware~~
4. ~~Optimize backend cross-compilation~~
5. ~~Add backend logging~~

### Phase 1: Smoke Test on Real Hardware
6. Desktop validation matrix
7. Windows + WSL end-to-end validation
8. Signing/notarization/installer readiness
9. Crash and diagnostics baseline (log export, error boundary)

### Phase 2: Product-Quality UX
10. First-run onboarding
11. Workspace model UX cleanup
12. Error handling and recovery UX
13. Workspace switching coverage

### Phase 3: Test and Scale
14. Desktop E2E coverage
15. Session management improvements
16. Native OS integration
17. Large repository performance

### Phase 4: Release Hardening
18. Auto-update
19. State migration hardening
20. Release operations
21. Product analytics
