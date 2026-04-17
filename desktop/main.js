/**
 * Electron main process entry point.
 * Orchestrates the app lifecycle, IPC handlers, and startup logic.
 * Heavy lifting is delegated to focused modules in this directory.
 */

const { app, BrowserWindow, dialog, ipcMain } = require('electron');
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
const { initAutoUpdater } = require('./updater');

// ── Single-instance lock ──────────────────────────────────────────────────────
const gotSingleInstanceLock = app.requestSingleInstanceLock();
if (!gotSingleInstanceLock) {
  app.quit();
}

// ── Protocol registration ─────────────────────────────────────────────────────
function registerDesktopProtocol() {
  if (app.isPackaged) {
    app.setAsDefaultProtocolClient('ledit');
    return;
  }

  if (process.defaultApp && process.argv.length >= 2) {
    app.setAsDefaultProtocolClient('ledit', process.execPath, [path.resolve(process.argv[1])]);
    return;
  }

  app.setAsDefaultProtocolClient('ledit');
}

// ── URL / path helpers ────────────────────────────────────────────────────────
function extractWorkspacePathFromOpenTarget(candidate) {
  if (!candidate) {
    return null;
  }

  if (candidate.startsWith('ledit://')) {
    try {
      const parsed = new URL(candidate);
      const requestedPath = parsed.searchParams.get('path') || parsed.searchParams.get('workspace');
      if (!requestedPath) {
        return null;
      }
      return path.resolve(requestedPath);
    } catch {
      return null;
    }
  }

  return path.resolve(candidate);
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
  for (const workspacePath of restorable) {
    try {
      const result = await createWorkspaceWindow({ ...workspacePath, forceNewWindow: true });
      if (result) {
        opened += 1;
      }
    } catch (error) {
      console.error(`Failed to restore worktree ${workspacePath}:`, error);
    }
  }

  return opened > 0;
}

function resolveWorkspaceArg(argv) {
  for (const arg of argv) {
    if (!arg || arg.startsWith('-')) {
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

  const launchWorkspace = resolveWorkspaceArg(process.argv.slice(1));
  if (launchWorkspace) {
    await createWorkspaceWindow({ workspacePath: launchWorkspace });
  } else {
    const restored = await restorePreviousSession();
    if (!restored) {
      const openedRecent = await openMostRecentWorkspace();
      if (!openedRecent) {
        await openInitialWindow();
      }
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

app.on('will-quit', () => {
  for (const [id, record] of ctx.instanceRegistry) {
    const child = record.child;
    child.kill();
    const killTimer = setTimeout(() => {
      try { child.kill('SIGKILL'); } catch (_) { /* already dead */ }
    }, 3000);
    child.once('exit', () => clearTimeout(killTimer));
    ctx.instanceRegistry.delete(id);
  }
});
