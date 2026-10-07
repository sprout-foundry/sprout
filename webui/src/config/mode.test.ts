/**
 * Tests for Sprout Mode Configuration
 *
 * The mode module reads process.env.VITE_SPROUT_MODE at module load time,
 * so testing cloud mode requires resetting the module registry and re-importing
 * with the env var set before import.
 *
 * host.8 removed the `isCloud` and `mode` exports: no module outside
 * config/mode.ts may read the build mode, so the binding is internal. These
 * tests therefore assert the capability bindings only — the observable
 * behavior the module still exposes. The build-mode switch is exercised
 * indirectly through the local-vs-cloud capability defaults.
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

  it('does not export the build mode or an isCloud flag (host.8)', () => {
    expect('isCloud' in modeModule).toBe(false);
    expect('mode' in modeModule).toBe(false);
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

  it('exports supportsAutomations as true in local mode', () => {
    expect(modeModule.supportsAutomations).toBe(true);
  });

  it('exports supportsAgentChanges as true in local mode', () => {
    expect(modeModule.supportsAgentChanges).toBe(true);
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

  // host.8: with no active host and no adapter, the bindings use the LOCAL
  // default — the build mode no longer seeds them. The hosted build's cloud
  // values come from cloudHost, which the entry always sets; the cloud
  // capability values themselves are pinned by hosts.test.ts.
  it('exports the local defaults in cloud mode with no host and no adapter', () => {
    expect(modeModule.supportsSSH).toBe(true);
    expect(modeModule.supportsInstances).toBe(false);
    expect(modeModule.supportsLocalTerminal).toBe(true);
  });

  // supportsSettings is true in both modes — BYOK settings are available.
  it('exports supportsSettings as true in cloud mode (BYOK settings)', () => {
    expect(modeModule.supportsSettings).toBe(true);
  });

  // supportsGit is true in both modes — browser-native git (isomorphic-git +
  // lightning-fs) powers the core git flow in the hosted build. Unimplemented
  // ops return honest errors; see browserGit.ts.
  it('exports supportsGit as true in cloud mode (browser-native git)', () => {
    expect(modeModule.supportsGit).toBe(true);
  });

  it('exports supportsChat as true in cloud mode (BYOK proxy)', () => {
    expect(modeModule.supportsChat).toBe(true);
  });

  it('exports supportsWorkspaceSwitching as true in cloud mode with no host (local default)', () => {
    expect(modeModule.supportsWorkspaceSwitching).toBe(true);
  });

  it('exports supportsExport as true in cloud mode with no host (local default)', () => {
    expect(modeModule.supportsExport).toBe(true);
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
    expect(modeModule.supportsSSH).toBe(true);
    expect(modeModule.supportsInstances).toBe(false);
    expect(modeModule.supportsLocalTerminal).toBe(true);
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

    it('supportsInstances is off and localTerminal is on in local mode', () => {
      expect(modeModule.supportsInstances).toBe(false);
      expect(modeModule.supportsLocalTerminal).toBe(true);
    });

    it('supportsSettings is true in local mode', () => {
      expect(modeModule.supportsSettings).toBe(true);
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

    // host.8: with no adapter and no host, the local defaults apply — the
    // build mode no longer seeds them. cloudHost carries the cloud values.
    it('supportsSSH is true in cloud mode with no adapter and no host (local default)', () => {
      expect(modeModule.supportsSSH).toBe(true);
    });

    it('supportsInstances is off and localTerminal is on in cloud mode with no host', () => {
      expect(modeModule.supportsInstances).toBe(false);
      expect(modeModule.supportsLocalTerminal).toBe(true);
    });

    // supportsSettings is true in both modes (BYOK settings in cloud).
    it('supportsSettings is true in cloud mode (BYOK settings)', () => {
      expect(modeModule.supportsSettings).toBe(true);
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
    expect(modeModule.supportsGit).toBe(true);
    expect(modeModule.supportsChat).toBe(true);
    expect(modeModule.supportsWorkspaceSwitching).toBe(false);
    expect(modeModule.supportsExport).toBe(false);
    expect(modeModule.supportsInstances).toBe(true);
    expect(modeModule.supportsLocalTerminal).toBe(false);
    expect(modeModule.supportsSettings).toBe(true);
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

describe('live capability refresh (adapter installs AFTER mode.ts loads)', () => {
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
    window.dispatchEvent(new Event('sprout:adapter-installed'));
  });

  // The REAL app sequence: config/mode.ts is imported by the component tree
  // (module scope, before bootstrap resolves), then bootstrapAdapter installs
  // the adapter asynchronously. The flags must flip WITHOUT re-importing the
  // module — this is the case the previous frozen-const design silently
  // failed (the "adapter flags override" tests above only pass because they
  // install BEFORE import).
  it('flags re-evaluate when ADAPTER_INSTALLED_EVENT fires post-load', async () => {
    const modeModule = await import('./mode');
    expect(modeModule.supportsWorkspaceSwitching).toBe(true); // local default
    expect(modeModule.supportsLocalTerminal).toBe(true);

    const { installAdapter } = await import('../services/apiAdapter');
    installAdapter({
      name: 'late-install-adapter',
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
      supportsLocalTerminal: false,
      supportsSettings: false,
    });

    // Live bindings: importers see the new values with no re-import.
    expect(modeModule.supportsWorkspaceSwitching).toBe(false);
    expect(modeModule.supportsLocalTerminal).toBe(false);
    expect(modeModule.supportsSSH).toBe(false);
    expect(modeModule.supportsGit).toBe(false);
    expect(modeModule.supportsExport).toBe(false);
    expect(modeModule.supportsInstances).toBe(true);
    expect(modeModule.supportsSettings).toBe(false);
  });

  it('malformed adapter flags do not crash the refresh (defaults stay)', async () => {
    const modeModule = await import('./mode');
    const { installAdapter } = await import('../services/apiAdapter');
    installAdapter({
      name: 'partial-adapter',
      fetch: async () => new Response(),
      getWebSocketURL: () => null,
      requiresBackendHealthCheck: false,
      fileOpsViaAPI: true,
      showOnboarding: true,
      supportsSSH: false,
      // git/chat/workspaceSwitching/export/instances/localTerminal/settings
      // deliberately omitted (undefined) — capability() falls back per-key.
      supportsGit: undefined,
      supportsChat: undefined,
      supportsWorkspaceSwitching: undefined,
      supportsExport: undefined,
      supportsInstances: undefined,
      supportsLocalTerminal: undefined,
      supportsSettings: undefined,
    } as unknown as Parameters<typeof installAdapter>[0]);

    // Adapter-declared key applies; undefined keys keep mode defaults.
    expect(modeModule.supportsSSH).toBe(false);
    expect(modeModule.supportsGit).toBe(true); // local default, adapter said nothing
  });
});
