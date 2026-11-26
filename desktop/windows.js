/**
 * BrowserWindow creation: launcher, workspace, and SSH workspace windows, plus menu builder.
 */

const { app, BrowserWindow, dialog, Menu, shell } = require('electron');
const fs = require('node:fs');
const path = require('node:path');
const ctx = require('./context');
const { getProjectFolderKey } = require('./utils');
const {
  getLogDirectory,
  getSavedWindowBounds,
  writeWindowBounds,
  scheduleWindowBoundsPersist,
  addRecentWorktree,
  persistOpenProjectFolders,
  getRecentWorktrees,
} = require('./state-manager');
const { renderLoadingPage, renderErrorPage } = require('./error-pages');
const { startBackendForProjectFolder, registerExitHandler, generateSecret, closeSocketProxy } = require('./backend');
const { startSSHBackendForHost } = require('./ssh');
const { resolveProjectFolderDirectory, promptForProjectFolder } = require('./projectFolder');

function isSmokeTestMode() {
  return process.env.SPROUT_SMOKE_TEST === '1';
}

function writeSmokeStatus(payload) {
  if (!isSmokeTestMode()) {
    return;
  }

  const smokeStatusFile = process.env.SPROUT_SMOKE_STATUS_FILE;
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
    title: 'Sprout Launcher',
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
      ? `${worktreePath.projectFolderPath} (${worktreePath.wslDistro || 'WSL'})`
      : worktreePath.projectFolderPath,
    click: () => {
      createProjectFolderWindow({ ...worktreePath }).catch((error) => {
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
          click: () => createProjectFolderWindow().catch((error) => {
            dialog.showErrorBox('Failed to Open Folder', String(error?.message || error));
          }),
        },
        {
          label: 'Open Folder in New Window',
          accelerator: 'CmdOrCtrl+Shift+O',
          click: () => createProjectFolderWindow({ forceNewWindow: true }).catch((error) => {
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
          label: 'Check for Updates...',
          click: async () => {
            const mainWindow = BrowserWindow.getFocusedWindow();
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('desktop:trigger-update-check');
            }
          },
        },
        {
          label: 'Open Project Homepage',
          click: () => shell.openExternal('https://github.com/sprout-foundry/sprout'),
        },
        { type: 'separator' },
        {
          label: `Version ${app.getVersion()}`,
          enabled: false,
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
    if (url.startsWith('sprout://reload')) {
      event.preventDefault();
      const record = ctx.instanceRegistry.get(browserWindow.id);
      if (record && record.reloadCallback) {
        record.reloadCallback();
      }
    } else if (url.startsWith('sprout://open-log-dir')) {
      event.preventDefault();
      try {
        const params = new URL(url.replace('sprout://', 'https://sprout/'));
        const dir = params.searchParams.get('dir') || getLogDirectory();
        shell.openPath(dir);
      } catch {
        shell.openPath(getLogDirectory());
      }
    }
  });
}

function injectAuthHeaders(browserWindow) {
  const session = browserWindow.webContents.session;
  const token = generateSecret();
  session.webRequest.onBeforeSendHeaders(
    { urls: ['http://127.0.0.1:*/*'] },
    (details, callback) => {
      const headers = { ...details.requestHeaders };
      headers['Authorization'] = `Bearer ${token}`;
      callback({ requestHeaders: headers });
    }
  );
}

async function createProjectFolderWindow(options = {}) {
  const backendMode = options.backendMode === 'wsl' ? 'wsl' : 'native';
  const wslDistro = options.wslDistro || null;
  let projectFolderPath = options.projectFolderPath || null;

  if (!projectFolderPath && backendMode === 'native') {
    projectFolderPath = await promptForProjectFolder(BrowserWindow.getFocusedWindow() || null);
  }

  if (!projectFolderPath) {
    return null;
  }

  if (backendMode === 'native') {
    projectFolderPath = resolveProjectFolderDirectory(projectFolderPath);
  }

  if (!projectFolderPath) {
    throw new Error('Selected working directory does not exist.');
  }

  const projectFolderEntry = { projectFolderPath, backendMode, wslDistro };
  const projectFolderKey = getProjectFolderKey(projectFolderEntry);

  const existingWindowId = ctx.projectFolderWindowMap.get(projectFolderKey);
  if (existingWindowId && !options.forceNewWindow) {
    const existing = BrowserWindow.fromId(existingWindowId);
    if (existing) {
      existing.show();
      existing.focus();
      return existing;
    }
  }

  const savedBounds = getSavedWindowBounds(projectFolderKey);

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
      ? `Sprout · ${path.basename(projectFolderPath)} (${wslDistro || 'WSL'})`
      : `Sprout · ${path.basename(projectFolderPath)}`,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });

  browserWindow.loadURL(renderLoadingPage(projectFolderPath));
  browserWindow.once('ready-to-show', () => browserWindow.show());
  browserWindow.once('ready-to-show', () => {
    if (savedBounds?.isMaximized && !browserWindow.isDestroyed()) {
      browserWindow.maximize();
    }
  });
  attachNavigationHandlers(browserWindow);
  injectAuthHeaders(browserWindow);

  const backend = await startBackendForProjectFolder(projectFolderEntry);
  ctx.instanceRegistry.set(browserWindow.id, { ...backend, ...projectFolderEntry });

  let crashCount = 0;

  const performReload = async () => {
    try {
      // Close the old proxy server (macOS/Linux native) before restarting
      const oldRecord = ctx.instanceRegistry.get(browserWindow.id);
      if (oldRecord?.proxyServer) {
        closeSocketProxy(oldRecord.proxyServer);
      }
      const newBackend = await startBackendForProjectFolder(projectFolderEntry);
      crashCount = 0;
      ctx.instanceRegistry.set(browserWindow.id, { ...newBackend, ...projectFolderEntry });
      registerExitHandler(browserWindow, newBackend.port, projectFolderEntry, performReload);
      await browserWindow.loadURL(`http://127.0.0.1:${newBackend.port}`);
    } catch (error) {
      crashCount++;
      if (crashCount >= 3) {
        console.error('Backend failed to restart after 3 attempts');
        browserWindow.loadURL(renderErrorPage(projectFolderEntry.projectFolderPath, null, null, true));
        return;
      }
      console.error('Failed to restart backend:', error);
      browserWindow.loadURL(renderErrorPage(projectFolderEntry.projectFolderPath, null, null));
    }
  };

  registerExitHandler(browserWindow, backend.port, projectFolderEntry, performReload);

  ctx.projectFolderWindowMap.set(projectFolderKey, browserWindow.id);
  addRecentWorktree(projectFolderEntry);
  persistOpenProjectFolders();
  buildMenu();
  if (ctx.launcherWindow && !ctx.launcherWindow.isDestroyed()) {
    ctx.launcherWindow.close();
  }

  browserWindow.on('move', () => scheduleWindowBoundsPersist(projectFolderKey, browserWindow));
  browserWindow.on('resize', () => scheduleWindowBoundsPersist(projectFolderKey, browserWindow));
  browserWindow.on('maximize', () => scheduleWindowBoundsPersist(projectFolderKey, browserWindow));
  browserWindow.on('unmaximize', () => scheduleWindowBoundsPersist(projectFolderKey, browserWindow));
  browserWindow.on('close', () => {
    writeWindowBounds(projectFolderKey, browserWindow);
  });

  browserWindow.on('closed', () => {
    const timer = ctx.windowStateWriteTimers.get(projectFolderKey);
    if (timer) {
      clearTimeout(timer);
      ctx.windowStateWriteTimers.delete(projectFolderKey);
    }
    const record = ctx.instanceRegistry.get(browserWindow.id);
    if (record) {
      // Close the proxy server (macOS/Linux native) before killing the child
      if (record.proxyServer) {
        closeSocketProxy(record.proxyServer);
      }
      const child = record.child;
      child.kill();
      const killTimer = setTimeout(() => {
        try { child.kill('SIGKILL'); } catch (_) { /* already dead */ }
      }, 3000);
      child.once('exit', () => clearTimeout(killTimer));
      ctx.instanceRegistry.delete(browserWindow.id);
      if (ctx.projectFolderWindowMap.get(projectFolderKey) === browserWindow.id) {
        ctx.projectFolderWindowMap.delete(projectFolderKey);
      }
      persistOpenProjectFolders();
      buildMenu();
    }
  });

  if (isSmokeTestMode()) {
    writeSmokeStatus({
      event: 'project-folder-opened',
      port: backend.port,
      projectFolderPath,
      url: `http://127.0.0.1:${backend.port}`,
    });
  }

  await browserWindow.loadURL(`http://127.0.0.1:${backend.port}`);
  return browserWindow;
}

