/**
 * WSL-specific utilities: distro discovery, binary staging, and installer helpers.
 */

const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const { shell } = require('electron');
const { shellEscape } = require('./utils');

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
    .map((line) => line.trim().replace(/^\*\s*/, ''))
    .filter(Boolean);
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

function ensureWslBackendBinary(distro, resolveBackendBinary) {
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

module.exports = {
  listWslDistros,
  runWslCommand,
  commandExists,
  startDetached,
  installWslFromDesktop,
  installGitForWindowsFromDesktop,
  toWslPath,
  ensureWslBackendBinary,
};
