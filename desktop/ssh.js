/**
 * SSH remote backend management: host discovery, binary upload, tunnel setup.
 */

const { app } = require('electron');
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const { shellEscape } = require('./utils');
const { resolveBackendBinary, findFreePort, waitForHealth } = require('./backend');

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
          hostsMap.set(alias, { alias, hostname: '', user: '', port: '' });
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
      if (!existing) continue;
      if (key === 'hostname' && !existing.hostname) existing.hostname = value;
      if (key === 'user' && !existing.user) existing.user = value;
      if (key === 'port' && !existing.port) existing.port = value;
    }
  }
}

function listSshHosts() {
  const configPath = path.join(getHomeDir(), '.ssh', 'config');
  const hostsMap = new Map();
  parseSSHConfigFile(configPath, hostsMap);
  return Array.from(hostsMap.values()).sort((a, b) => a.alias.localeCompare(b.alias));
}

function normalizeRemotePlatform(rawPlatform) {
  const normalized = String(rawPlatform || '').trim().toLowerCase();
  if (normalized === 'linux') return 'linux';
  if (normalized === 'darwin') return 'darwin';
  return '';
}

function normalizeRemoteArch(rawArch) {
  const normalized = String(rawArch || '').trim().toLowerCase();
  if (normalized === 'x86_64' || normalized === 'amd64') return 'amd64';
  if (normalized === 'aarch64' || normalized === 'arm64') return 'arm64';
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
  return spawnSync('ssh', args, { encoding: 'utf8', windowsHide: true, ...options });
}

function runSCP(args, options = {}) {
  return spawnSync('scp', args, { encoding: 'utf8', windowsHide: true, ...options });
}

function ensureSSHClientAvailable() {
  const locator = process.platform === 'win32' ? 'where' : 'which';
  const sshProbe = spawnSync(locator, ['ssh'], { stdio: 'ignore', windowsHide: true });
  if (sshProbe.status !== 0) throw new Error('ssh is not available on this machine.');
  const scpProbe = spawnSync(locator, ['scp'], { stdio: 'ignore', windowsHide: true });
  if (scpProbe.status !== 0) throw new Error('scp is not available on this machine.');
}

function inspectRemoteSSHHost(hostAlias) {
  const result = runSSH([
    '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=accept-new',
    hostAlias, 'bash', '-lc', 'uname -s; uname -m; printf "%s\\n" "$HOME"',
  ]);

  if (result.status !== 0) {
    throw new Error(result.stderr?.trim() || result.stdout?.trim() || `Failed to inspect remote host ${hostAlias}.`);
  }

  const [platform, archRaw, homeDir] = result.stdout.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);

  const normalizedPlatform = normalizeRemotePlatform(platform);
  if (!normalizedPlatform) {
    throw new Error(`Remote host ${hostAlias} is ${platform || 'unknown'}; only Linux and macOS SSH targets are supported.`);
  }

  const arch = normalizeRemoteArch(archRaw);
  if (!arch) throw new Error(`Unsupported remote architecture: ${archRaw || 'unknown'}.`);

  return { platform: normalizedPlatform, arch, homeDir: homeDir || '~' };
}

function ensureRemoteBackendBinary(hostAlias, appVersion, remotePlatform, remoteArch) {
  const localBinary = ensureRemoteBackendBinaryArtifact(remotePlatform, remoteArch);
  const remoteDirForSSH = `$HOME/.cache/ledit-desktop/backend/${appVersion}/${remotePlatform}-${remoteArch}`;
  const remoteDirForSCP = `~/.cache/ledit-desktop/backend/${appVersion}/${remotePlatform}-${remoteArch}`;
  const remoteBinaryForSSH = `${remoteDirForSSH}/ledit`;
  const remoteBinaryForSCP = `${remoteDirForSCP}/ledit`;

  const mkdir = runSSH(['-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=accept-new', hostAlias, 'bash', '-lc', `mkdir -p ${shellEscape(remoteDirForSSH)}`]);
  if (mkdir.status !== 0) throw new Error(mkdir.stderr?.trim() || mkdir.stdout?.trim() || `Failed to prepare remote backend directory on ${hostAlias}.`);

  const copy = runSCP(['-q', localBinary, `${hostAlias}:${remoteBinaryForSCP}.tmp`]);
  if (copy.status !== 0) throw new Error(copy.stderr?.trim() || copy.stdout?.trim() || `Failed to copy backend to ${hostAlias}.`);

  const install = runSSH(['-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=accept-new', hostAlias, 'bash', '-lc', `mv ${shellEscape(`${remoteBinaryForSSH}.tmp`)} ${shellEscape(remoteBinaryForSSH)} && chmod +x ${shellEscape(remoteBinaryForSSH)}`]);
  if (install.status !== 0) throw new Error(install.stderr?.trim() || install.stdout?.trim() || `Failed to install backend on ${hostAlias}.`);

  return remoteBinaryForSSH;
}

function startSSHBackendForHost(options = {}) {
  ensureSSHClientAvailable();

  const hostAlias = String(options.hostAlias || '').trim();
  if (!hostAlias) throw new Error('SSH host alias is required.');

  const remoteWorkspacePath = String(options.remoteWorkspacePath || '$HOME').trim() || '$HOME';
  const appVersion = app.getVersion();
  const remoteInfo = inspectRemoteSSHHost(hostAlias);
  const remoteBinary = ensureRemoteBackendBinary(hostAlias, appVersion, remoteInfo.platform, remoteInfo.arch);
  const localPortPromise = findFreePort();

  return Promise.resolve(localPortPromise).then(async (localPort) => {
    const launch = runSSH([
      '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=accept-new',
      hostAlias, 'bash', '-lc',
      [
        'set -e',
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

    const [remotePortRaw, remotePIDRaw] = launch.stdout.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
    const remotePort = Number(remotePortRaw);
    const remotePID = Number(remotePIDRaw);

    if (!Number.isFinite(remotePort) || remotePort <= 0) {
      throw new Error(`Failed to determine remote web port for ${hostAlias}.`);
    }

    const tunnel = spawn('ssh', [
      '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=accept-new',
      '-N', '-L', `${localPort}:127.0.0.1:${remotePort}`, hostAlias,
    ], { stdio: 'ignore', windowsHide: true });

    try {
      await waitForHealth(localPort);
    } catch (error) {
      try { tunnel.kill(); } catch (_) { /* noop */ }
      throw error;
    }

    return { child: tunnel, port: localPort, remotePort, remotePID, hostAlias, workspacePath: `ssh://${hostAlias}`, backendMode: 'ssh' };
  });
}

module.exports = {
  getHomeDir,
  expandHomePath,
  resolveIncludePattern,
  parseSSHConfigFile,
  listSshHosts,
  normalizeRemotePlatform,
  normalizeRemoteArch,
  ensureRemoteBackendBinaryArtifact,
  runSSH,
  runSCP,
  ensureSSHClientAvailable,
  inspectRemoteSSHHost,
  ensureRemoteBackendBinary,
  startSSHBackendForHost,
};
