# Desktop (Dormant)

This directory contains the Electron desktop shell for Sprout. Desktop development is **suspended** pending security hardening and compliance work.

## Why suspended

Desktop is the lowest-value distribution channel at the current product stage. The CLI and web UI are the primary distribution paths.

## Re-engagement

See [roadmap/SP-080-desktop-release-security.md](../roadmap/SP-080-desktop-release-security.md) for the complete security, compliance, and distribution readiness spec.

## What's here

- `main.js` — Electron main process entry point
- `preload.js` — Context bridge (IPC API exposed to renderer)
- `backend.js` — Per-workspace Go backend spawn and management
- `windows.js` — BrowserWindow creation, menus, auth injection
- `updater.js` — Auto-update via electron-updater
- `ssh.js` — SSH remote backend support
- `wsl.js` — WSL backend support (Windows)
- `launcher.*` — Launcher UI (HTML/CSS/JS)
- `state-manager.js` — Persistent state, window bounds, log streams
- `workspace.js` — Git worktree creation and resolution
- `protocol.js` — `sprout://` URL scheme registration
- `error-pages.js` — Loading/error page HTML generators
- `context.js`, `utils.js` — Shared state and helpers
- `*_test.js` — Unit tests
- `resources/` — Icons and macOS entitlements

## To re-enable

1. Add `electron`, `electron-builder`, `electron-updater` back to `package.json`
2. Restore desktop scripts in `package.json`
3. Rename `.github/workflows/desktop-*.yml.disabled` back to active
4. Implement Phase 1 of SP-080
