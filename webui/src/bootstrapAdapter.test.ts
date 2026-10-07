/**
 * Tests for bootstrapAdapter.ts
 *
 * Verifies that CloudAdapter is installed at startup when the active host is
 * the hosted/cloud host, and that no adapter is installed for the local host.
 *
 * host.8: the adapter install branches on the ACTIVE HOST's transport (bearer
 * = cloud), not on a build flag or an appMode field. These tests set the host
 * through the accessor exactly as the entry point does, and assert the
 * transport + installed-adapter behavior the old appMode assertions protected.
 */

import type { CloudAdapter } from './services/cloudAdapter';

// Mock window.location used by bootstrapAdapter
const originalLocation = window.location;

function mockWindowLocation(origin: string, protocol: string, host: string) {
  Object.defineProperty(window, 'location', {
    writable: true,
    value: { origin, protocol, host },
    configurable: true,
  });
}

function restoreWindowLocation() {
  Object.defineProperty(window, 'location', {
    writable: true,
    value: originalLocation,
    configurable: true,
  });
}

/**
 * Record the active host the way the app entry does. Called after
 * vi.resetModules() so the accessor singleton bootstrapAdapter imports is the
 * one we configure.
 */
async function activateCloudHost() {
  const { setActiveHost } = await import('./host/accessor');
  const { cloudHost } = await import('./host/cloudHost');
  setActiveHost(cloudHost);
}

async function activateLocalHost() {
  const { setActiveHost } = await import('./host/accessor');
  const { localHost } = await import('./host/localHost');
  setActiveHost(localHost);
}

/**
 * Import bootstrapAdapter and drive the async adapter install to completion.
 *
 * bootstrapAdapter.ts auto-runs fetchRuntimeConfig() on import, but adapter
 * installation is now async (dynamic import of CloudAdapter). This helper
 * ensures the fire-and-forget bootstrap settles before assertions run.
 */
async function importWithBootstrap() {
  const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
  await fetchRuntimeConfig();
}

