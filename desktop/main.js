const { app, BrowserWindow, dialog, Menu, shell, ipcMain } = require('electron');
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const { createWriteStream } = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const path = require('node:path');

const instanceRegistry = new Map();
const workspaceWindowMap = new Map();
const sshWindowMap = new Map();
let launcherWindow = null;
const windowStateWriteTimers = new Map();
const pendingOpenTargets = [];
let isAppReady = false;

function shellEscape(value) {
  return `'${String(value).replace(/'/g, `'\\''`)}'`;
}

function normalizeWorkspaceEntry(entry) {
  if (!entry) {
    return null;
  }

  if (typeof entry === 'string') {
    return {
      workspacePath: entry,
      backendMode: 'native',
      wslDistro: null,
    };
  }

  if (typeof entry.workspacePath !== 'string' || !entry.workspacePath.trim()) {
    return null;
  }

  return {
    workspacePath: entry.workspacePath,
    backendMode: entry.backendMode === 'wsl' ? 'wsl' : 'native',
    wslDistro: typeof entry.wslDistro === 'string' && entry.wslDistro.trim() ? entry.wslDistro.trim() : null,
  };
}

function getWorkspaceKey(entry) {
  const normalized = normalizeWorkspaceEntry(entry);
  if (!normalized) {
    return '';
  }

  return JSON.stringify({
    workspacePath: normalized.workspacePath,
    backendMode: normalized.backendMode,
    wslDistro: normalized.wslDistro || '',
  });
}

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

function getLogDirectory() {
  const dir = path.join(app.getPath('userData'), 'logs');
  fs.mkdirSync(dir, { recursive: true });
  return dir;
}

function openBackendLogStream(label) {
  const dir = getLogDirectory();
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
  const logPath = path.join(dir, `backend-${label}-${timestamp}.log`);
  const stream = createWriteStream(logPath, { flags: 'a' });
  stream.on('error', () => {});
  console.log(`[desktop] backend log: ${logPath}`);
  return stream;
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
  const normalizedEntry = normalizeWorkspaceEntry(worktreePath);
  if (!normalizedEntry) {
    return;
  }

  const state = readDesktopState();
  const normalizedKey = getWorkspaceKey(normalizedEntry);
  const recent = [
    normalizedEntry,
    ...(state.recentWorktrees || [])
      .map((item) => normalizeWorkspaceEntry(item))
      .filter((item) => item && getWorkspaceKey(item) !== normalizedKey),
  ].slice(0, 10);
  writeDesktopState({ ...state, recentWorktrees: recent });
}

function persistOpenWorkspaces() {
  const state = readDesktopState();
  const openWorkspaces = Array.from(instanceRegistry.values())
    .map((entry) => normalizeWorkspaceEntry(entry))
    .filter(Boolean);
  writeDesktopState({
    ...state,
    openWorktrees: openWorkspaces,
  });
}

function getRecentWorktrees() {
  return (readDesktopState().recentWorktrees || [])
    .map((entry) => normalizeWorkspaceEntry(entry))
    .filter(Boolean);
}

function getRecentWorktreeEntries() {
  return getRecentWorktrees().map((entry) => ({
    path: entry.workspacePath,
    name: path.basename(entry.workspacePath),
    backendMode: entry.backendMode,
    wslDistro: entry.wslDistro,
  }));
}

function getRestorableWorktrees() {
  return (readDesktopState().openWorktrees || [])
    .map((entry) => normalizeWorkspaceEntry(entry))
    .filter(Boolean);
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
  const platform = arguments[0] || (process.platform === 'win32' ? 'windows' : process.platform);
  const arch = arguments[1] || (process.arch === 'x64' ? 'amd64' : process.arch);
  const binaryName = platform === 'windows' ? 'ledit.exe' : 'ledit';

  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'backend', `${platform}-${arch}`, binaryName);
  }

  return path.join(app.getAppPath(), 'desktop', 'dist', 'backend', `${platform}-${arch}`, binaryName);
}

function listWslDistros() {
  if (process.platform !== 'win32') {
    return [];
  }

  const result = spawnSync('wsl.exe', ['-l', '-q'], {
    encoding: 'utf8',
    windowsHide: true,
  });
  if (result.status !== 0) {
    return [];
  }

  return result.stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function getHomeDir() {
  return app.getPath('home');
}

function expandHomePath(inputPath) {
  if (!inputPath) {
    return inputPath;
  }
  if (inputPath === '~') {
    return getHomeDir();
  }
  if (inputPath.startsWith('~/')) {
    return path.join(getHomeDir(), inputPath.slice(2));
  }
  return inputPath;
}

function resolveIncludePattern(baseDir, includePattern) {
  const expanded = expandHomePath(includePattern.trim());
  if (!expanded) {
    return [];
  }

  const isAbsolute = path.isAbsolute(expanded);
  const fullPattern = isAbsolute ? expanded : path.join(baseDir, expanded);
  if (!/[*?]/.test(fullPattern)) {
    return fs.existsSync(fullPattern) ? [fullPattern] : [];
  }

  const directory = path.dirname(fullPattern);
  const basenamePattern = path.basename(fullPattern)
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*/g, '.*')
    .replace(/\?/g, '.');
  const matcher = new RegExp(`^${basenamePattern}$`);

  try {
    return fs.readdirSync(directory)
      .filter((entry) => matcher.test(entry))
      .map((entry) => path.join(directory, entry));
  } catch {
    return [];
  }
}

function parseSSHConfigFile(filePath, hostsMap, visited = new Set()) {
  const resolvedPath = expandHomePath(filePath);
  if (!resolvedPath || visited.has(resolvedPath) || !fs.existsSync(resolvedPath)) {
    return;
  }

  visited.add(resolvedPath);

  const lines = fs.readFileSync(resolvedPath, 'utf8').split(/\r?\n/);
  let currentAliases = [];

  for (const rawLine of lines) {
    const line = rawLine.replace(/^\s+|\s+$/g, '');
    if (!line || line.startsWith('#')) {
      continue;
    }

    const includeMatch = line.match(/^Include\s+(.+)$/i);
    if (includeMatch) {
      const includePatterns = includeMatch[1].split(/\s+/).filter(Boolean);
      for (const includePattern of includePatterns) {
        for (const includePath of resolveIncludePattern(path.dirname(resolvedPath), includePattern)) {
          parseSSHConfigFile(includePath, hostsMap, visited);
        }
      }
      continue;
    }

    const hostMatch = line.match(/^Host\s+(.+)$/i);
    if (hostMatch) {
      currentAliases = hostMatch[1]
        .split(/\s+/)
        .map((token) => token.trim())
        .filter((token) => token && !/[*!?]/.test(token) && !token.startsWith('!'));

      for (const alias of currentAliases) {
        if (!hostsMap.has(alias)) {
          hostsMap.set(alias, {
            alias,
            hostname: '',
            user: '',
            port: '',
          });
        }
      }
      continue;
    }

    if (currentAliases.length === 0) {
      continue;
    }

    const kvMatch = line.match(/^(\S+)\s+(.+)$/);
    if (!kvMatch) {
      continue;
    }

    const key = kvMatch[1].toLowerCase();
    const value = kvMatch[2].trim();
    if (!['hostname', 'user', 'port'].includes(key)) {
      continue;
    }

    for (const alias of currentAliases) {
      const existing = hostsMap.get(alias);
      if (!existing) {
        continue;
      }
      if (key === 'hostname' && !existing.hostname) {
        existing.hostname = value;
      }
      if (key === 'user' && !existing.user) {
        existing.user = value;
      }
      if (key === 'port' && !existing.port) {
        existing.port = value;
      }
    }
  }
}

function listSshHosts() {
  const configPath = path.join(getHomeDir(), '.ssh', 'config');
  const hostsMap = new Map();
  parseSSHConfigFile(configPath, hostsMap);
  return Array.from(hostsMap.values())
    .sort((a, b) => a.alias.localeCompare(b.alias));
}

function normalizeRemotePlatform(rawPlatform) {
  const normalized = String(rawPlatform || '').trim().toLowerCase();
  if (normalized === 'linux') {
    return 'linux';
  }
  if (normalized === 'darwin') {
    return 'darwin';
  }
  return '';
}

function normalizeRemoteArch(rawArch) {
  const normalized = String(rawArch || '').trim().toLowerCase();
  if (normalized === 'x86_64' || normalized === 'amd64') {
    return 'amd64';
  }
  if (normalized === 'aarch64' || normalized === 'arm64') {
    return 'arm64';
  }
  return '';
}

function ensureRemoteBackendBinaryArtifact(platform, arch) {
  const normalizedPlatform = platform || 'linux';
  const normalizedArch = arch || 'amd64';
  const outputPath = resolveBackendBinary(normalizedPlatform, normalizedArch);
  if (fs.existsSync(outputPath)) {
    return outputPath;
  }

  if (app.isPackaged) {
    throw new Error(`Missing bundled ${normalizedPlatform} backend binary for architecture ${normalizedArch}.`);
  }

  const outputDir = path.dirname(outputPath);
  fs.mkdirSync(outputDir, { recursive: true });
  const build = spawnSync('go', ['build', '-o', outputPath, '.'], {
    cwd: app.getAppPath(),
    encoding: 'utf8',
    env: {
      ...process.env,
      GOOS: normalizedPlatform,
      GOARCH: normalizedArch,
      CGO_ENABLED: process.env.CGO_ENABLED || '0',
    },
  });

  if (build.status !== 0 || !fs.existsSync(outputPath)) {
    throw new Error(build.stderr?.trim() || build.stdout?.trim() || `Failed to build ${normalizedPlatform} backend for ${normalizedArch}.`);
  }

  return outputPath;
}

function runSSH(args, options = {}) {
  return spawnSync('ssh', args, {
    encoding: 'utf8',
    windowsHide: true,
    ...options,
  });
}

function runSCP(args, options = {}) {
  return spawnSync('scp', args, {
    encoding: 'utf8',
    windowsHide: true,
    ...options,
  });
}

function ensureSSHClientAvailable() {
  const locator = process.platform === 'win32' ? 'where' : 'which';
  const sshProbe = spawnSync(locator, ['ssh'], { stdio: 'ignore', windowsHide: true });
  if (sshProbe.status !== 0) {
    throw new Error('ssh is not available on this machine.');
  }
  const scpProbe = spawnSync(locator, ['scp'], { stdio: 'ignore', windowsHide: true });
  if (scpProbe.status !== 0) {
    throw new Error('scp is not available on this machine.');
  }
}

function inspectRemoteSSHHost(hostAlias) {
  const result = runSSH([
    '-o', 'BatchMode=yes',
    '-o', 'StrictHostKeyChecking=accept-new',
    hostAlias,
    'bash', '-lc',
    'uname -s; uname -m; printf "%s\\n" "$HOME"',
  ]);

  if (result.status !== 0) {
    throw new Error(result.stderr?.trim() || result.stdout?.trim() || `Failed to inspect remote host ${hostAlias}.`);
  }

  const [platform, archRaw, homeDir] = result.stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);

  const normalizedPlatform = normalizeRemotePlatform(platform);
  if (!normalizedPlatform) {
    throw new Error(`Remote host ${hostAlias} is ${platform || 'unknown'}; only Linux and macOS SSH targets are supported.`);
  }

  const arch = normalizeRemoteArch(archRaw);
  if (!arch) {
    throw new Error(`Unsupported remote architecture: ${archRaw || 'unknown'}.`);
  }

  return {
    platform: normalizedPlatform,
    arch,
    homeDir: homeDir || '~',
  };
}

function ensureRemoteBackendBinary(hostAlias, appVersion, remotePlatform, remoteArch) {
  const localBinary = ensureRemoteBackendBinaryArtifact(remotePlatform, remoteArch);
  const remoteDirForSSH = `$HOME/.cache/ledit-desktop/backend/${appVersion}/${remotePlatform}-${remoteArch}`;
  const remoteDirForSCP = `~/.cache/ledit-desktop/backend/${appVersion}/${remotePlatform}-${remoteArch}`;
  const remoteBinaryForSSH = `${remoteDirForSSH}/ledit`;
  const remoteBinaryForSCP = `${remoteDirForSCP}/ledit`;

  const mkdir = runSSH([
    '-o', 'BatchMode=yes',
    '-o', 'StrictHostKeyChecking=accept-new',
    hostAlias,
    'bash', '-lc',
    `mkdir -p ${shellEscape(remoteDirForSSH)}`,
  ]);
  if (mkdir.status !== 0) {
    throw new Error(mkdir.stderr?.trim() || mkdir.stdout?.trim() || `Failed to prepare remote backend directory on ${hostAlias}.`);
  }

  const copy = runSCP([
    '-q',
    localBinary,
    `${hostAlias}:${remoteBinaryForSCP}.tmp`,
  ]);
  if (copy.status !== 0) {
    throw new Error(copy.stderr?.trim() || copy.stdout?.trim() || `Failed to copy backend to ${hostAlias}.`);
  }

  const install = runSSH([
    '-o', 'BatchMode=yes',
    '-o', 'StrictHostKeyChecking=accept-new',
    hostAlias,
    'bash', '-lc',
    `mv ${shellEscape(`${remoteBinaryForSSH}.tmp`)} ${shellEscape(remoteBinaryForSSH)} && chmod +x ${shellEscape(remoteBinaryForSSH)}`,
  ]);
  if (install.status !== 0) {
    throw new Error(install.stderr?.trim() || install.stdout?.trim() || `Failed to install backend on ${hostAlias}.`);
  }

  return remoteBinaryForSSH;
}

function startSSHBackendForHost(options = {}) {
  ensureSSHClientAvailable();

  const hostAlias = String(options.hostAlias || '').trim();
  if (!hostAlias) {
    throw new Error('SSH host alias is required.');
  }

  const remoteWorkspacePath = String(options.remoteWorkspacePath || '$HOME').trim() || '$HOME';
  const appVersion = app.getVersion();
  const remoteInfo = inspectRemoteSSHHost(hostAlias);
  const remoteBinary = ensureRemoteBackendBinary(hostAlias, appVersion, remoteInfo.platform, remoteInfo.arch);
  const localPortPromise = findFreePort();

  return Promise.resolve(localPortPromise).then(async (localPort) => {
    const launch = runSSH([
      '-o', 'BatchMode=yes',
      '-o', 'StrictHostKeyChecking=accept-new',
      hostAlias,
      'bash', '-lc',
      [
        'set -e',
        // Source the user's shell startup files so that API-key environment
        // variables (typically exported in ~/.zshrc, ~/.bashrc, etc.) are
        // available to the daemon.  SSH non-interactive sessions skip these
        // files, but daemon startup depends on the keys they define.
        '_src_rc() { [ -f "$1" ] && . "$1" 2>/dev/null; }',
        'case "$(basename "${SHELL:-sh}")" in',
        '  zsh) _src_rc "$HOME/.zshenv"; _src_rc "$HOME/.zprofile"; _src_rc "$HOME/.zshrc" ;;',
        '  bash) _src_rc "$HOME/.bash_profile"; _src_rc "$HOME/.bashrc" ;;',
        '  fish) ;;',
        '  *)   _src_rc "$HOME/.profile" ;;',
        'esac',
        'unset -f _src_rc',
        'choose_port() {',
        '  if command -v python3 >/dev/null 2>&1; then',
        '    python3 - <<\'PY\'',
        'import socket',
        's = socket.socket()',
        's.bind(("127.0.0.1", 0))',
        'print(s.getsockname()[1])',
        's.close()',
        'PY',
        '    return',
        '  fi',
        '  if command -v python >/dev/null 2>&1; then',
        '    python - <<\'PY\'',
        'import socket',
        's = socket.socket()',
        's.bind(("127.0.0.1", 0))',
        'print(s.getsockname()[1])',
        's.close()',
        'PY',
        '    return',
        '  fi',
        '  echo "python3 or python is required on the remote host" >&2',
        '  exit 1',
        '}',
        `mkdir -p "$HOME/.cache/ledit-desktop/logs"`,
        `cd ${remoteWorkspacePath === '$HOME' ? '"$HOME"' : shellEscape(remoteWorkspacePath)}`,
        'REMOTE_PORT="$(choose_port)"',
        `LOG_FILE="$HOME/.cache/ledit-desktop/logs/${hostAlias.replace(/[^a-zA-Z0-9_.-]/g, '_')}.log"`,
        `nohup env BROWSER=none LEDIT_DESKTOP=1 LEDIT_DESKTOP_BACKEND_MODE=ssh ${shellEscape(remoteBinary)} --isolated-config agent --daemon --web-port "$REMOTE_PORT" >"$LOG_FILE" 2>&1 < /dev/null &`,
        'REMOTE_PID=$!',
        'printf "%s\\n%s\\n" "$REMOTE_PORT" "$REMOTE_PID"',
      ].join('; '),
    ]);

    if (launch.status !== 0) {
      throw new Error(launch.stderr?.trim() || launch.stdout?.trim() || `Failed to start remote backend on ${hostAlias}.`);
    }

    const [remotePortRaw, remotePIDRaw] = launch.stdout
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean);
    const remotePort = Number(remotePortRaw);
    const remotePID = Number(remotePIDRaw);

    if (!Number.isFinite(remotePort) || remotePort <= 0) {
      throw new Error(`Failed to determine remote web port for ${hostAlias}.`);
    }

    const tunnel = spawn('ssh', [
      '-o', 'BatchMode=yes',
      '-o', 'StrictHostKeyChecking=accept-new',
      '-N',
      '-L',
      `${localPort}:127.0.0.1:${remotePort}`,
      hostAlias,
    ], {
      stdio: 'ignore',
      windowsHide: true,
    });

    try {
      await waitForHealth(localPort);
    } catch (error) {
      try { tunnel.kill(); } catch (_) { /* noop */ }
      throw error;
    }

    return {
      child: tunnel,
      port: localPort,
      remotePort,
      remotePID,
      hostAlias,
      workspacePath: `ssh://${hostAlias}`,
      backendMode: 'ssh',
    };
  });
}

