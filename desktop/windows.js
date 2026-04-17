/**
 * BrowserWindow creation: launcher, workspace, and SSH workspace windows, plus menu builder.
 */

const { app, BrowserWindow, dialog, Menu, shell } = require('electron');
const fs = require('node:fs');
const path = require('node:path');
const ctx = require('./context');
const { getWorkspaceKey } = require('./utils');
const {
  getLogDirectory,
  getSavedWindowBounds,
  writeWindowBounds,
  scheduleWindowBoundsPersist,
  addRecentWorktree,
  persistOpenWorkspaces,
  getRecentWorktrees,
} = require('./state-manager');
const { renderLoadingPage, renderErrorPage } = require('./error-pages');
const { startBackendForWorkspace, registerExitHandler } = require('./backend');
const { startSSHBackendForHost } = require('./ssh');
const { resolveWorkspaceDirectory, promptForWorkspace } = require('./workspace');

function isSmokeTestMode() {
  return process.env.LEDIT_SMOKE_TEST === '1';
}

function writeSmokeStatus(payload) {
  if (!isSmokeTestMode()) {
    return;
  }

  const smokeStatusFile = process.env.LEDIT_SMOKE_STATUS_FILE;
  if (!smokeStatusFile) {
    return;
  }

  try {
    fs.writeFileSync(smokeStatusFile, `${JSON.stringify({
      timestamp: new Date().toISOString(),
      ...payload,
    }, null, 2)}\n`, 'utf8');
  } catch (error) {
    console.error('Failed to write smoke status file:', error);
  }
}

function getLauncherPath() {
  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'app.asar', 'desktop', 'launcher.html');
  }
  return path.join(app.getAppPath(), 'desktop', 'launcher.html');
}

function createLauncherWindow() {
  if (ctx.launcherWindow && !ctx.launcherWindow.isDestroyed()) {
    ctx.launcherWindow.show();
    ctx.launcherWindow.focus();
    return ctx.launcherWindow;
  }

  ctx.launcherWindow = new BrowserWindow({
    width: 1080,
    height: 720,
    minWidth: 900,
    minHeight: 620,
    show: isSmokeTestMode(),
    backgroundColor: '#171b22',
    title: 'Ledit Launcher',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });

  ctx.launcherWindow.loadFile(getLauncherPath());
  if (!isSmokeTestMode()) {
    ctx.launcherWindow.once('ready-to-show', () => ctx.launcherWindow.show());
  }
  ctx.launcherWindow.webContents.once('did-finish-load', () => {
    if (ctx.launcherWindow && !ctx.launcherWindow.isDestroyed() && !ctx.launcherWindow.isVisible()) {
      ctx.launcherWindow.show();
    }
    if (ctx.launcherWindow && !ctx.launcherWindow.isDestroyed()) {
      writeSmokeStatus({
        event: 'launcher-loaded',
        title: ctx.launcherWindow.getTitle(),
        bounds: ctx.launcherWindow.getBounds(),
        visible: ctx.launcherWindow.isVisible(),
      });
    }
  });
  ctx.launcherWindow.once('ready-to-show', () => {
    if (ctx.launcherWindow && !ctx.launcherWindow.isDestroyed()) {
      writeSmokeStatus({
        event: 'launcher-ready-to-show',
        title: ctx.launcherWindow.getTitle(),
        bounds: ctx.launcherWindow.getBounds(),
        visible: ctx.launcherWindow.isVisible(),
      });
    }
  });
  ctx.launcherWindow.on('closed', () => {
    ctx.launcherWindow = null;
  });

  return ctx.launcherWindow;
}

