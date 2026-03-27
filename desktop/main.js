const { app, BrowserWindow, dialog, Menu, shell, ipcMain } = require('electron');
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const path = require('node:path');

const instanceRegistry = new Map();
const workspaceWindowMap = new Map();
let launcherWindow = null;
const windowStateWriteTimers = new Map();
const pendingOpenTargets = [];
let isAppReady = false;

const gotSingleInstanceLock = app.requestSingleInstanceLock();
if (!gotSingleInstanceLock) {
  app.quit();
}

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

function getUserStatePath() {
  return path.join(app.getPath('userData'), 'desktop-state.json');
}

function readDesktopState() {
  try {
    const state = JSON.parse(fs.readFileSync(getUserStatePath(), 'utf8'));
    return {
      recentWorktrees: Array.isArray(state?.recentWorktrees) ? state.recentWorktrees : [],
      openWorktrees: Array.isArray(state?.openWorktrees) ? state.openWorktrees : [],
      windowBoundsByWorktree: state?.windowBoundsByWorktree || {},
    };
  } catch {
    return { recentWorktrees: [], openWorktrees: [], windowBoundsByWorktree: {} };
  }
}

function writeDesktopState(state) {
  fs.mkdirSync(path.dirname(getUserStatePath()), { recursive: true });
  fs.writeFileSync(getUserStatePath(), JSON.stringify(state, null, 2));
}

function addRecentWorktree(worktreePath) {
  const state = readDesktopState();
  const recent = [worktreePath, ...(state.recentWorktrees || []).filter((item) => item !== worktreePath)].slice(0, 10);
  writeDesktopState({ ...state, recentWorktrees: recent });
}

function persistOpenWorkspaces() {
  const state = readDesktopState();
  const openWorkspaces = Array.from(instanceRegistry.values())
    .map((entry) => entry.workspacePath)
    .filter(Boolean);
  writeDesktopState({
    ...state,
    openWorktrees: openWorkspaces,
  });
}

function getRecentWorktrees() {
  return (readDesktopState().recentWorktrees || []).filter((entry) => {
    try {
      return fs.statSync(entry).isDirectory();
    } catch {
      return false;
    }
  });
}

function getRecentWorktreeEntries() {
  return getRecentWorktrees().map((entry) => ({
    path: entry,
    name: path.basename(entry),
  }));
}

function getRestorableWorktrees() {
  return (readDesktopState().openWorktrees || []).filter((entry) => {
    try {
      return fs.statSync(entry).isDirectory();
    } catch {
      return false;
    }
  });
}

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

function sanitizeWindowBounds(bounds) {
  if (!bounds || typeof bounds !== 'object') {
    return null;
  }

  const width = Number(bounds.width);
  const height = Number(bounds.height);
  if (!Number.isFinite(width) || !Number.isFinite(height)) {
    return null;
  }

  const sanitized = {
    width: Math.max(1100, Math.round(width)),
    height: Math.max(700, Math.round(height)),
    isMaximized: Boolean(bounds.isMaximized),
  };

  const x = Number(bounds.x);
  const y = Number(bounds.y);
  if (Number.isFinite(x) && Number.isFinite(y)) {
    sanitized.x = Math.round(x);
    sanitized.y = Math.round(y);
  }

  return sanitized;
}

function getSavedWindowBounds(worktreePath) {
  const state = readDesktopState();
  return sanitizeWindowBounds(state.windowBoundsByWorktree?.[worktreePath]);
}

function writeWindowBounds(worktreePath, browserWindow) {
  if (!worktreePath || !browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const state = readDesktopState();
  const currentBounds = browserWindow.isMaximized()
    ? browserWindow.getNormalBounds()
    : browserWindow.getBounds();

  const sanitized = sanitizeWindowBounds({
    ...currentBounds,
    isMaximized: browserWindow.isMaximized(),
  });

  writeDesktopState({
    ...state,
    windowBoundsByWorktree: {
      ...(state.windowBoundsByWorktree || {}),
      [worktreePath]: sanitized,
    },
  });
}

function scheduleWindowBoundsPersist(worktreePath, browserWindow) {
  if (!worktreePath || !browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const existingTimer = windowStateWriteTimers.get(worktreePath);
  if (existingTimer) {
    clearTimeout(existingTimer);
  }

  const timer = setTimeout(() => {
    writeWindowBounds(worktreePath, browserWindow);
    windowStateWriteTimers.delete(worktreePath);
  }, 200);

  windowStateWriteTimers.set(worktreePath, timer);
}

function resolveBackendBinary() {
  const platform = process.platform === 'win32' ? 'windows' : process.platform;
  const arch = process.arch === 'x64' ? 'amd64' : process.arch;
  const binaryName = process.platform === 'win32' ? 'ledit.exe' : 'ledit';

  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'backend', `${platform}-${arch}`, binaryName);
  }

  return path.join(app.getAppPath(), 'desktop', 'dist', 'backend', `${platform}-${arch}`, binaryName);
}