function runWslCommand(args, options = {}) {
  return spawnSync('wsl.exe', args, {
    encoding: 'utf8',
    windowsHide: true,
    ...options,
  });
}

function commandExists(command) {
  const probe = spawnSync(command, ['--version'], {
    stdio: 'ignore',
    windowsHide: true,
  });
  return probe.status === 0;
}

function startDetached(command, args) {
  const child = spawn(command, args, {
    detached: true,
    stdio: 'ignore',
    windowsHide: false,
  });
  child.unref();
}

function installWslFromDesktop() {
  if (process.platform !== 'win32') {
    return { ok: false, message: 'WSL installation is only available from the Windows desktop app.' };
  }

  if (commandExists('wsl.exe')) {
    startDetached('wsl.exe', ['--install', '-d', 'Ubuntu']);
    return { ok: true, message: 'Started the WSL installer. Windows may prompt for elevation and a restart.' };
  }

  shell.openExternal('https://learn.microsoft.com/windows/wsl/install');
  return { ok: true, message: 'Opened the WSL installation guide in your browser.' };
}

function installGitForWindowsFromDesktop() {
  if (process.platform !== 'win32') {
    return { ok: false, message: 'Git for Windows installation is only available from the Windows desktop app.' };
  }

  if (commandExists('winget')) {
    startDetached('winget', ['install', '--id', 'Git.Git', '-e', '--source', 'winget']);
    return { ok: true, message: 'Started the Git for Windows installation through winget.' };
  }

  shell.openExternal('https://gitforwindows.org/');
  return { ok: true, message: 'Opened the Git for Windows download page in your browser.' };
}

