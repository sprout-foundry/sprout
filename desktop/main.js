/**
 * Electron main process entry point.
 * Orchestrates the app lifecycle, IPC handlers, and startup logic.
 * Heavy lifting is delegated to focused modules in this directory.
 */

const { app, BrowserWindow, dialog, ipcMain } = require('electron');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const ctx = require('./context');
const { getWorkspaceKey } = require('./utils');
const {
  getLogDirectory,
  getRecentWorktreeEntries,
  getRecentWorktrees,
  getRestorableWorktrees,
} = require('./state-manager');
const { listWslDistros, installWslFromDesktop, installGitForWindowsFromDesktop } = require('./wsl');
const { listSshHosts } = require('./ssh');
const {
  resolveWorkspaceDirectory,
  promptForWorkspace,
  promptForRepository,
  promptForWorktreeParent,
  createWorktree,
} = require('./workspace');
const {
  createLauncherWindow,
  buildMenu,
  createWorkspaceWindow,
  createSSHWorkspaceWindow,
} = require('./windows');
const { initAutoUpdater, isUpdatePending } = require('./updater');
const { autoUpdater } = require('electron-updater');
const { registerDesktopProtocol, extractWorkspacePathFromOpenTarget } = require('./protocol');

function isSmokeTestMode() {
  return process.env.SPROUT_SMOKE_TEST === '1';
}

function shouldSkipRestore() {
  return process.env.SPROUT_SKIP_RESTORE === '1';
}

function isAppEntryArgument(candidate) {
  if (!candidate || candidate.startsWith('-')) {
    return false;
  }

  try {
    return path.resolve(candidate) === path.resolve(app.getAppPath());
  } catch {
    return false;
  }
}

function configureSmokeTestPaths() {
  if (!isSmokeTestMode()) {
    return;
  }

  const baseDir = fs.mkdtempSync(path.join(os.tmpdir(), 'sprout-smoke-'));
  app.setPath('userData', path.join(baseDir, 'user-data'));
  app.setPath('sessionData', path.join(baseDir, 'session-data'));
  app.setPath('cache', path.join(baseDir, 'cache'));
  app.setPath('logs', path.join(baseDir, 'logs'));
}

configureSmokeTestPaths();

// ── Single-instance lock ──────────────────────────────────────────────────────
if (!isSmokeTestMode()) {
  const gotSingleInstanceLock = app.requestSingleInstanceLock();
  if (!gotSingleInstanceLock) {
    app.quit();
  }
}

// ── Startup helpers ───────────────────────────────────────────────────────────
async function openInitialWindow() {
  if (BrowserWindow.getAllWindows().length > 0) {
    return;
  }
  createLauncherWindow();
}

async function openMostRecentWorkspace() {
  const recent = getRecentWorktrees();
  if (recent.length === 0) {
    return false;
  }

  for (const entry of recent) {
    try {
      const result = await createWorkspaceWindow({ ...entry, forceNewWindow: true });
      if (result) {
        return true;
      }
    } catch (error) {
      console.error(`Failed to open recent workspace ${entry.workspacePath}:`, error);
    }
  }

  return false;
}

async function restorePreviousSession() {
  const restorable = getRestorableWorktrees();
  if (restorable.length === 0) {
    return false;
  }

  let opened = 0;
  for (const entry of restorable) {
    try {
      const result = await createWorkspaceWindow({ ...entry, forceNewWindow: true });
      if (result) {
        opened += 1;
      }
    } catch (error) {
      console.error(`Failed to restore worktree ${entry.workspacePath}:`, error);
    }
  }

  return opened > 0;
}

function resolveWorkspaceArg(argv) {
  for (const arg of argv) {
    if (!arg || arg.startsWith('-')) {
      continue;
    }
    if (isAppEntryArgument(arg)) {
      continue;
    }
    const candidate = extractWorkspacePathFromOpenTarget(arg);
    if (!candidate) {
      continue;
    }
    const workspacePath = resolveWorkspaceDirectory(candidate);
    if (workspacePath) {
      return workspacePath;
    }
  }
  return null;
}

async function openWorkspaceFromTarget(candidate, options = {}) {
  const workspacePath = extractWorkspacePathFromOpenTarget(candidate);
  if (!workspacePath) {
    return null;
  }

  const resolvedWorkspace = resolveWorkspaceDirectory(workspacePath);
  if (!resolvedWorkspace) {
    return null;
  }

  return createWorkspaceWindow({
    workspacePath: resolvedWorkspace,
    forceNewWindow: Boolean(options.forceNewWindow),
  });
}

async function handleOpenTarget(candidate, options = {}) {
  if (!candidate) {
    return;
  }

  if (!ctx.isAppReady) {
    ctx.pendingOpenTargets.push({ candidate, forceNewWindow: Boolean(options.forceNewWindow) });
    return;
  }

  const browserWindow = await openWorkspaceFromTarget(candidate, options);
  if (!browserWindow && !BrowserWindow.getAllWindows().length) {
    await openInitialWindow();
  }
}

// ── Multi-instance / OS open events ──────────────────────────────────────────
app.on('second-instance', async (_event, argv) => {
  const workspacePath = resolveWorkspaceArg(argv);
  if (workspacePath) {
    await createWorkspaceWindow({ workspacePath, forceNewWindow: true });
    return;
  }
  await openInitialWindow();
});

app.on('open-file', (event, filePath) => {
  event.preventDefault();
  handleOpenTarget(filePath, { forceNewWindow: true }).catch((error) => {
    dialog.showErrorBox('Failed to Open Folder', String(error?.message || error));
  });
});