function findFreePort() {
  return new Promise((resolvePromise, rejectPromise) => {
    const server = net.createServer();
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      const port = typeof address === 'object' && address ? address.port : 0;
      server.close((err) => {
        if (err) {
          rejectPromise(err);
          return;
        }
        resolvePromise(port);
      });
    });
    server.on('error', rejectPromise);
  });
}

function waitForHealth(port, timeoutMs = 20000) {
  const startedAt = Date.now();

  return new Promise((resolvePromise, rejectPromise) => {
    const probe = () => {
      const request = http.get(`http://127.0.0.1:${port}/health`, (response) => {
        if (response.statusCode === 200) {
          response.resume();
          resolvePromise();
          return;
        }
        response.resume();
        retry();
      });

      request.on('error', retry);
    };

    const retry = () => {
      if (Date.now() - startedAt >= timeoutMs) {
        rejectPromise(new Error(`Timed out waiting for backend on port ${port}`));
        return;
      }
      setTimeout(probe, 300);
    };

    probe();
  });
}

function getGitResolutionPath(candidatePath) {
  if (!candidatePath) {
    return null;
  }

  try {
    const stat = fs.statSync(candidatePath);
    return stat.isDirectory() ? candidatePath : path.dirname(candidatePath);
  } catch {
    return fs.existsSync(path.dirname(candidatePath)) ? path.dirname(candidatePath) : null;
  }
}

function resolveWorkspaceDirectory(candidatePath) {
  return getGitResolutionPath(candidatePath);
}

function validateGitWorktree(worktreePath) {
  const gitPath = getGitResolutionPath(worktreePath);
  if (!gitPath) {
    return { ok: false, error: 'Selected path does not exist.' };
  }

  const gitCheck = spawnSync('git', ['rev-parse', '--is-inside-work-tree'], {
    cwd: gitPath,
    encoding: 'utf8',
  });

  if (gitCheck.status !== 0 || gitCheck.stdout.trim() !== 'true') {
    return { ok: false, error: 'Selected folder is not inside a Git worktree.' };
  }

  const rootCheck = spawnSync('git', ['rev-parse', '--show-toplevel'], {
    cwd: gitPath,
    encoding: 'utf8',
  });

  if (rootCheck.status !== 0) {
    return { ok: false, error: 'Failed to resolve Git worktree root.' };
  }

  return { ok: true, root: rootCheck.stdout.trim() };
}

function resolveGitRoot(candidatePath) {
  const gitPath = getGitResolutionPath(candidatePath);
  if (!gitPath) {
    return { ok: false, error: 'Selected path does not exist.' };
  }

  const rootCheck = spawnSync('git', ['rev-parse', '--show-toplevel'], {
    cwd: gitPath,
    encoding: 'utf8',
  });

  if (rootCheck.status !== 0) {
    return { ok: false, error: 'Selected folder is not inside a Git repository.' };
  }

  return { ok: true, root: rootCheck.stdout.trim() };
}

async function promptForWorkspace(browserWindow) {
  const selection = await dialog.showOpenDialog(browserWindow ?? null, {
    title: 'Open Folder',
    properties: ['openDirectory', 'createDirectory'],
    message: 'Choose the working directory for this Ledit window.',
  });

  if (selection.canceled || selection.filePaths.length === 0) {
    return null;
  }

  return selection.filePaths[0];
}

async function promptForRepository(browserWindow) {
  const selection = await dialog.showOpenDialog(browserWindow ?? null, {
    title: 'Choose Git Repository',
    properties: ['openDirectory', 'createDirectory'],
    message: 'Choose a Git repository or an existing worktree.',
  });

  if (selection.canceled || selection.filePaths.length === 0) {
    return null;
  }

  const candidate = selection.filePaths[0];
  const resolution = resolveGitRoot(candidate);
  if (!resolution.ok) {
    await dialog.showMessageBox(browserWindow ?? null, {
      type: 'error',
      title: 'Invalid Repository',
      message: resolution.error,
      detail: 'Choose a folder that is already part of a Git repository.',
    });
    return null;
  }

  return resolution.root;
}

async function promptForWorktreeParent(browserWindow) {
  const selection = await dialog.showOpenDialog(browserWindow ?? null, {
    title: 'Choose Worktree Parent Folder',
    properties: ['openDirectory', 'createDirectory'],
    message: 'Choose the parent directory for the new worktree.',
  });

  if (selection.canceled || selection.filePaths.length === 0) {
    return null;
  }

  return selection.filePaths[0];
}