describe('bootstrapAdapter', () => {
  describe('cloud host (bearer transport)', () => {
    beforeEach(async () => {
      vi.resetModules();
      // Mock fetch to reject (tier 1 fails) so tier 2 env vars are used
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
      vi.stubEnv('VITE_FOUNDRY_API_URL', 'https://foundry.test.sprout.dev/api');
      vi.stubEnv('VITE_FOUNDRY_WS_URL', 'wss://foundry.test.sprout.dev/ws');
      mockWindowLocation('https://app.test.sprout.dev', 'https:', 'app.test.sprout.dev');
      await activateCloudHost();
    });

    afterEach(() => {
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
      restoreWindowLocation();
      vi.resetModules();
    });

    it('installs CloudAdapter at startup', async () => {
      await importWithBootstrap();

      const { hasAdapter } = await import('./services/apiAdapter');
      expect(hasAdapter()).toBe(true);
    });

    it('installs an adapter named "foundry-cloud"', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter).not.toBeNull();
      expect(adapter!.name).toBe('foundry-cloud');
    });

    it('installs a CloudAdapter instance', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const { CloudAdapter: CloudAdapterClass } = await import('./services/cloudAdapter');
      const adapter = getAdapter();
      expect(adapter).toBeInstanceOf(CloudAdapterClass);
    });

    it('configures adapter with correct WebSocket URL from env var', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter!.getWebSocketURL()).toBe('wss://foundry.test.sprout.dev/ws');
    });

    it('configures adapter with cloud platform nav items', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter() as CloudAdapter | null;
      expect(adapter!.platformNavItems).toBeDefined();
      expect(adapter!.platformNavItems!.length).toBeGreaterThanOrEqual(6);

      const navIds = adapter!.platformNavItems!.map((item) => item.id);
      expect(navIds).toContain('tasks');
      expect(navIds).toContain('billing');
      expect(navIds).toContain('team');
    });

    it('adapter has correct capability flags for cloud mode', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter!.requiresBackendHealthCheck).toBe(true);
      expect(adapter!.fileOpsViaAPI).toBe(false);
      expect(adapter!.showOnboarding).toBe(false);
      expect(adapter!.supportsSSH).toBe(false);
      expect(adapter!.supportsInstances).toBe(true);
      expect(adapter!.supportsLocalTerminal).toBe(false);
      expect(adapter!.supportsSettings).toBe(true); // SP-CLOUD-3: BYOK settings enabled
    });
  });

  describe('local host (none transport)', () => {
    beforeEach(async () => {
      vi.resetModules();
      // Mock fetch to reject and clear env vars
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      vi.unstubAllEnvs();
      mockWindowLocation('http://localhost:3000', 'http:', 'localhost:3000');
      await activateLocalHost();
    });

    afterEach(() => {
      vi.unstubAllGlobals();
      vi.unstubAllEnvs();
      restoreWindowLocation();
      vi.resetModules();
    });

    it('does not install any adapter', async () => {
      await import('./bootstrapAdapter');

      const { hasAdapter } = await import('./services/apiAdapter');
      expect(hasAdapter()).toBe(false);
    });

    it('getAdapter returns null for the local host', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      expect(getAdapter()).toBeNull();
    });

    it('the resolved config carries the local (none) transport', async () => {
      const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
      const config = await fetchRuntimeConfig();
      expect(config.authMode).toBe('none');
    });
  });

  describe('local host with VITE_SPROUT_MODE=local', () => {
    beforeEach(async () => {
      vi.resetModules();
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      vi.stubEnv('VITE_SPROUT_MODE', 'local');
      mockWindowLocation('http://localhost:3000', 'http:', 'localhost:3000');
      await activateLocalHost();
    });

    afterEach(() => {
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
      restoreWindowLocation();
      vi.resetModules();
    });

    it('does not install any adapter', async () => {
      await import('./bootstrapAdapter');

      const { hasAdapter } = await import('./services/apiAdapter');
      expect(hasAdapter()).toBe(false);
    });
  });

  describe('fallback when env vars are not set in a cloud build (Pages Functions proxy case)', () => {
    beforeEach(async () => {
      vi.resetModules();
      // Mock fetch to reject (tier 1 fails). With no Foundry URLs the runtime
      // config derives same-origin URLs from window.location (Cloudflare Pages
      // Functions proxy). The CloudAdapter install is driven by the host.
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
      // Do NOT set VITE_FOUNDRY_API_URL or VITE_FOUNDRY_WS_URL.
      mockWindowLocation('https://app.test.sprout.dev', 'https:', 'app.test.sprout.dev');
      await activateCloudHost();
    });

    afterEach(() => {
      vi.unstubAllGlobals();
      vi.unstubAllEnvs();
      restoreWindowLocation();
      vi.resetModules();
    });

    it('installs CloudAdapter with same-origin defaults', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter).not.toBeNull();
      expect(adapter!.name).toBe('foundry-cloud');
    });

    it('derives WebSocket URL from window.location when VITE_FOUNDRY_WS_URL is not set', async () => {
      await importWithBootstrap();

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter!.getWebSocketURL()).toBe('wss://app.test.sprout.dev/ws');
    });
  });

  describe('fetchRuntimeConfig — three-tier fallback', () => {
    let consoleLogSpy: ReturnType<typeof vi.spyOn>;
    let consoleWarnSpy: ReturnType<typeof vi.spyOn>;
    let fetchSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
      vi.resetModules();
      consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
      consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      fetchSpy = vi.spyOn(globalThis, 'fetch');
    });

    afterEach(() => {
      consoleLogSpy.mockRestore();
      consoleWarnSpy.mockRestore();
      fetchSpy.mockRestore();
      vi.unstubAllEnvs();
      vi.resetModules();
    });

    describe('Tier 1: server /api/bootstrap', () => {
      it('returns server config when fetch succeeds', async () => {
        const serverConfig = {
          apiBaseURL: 'http://server:8080',
          wsURL: 'ws://server:8080/ws',
          authMode: 'bearer',
          appMode: 'cloud',
          buildVersion: '1.0.0',
          sharedMode: false,
        };
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve(serverConfig),
        } as any);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        // host.8: appMode is gone from RuntimeConfig; the transport is asserted
        // instead — the behavior the appMode field used to carry.
        expect(config).toEqual({
          apiBaseURL: 'http://server:8080',
          wsURL: 'ws://server:8080/ws',
          authMode: 'bearer',
          buildVersion: '1.0.0',
          sharedMode: false,
          navItems: undefined,
          user: undefined,
          sync: undefined,
          update: undefined,
          platformURL: undefined,
        });
        expect('appMode' in config).toBe(false);
      });

      it('caches the fetched config in getBootstrapConfig', async () => {
        const serverConfig = {
          apiBaseURL: 'http://server:8080',
          wsURL: 'ws://server:8080/ws',
          authMode: 'none',
          appMode: 'local',
          buildVersion: '1.0.0',
          sharedMode: false,
        };
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve(serverConfig),
        } as any);

        const { fetchRuntimeConfig, getBootstrapConfig } = await import('./bootstrapAdapter');
        await fetchRuntimeConfig();

        const cached = getBootstrapConfig();
        expect(cached.apiBaseURL).toBe('http://server:8080');
        expect(cached.wsURL).toBe('ws://server:8080/ws');
        expect(cached.authMode).toBe('none');
        expect(cached.buildVersion).toBe('1.0.0');
      });
    });

    describe('Tier 2: VITE env vars fallback', () => {
      it('falls back to env vars when fetch rejects', async () => {
        fetchSpy.mockRejectedValue(new Error('network error'));
        vi.stubEnv('VITE_FOUNDRY_API_URL', 'http://env:9090');
        vi.stubEnv('VITE_FOUNDRY_WS_URL', 'ws://env:9090/ws');
        vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
        vi.stubEnv('VITE_BUILD_VERSION', '2.0.0');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://env:9090');
        expect(config.wsURL).toBe('ws://env:9090/ws');
        expect(config.buildVersion).toBe('2.0.0');
        expect(consoleWarnSpy).toHaveBeenCalledWith(expect.stringContaining('using VITE env vars'), expect.anything());
      });

      it('falls back to env vars when server returns 500', async () => {
        fetchSpy.mockResolvedValue({
          ok: false,
          status: 500,
          json: () => Promise.reject(new Error('Internal Server Error')),
        } as any);
        vi.stubEnv('VITE_FOUNDRY_API_URL', 'http://env:9090');
        vi.stubEnv('VITE_FOUNDRY_WS_URL', 'ws://env:9090/ws');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://env:9090');
        expect(config.wsURL).toBe('ws://env:9090/ws');
        expect(consoleWarnSpy).toHaveBeenCalledWith(expect.stringContaining('using VITE env vars'), expect.anything());
      });

      it('uses partial env vars with defaults for missing fields in local mode', async () => {
        fetchSpy.mockRejectedValue(new Error('no network'));
        vi.stubEnv('VITE_FOUNDRY_API_URL', 'http://partial:7777');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://partial:7777');
        expect(config.wsURL).toBe('ws://localhost:56000/ws'); // local-mode fallback
        expect(config.buildVersion).toBe('dev'); // falls back to default
      });
    });

    describe('Tier 3: localhost defaults', () => {
      it('falls back to localhost defaults when fetch fails and no env vars', async () => {
        fetchSpy.mockRejectedValue(new Error('network error'));
        vi.stubEnv('VITE_FOUNDRY_API_URL', undefined);
        vi.stubEnv('VITE_FOUNDRY_WS_URL', undefined);
        vi.stubEnv('VITE_SPROUT_MODE', undefined);
        vi.stubEnv('VITE_BUILD_VERSION', undefined);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://localhost:56000');
        expect(config.wsURL).toBe('ws://localhost:56000/ws');
        expect(config.authMode).toBe('none');
        expect(config.buildVersion).toBe('dev');
      });

      it('uses localhost defaults when server returns non-JSON and no env vars', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.reject(new Error('Unexpected token')),
        } as any);
        vi.stubEnv('VITE_FOUNDRY_API_URL', undefined);
        vi.stubEnv('VITE_FOUNDRY_WS_URL', undefined);
        vi.stubEnv('VITE_SPROUT_MODE', undefined);
        vi.stubEnv('VITE_BUILD_VERSION', undefined);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://localhost:56000');
        expect(config.wsURL).toBe('ws://localhost:56000/ws');
      });
    });

    describe('edge cases', () => {
      it('falls back to env vars when server returns non-JSON', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.reject(new Error('invalid json')),
        } as any);
        vi.stubEnv('VITE_FOUNDRY_API_URL', 'http://fallback:3000');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://fallback:3000');
        expect(consoleWarnSpy).toHaveBeenCalledWith(expect.stringContaining('using VITE env vars'), expect.anything());
      });

      it('falls back when server returns JSON without apiBaseURL', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve({ foo: 'bar' }),
        } as any);
        vi.stubEnv('VITE_FOUNDRY_API_URL', 'http://fallback:3000');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://fallback:3000');
        // fetchError is null here because JSON parsed OK but validation failed
        expect(consoleWarnSpy).toHaveBeenCalledWith('bootstrap: using VITE env vars (fetch failed: %s)', null);
      });

      it('uses server config even when partially missing optional fields', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve({ apiBaseURL: 'http://srv:1234' }),
        } as any);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://srv:1234');
        expect(config.authMode).toBe('none'); // defaults
        expect(config.buildVersion).toBe('dev'); // defaults
      });

      it('hosted host without explicit URLs derives same-origin defaults', async () => {
        vi.resetModules();
        fetchSpy.mockRejectedValue(new Error('no bootstrap endpoint'));
        vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
        mockWindowLocation('https://app.example.com', 'https:', 'app.example.com');
        // host.8: the same-origin fallback is chosen by the ACTIVE HOST (the
        // hosted transport), not the build flag.
        await activateCloudHost();

        try {
          const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
          const config = await fetchRuntimeConfig();

          expect(config.apiBaseURL).toBe('https://app.example.com');
          expect(config.wsURL).toBe('wss://app.example.com/ws');
        } finally {
          restoreWindowLocation();
        }
      });
    });
  });

  describe('getBootstrapConfig', () => {
    let consoleLogSpy: ReturnType<typeof vi.spyOn>;
    let consoleWarnSpy: ReturnType<typeof vi.spyOn>;
    let fetchSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
      vi.resetModules();
      consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
      consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      fetchSpy = vi.spyOn(globalThis, 'fetch');
    });

    afterEach(() => {
      consoleLogSpy.mockRestore();
      consoleWarnSpy.mockRestore();
      fetchSpy.mockRestore();
      vi.unstubAllEnvs();
      vi.resetModules();
    });

    it('returns the last resolved config after fetchRuntimeConfig', async () => {
      const serverConfig = {
        apiBaseURL: 'http://cached:9999',
        wsURL: 'ws://cached:9999/ws',
        authMode: 'bearer',
        appMode: 'cloud',
        buildVersion: '3.0.0',
        sharedMode: false,
      };
      fetchSpy.mockResolvedValue({
        json: () => Promise.resolve(serverConfig),
      } as any);

      const { fetchRuntimeConfig, getBootstrapConfig } = await import('./bootstrapAdapter');
      await fetchRuntimeConfig();

      const cached = getBootstrapConfig();
      expect(cached.apiBaseURL).toBe('http://cached:9999');
      expect(cached.wsURL).toBe('ws://cached:9999/ws');
      expect(cached.authMode).toBe('bearer');
      expect(cached.buildVersion).toBe('3.0.0');
      expect('appMode' in cached).toBe(false);
    });

    it('returns localhost defaults before fetchRuntimeConfig resolves', async () => {
      // Mock fetch to hang so the auto-run never completes
      fetchSpy.mockImplementation(() => new Promise(() => {}));

      const { getBootstrapConfig } = await import('./bootstrapAdapter');

      // The module-level fetchRuntimeConfig() is fire-and-forget and hasn't resolved
      expect(getBootstrapConfig()).toEqual({
        apiBaseURL: 'http://localhost:56000',
        wsURL: 'ws://localhost:56000/ws',
        authMode: 'none',
        buildVersion: 'dev',
      });
    });
  });
});
