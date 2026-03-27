# Electron Packaging

This repo can be wrapped in an Electron desktop shell without replacing the existing Go + React architecture.

## Model

- Electron main process owns desktop windows.
- Each desktop window launches its own `ledit` backend on a unique localhost port.
- Each window is bound to a working directory path.
- The Go backend still serves the existing embedded web UI and WebSocket APIs.
- On Windows, a workspace window can also run its backend inside a selected WSL distro instead of as a native Windows process.

## Why Git Worktrees

The desktop shell is designed around one workspace per window. Any folder can be opened as the CWD for a desktop window. Using Git worktrees is still useful when you want:

- isolated `.ledit/` config per workspace when launched with `--isolated-config`
- separate working directories for multiple concurrent instances
- independent terminal and Git state per desktop window

Open a normal folder from the launcher, or create git worktrees with:

```bash
git worktree add ../feature-xyz -b feature-xyz
```

Then open that directory from the desktop shell.

The launcher can also create a new worktree for you by running `git worktree add -b <branch> <path> <base-ref>` and then opening it in a new window.

## Windows + WSL

On Windows, the launcher now exposes a WSL section when installed distros are detected. That mode:

- keeps the Electron shell on Windows
- stages the bundled Linux backend into the chosen distro
- launches `ledit` inside WSL
- keeps the backend working directory, Git, terminal, and file operations inside that distro

Use a Linux path such as `/home/you/project` for WSL-backed windows. Recent entries remember whether they were opened natively or through WSL, including the selected distro.

## Development

Build the embedded web UI and current-platform backend:

```bash
npm run build:desktop
```

Launch Electron in development mode:

```bash
npm run desktop:dev
```

`desktop:dev` rebuilds the embedded web UI and current-platform backend first, then launches Electron against the local app bundle layout.

## Packaging

Package the desktop app for the current platform:

```bash
npm run desktop:dist
```

Verify that the unpacked desktop bundle is self-contained:

```bash
npm run desktop:verify
```

`electron-builder` is configured for:

- macOS: `dmg`, `zip`
- Windows: `nsis`, `zip`
- Linux: `AppImage`, `deb`, `rpm`

The packaging flow keeps the existing app architecture intact:

- Electron manages native windows, menus, and desktop lifecycle.
- Each workspace window launches the existing Go backend on its own localhost port.
- The backend still serves the embedded web UI and current WebSocket/HTTP APIs.
- `electron-builder` runs a `beforePack` hook that rebuilds the embedded web UI and the correct platform backend automatically, so the packaged app does not depend on a manual prebuild step.
- Windows packaging also includes the matching Linux backend so WSL-backed windows can launch without an external install.
- `desktop:verify` checks that the unpacked app includes the Electron app bundle and bundled Go backend before release artifacts are published.
- Windows and Linux installer assets now use checked-in desktop icons from [`desktop/resources`](/home/alanp/dev/personal/ledit-electron/desktop/resources).
- The desktop app now registers a `ledit://` protocol and handles OS open-file events by routing them into the existing multi-window worktree launcher.
- macOS signing/notarization is scaffolded via [`scripts/electron-after-sign.cjs`](/home/alanp/dev/personal/ledit-electron/scripts/electron-after-sign.cjs) and hardened-runtime entitlements in [`desktop/resources/entitlements.mac.plist`](/home/alanp/dev/personal/ledit-electron/desktop/resources/entitlements.mac.plist).

## Cross-Platform CI

For full cross-platform release builds, use a matrix CI job that runs on:

- `macos-latest`
- `windows-latest`
- `ubuntu-latest`

Each runner should:

1. install Go and Node
2. run `npm ci` in the repo root and `webui/`
3. run `npm run desktop:dist`

This repo now includes [`desktop-release.yml`](/home/alanp/dev/personal/ledit-electron/.github/workflows/desktop-release.yml) to produce platform-native desktop installers on Linux, macOS, and Windows.

For signed macOS builds, configure these CI secrets:

- `CSC_LINK`
- `CSC_KEY_PASSWORD`
- `APPLE_ID`
- `APPLE_TEAM_ID`
- `APPLE_APP_SPECIFIC_PASSWORD`

## Notes

- The desktop shell intentionally bypasses the single-port Web UI supervisor by always passing `--web-port`.
- Multiple windows are supported by running one backend per window.
- Re-opening the same worktree focuses the existing window unless `Open Worktree in New Window` is used.
- The launcher restores previously open worktrees on relaunch and now reuses each worktree window's last saved size, position, and maximized state.
- This keeps the existing backend-driven functionality where it already works well instead of reimplementing it in Electron IPC.
- Windows and Linux packaging now uses branded installer/application icons. macOS signing/notarization is scaffolded, but a proper `.icns` generated on a macOS host is still the remaining native-branding gap.
- Deep links can target a workspace with `ledit://open?path=/absolute/path/to/worktree` or `ledit://open?workspace=/absolute/path/to/worktree`.
- Native open events can now target either a worktree directory or a file inside a worktree; the desktop app resolves the containing Git root before opening the workspace window.

## Productization

The current productization backlog and suggested execution order live in [`docs/PRODUCT_BACKLOG.md`](/home/alanp/dev/personal/ledit/docs/PRODUCT_BACKLOG.md).