function createWorktree(options = {}) {
  const repositoryPath = options.repositoryPath;
  const worktreePath = options.worktreePath;
  const branchName = options.branchName;
  const baseRef = options.baseRef;

  if (!repositoryPath || !repositoryPath.trim()) {
    throw new Error('A repository path is required.');
  }

  const resolvedRepository = resolveGitRoot(repositoryPath);
  if (!resolvedRepository.ok) {
    throw new Error(resolvedRepository.error);
  }

  if (!branchName || !branchName.trim()) {
    throw new Error('A branch name is required.');
  }

  if (!worktreePath || !worktreePath.trim()) {
    throw new Error('A worktree path is required.');
  }

  const targetPath = path.resolve(worktreePath.trim());
  const targetParent = path.dirname(targetPath);
  fs.mkdirSync(targetParent, { recursive: true });

  if (fs.existsSync(targetPath)) {
    const stat = fs.statSync(targetPath);
    if (!stat.isDirectory()) {
      throw new Error('The target worktree path already exists and is not a directory.');
    }
    const existingEntries = fs.readdirSync(targetPath);
    if (existingEntries.length > 0) {
      throw new Error('The target worktree directory already exists and is not empty.');
    }
  }

  const args = ['worktree', 'add', '-b', branchName.trim(), targetPath];
  if (baseRef && baseRef.trim()) {
    args.push(baseRef.trim());
  }

  const result = spawnSync('git', args, {
    cwd: resolvedRepository.root,
    encoding: 'utf8',
  });

  if (result.status !== 0) {
    const detail = result.stderr?.trim() || result.stdout?.trim() || 'git worktree add failed.';
    throw new Error(detail);
  }

  const validation = validateGitWorktree(targetPath);
  if (!validation.ok) {
    throw new Error(validation.error);
  }

  return validation.root;
}

function getLauncherPath() {
  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'app.asar', 'desktop', 'launcher.html');
  }
  return path.join(app.getAppPath(), 'desktop', 'launcher.html');
}

function createLauncherWindow() {
  if (launcherWindow && !launcherWindow.isDestroyed()) {
    launcherWindow.show();
    launcherWindow.focus();
    return launcherWindow;
  }

  launcherWindow = new BrowserWindow({
    width: 1080,
    height: 720,
    minWidth: 900,
    minHeight: 620,
    show: false,
    backgroundColor: '#171b22',
    title: 'Ledit Launcher',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });

  launcherWindow.loadFile(getLauncherPath());
  launcherWindow.once('ready-to-show', () => launcherWindow.show());
  launcherWindow.on('closed', () => {
    launcherWindow = null;
  });

  return launcherWindow;
}