function buildMenu() {
  const recentItems = getRecentWorktrees().map((worktreePath) => ({
    label: worktreePath.backendMode === 'wsl'
      ? `${worktreePath.workspacePath} (${worktreePath.wslDistro || 'WSL'})`
      : worktreePath.workspacePath,
    click: () => {
      createWorkspaceWindow({ ...worktreePath }).catch((error) => {
        dialog.showErrorBox('Failed to Open Folder', String(error?.message || error));
      });
    },
  }));

  const template = [
    {
      label: 'File',
      submenu: [
        {
          label: 'Open Folder…',
          accelerator: 'CmdOrCtrl+O',
          click: () => createWorkspaceWindow().catch((error) => {
            dialog.showErrorBox('Failed to Open Folder', String(error?.message || error));
          }),
        },
        {
          label: 'Open Folder in New Window',
          accelerator: 'CmdOrCtrl+Shift+O',
          click: () => createWorkspaceWindow({ forceNewWindow: true }).catch((error) => {
            dialog.showErrorBox('Failed to Open Folder', String(error?.message || error));
          }),
        },
        {
          label: 'Close All Editors',
          accelerator: 'CmdOrCtrl+Shift+W',
          click: () => {
            const win = BrowserWindow.getFocusedWindow();
            if (win && !win.isDestroyed()) {
              win.webContents.send('desktop:hotkey', 'close_all_editors');
            }
          },
        },
        {
          label: 'Close Other Editors',
          accelerator: 'CmdOrCtrl+Alt+W',
          click: () => {
            const win = BrowserWindow.getFocusedWindow();
            if (win && !win.isDestroyed()) {
              win.webContents.send('desktop:hotkey', 'close_other_editors');
            }
          },
        },
        ...(recentItems.length > 0 ? [{ type: 'separator' }, ...recentItems] : []),
        { type: 'separator' },
        { role: 'close' },
        { role: process.platform === 'darwin' ? 'hide' : 'quit' },
      ],
    },
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        { role: 'toggleDevTools' },
        { type: 'separator' },
        {
          label: 'Split Editor Horizontal',
          accelerator: 'CmdOrCtrl+K',
          click: () => {
            const win = BrowserWindow.getFocusedWindow();
            if (win && !win.isDestroyed()) {
              win.webContents.send('desktop:hotkey', 'split_editor_horizontal');
            }
          },
        },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
      ],
    },
    {
      label: 'Window',
      submenu: [
        { role: 'minimize' },
        { role: 'zoom' },
        ...(process.platform === 'darwin' ? [{ type: 'separator' }, { role: 'front' }] : []),
      ],
    },
    {
      role: 'help',
      submenu: [
        {
          label: 'Open Project Homepage',
          click: () => shell.openExternal('https://github.com/alantheprice/ledit'),
        },
      ],
    },
  ];

  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function attachNavigationHandlers(browserWindow) {
  browserWindow.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });

  browserWindow.webContents.on('will-navigate', (event, url) => {
    if (url.startsWith('ledit://reload')) {
      event.preventDefault();
      const record = ctx.instanceRegistry.get(browserWindow.id);
      if (record && record.reloadCallback) {
        record.reloadCallback();
      }
    } else if (url.startsWith('ledit://open-log-dir')) {
      event.preventDefault();
      try {
        const params = new URL(url.replace('ledit://', 'https://ledit/'));
        const dir = params.searchParams.get('dir') || getLogDirectory();
        shell.openPath(dir);
      } catch {
        shell.openPath(getLogDirectory());
      }
    }
  });
}

