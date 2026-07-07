/**
 * Tests for Sprout Mode Configuration
 *
 * The mode module reads process.env.VITE_SPROUT_MODE at module load time,
 * so testing cloud mode requires resetting the module registry and re-importing
 * with the env var set before import.
 */

describe('mode config (default / local mode)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeAll(async () => {
    delete process.env.VITE_SPROUT_MODE;
    vi.resetModules();
    modeModule = await import('./mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('exports mode as "local" when VITE_SPROUT_MODE is not set', () => {
    expect(modeModule.mode).toBe('local');
  });

  it('exports isCloud as false', () => {
    expect(modeModule.isCloud).toBe(false);
  });

  it('exports supportsSSH as true in local mode', () => {
    expect(modeModule.supportsSSH).toBe(true);
  });

  it('exports supportsInstances as false in local mode', () => {
    expect(modeModule.supportsInstances).toBe(false);
  });

  it('exports supportsLocalTerminal as true in local mode', () => {
    expect(modeModule.supportsLocalTerminal).toBe(true);
  });

  it('exports supportsSettings as true in local mode', () => {
    expect(modeModule.supportsSettings).toBe(true);
  });

  it('exports supportsGit as true in local mode', () => {
    expect(modeModule.supportsGit).toBe(true);
  });

  it('exports supportsChat as true in local mode', () => {
    expect(modeModule.supportsChat).toBe(true);
  });

  it('exports supportsWorkspaceSwitching as true in local mode', () => {
    expect(modeModule.supportsWorkspaceSwitching).toBe(true);
  });

  it('exports supportsExport as true in local mode', () => {
    expect(modeModule.supportsExport).toBe(true);
  });

  it('mode is a valid SproutMode value', () => {
    expect(['local', 'cloud']).toContain(modeModule.mode);
  });

  it('isCloud is strictly a boolean', () => {
    expect(typeof modeModule.isCloud).toBe('boolean');
  });
});

describe('mode config (cloud mode)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeAll(async () => {
    // Set the env var before importing the module
    process.env.VITE_SPROUT_MODE = 'cloud';
    vi.resetModules();

    modeModule = await import('./mode');
  });

  afterAll(() => {
    // Restore the original env var
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('exports mode as "cloud" when VITE_SPROUT_MODE is "cloud"', () => {
    expect(modeModule.mode).toBe('cloud');
  });

  it('exports isCloud as true', () => {
    expect(modeModule.isCloud).toBe(true);
  });

  // In cloud build mode without an adapter installed, supportsSSH uses
  // the local default (true) because there's no adapter to say otherwise.
  it('exports supportsSSH as true in cloud build without adapter (local default applies)', () => {
    expect(modeModule.supportsSSH).toBe(true);
  });

  it('exports supportsInstances as true in cloud mode', () => {
    expect(modeModule.supportsInstances).toBe(true);
  });

  it('exports supportsLocalTerminal as false in cloud mode', () => {
    expect(modeModule.supportsLocalTerminal).toBe(false);
  });

  it('exports supportsSettings as false in cloud mode', () => {
    expect(modeModule.supportsSettings).toBe(false);
  });

  it('exports supportsGit as true in cloud mode without adapter (local default applies)', () => {
    expect(modeModule.supportsGit).toBe(true);
  });

  it('exports supportsChat as true in cloud mode without adapter (local default applies)', () => {
    expect(modeModule.supportsChat).toBe(true);
  });

  it('exports supportsWorkspaceSwitching as true in cloud mode without adapter (local default applies)', () => {
    expect(modeModule.supportsWorkspaceSwitching).toBe(true);
  });

  it('exports supportsExport as true in cloud mode without adapter (local default applies)', () => {
    expect(modeModule.supportsExport).toBe(true);
  });

  it('mode is a valid SproutMode value', () => {
    expect(['local', 'cloud']).toContain(modeModule.mode);
  });

  it('isCloud is strictly a boolean', () => {
    expect(typeof modeModule.isCloud).toBe('boolean');
  });
});

describe('mode config (invalid env var value)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeAll(async () => {
    // Any value other than 'cloud' should default to 'local'
    process.env.VITE_SPROUT_MODE = 'staging';
    vi.resetModules();

    modeModule = await import('./mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('falls back to "local" mode for unrecognized values', () => {
    expect(modeModule.mode).toBe('local');
  });

  it('isCloud is false for unrecognized values', () => {
    expect(modeModule.isCloud).toBe(false);
  });

  it('all local-mode flags are correct for unrecognized values', () => {
    expect(modeModule.supportsSSH).toBe(true);
    expect(modeModule.supportsGit).toBe(true);
    expect(modeModule.supportsChat).toBe(true);
    expect(modeModule.supportsWorkspaceSwitching).toBe(true);
    expect(modeModule.supportsExport).toBe(true);
    expect(modeModule.supportsInstances).toBe(false);
    expect(modeModule.supportsLocalTerminal).toBe(true);
    expect(modeModule.supportsSettings).toBe(true);
  });
});

describe('mode config (empty string env var)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeAll(async () => {
    process.env.VITE_SPROUT_MODE = '';
    vi.resetModules();

    modeModule = await import('./mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('treats empty string as local mode', () => {
    expect(modeModule.mode).toBe('local');
    expect(modeModule.isCloud).toBe(false);
  });
});