function toWslPath(pathValue, distro) {
  if (!pathValue) {
    return '';
  }
  if (pathValue.startsWith('/')) {
    return pathValue;
  }

  const result = runWslCommand(['-d', distro, '--', 'wslpath', '-a', pathValue]);
  if (result.status !== 0) {
    throw new Error(result.stderr?.trim() || result.stdout?.trim() || `Failed to translate ${pathValue} to a WSL path.`);
  }

  return result.stdout.trim();
}

function ensureWslBackendBinary(distro) {
  const sourceBinary = resolveBackendBinary('linux');
  if (!fs.existsSync(sourceBinary)) {
    throw new Error(`WSL backend binary not found: ${sourceBinary}`);
  }

  const sourceWslPath = toWslPath(sourceBinary, distro);
  const remoteDir = '$HOME/.cache/ledit-desktop/backend';
  const remotePath = `${remoteDir}/ledit`;
  const command = `mkdir -p ${remoteDir} && cp ${shellEscape(sourceWslPath)} ${shellEscape(remotePath)} && chmod +x ${shellEscape(remotePath)} && printf '%s' ${shellEscape(remotePath)}`;
  const result = runWslCommand(['-d', distro, '--', 'bash', '-lc', command]);
  if (result.status !== 0) {
    throw new Error(result.stderr?.trim() || result.stdout?.trim() || 'Failed to stage the WSL backend binary.');
  }
  return result.stdout.trim() || remotePath.replace('$HOME', '~');
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

async function startBackendForWorkspace(workspaceEntry) {
  const port = await findFreePort();
  const backendMode = workspaceEntry.backendMode === 'wsl' ? 'wsl' : 'native';

  if (backendMode === 'wsl') {
    const distro = workspaceEntry.wslDistro;
    if (!distro) {
      throw new Error('A WSL distro is required for WSL-backed workspaces.');
    }

    const backendBinary = ensureWslBackendBinary(distro);
    const workspaceWslPath = toWslPath(workspaceEntry.workspacePath, distro);
    const command = `cd ${shellEscape(workspaceWslPath)} && LEDIT_DESKTOP=1 LEDIT_HOST_PLATFORM=windows LEDIT_DESKTOP_BACKEND_MODE=wsl BROWSER=none ${shellEscape(backendBinary)} --isolated-config agent --daemon --web-port ${shellEscape(String(port))}`;
    const child = spawn('wsl.exe', ['-d', distro, '--', 'bash', '-lc', command], {
      env: {
        ...process.env,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true,
    });

    const logStream = openBackendLogStream(`wsl-${distro}`);
    if (child.stdout) child.stdout.pipe(logStream);
    if (child.stderr) child.stderr.pipe(logStream);

    child.unref();

    try {
      await waitForHealth(port);
    } catch (error) {
      child.kill();
      throw error;
    }

    return { child, port };
  }

  const binaryPath = resolveBackendBinary();

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Desktop backend binary not found: ${binaryPath}. Run "npm run build:desktop:backend" first.`);
  }

  const child = spawn(binaryPath, ['--isolated-config', 'agent', '--daemon', '--web-port', String(port)], {
    cwd: workspaceEntry.workspacePath,
      env: {
        ...process.env,
        LEDIT_DESKTOP: '1',
        LEDIT_HOST_PLATFORM: process.platform === 'win32' ? 'windows' : process.platform,
        LEDIT_DESKTOP_BACKEND_MODE: 'native',
        BROWSER: 'none',
      },
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });

  const logStream = openBackendLogStream('native');
  if (child.stdout) child.stdout.pipe(logStream);
  if (child.stderr) child.stderr.pipe(logStream);

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

function getRecentLogLines(maxLines = 40) {
  try {
    const logDir = getLogDirectory();
    const files = fs.readdirSync(logDir)
      .filter((f) => f.startsWith('backend-') && f.endsWith('.log'))
      .map((f) => ({ name: f, mtime: fs.statSync(path.join(logDir, f)).mtimeMs }))
      .sort((a, b) => b.mtime - a.mtime);
    if (files.length === 0) return { lines: [], logPath: null };
    const logPath = path.join(logDir, files[0].name);
    const content = fs.readFileSync(logPath, 'utf8');
    const lines = content.split('\n').filter(Boolean);
    return { lines: lines.slice(-maxLines), logPath };
  } catch {
    return { lines: [], logPath: null };
  }
}

function likelyCause(exitCode, signal) {
  if (signal === 'SIGKILL') return 'The backend process was killed by the OS (possibly OOM or force-quit).';
  if (signal === 'SIGSEGV') return 'The backend process crashed with a segmentation fault.';
  if (signal) return `The backend process was terminated by signal ${signal}.`;
  if (exitCode === 1) return 'The backend exited with an error. Check the log for details.';
  if (exitCode === 2) return 'The backend could not start — a required resource or permission is missing.';
  if (exitCode === 127) return 'The backend binary was not found. Try reinstalling.';
  if (exitCode !== null && exitCode !== undefined) return `The backend exited with code ${exitCode}.`;
  return 'The backend stopped unexpectedly.';
}

function renderErrorPage(workspacePath, exitCode, signal, retriesExhausted = false) {
  const { lines: logLines, logPath } = getRecentLogLines(40);
  const logDir = logPath ? path.dirname(logPath) : getLogDirectory();

  const exitInfo = [];
  if (exitCode !== null && exitCode !== undefined) exitInfo.push(`Exit code: ${exitCode}`);
  if (signal !== null && signal !== undefined) exitInfo.push(`Signal: ${signal}`);
  const exitDetails = exitInfo.length > 0
    ? `<div class="meta">${exitInfo.join(' &bull; ')}</div>`
    : '';

  const cause = likelyCause(exitCode, signal);

  const heading = retriesExhausted
    ? 'Backend process could not be restarted'
    : 'Backend process exited unexpectedly';

  const reloadButton = retriesExhausted
    ? '<button disabled>Reload</button>'
    : '<button id="reloadBtn">Reload</button>';

  const logHtml = logLines.length > 0
    ? `<details><summary>Recent log (${logLines.length} lines)</summary><pre id="logPre">${logLines.map((l) => l.replace(/&/g, '&amp;').replace(/</g, '&lt;')).join('\n')}</pre></details>`
    : '<p class="meta">No log output available.</p>';

  const diagnosticsText = JSON.stringify({
    exitCode,
    signal,
    cause,
    workspace: workspacePath,
    recentLog: logLines,
  }, null, 2);

  return `data:text/html;charset=UTF-8,${encodeURIComponent(`
    <!doctype html>
    <html>
      <head>
        <style>
          * { box-sizing: border-box; }
          body { margin: 0; font-family: system-ui, sans-serif; background: #1f242d; color: #d6deeb; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 24px; }
          .card { max-width: 640px; width: 100%; }
          h2 { font-size: 18px; font-weight: 600; margin: 0 0 8px; color: #ff6b6b; }
          .cause { font-size: 14px; margin-bottom: 12px; color: #c8d0e0; }
          .meta { font-size: 12px; color: #8a9ab5; margin-bottom: 12px; }
          details { margin: 12px 0; }
          summary { cursor: pointer; font-size: 13px; color: #8a9ab5; user-select: none; }
          summary:hover { color: #c8d0e0; }
          pre { background: #161b22; border: 1px solid #2d3748; border-radius: 4px; padding: 10px; font-size: 11px; line-height: 1.5; max-height: 220px; overflow-y: auto; white-space: pre-wrap; word-break: break-all; margin: 6px 0 0; color: #a8b2c8; }
          .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 16px; }
          button { background: #4a9eff; color: white; border: none; padding: 9px 16px; font-size: 13px; border-radius: 4px; cursor: pointer; }
          button:hover { background: #3a8eef; }
          button.secondary { background: #2d3748; color: #c8d0e0; }
          button.secondary:hover { background: #3a4a5e; }
          button[disabled] { background: #3a4050; color: #6a7080; cursor: not-allowed; }
          .log-path { font-size: 11px; color: #6a7890; margin-top: 10px; word-break: break-all; }
        </style>
      </head>
      <body>
        <div class="card">
          <h2>${heading}</h2>
          <p class="cause">${cause}</p>
          ${exitDetails}
          <div class="meta">Workspace: ${workspacePath}</div>
          ${logHtml}
          <div class="actions">
            ${reloadButton}
            <button class="secondary" id="copyBtn">Copy Diagnostics</button>
            <button class="secondary" id="logsBtn">Open Log Folder</button>
          </div>
          ${logPath ? `<div class="log-path">Log: ${logPath}</div>` : ''}
        </div>
        <script>
          const diagnostics = ${JSON.stringify(diagnosticsText)};
          const logDir = ${JSON.stringify(logDir)};
          document.getElementById('reloadBtn') && document.getElementById('reloadBtn').addEventListener('click', () => { location.href = 'ledit://reload'; });
          document.getElementById('copyBtn').addEventListener('click', () => { navigator.clipboard.writeText(diagnostics).then(() => { document.getElementById('copyBtn').textContent = 'Copied!'; setTimeout(() => { document.getElementById('copyBtn').textContent = 'Copy Diagnostics'; }, 2000); }); });
          document.getElementById('logsBtn').addEventListener('click', () => { location.href = 'ledit://open-log-dir?dir=' + encodeURIComponent(logDir); });
        </script>
      </body>
    </html>
  `)}`;
}

function registerExitHandler(browserWindow, port, workspaceEntry, reloadCallback) {
  const record = instanceRegistry.get(browserWindow.id);
  if (!record || !record.child) {
    return;
  }

  // Store the reload callback for later use
  record.reloadCallback = reloadCallback;

  record.child.on('exit', (exitCode, signal) => {
    // Ignore normal shutdown (SIGTERM from window close)
    if (signal === 'SIGTERM') {
      return;
    }

    // Only act on unexpected exits (non-zero exit code or unexpected signal)
    if (exitCode === 0 && !signal) {
      return;
    }

    // Log the crash
    console.error(`Backend daemon crashed on port ${port}: exitCode=${exitCode}, signal=${signal}`);

    // Show error page
    browserWindow.loadURL(renderErrorPage(workspaceEntry.workspacePath, exitCode, signal));
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

  const workspaceEntry = {
    workspacePath,
    backendMode,
    wslDistro,
  };
  const workspaceKey = getWorkspaceKey(workspaceEntry);

  const existingWindowId = workspaceWindowMap.get(workspaceKey);
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
  browserWindow.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });
  
  // Intercept reload navigation to trigger backend restart
  browserWindow.webContents.on('will-navigate', (event, url) => {
    if (url.startsWith('ledit://reload')) {
      event.preventDefault();
      // Trigger reload via the registered handler
      const record = instanceRegistry.get(browserWindow.id);
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

  const backend = await startBackendForWorkspace(workspaceEntry);
  instanceRegistry.set(browserWindow.id, { ...backend, ...workspaceEntry });
  
  // Crash loop protection: limit restart attempts
  let crashCount = 0;
  
  // Register crash detection handler for the backend daemon
  const performReload = async () => {
    try {
      const newBackend = await startBackendForWorkspace(workspaceEntry);
      crashCount = 0;
      instanceRegistry.set(browserWindow.id, { ...newBackend, ...workspaceEntry });
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
  
  workspaceWindowMap.set(workspaceKey, browserWindow.id);
  addRecentWorktree(workspaceEntry);
  persistOpenWorkspaces();
  buildMenu();
  if (launcherWindow && !launcherWindow.isDestroyed()) {
    launcherWindow.close();
  }

  browserWindow.on('move', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('resize', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('maximize', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('unmaximize', () => scheduleWindowBoundsPersist(workspaceKey, browserWindow));
  browserWindow.on('close', () => {
    writeWindowBounds(workspaceKey, browserWindow);
  });

  browserWindow.on('closed', () => {
    const timer = windowStateWriteTimers.get(workspaceKey);
    if (timer) {
      clearTimeout(timer);
      windowStateWriteTimers.delete(workspaceKey);
    }
    const record = instanceRegistry.get(browserWindow.id);
    if (record) {
      const child = record.child;
      child.kill();
      // Escalate to SIGKILL after 3 seconds if the process hasn't terminated
      const killTimer = setTimeout(() => {
        try { child.kill('SIGKILL'); } catch (_) { /* already dead */ }
      }, 3000);
      child.once('exit', () => clearTimeout(killTimer));
      instanceRegistry.delete(browserWindow.id);
      if (workspaceWindowMap.get(workspaceKey) === browserWindow.id) {
        workspaceWindowMap.delete(workspaceKey);
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
  const existingWindowId = sshWindowMap.get(remoteKey);
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
  browserWindow.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });
  browserWindow.webContents.on('will-navigate', (event, url) => {
    if (url.startsWith('ledit://reload')) {
      event.preventDefault();
      const record = instanceRegistry.get(browserWindow.id);
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

  const backend = await startSSHBackendForHost({
    hostAlias,
    remoteWorkspacePath,
  });

  instanceRegistry.set(browserWindow.id, {
    ...backend,
    workspacePath: `ssh://${hostAlias}`,
    remoteWorkspacePath,
  });

  const performReload = async () => {
    try {
      const record = instanceRegistry.get(browserWindow.id);
      if (record?.child) {
        try { record.child.kill(); } catch (_) { /* noop */ }
      }
      const newBackend = await startSSHBackendForHost({ hostAlias, remoteWorkspacePath });
      instanceRegistry.set(browserWindow.id, {
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
  sshWindowMap.set(remoteKey, browserWindow.id);

  browserWindow.on('closed', () => {
    const record = instanceRegistry.get(browserWindow.id);
    if (record) {
      if (record.child) {
        try { record.child.kill(); } catch (_) { /* noop */ }
      }
      instanceRegistry.delete(browserWindow.id);
    }
    if (sshWindowMap.get(remoteKey) === browserWindow.id) {
      sshWindowMap.delete(remoteKey);
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
ipcMain.handle('desktop:listSshHosts', async () => listSshHosts());
ipcMain.handle('desktop:listWslDistros', async () => listWslDistros());
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
    backendMode: options.backendMode,
    wslDistro: options.wslDistro,
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
      const openedRecent = await openMostRecentWorkspace();
      if (openedRecent) {
        return;
      }
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

app.on('will-quit', () => {
  for (const [id, record] of instanceRegistry) {
    const child = record.child;
    child.kill();
    const killTimer = setTimeout(() => {
      try { child.kill('SIGKILL'); } catch (_) { /* already dead */ }
    }, 3000);
    child.once('exit', () => clearTimeout(killTimer));
    instanceRegistry.delete(id);
  }
});