app.on('open-url', (event, url) => {
  event.preventDefault();
  handleOpenTarget(url, { forceNewWindow: true }).catch((error) => {
    dialog.showErrorBox('Failed to Open Folder', String(error?.message || error));
  });
});

// ── IPC handlers ──────────────────────────────────────────────────────────────
ipcMain.handle('desktop:listRecentWorktrees', async () => getRecentWorktreeEntries());
ipcMain.handle('desktop:listSshHosts', async () => listSshHosts());
ipcMain.handle('desktop:listWslDistros', async () => listWslDistros());
ipcMain.handle('desktop:pickRepository', async () =>
  promptForRepository(BrowserWindow.getFocusedWindow() || ctx.launcherWindow || null));
ipcMain.handle('desktop:pickWorkspace', async () =>
  promptForWorkspace(BrowserWindow.getFocusedWindow() || ctx.launcherWindow || null));
ipcMain.handle('desktop:pickWorktree', async () =>
  promptForWorkspace(BrowserWindow.getFocusedWindow() || ctx.launcherWindow || null));
ipcMain.handle('desktop:pickWorktreeParent', async () =>
  promptForWorktreeParent(BrowserWindow.getFocusedWindow() || ctx.launcherWindow || null));

ipcMain.handle('desktop:openWorkspace', async (_event, options = {}) => {
  const workspacePath = options.workspacePath
    || await promptForWorkspace(BrowserWindow.getFocusedWindow() || ctx.launcherWindow || null);
  if (!workspacePath) {
    return null;
  }
  const browserWindow = await createWorkspaceWindow({
    workspacePath,
    forceNewWindow: Boolean(options.forceNewWindow),
    backendMode: options.backendMode,
    wslDistro: options.wslDistro,
  });
  return browserWindow ? { ok: true } : null;
});

ipcMain.handle('desktop:openWorktree', async (_event, options = {}) => {
  const workspacePath = options.workspacePath
    || await promptForWorkspace(BrowserWindow.getFocusedWindow() || ctx.launcherWindow || null);
  if (!workspacePath) {
    return null;
  }
  const browserWindow = await createWorkspaceWindow({
    workspacePath,
    forceNewWindow: Boolean(options.forceNewWindow),
  });
  return browserWindow ? { ok: true } : null;
});

ipcMain.handle('desktop:appVersion', async () => app.getVersion());

ipcMain.handle('desktop:openSshWorkspace', async (_event, options = {}) => {
  const browserWindow = await createSSHWorkspaceWindow({
    hostAlias: options.hostAlias,
    remoteWorkspacePath: options.remoteWorkspacePath,
    forceNewWindow: Boolean(options.forceNewWindow),
  });
  return browserWindow ? { ok: true } : null;
});

ipcMain.handle('desktop:createWorktree', async (_event, options = {}) => {
  const workspacePath = createWorktree(options);
  const browserWindow = await createWorkspaceWindow({
    workspacePath,
    forceNewWindow: true,
  });
  return browserWindow ? { ok: true, workspacePath } : null;
});

ipcMain.handle('desktop:installWsl', async () => installWslFromDesktop());
ipcMain.handle('desktop:installGitForWindows', async () => installGitForWindowsFromDesktop());

// ── App lifecycle ─────────────────────────────────────────────────────────────
app.whenReady().then(async () => {
  ctx.isAppReady = true;
  registerDesktopProtocol();
  buildMenu();
  initAutoUpdater();

  while (ctx.pendingOpenTargets.length > 0) {
    const pending = ctx.pendingOpenTargets.shift();
    await handleOpenTarget(pending.candidate, { forceNewWindow: pending.forceNewWindow });
  }

  if (isSmokeTestMode()) {
    const smokeWorkspace = process.env.SPROUT_SMOKE_WORKSPACE;
    if (smokeWorkspace) {
      const resolvedWorkspace = resolveWorkspaceDirectory(smokeWorkspace);
      if (resolvedWorkspace) {
        await createWorkspaceWindow({ workspacePath: resolvedWorkspace });
        return;
      }
    }
    await openInitialWindow();
    return;
  }

  const launchWorkspace = resolveWorkspaceArg(process.argv.slice(1));
  if (launchWorkspace) {
    await createWorkspaceWindow({ workspacePath: launchWorkspace });
  } else {
    let restored = false;
    let openedRecent = false;

    if (!shouldSkipRestore()) {
      restored = await restorePreviousSession();
      if (!restored) {
        openedRecent = await openMostRecentWorkspace();
      }
    }

    if (!restored && !openedRecent) {
      await openInitialWindow();
    }
  }

  app.on('activate', async () => {
    await openInitialWindow();
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

// Guard to prevent a quit→install→quit loop when the update
// state leaks between will-quit firings.
let isInstallingUpdate = false;

app.on('will-quit', (event) => {
  // Install deferred update if one is pending (but only once).
  if (isUpdatePending() && !isInstallingUpdate) {
    console.log('[updater] Installing pending update before quit');
    isInstallingUpdate = true;
    event.preventDefault();
    autoUpdater.quitAndInstall();
    return;
  }

  // Normal cleanup – kill all backend / SSH-tunnel child processes.
  for (const [id, record] of ctx.instanceRegistry) {
    const child = record.child;
    try { child.kill(); } catch (_) { /* already dead */ }
    // Force-kill after 3 s if the process hasn't exited yet.
    const killTimer = setTimeout(() => {
      try { child.kill('SIGKILL'); } catch (_) { /* already dead */ }
    }, 3000);
    // Prevent the timer from keeping the event loop alive.
    if (killTimer.unref) { killTimer.unref(); }
    child.once('exit', () => clearTimeout(killTimer));
    ctx.instanceRegistry.delete(id);
  }
});
