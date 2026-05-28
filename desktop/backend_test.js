/**
 * Unit tests for desktop/backend.js — generateSecret and SPROUT_AUTH_TOKEN.
 *
 * Run with: node desktop/backend_test.js
 *
 * Because backend.js requires 'electron' at the top level, these tests mock
 * Electron's `app` and `shell` modules before requiring the module under test.
 * All other desktop dependencies (utils, state-manager, wsl, error-pages,
 * context) are loaded as-is — they work with the Electron mock in place.
 */

const assert = require('node:assert');
const { test, describe } = require('node:test');
const Module = require('module');
const fs = require('node:fs');
const path = require('node:path');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Stub Electron so that desktop modules can be required in plain Node.js.
 * Purges the desktop module cache *before* patching require so the next
 * require() gets a fresh module instance (and thus a fresh authToken).
 *
 * Returns a cleanup function that restores the original require.
 */
function mockElectron() {
  // Purge every cached desktop module *before* patching require,
  // so the subsequent require() gets a fresh module instance.
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
          getPath: () => '/fake/path',
        },
        shell: { openExternal: () => {} },
      };
    }
    return originalRequire.apply(this, arguments);
  };

  return () => {
    Module.prototype.require = originalRequire;
  };
}

/**
 * Require a fresh copy of backend.js with Electron mocked.
 * mockElectron() purges the desktop module cache before patching require,
 * so the next require() gets a fresh module instance.
 * Returns { backend, cleanup } — call cleanup() after the test.
 */
function loadFreshBackend() {
  const cleanup = mockElectron();
  const backend = require('./backend.js');
  return { backend, cleanup };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('generateSecret()', () => {
  test('returns a 64-character string (256 bits)', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      const secret = backend.generateSecret();
      assert.strictEqual(secret.length, 64, 'expected 64 hex characters (32 bytes)');
    } finally {
      cleanup();
    }
  });

  test('returns a valid lowercase hexadecimal string', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      const secret = backend.generateSecret();
      assert.ok(
        /^[0-9a-f]{64}$/.test(secret),
        'expected only lowercase hex characters 0-9 a-f',
      );
    } finally {
      cleanup();
    }
  });

  test('caches the value — second call returns the same secret', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      const first = backend.generateSecret();
      const second = backend.generateSecret();
      assert.strictEqual(first, second, 'expected the cached secret to be identical');
    } finally {
      cleanup();
    }
  });

  test('generates a different secret on each fresh module load', () => {
    // Load the module twice independently and verify randomness.
    const { backend: b1, cleanup: c1 } = loadFreshBackend();
    try {
      const secret1 = b1.generateSecret();

      const { backend: b2, cleanup: c2 } = loadFreshBackend();
      try {
        const secret2 = b2.generateSecret();
        assert.notStrictEqual(
          secret1,
          secret2,
          'two fresh module loads should produce different secrets',
        );
      } finally {
        c2();
      }
    } finally {
      c1();
    }
  });

  test('is exported from the module', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      assert.strictEqual(
        typeof backend.generateSecret,
        'function',
        'generateSecret should be exported as a function',
      );
    } finally {
      cleanup();
    }
  });
});