function buildMenu() {
  const recentItems = getRecentWorktrees().map((worktreePath) => ({
    label: worktreePath,
    click: () => {
      createWorkspaceWindow({ workspacePath: worktreePath }).catch((error) => {
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

async function startBackendForWorkspace(workspacePath) {
  const port = await findFreePort();
  const binaryPath = resolveBackendBinary();

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Desktop backend binary not found: ${binaryPath}. Run "npm run build:desktop:backend" first.`);
  }

  const child = spawn(binaryPath, ['--isolated-config', 'agent', '--daemon', '--web-port', String(port)], {
    cwd: workspacePath,
    env: {
      ...process.env,
      LEDIT_DESKTOP: '1',
      BROWSER: 'none',
    },
    stdio: 'ignore',
    windowsHide: true,
  });

  child.unref();

  try {
    await waitForHealth(port);
  } catch (error) {
    child.kill();
    throw error;
  }

  return { child, port };
}

function renderLoadingPage(workspacePath) {
  return `data:text/html;charset=UTF-8,${encodeURIComponent(`
    <!doctype html>
    <html>
      <body style="margin:0;font-family:sans-serif;background:#1f242d;color:#d6deeb;display:flex;align-items:center;justify-content:center;height:100vh;">
        <div style="text-align:center;">
          <div style="font-size:18px;font-weight:600;margin-bottom:8px;">Starting Ledit…</div>
          <div style="font-size:13px;opacity:.75;">${workspacePath}</div>
        </div>
      </body>
    </html>
  `)}`;
}

async function createWorkspaceWindow(options = {}) {
  let workspacePath = options.workspacePath || null;

  if (!workspacePath) {
    workspacePath = await promptForWorkspace(BrowserWindow.getFocusedWindow() || null);
  }

  if (!workspacePath) {
    return null;
  }

  workspacePath = resolveWorkspaceDirectory(workspacePath);
  if (!workspacePath) {
    throw new Error('Selected working directory does not exist.');
  }

  const existingWindowId = workspaceWindowMap.get(workspacePath);
  if (existingWindowId && !options.forceNewWindow) {
    const existing = BrowserWindow.fromId(existingWindowId);
    if (existing) {
      existing.show();
      existing.focus();
      return existing;
    }
  }

  const savedBounds = getSavedWindowBounds(workspacePath);

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
    title: `Ledit · ${path.basename(workspacePath)}`,
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
  browserWindow.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });

  const backend = await startBackendForWorkspace(workspacePath);
  instanceRegistry.set(browserWindow.id, { ...backend, workspacePath });
  workspaceWindowMap.set(workspacePath, browserWindow.id);
  addRecentWorktree(workspacePath);
  persistOpenWorkspaces();
  buildMenu();
  if (launcherWindow && !launcherWindow.isDestroyed()) {
    launcherWindow.close();
  }

  browserWindow.on('move', () => scheduleWindowBoundsPersist(workspacePath, browserWindow));
  browserWindow.on('resize', () => scheduleWindowBoundsPersist(workspacePath, browserWindow));
  browserWindow.on('maximize', () => scheduleWindowBoundsPersist(workspacePath, browserWindow));
  browserWindow.on('unmaximize', () => scheduleWindowBoundsPersist(workspacePath, browserWindow));
  browserWindow.on('close', () => {
    writeWindowBounds(workspacePath, browserWindow);
  });

  browserWindow.on('closed', () => {
    const timer = windowStateWriteTimers.get(workspacePath);
    if (timer) {
      clearTimeout(timer);
      windowStateWriteTimers.delete(workspacePath);
    }
    const record = instanceRegistry.get(browserWindow.id);
    if (record) {
      record.child.kill();
      instanceRegistry.delete(browserWindow.id);
      if (workspaceWindowMap.get(record.workspacePath) === browserWindow.id) {
        workspaceWindowMap.delete(record.workspacePath);
      }
      persistOpenWorkspaces();
      buildMenu();
    }
  });

  await browserWindow.loadURL(`http://127.0.0.1:${backend.port}`);
  return browserWindow;
}

async function openInitialWindow() {
  if (BrowserWindow.getAllWindows().length > 0) {
    return;
  }
  createLauncherWindow();
}

async function restorePreviousSession() {
  const restorable = getRestorableWorktrees();
  if (restorable.length === 0) {
    return false;
  }

  let opened = 0;
  for (const workspacePath of restorable) {
    try {
      const result = await createWorkspaceWindow({ workspacePath, forceNewWindow: true });
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

  if (!isAppReady) {
    pendingOpenTargets.push({ candidate, forceNewWindow: Boolean(options.forceNewWindow) });
    return;
  }

  const browserWindow = await openWorkspaceFromTarget(candidate, options);
  if (!browserWindow && !BrowserWindow.getAllWindows().length) {
    await openInitialWindow();
  }
}

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

ipcMain.handle('desktop:listRecentWorktrees', async () => getRecentWorktreeEntries());
ipcMain.handle('desktop:pickRepository', async () => promptForRepository(BrowserWindow.getFocusedWindow() || launcherWindow || null));
ipcMain.handle('desktop:pickWorkspace', async () => promptForWorkspace(BrowserWindow.getFocusedWindow() || launcherWindow || null));
ipcMain.handle('desktop:pickWorktree', async () => promptForWorkspace(BrowserWindow.getFocusedWindow() || launcherWindow || null));
ipcMain.handle('desktop:pickWorktreeParent', async () => promptForWorktreeParent(BrowserWindow.getFocusedWindow() || launcherWindow || null));
ipcMain.handle('desktop:openWorkspace', async (_event, options = {}) => {
  const workspacePath = options.workspacePath || await promptForWorkspace(BrowserWindow.getFocusedWindow() || launcherWindow || null);
  if (!workspacePath) {
    return null;
  }
  const browserWindow = await createWorkspaceWindow({
    workspacePath,
    forceNewWindow: Boolean(options.forceNewWindow),
  });
  return browserWindow ? { ok: true } : null;
});
ipcMain.handle('desktop:openWorktree', async (_event, options = {}) => {
  const workspacePath = options.workspacePath || await promptForWorkspace(BrowserWindow.getFocusedWindow() || launcherWindow || null);
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
ipcMain.handle('desktop:createWorktree', async (_event, options = {}) => {
  const workspacePath = createWorktree(options);
  const browserWindow = await createWorkspaceWindow({
    workspacePath,
    forceNewWindow: true,
  });
  return browserWindow ? { ok: true, workspacePath } : null;
});

app.whenReady().then(async () => {
  isAppReady = true;
  registerDesktopProtocol();
  buildMenu();
  while (pendingOpenTargets.length > 0) {
    const pending = pendingOpenTargets.shift();
    await handleOpenTarget(pending.candidate, { forceNewWindow: pending.forceNewWindow });
  }
  const launchWorkspace = resolveWorkspaceArg(process.argv.slice(1));
  if (launchWorkspace) {
    await createWorkspaceWindow({ workspacePath: launchWorkspace });
  } else {
    const restored = await restorePreviousSession();
    if (!restored) {
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
