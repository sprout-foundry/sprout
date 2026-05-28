/**
 * Backend process management: port discovery, health polling, spawn, and crash detection.
 */

const { app } = require('electron');
const { spawn } = require('node:child_process');
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');
const { shellEscape } = require('./utils');
const { openBackendLogStream } = require('./state-manager');
const { toWslPath, ensureWslBackendBinary } = require('./wsl');
const { renderErrorPage } = require('./error-pages');

// 256-bit random auth secret for Electron ↔ backend communication.
// Generated once per app lifecycle.
let authToken;

function generateSecret() {
  if (!authToken) {
    authToken = crypto.randomBytes(32).toString('hex');
  }
  return authToken;
}

function resolveBackendBinary() {
  const platform = arguments[0] || (process.platform === 'win32' ? 'windows' : process.platform);
  const arch = arguments[1] || (process.arch === 'x64' ? 'amd64' : process.arch);
  const binaryName = platform === 'windows' ? 'sprout.exe' : 'sprout';

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

/* -------------------------------------------------------------------------- */
/*  Unix-socket helpers (macOS / Linux native mode)                          */
/* -------------------------------------------------------------------------- */

/**
 * Generate a random socket path in the system temp directory.
 */
function generateSocketPath() {
  return path.join(os.tmpdir(), 'sprout-desktop-' + crypto.randomBytes(8).toString('hex') + '.sock');
}

/**
 * Wait for the backend to become healthy via a Unix-domain socket.
 */
function waitForHealthOnSocket(socketPath, timeoutMs) {
  if (timeoutMs === undefined) timeoutMs = 20000;
  const startedAt = Date.now();

  return new Promise((resolvePromise, rejectPromise) => {
    let resolved = false;

    const probe = () => {
      const socket = net.connect({ socketPath }, () => {
        socket.write('GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n');
      });

      let data = '';
      socket.on('data', (chunk) => {
        data += chunk;
        if (data.includes('HTTP/1.1 200')) {
          resolved = true;
          socket.destroy();
          resolvePromise();
        }
      });

      socket.on('end', () => {
        if (!resolved) {
          retry();
        }
      });

      socket.on('error', () => {
        if (!resolved) {
          retry();
        }
      });
    };

    const retry = () => {
      if (Date.now() - startedAt >= timeoutMs) {
        rejectPromise(new Error(`Timed out waiting for backend on socket ${socketPath}`));
        return;
      }
      setTimeout(probe, 300);
    };

    probe();
  });
}

/**
 * Create an HTTP proxy that listens on a random TCP port and forwards all
 * requests (including WebSocket upgrades) to the Go backend over a Unix
 * domain socket.  Injects an Authorization header on every forwarded request.
 *
 * Returns { proxyServer, port }.
 */
async function createSocketProxy(socketPath, token) {
  const port = await findFreePort();

  const proxyServer = http.createServer();

  proxyServer.on('request', (req, res) => {
    const options = {
      socketPath: socketPath,
      path: req.url,
      method: req.method,
      headers: {
        ...req.headers,
        Authorization: `Bearer ${token}`,
        // Strip hop-by-hop headers that must not be forwarded
        connection: undefined,
        transferEncoding: undefined,
      },
    };

    const proxyReq = http.request(options, (proxyRes) => {
      res.writeHead(proxyRes.statusCode, proxyRes.rawHeaders);
      proxyRes.on('error', (err) => {
        console.error('Proxy upstream response error:', err.message);
        if (!res.headersSent) {
          res.writeHead(502, { 'Content-Type': 'text/plain' });
        }
        res.end('Bad gateway: upstream connection dropped');
      });
      proxyRes.pipe(res);
    });

    proxyReq.on('error', (err) => {
      if (!res.headersSent) {
        res.writeHead(502, { 'Content-Type': 'text/plain' });
      }
      res.end(`Bad gateway: ${err.message}`);
    });

    // Pipe request body for non-GET/HEAD requests
    req.pipe(proxyReq);
    req.on('error', () => { proxyReq.destroy(); });
  });

  /* ---- WebSocket upgrade forwarding ---- */
  proxyServer.on('upgrade', (req, socket, head) => {
    const options = {
      socketPath: socketPath,
      path: req.url,
      method: 'GET',
      headers: {
        ...req.headers,
        Authorization: `Bearer ${token}`,
      },
    };

    const proxy = net.connect({ socketPath }, () => {
      // Build the raw upgrade request to send over the Unix socket
      let reqStr = `GET ${req.url} HTTP/1.1\r\n`;
      for (const key of Object.keys(options.headers)) {
        reqStr += `${key}: ${options.headers[key]}\r\n`;
      }
      reqStr += '\r\n';
      proxy.write(reqStr);
      if (head && head.length) {
        proxy.write(head);
      }
    });

    socket.pipe(proxy);
    proxy.pipe(socket);

    socket.on('error', () => { proxy.destroy(); });
    proxy.on('error', () => { socket.destroy(); });
  });

  return new Promise((resolvePromise, rejectPromise) => {
    proxyServer.listen(port, '127.0.0.1', () => {
      resolvePromise({ proxyServer, port });
    });
    proxyServer.on('error', rejectPromise);
  });
}

/**
 * Close a socket proxy HTTP server gracefully.
 */
function closeSocketProxy(proxyServer) {
  if (!proxyServer) return;
  proxyServer.close(() => {
    /* closed cleanly */
  });
}

/* -------------------------------------------------------------------------- */
/*  Spawn helpers                                                            */
/* -------------------------------------------------------------------------- */

async function startBackendForWorkspace(workspaceEntry) {
  generateSecret();
  const backendMode = workspaceEntry.backendMode === 'wsl' ? 'wsl' : 'native';

  /* ------------------------------------------------------------------ */
  /*  WSL mode — unchanged, spawns inside WSL via TCP                    */
  /* ------------------------------------------------------------------ */
  if (backendMode === 'wsl') {
    const distro = workspaceEntry.wslDistro;
    if (!distro) {
      throw new Error('A WSL distro is required for WSL-backed workspaces.');
    }

    const port = await findFreePort();
    const backendBinary = ensureWslBackendBinary(distro, resolveBackendBinary);
    const workspaceWslPath = toWslPath(workspaceEntry.workspacePath, distro);
    const command = `cd ${shellEscape(workspaceWslPath)} && SPROUT_DESKTOP=1 SPROUT_HOST_PLATFORM=windows SPROUT_DESKTOP_BACKEND_MODE=wsl BROWSER=none ${shellEscape(backendBinary)} --isolated-config agent --daemon --web-port ${shellEscape(String(port))}`;
    const child = spawn('wsl.exe', ['-d', distro, '--', 'bash', '-lc', command], {
      env: { ...process.env, SPROUT_AUTH_TOKEN: authToken },
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

  /* ------------------------------------------------------------------ */
  /*  macOS / Linux native — Unix-socket proxy (SP-060-B2)               */
  /* ------------------------------------------------------------------ */
  if (process.platform !== 'win32') {
    const socketPath = generateSocketPath();
    const binaryPath = resolveBackendBinary();

    if (!fs.existsSync(binaryPath)) {
      throw new Error(`Desktop backend binary not found: ${binaryPath}. Run "npm run build:desktop:backend" first.`);
    }

    const child = spawn(binaryPath, [
      '--isolated-config',
      'agent',
      '--daemon',
      '--bind-socket',
      socketPath,
      '--secret',
      authToken,
    ], {
      cwd: workspaceEntry.workspacePath,
      env: {
        ...process.env,
        SPROUT_DESKTOP: '1',
        SPROUT_HOST_PLATFORM: process.platform,
        SPROUT_DESKTOP_BACKEND_MODE: 'native',
        BROWSER: 'none',
      },
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true,
    });

    const logStream = openBackendLogStream('native');
    if (child.stdout) child.stdout.pipe(logStream);
    if (child.stderr) child.stderr.pipe(logStream);
    child.unref();

    // Wait for the backend to be healthy over the Unix socket
    try {
      await waitForHealthOnSocket(socketPath);
    } catch (error) {
      child.kill();
      throw error;
    }

    // Start the TCP proxy in front of the socket
    const { proxyServer, port } = await createSocketProxy(socketPath, authToken);

    // Verify the proxy is working (hits the socket via the proxy)
    try {
      await waitForHealth(port);
    } catch (error) {
      child.kill();
      closeSocketProxy(proxyServer);
      throw error;
    }

    return { child, port, socketPath, proxyServer };
  }

  /* ------------------------------------------------------------------ */
  /*  Windows native — TCP (unchanged behavior)                          */
  /* ------------------------------------------------------------------ */
  const port = await findFreePort();
  const binaryPath = resolveBackendBinary();

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Desktop backend binary not found: ${binaryPath}. Run "npm run build:desktop:backend" first.`);
  }

  const child = spawn(binaryPath, ['--isolated-config', 'agent', '--daemon', '--web-port', String(port)], {
    cwd: workspaceEntry.workspacePath,
    env: {
      ...process.env,
      SPROUT_DESKTOP: '1',
      SPROUT_HOST_PLATFORM: process.platform === 'win32' ? 'windows' : process.platform,
      SPROUT_DESKTOP_BACKEND_MODE: 'native',
      BROWSER: 'none',
      SPROUT_AUTH_TOKEN: authToken,
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
  generateSecret,
  generateSocketPath,
  createSocketProxy,
  waitForHealthOnSocket,
  closeSocketProxy,
};