async function createWorkspaceWindow(options = {}) {
  const backendMode = options.backendMode === 'wsl' ? 'wsl' : 'native';
  const wslDistro = options.wslDistro || null;
  let workspacePath = options.workspacePath || null;

  if (!workspacePath && backendMode === 'native') {
    workspacePath = await promptForWorkspace(BrowserWindow.getFocusedWindow() || null);
  }

  if (!workspacePath) {
    return null;
  }

  if (backendMode === 'native') {
    workspacePath = resolveWorkspaceDirectory(workspacePath);
  }

  if (!workspacePath) {
    throw new Error('Selected working directory does not exist.');
  }

  const workspaceEntry = { workspacePath, backendMode, wslDistro };
  const workspaceKey = getWorkspaceKey(workspaceEntry);

  const existingWindowId = ctx.workspaceWindowMap.get(workspaceKey);
  if (existingWindowId && !options.forceNewWindow) {
    const existing = BrowserWindow.fromId(existingWindowId);
    if (existing) {
      existing.show();
      existing.focus();
      return existing;
    }
  }

  const savedBounds = getSavedWindowBounds(workspaceKey);

  const browserWindow = new BrowserWindow({
    width: savedBounds?.width || 1600,
    height: savedBounds?.height || 980,
    ...(savedBounds && Number.isFinite(savedBounds.x) && Number.isFinite(savedBounds.y)
      ? { x: savedBounds.x, y: savedBounds.y }
      : {}),
    minWidth: 1100,
    minHeight: 700,
    show: false,
    backgroundColor: '#1f242d',
    title: backendMode === 'wsl'
      ? `Ledit · ${path.basename(workspacePath)} (${wslDistro || 'WSL'})`
      : `Ledit · ${path.basename(workspacePath)}`,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });

  browserWindow.loadURL(renderLoadingPage(workspacePath));
  browserWindow.once('ready-to-show', () => browserWindow.show());
  browserWindow.once('ready-to-show', () => {
    if (savedBounds?.isMaximized && !browserWindow.isDestroyed()) {
      browserWindow.maximize();
    }
  });
  attachNavigationHandlers(browserWindow);

  const backend = await startBackendForWorkspace(workspaceEntry);
  ctx.instanceRegistry.set(browserWindow.id, { ...backend, ...workspaceEntry });

  let crashCount = 0;

  const performReload = async () => {
    try {
      const newBackend = await startBackendForWorkspace(workspaceEntry);
      crashCount = 0;
      ctx.instanceRegistry.set(browserWindow.id, { ...newBackend, ...workspaceEntry });
      registerExitHandler(browserWindow, newBackend.port, workspaceEntry, performReload);
      await browserWindow.loadURL(`http://127.0.0.1:${newBackend.port}`);
    } catch (error) {
      crashCount++;
      if (crashCount >= 3) {
        console.error('Backend failed to restart after 3 attempts');
        browserWindow.loadURL(renderErrorPage(workspaceEntry.workspacePath, null, null, true));
        return;
      }
      console.error('Failed to restart backend:', error);
      browserWindow.loadURL(renderErrorPage(workspaceEntry.workspacePath, null, null));
    }
  };

  registerExitHandler(browserWindow, backend.port, workspaceEntry, performReload);

  ctx.workspaceWindowMap.set(workspaceKey, browserWindow.id);
  addRecentWorktree(workspaceEntry);
  persistOpenWorkspaces();
  buildMenu();
  if (ctx.launcherWindow && !ctx.launcherWindow.isDestroyed()) {
    ctx.launcherWindow.close();
  }

  browserWindow.on('move', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('resize', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('maximize', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('unmaximize', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('close', () => {
    writeWindowBounds(workspaceKey, browserWindow);
  });

  browserWindow.on('closed', () => {
    const timer = ctx.windowStateWriteTimers.get(workspaceKey);
    if (timer) {
      clearTimeout(timer);
      ctx.windowStateWriteTimers.delete(workspaceKey);
    }
    const record = ctx.instanceRegistry.get(browserWindow.id);
    if (record) {
      const child = record.child;
      child.kill();
      const killTimer = setTimeout(() => {
        try { child.kill('SIGKILL'); } catch (_) { /* already dead */ }
      }, 3000);
      child.once('exit', () => clearTimeout(killTimer));
      ctx.instanceRegistry.delete(browserWindow.id);
      if (ctx.workspaceWindowMap.get(workspaceKey) === browserWindow.id) {
        ctx.workspaceWindowMap.delete(workspaceKey);
      }
      persistOpenWorkspaces();
      buildMenu();
    }
  });

  await browserWindow.loadURL(`http://127.0.0.1:${backend.port}`);
  return browserWindow;
}

async function createSSHWorkspaceWindow(options = {}) {
  const hostAlias = String(options.hostAlias || '').trim();
  if (!hostAlias) {
    throw new Error('SSH host alias is required.');
  }

  const remoteWorkspacePath = String(options.remoteWorkspacePath || '$HOME').trim() || '$HOME';
  const remoteKey = `ssh:${hostAlias}:${remoteWorkspacePath}`;
  const existingWindowId = ctx.sshWindowMap.get(remoteKey);
  if (existingWindowId && !options.forceNewWindow) {
    const existing = BrowserWindow.fromId(existingWindowId);
    if (existing) {
      existing.show();
      existing.focus();
      return existing;
    }
  }

  const browserWindow = new BrowserWindow({
    width: 1600,
    height: 980,
    minWidth: 1100,
    minHeight: 700,
    show: false,
    backgroundColor: '#1f242d',
    title: `Ledit · ${hostAlias} (SSH)`,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });

  browserWindow.loadURL(renderLoadingPage(`ssh://${hostAlias}`));
  browserWindow.once('ready-to-show', () => browserWindow.show());
  attachNavigationHandlers(browserWindow);

  const backend = await startSSHBackendForHost({ hostAlias, remoteWorkspacePath });
  ctx.instanceRegistry.set(browserWindow.id, {
    ...backend,
    workspacePath: `ssh://${hostAlias}`,
    remoteWorkspacePath,
  });

  const performReload = async () => {
    try {
      const record = ctx.instanceRegistry.get(browserWindow.id);
      if (record?.child) {
        try { record.child.kill(); } catch (_) { /* noop */ }
      }
      const newBackend = await startSSHBackendForHost({ hostAlias, remoteWorkspacePath });
      ctx.instanceRegistry.set(browserWindow.id, {
        ...newBackend,
        workspacePath: `ssh://${hostAlias}`,
        remoteWorkspacePath,
      });
      registerExitHandler(browserWindow, newBackend.port, { workspacePath: `ssh://${hostAlias}` }, performReload);
      await browserWindow.loadURL(`http://127.0.0.1:${newBackend.port}`);
    } catch (error) {
      console.error('Failed to restart SSH backend:', error);
      browserWindow.loadURL(renderErrorPage(`ssh://${hostAlias}`, null, null));
    }
  };

  registerExitHandler(browserWindow, backend.port, { workspacePath: `ssh://${hostAlias}` }, performReload);
  ctx.sshWindowMap.set(remoteKey, browserWindow.id);

  browserWindow.on('closed', () => {
    const record = ctx.instanceRegistry.get(browserWindow.id);
    if (record) {
      if (record.child) {
        try { record.child.kill(); } catch (_) { /* noop */ }
      }
      ctx.instanceRegistry.delete(browserWindow.id);
    }
    if (ctx.sshWindowMap.get(remoteKey) === browserWindow.id) {
      ctx.sshWindowMap.delete(remoteKey);
    }
  });

  await browserWindow.loadURL(`http://127.0.0.1:${backend.port}`);
  return browserWindow;
}

module.exports = {
  getLauncherPath,
  createLauncherWindow,
  buildMenu,
  createWorkspaceWindow,
  createSSHWorkspaceWindow,
};