async function createSSHProjectFolderWindow(options = {}) {
  const hostAlias = String(options.hostAlias || '').trim();
  if (!hostAlias) {
    throw new Error('SSH host alias is required.');
  }

  const remoteProjectFolderPath = String(options.remoteProjectFolderPath || '$HOME').trim() || '$HOME';
  const remoteKey = `ssh:${hostAlias}:${remoteProjectFolderPath}`;
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
    title: `Sprout · ${hostAlias} (SSH)`,
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
  injectAuthHeaders(browserWindow);

  const backend = await startSSHBackendForHost({ hostAlias, remoteProjectFolderPath });
  ctx.instanceRegistry.set(browserWindow.id, {
    ...backend,
    projectFolderPath: `ssh://${hostAlias}`,
    remoteProjectFolderPath,
  });

  const performReload = async () => {
    try {
      const record = ctx.instanceRegistry.get(browserWindow.id);
      if (record?.child) {
        try { record.child.kill(); } catch (_) { /* noop */ }
      }
      const newBackend = await startSSHBackendForHost({ hostAlias, remoteProjectFolderPath });
      ctx.instanceRegistry.set(browserWindow.id, {
        ...newBackend,
        projectFolderPath: `ssh://${hostAlias}`,
        remoteProjectFolderPath,
      });
      registerExitHandler(browserWindow, newBackend.port, { projectFolderPath: `ssh://${hostAlias}` }, performReload);
      await browserWindow.loadURL(`http://127.0.0.1:${newBackend.port}`);
    } catch (error) {
      console.error('Failed to restart SSH backend:', error);
      browserWindow.loadURL(renderErrorPage(`ssh://${hostAlias}`, null, null));
    }
  };

  registerExitHandler(browserWindow, backend.port, { projectFolderPath: `ssh://${hostAlias}` }, performReload);
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
  createProjectFolderWindow,
  createSSHProjectFolderWindow,
  injectAuthHeaders,
};