describe('SPROUT_AUTH_TOKEN integration', () => {
  /**
   * NOTE: We verify SPROUT_AUTH_TOKEN integration via source inspection
   * because startBackendForWorkspace() cannot run in plain Node.js
   * (it requires the Electron runtime and a compiled backend binary).
   * Consider replacing with a behavioral test once an integration
   * harness is available.
   */
  const sourcePath = path.join(__dirname, 'backend.js');
  const source = fs.readFileSync(sourcePath, 'utf8');

  test('native spawn env includes SPROUT_AUTH_TOKEN: authToken', () => {
    // The native spawn block should contain an env object that spreads
    // process.env and sets SPROUT_AUTH_TOKEN to authToken.
    assert.ok(
      source.includes('SPROUT_AUTH_TOKEN: authToken'),
      'expected SPROUT_AUTH_TOKEN: authToken in the spawn env',
    );
  });

  test('WSL spawn env includes SPROUT_AUTH_TOKEN: authToken', () => {
    // The WSL spawn also uses { ...process.env, SPROUT_AUTH_TOKEN: authToken }
    // (verified by the same presence check; there should be TWO occurrences).
    const count = (source.match(/SPROUT_AUTH_TOKEN: authToken/g) || []).length;
    assert.strictEqual(
      count,
      2,
      'expected SPROUT_AUTH_TOKEN: authToken in BOTH native and WSL spawn envs',
    );
  });

  test('generateSecret() is called in startBackendForWorkspace()', () => {
    // Ensure the function is invoked at the start of the spawn flow.
    assert.ok(
      source.includes('generateSecret();'),
      'expected generateSecret() to be called inside startBackendForWorkspace',
    );
  });

  test('authToken is initialized as a module-level variable', () => {
    assert.ok(
      /let authToken/.test(source),
      'expected a module-level "let authToken" declaration',
    );
  });

  test('crypto import is present', () => {
    assert.ok(
      /require\(['"]node:crypto['"]\)/.test(source),
      'expected crypto import for randomBytes()',
    );
  });

  test('generateSecret is included in module.exports', () => {
    assert.ok(
      source.includes('generateSecret,') || source.includes('generateSecret\n'),
      'expected generateSecret in module.exports',
    );
  });
});

// ---------------------------------------------------------------------------
// Unix socket proxy (SP-060-B2)
// ---------------------------------------------------------------------------

describe('Unix socket proxy (SP-060-B2)', () => {
  const source = fs.readFileSync(path.join(__dirname, 'backend.js'), 'utf8');

  test('macOS/Linux native spawn args include --bind-socket', () => {
    assert.ok(
      source.includes('--bind-socket'),
      'expected --bind-socket in the macOS/Linux native spawn args',
    );
  });

  test('macOS/Linux native spawn args include --secret', () => {
    assert.ok(
      source.includes('--secret'),
      'expected --secret in the macOS/Linux native spawn args',
    );
  });

  test('generateSocketPath uses os.tmpdir()', () => {
    assert.ok(
      source.includes('os.tmpdir()'),
      'expected os.tmpdir() in generateSocketPath',
    );
  });

  test('generateSocketPath is exported', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      assert.strictEqual(
        typeof backend.generateSocketPath,
        'function',
        'generateSocketPath should be exported as a function',
      );
    } finally {
      cleanup();
    }
  });

  test('createSocketProxy is exported', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      assert.strictEqual(
        typeof backend.createSocketProxy,
        'function',
        'createSocketProxy should be exported as a function',
      );
    } finally {
      cleanup();
    }
  });

  test('waitForHealthOnSocket is exported', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      assert.strictEqual(
        typeof backend.waitForHealthOnSocket,
        'function',
        'waitForHealthOnSocket should be exported as a function',
      );
    } finally {
      cleanup();
    }
  });

  test('closeSocketProxy is exported', () => {
    const { backend, cleanup } = loadFreshBackend();
    try {
      assert.strictEqual(
        typeof backend.closeSocketProxy,
        'function',
        'closeSocketProxy should be exported as a function',
      );
    } finally {
      cleanup();
    }
  });

  test('os module is imported', () => {
    assert.ok(
      /require\(['"]node:os['"]\)/.test(source),
      'expected require("node:os") for os.tmpdir()',
    );
  });

  test('macOS/Linux native spawn env does NOT include SPROUT_AUTH_TOKEN', () => {
    // The macOS/Linux native block (between its section header comment and
    // the Windows native section header) should NOT contain SPROUT_AUTH_TOKEN.
    // We pass the secret via --secret flag instead of an env variable.
    // Find the macOS/Linux marker *inside* startBackendForWorkspace, not the
    // section header above the helper functions.
    const funcStart = source.indexOf('startBackendForWorkspace');
    const startIdx = source.indexOf('macOS / Linux native', funcStart);
    const endIdx = source.indexOf('Windows native', startIdx);
    assert.ok(startIdx >= 0, 'expected macOS/Linux native section marker inside startBackendForWorkspace');
    assert.ok(endIdx > startIdx, 'expected Windows native section after macOS/Linux');

    const nativeBlock = source.slice(startIdx, endIdx);
    assert.ok(
      !nativeBlock.includes('SPROUT_AUTH_TOKEN'),
      'expected macOS/Linux native spawn env to NOT include SPROUT_AUTH_TOKEN ' +
        '(secret is passed via --secret flag, not an env var)',
    );
  });

  test('createSocketProxy handles WebSocket upgrade', () => {
    assert.ok(
      source.includes("'upgrade'") || source.includes('"upgrade"'),
      'expected createSocketProxy to have an upgrade event handler for WebSocket forwarding',
    );
  });

  test('createSocketProxy injects Authorization header', () => {
    assert.ok(
      source.includes("Authorization: `Bearer ${token}`"),
      'expected Authorization: Bearer header injection in the socket proxy',
    );
  });

  test('Windows native spawn still uses --web-port', () => {
    // The Windows native branch should still use --web-port (TCP mode).
    assert.ok(
      source.includes('--web-port'),
      'expected --web-port in the Windows native spawn args',
    );
  });
});
