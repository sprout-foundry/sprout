/**
 * Unit tests for desktop/windows.js — injectAuthHeaders.
 *
 * Run with: node desktop/windows_test.js
 *
 * Because windows.js requires 'electron' at the top level, these tests mock
 * Electron (and ./backend) before requiring the module under test.
 */

const assert = require('node:assert');
const { test, describe } = require('node:test');
const Module = require('module');
const fs = require('node:fs');
const path = require('node:path');

// ---------------------------------------------------------------------------
// Test constants
// ---------------------------------------------------------------------------

const TEST_TOKEN = 'test-secret-token-123';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Create a mock BrowserWindow with a stubbed session.webRequest
 * that captures the onBeforeSendHeaders callback for inspection.
 */
function createMockBrowserWindow() {
  const captured = { urlsArg: null, callback: null };

  return {
    id: 1,
    webContents: {
      session: {
        webRequest: {
          onBeforeSendHeaders(urlsArg, cb) {
            captured.urlsArg = urlsArg;
            captured.callback = cb;
          },
        },
      },
    },
    _captured: captured,
  };
}

/**
 * Load a fresh windows module with Electron + backend mocked.
 * Purges the desktop module cache, patches require, and requires
 * ./windows.js.  generateSecret() is stubbed to return TEST_TOKEN.
 *
 * Returns { windows, cleanup } — call cleanup() after the test.
 */
