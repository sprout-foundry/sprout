/**
 * Backend process management: port discovery, health polling, spawn, and crash detection.
 */

const { app } = require('electron');
const { spawn } = require('node:child_process');
const fs = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const path = require('node:path');
const { shellEscape } = require('./utils');
const { openBackendLogStream } = require('./state-manager');
const { toWslPath, ensureWslBackendBinary } = require('./wsl');
const { renderErrorPage } = require('./error-pages');

function resolveBackendBinary() {
  const platform = arguments[0] || (process.platform === 'win32' ? 'windows' : process.platform);
  const arch = arguments[1] || (process.arch === 'x64' ? 'amd64' : process.arch);
  const binaryName = platform === 'windows' ? 'ledit.exe' : 'ledit';

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

async function startBackendForWorkspace(workspaceEntry) {
  const port = await findFreePort();
  const backendMode = workspaceEntry.backendMode === 'wsl' ? 'wsl' : 'native';

  if (backendMode === 'wsl') {
    const distro = workspaceEntry.wslDistro;
    if (!distro) {
      throw new Error('A WSL distro is required for WSL-backed workspaces.');
    }

    const backendBinary = ensureWslBackendBinary(distro, resolveBackendBinary);
    const workspaceWslPath = toWslPath(workspaceEntry.workspacePath, distro);
    const command = `cd ${shellEscape(workspaceWslPath)} && LEDIT_DESKTOP=1 LEDIT_HOST_PLATFORM=windows LEDIT_DESKTOP_BACKEND_MODE=wsl BROWSER=none ${shellEscape(backendBinary)} --isolated-config agent --daemon --web-port ${shellEscape(String(port))}`;
    const child = spawn('wsl.exe', ['-d', distro, '--', 'bash', '-lc', command], {
      env: { ...process.env },
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

function registerExitHandler(browserWindow, port, workspaceEntry, reloadCallback) {
  const ctx = require('./context');
  const record = ctx.instanceRegistry.get(browserWindow.id);
  if (!record || !record.child) {
    return;
  }

  record.reloadCallback = reloadCallback;

  record.child.on('exit', (exitCode, signal) => {
    if (signal === 'SIGTERM') {
      return;
    }
    if (exitCode === 0 && !signal) {
      return;
    }

    console.error(`Backend daemon crashed on port ${port}: exitCode=${exitCode}, signal=${signal}`);
    browserWindow.loadURL(renderErrorPage(workspaceEntry.workspacePath, exitCode, signal));
  });
}

module.exports = {
  resolveBackendBinary,
  findFreePort,
  waitForHealth,
  startBackendForWorkspace,
  registerExitHandler,
};