describe('mode config flag invariants', () => {
  // Re-import to ensure clean state regardless of test ordering
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.VITE_SPROUT_MODE;

  describe('in local mode', () => {
    beforeAll(async () => {
      delete process.env.VITE_SPROUT_MODE;
      vi.resetModules();
      modeModule = await import('./mode');
    });

    afterAll(() => {
      if (originalEnv !== undefined) {
        process.env.VITE_SPROUT_MODE = originalEnv;
      }
      vi.resetModules();
    });

    it('supportsSSH is true without an adapter (local default)', () => {
      expect(modeModule.supportsSSH).toBe(true);
    });

    it('supportsInstances equals isCloud', () => {
      expect(modeModule.supportsInstances).toBe(modeModule.isCloud);
    });

    it('supportsLocalTerminal is the negation of isCloud', () => {
      expect(modeModule.supportsLocalTerminal).toBe(!modeModule.isCloud);
    });

    it('supportsSettings is the negation of isCloud', () => {
      expect(modeModule.supportsSettings).toBe(!modeModule.isCloud);
    });

    it('supportsGit is true without an adapter (local default)', () => {
      expect(modeModule.supportsGit).toBe(true);
    });

    it('supportsChat is true without an adapter (local default)', () => {
      expect(modeModule.supportsChat).toBe(true);
    });

    it('supportsWorkspaceSwitching is true without an adapter (local default)', () => {
      expect(modeModule.supportsWorkspaceSwitching).toBe(true);
    });

    it('supportsExport is true without an adapter (local default)', () => {
      expect(modeModule.supportsExport).toBe(true);
    });
  });

  describe('in cloud mode', () => {
    beforeAll(async () => {
      process.env.VITE_SPROUT_MODE = 'cloud';
      vi.resetModules();
      modeModule = await import('./mode');
    });

    afterAll(() => {
      if (originalEnv === undefined) {
        delete process.env.VITE_SPROUT_MODE;
      } else {
        process.env.VITE_SPROUT_MODE = originalEnv;
      }
      vi.resetModules();
    });

    // Without an adapter installed, the local default (true) applies even
    // in cloud build mode. The "with CloudAdapter installed" test suite
    // covers the case where supportsSSH is explicitly false.
    it('supportsSSH is true without an adapter (local default applies)', () => {
      expect(modeModule.supportsSSH).toBe(true);
    });

    it('supportsInstances equals isCloud', () => {
      expect(modeModule.supportsInstances).toBe(modeModule.isCloud);
    });

    it('supportsLocalTerminal is the negation of isCloud', () => {
      expect(modeModule.supportsLocalTerminal).toBe(!modeModule.isCloud);
    });

    it('supportsSettings is the negation of isCloud', () => {
      expect(modeModule.supportsSettings).toBe(!modeModule.isCloud);
    });
  });
});

describe('with CloudAdapter installed', () => {
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeEach(() => {
    vi.resetModules();
    process.env.VITE_SPROUT_MODE = 'local';
  });

  afterEach(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('adapter flags override build-time defaults', async () => {
    const { installAdapter } = await import('../services/apiAdapter');
    const { CloudAdapter } = await import('../services/cloudAdapter');
    installAdapter(
      new CloudAdapter({
        apiBase: 'https://api.test.sprout.dev',
        wsUrl: 'wss://api.test.sprout.dev/ws',
      }),
    );

    // Import mode.ts — it evaluates getAdapter() at load time and sees the CloudAdapter
    const modeModule = await import('./mode');

    // Build-time says local, but CloudAdapter flags win
    expect(modeModule.supportsSSH).toBe(false);
    expect(modeModule.supportsGit).toBe(false);
    expect(modeModule.supportsChat).toBe(true);
    expect(modeModule.supportsWorkspaceSwitching).toBe(false);
    expect(modeModule.supportsExport).toBe(false);
    expect(modeModule.supportsInstances).toBe(true);
    expect(modeModule.supportsLocalTerminal).toBe(false);
    expect(modeModule.supportsSettings).toBe(true);
  });
  it('mode and isCloud remain based on env var, not adapter', async () => {
    const { installAdapter } = await import('../services/apiAdapter');
    const { CloudAdapter } = await import('../services/cloudAdapter');
    installAdapter(
      new CloudAdapter({
        apiBase: 'https://api.test.sprout.dev',
        wsUrl: 'wss://api.test.sprout.dev/ws',
      }),
    );

    const modeModule = await import('./mode');

    // mode and isCloud are determined by env var alone, not by the adapter
    expect(modeModule.mode).toBe('local');
    expect(modeModule.isCloud).toBe(false);
  });
});

describe('with custom adapter installed', () => {
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeEach(() => {
    vi.resetModules();
    process.env.VITE_SPROUT_MODE = 'local';
  });

  afterEach(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('custom adapter flags are read exactly as provided', async () => {
    const { installAdapter } = await import('../services/apiAdapter');

    // Install a mock adapter with non-standard flag combination that
    // differs from both local and cloud defaults — this proves the
    // adapter path is truly exercised (not just matching defaults).
    installAdapter({
      name: 'custom-test-adapter',
      fetch: async () => new Response(),
      getWebSocketURL: () => null,
      requiresBackendHealthCheck: false,
      fileOpsViaAPI: true,
      showOnboarding: true,
      supportsSSH: false,
      supportsGit: false,
      supportsChat: true,
      supportsWorkspaceSwitching: false,
      supportsExport: false,
      supportsInstances: true,
      supportsLocalTerminal: true,
      supportsSettings: false,
    });

    const modeModule = await import('./mode');

    // mode.ts reads from the adapter, not build-time constants
    expect(modeModule.mode).toBe('local');
    expect(modeModule.isCloud).toBe(false);
    expect(modeModule.supportsSSH).toBe(false);
    expect(modeModule.supportsGit).toBe(false);
    expect(modeModule.supportsChat).toBe(true);
    expect(modeModule.supportsWorkspaceSwitching).toBe(false);
    expect(modeModule.supportsExport).toBe(false);
    expect(modeModule.supportsInstances).toBe(true);
    expect(modeModule.supportsLocalTerminal).toBe(true);
    expect(modeModule.supportsSettings).toBe(false);
  });
});