function loadFreshWindows() {
  // Purge cached desktop modules so the next require() re-evaluates.
  const root = __dirname;
  Object.keys(Module._cache).forEach((key) => {
    if (key.startsWith(root)) {
      delete Module._cache[key];
    }
  });

  const originalRequire = Module.prototype.require;

  Module.prototype.require = function (id) {
    if (id === 'electron') {
      return {
        app: {
          isPackaged: false,
          getAppPath: () => '/fake/path',
          getVersion: () => '0.0.0-test',
          getPath: () => '/fake/path',
        },
        BrowserWindow: class {
          constructor() {
            this.id = 1;
            this.webContents = {
              session: { webRequest: { onBeforeSendHeaders() {} } },
            };
          }
          static fromId() { return null; }
          static getFocusedWindow() { return null; }
        },
        dialog: { showErrorBox: () => {} },
        Menu: {
          buildFromTemplate: () => ({}),
          setApplicationMenu: () => {},
        },
        shell: { openExternal: () => {}, openPath: () => {} },
      };
    }

    if (id === './backend' || id.endsWith('/backend') || id.endsWith('/backend.js')) {
      return {
        generateSecret: () => TEST_TOKEN,
        startBackendForWorkspace: async () => ({ child: { kill: () => {} }, port: 9999 }),
        registerExitHandler: () => {},
        resolveBackendBinary: () => '/fake/sprout',
        findFreePort: () => Promise.resolve(9999),
        waitForHealth: () => Promise.resolve(),
      };
    }

    if (id === './context' || id.endsWith('/context') || id.endsWith('/context.js')) {
      return {
        launcherWindow: null,
        instanceRegistry: new Map(),
        workspaceWindowMap: new Map(),
        sshWindowMap: new Map(),
        windowStateWriteTimers: new Map(),
      };
    }

    if (id === './utils' || id.endsWith('/utils') || id.endsWith('/utils.js')) {
      return { getWorkspaceKey: (e) => `ws:${e.workspacePath}` };
    }

    const stubFn = () => {};
    const stubAsync = () => Promise.resolve();

    if (id === './state-manager' || id.endsWith('/state-manager') || id.endsWith('/state-manager.js')) {
      return {
        getLogDirectory: stubFn,
        getSavedWindowBounds: stubFn,
        writeWindowBounds: stubFn,
        scheduleWindowBoundsPersist: stubFn,
        addRecentWorktree: stubFn,
        persistOpenWorkspaces: stubFn,
        getRecentWorktrees: () => [],
        openBackendLogStream: () => ({ write: stubFn, end: stubFn }),
      };
    }

    if (id === './error-pages' || id.endsWith('/error-pages') || id.endsWith('/error-pages.js')) {
      return {
        renderLoadingPage: () => 'data:text/html,loading',
        renderErrorPage: () => 'data:text/html,error',
      };
    }

    if (id === './ssh' || id.endsWith('/ssh') || id.endsWith('/ssh.js')) {
      return {
        startSSHBackendForHost: async () => ({ child: { kill: () => {} }, port: 8888 }),
      };
    }

    if (id === './workspace' || id.endsWith('/workspace') || id.endsWith('/workspace.js')) {
      return {
        resolveWorkspaceDirectory: (p) => p,
        promptForWorkspace: async () => '/fake/workspace',
      };
    }

    return originalRequire.apply(this, arguments);
  };

  const windows = require('./windows.js');

  return {
    windows,
    cleanup: () => {
      Module.prototype.require = originalRequire;
    },
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('injectAuthHeaders()', () => {
  test('registers onBeforeSendHeaders on the window session', () => {
    const { windows, cleanup } = loadFreshWindows();
    try {
      const mockWindow = createMockBrowserWindow();
      windows.injectAuthHeaders(mockWindow);

      const cap = mockWindow._captured;
      assert.ok(cap.callback !== null, 'expected onBeforeSendHeaders to be called');
      assert.deepStrictEqual(
        cap.urlsArg,
        { urls: ['http://127.0.0.1:*/*'] },
        'expected the correct URL filter pattern',
      );
      assert.strictEqual(
        typeof cap.callback,
        'function',
        'expected a callback function to be passed',
      );
    } finally {
      cleanup();
    }
  });

  test('callback injects Authorization header with Bearer token', () => {
    const { windows, cleanup } = loadFreshWindows();
    try {
      const mockWindow = createMockBrowserWindow();
      windows.injectAuthHeaders(mockWindow);

      const cap = mockWindow._captured;
      const callback = cap.callback;

      let callbackResult = null;
      callback(
        { requestHeaders: { 'Content-Type': 'application/json' } },
        (result) => { callbackResult = result; },
      );

      assert.strictEqual(
        callbackResult.requestHeaders['Authorization'],
        `Bearer ${TEST_TOKEN}`,
        `expected Authorization header to be "Bearer ${TEST_TOKEN}"`,
      );
    } finally {
      cleanup();
    }
  });

  test('callback preserves existing headers', () => {
    const { windows, cleanup } = loadFreshWindows();
    try {
      const mockWindow = createMockBrowserWindow();
      windows.injectAuthHeaders(mockWindow);

      const cap = mockWindow._captured;
      const callback = cap.callback;

      let callbackResult = null;
      callback(
        {
          requestHeaders: {
            'Content-Type': 'application/json',
            'X-Custom': 'custom-value',
            'Accept': '*/*',
          },
        },
        (result) => { callbackResult = result; },
      );

      const headers = callbackResult.requestHeaders;
      assert.strictEqual(headers['Content-Type'], 'application/json', 'existing Content-Type should be preserved');
      assert.strictEqual(headers['X-Custom'], 'custom-value', 'existing X-Custom should be preserved');
      assert.strictEqual(headers['Accept'], '*/*', 'existing Accept should be preserved');
      assert.strictEqual(
        headers['Authorization'],
        `Bearer ${TEST_TOKEN}`,
        'Authorization should be added alongside existing headers',
      );
    } finally {
      cleanup();
    }
  });

  test('uses the same token from generateSecret across multiple calls', () => {
    const { windows, cleanup } = loadFreshWindows();
    try {
      // Two separate window instances
      const mockWindow1 = createMockBrowserWindow();
      const mockWindow2 = createMockBrowserWindow();

      windows.injectAuthHeaders(mockWindow1);
      windows.injectAuthHeaders(mockWindow2);

      // Both should use the same cached token
      let result1 = null, result2 = null;
      mockWindow1._captured.callback({ requestHeaders: {} }, (r) => { result1 = r; });
      mockWindow2._captured.callback({ requestHeaders: {} }, (r) => { result2 = r; });

      assert.strictEqual(
        result1.requestHeaders['Authorization'],
        result2.requestHeaders['Authorization'],
        'both calls should use the same cached token',
      );
      assert.strictEqual(
        result1.requestHeaders['Authorization'],
        `Bearer ${TEST_TOKEN}`,
        'the token should be the mocked value',
      );
    } finally {
      cleanup();
    }
  });

  test('is exported from the module as a function', () => {
    const { windows, cleanup } = loadFreshWindows();
    try {
      assert.strictEqual(
        typeof windows.injectAuthHeaders,
        'function',
        'injectAuthHeaders should be exported as a function',
      );
    } finally {
      cleanup();
    }
  });
});

describe('injectAuthHeaders integration (source inspection)', () => {
  /**
   * Verify that injectAuthHeaders is called in the window-creation flows
   * via source inspection (similar approach to backend_test.js).
   * Full behavioral testing of createWorkspaceWindow / createSSHWorkspaceWindow
   * would require a compiled backend binary and Electron runtime.
   */
  const sourcePath = path.join(__dirname, 'windows.js');
  const source = fs.readFileSync(sourcePath, 'utf8');

  test('createWorkspaceWindow calls injectAuthHeaders', () => {
    assert.ok(
      source.includes('injectAuthHeaders(browserWindow)'),
      'expected injectAuthHeaders(browserWindow) call in createWorkspaceWindow',
    );
  });

  test('createSSHWorkspaceWindow calls injectAuthHeaders', () => {
    // Both functions call it, so there should be at least 2 occurrences
    // (plus the function definition itself).
    const count = (source.match(/injectAuthHeaders\(browserWindow\)/g) || []).length;
    assert.ok(
      count >= 2,
      `expected at least 2 calls to injectAuthHeaders(browserWindow), found ${count}`,
    );
  });

  test('generateSecret is imported from ./backend', () => {
    assert.ok(
      source.includes("require('./backend')") || source.includes("require('./backend.js')"),
      'expected require of ./backend for generateSecret',
    );
    assert.ok(
      source.includes('generateSecret'),
      'expected generateSecret in the import or usage',
    );
  });

  test('injectAuthHeaders is included in module.exports', () => {
    assert.ok(
      source.includes('injectAuthHeaders,') ||
      source.match(/injectAuthHeaders\s*[\n,}\]]/),
      'expected injectAuthHeaders in module.exports',
    );
  });
});
