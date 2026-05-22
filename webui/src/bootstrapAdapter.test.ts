/**
 * Tests for bootstrapAdapter.ts
 *
 * Verifies that CloudAdapter is installed at startup when VITE_SPROUT_MODE=cloud,
 * and that no adapter is installed in local mode.
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

describe('bootstrapAdapter', () => {
  describe('cloud mode (VITE_APP_MODE=cloud)', () => {
    beforeEach(() => {
      vi.resetModules();
      // Mock fetch to reject (tier 1 fails) so tier 2 env vars are used
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      // Set cloud mode via import.meta.env (new env var names)
      vi.stubEnv('VITE_APP_MODE', 'cloud');
      vi.stubEnv('VITE_API_BASE_URL', 'https://foundry.test.sprout.dev/api');
      vi.stubEnv('VITE_WS_URL', 'wss://foundry.test.sprout.dev/ws');
      mockWindowLocation('https://app.test.sprout.dev', 'https:', 'app.test.sprout.dev');
    });

    afterEach(() => {
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
      restoreWindowLocation();
      vi.resetModules();
    });

    it('installs CloudAdapter at startup', async () => {
      // Import bootstrapAdapter — this triggers the adapter installation
      await import('./bootstrapAdapter');

      const { hasAdapter } = await import('./services/apiAdapter');
      expect(hasAdapter()).toBe(true);
    });

    it('installs an adapter named "foundry-cloud"', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter).not.toBeNull();
      expect(adapter!.name).toBe('foundry-cloud');
    });

    it('installs a CloudAdapter instance', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const { CloudAdapter: CloudAdapterClass } = await import('./services/cloudAdapter');
      const adapter = getAdapter();
      expect(adapter).toBeInstanceOf(CloudAdapterClass);
    });

    it('configures adapter with correct WebSocket URL from env var', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter!.getWebSocketURL()).toBe('wss://foundry.test.sprout.dev/ws');
    });

    it('configures adapter with cloud platform nav items', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter() as CloudAdapter | null;
      expect(adapter!.platformNavItems).toBeDefined();
      expect(adapter!.platformNavItems!.length).toBe(3);

      const navIds = adapter!.platformNavItems!.map((item) => item.id);
      expect(navIds).toContain('tasks');
      expect(navIds).toContain('billing');
      expect(navIds).toContain('team');
    });

    it('adapter has correct capability flags for cloud mode', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter!.requiresBackendHealthCheck).toBe(true);
      expect(adapter!.fileOpsViaAPI).toBe(false);
      expect(adapter!.showOnboarding).toBe(false);
      expect(adapter!.supportsSSH).toBe(false);
      expect(adapter!.supportsInstances).toBe(true);
      expect(adapter!.supportsLocalTerminal).toBe(false);
      expect(adapter!.supportsSettings).toBe(false);
    });
  });

  describe('local mode (default)', () => {
    beforeEach(() => {
      vi.resetModules();
      // Mock fetch to reject and clear env vars
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      vi.unstubAllEnvs();
      mockWindowLocation('http://localhost:3000', 'http:', 'localhost:3000');
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

    it('getAdapter returns null in local mode', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      expect(getAdapter()).toBeNull();
    });
  });

  describe('local mode (VITE_SPROUT_MODE=local)', () => {
    beforeEach(() => {
      vi.resetModules();
      process.env.VITE_SPROUT_MODE = 'local';
      delete process.env.VITE_FOUNDRY_API_URL;
      delete process.env.VITE_FOUNDRY_WS_URL;
      mockWindowLocation('http://localhost:3000', 'http:', 'localhost:3000');
    });

    afterEach(() => {
      delete process.env.VITE_SPROUT_MODE;
      restoreWindowLocation();
      vi.resetModules();
    });

    it('does not install any adapter', async () => {
      await import('./bootstrapAdapter');

      const { hasAdapter } = await import('./services/apiAdapter');
      expect(hasAdapter()).toBe(false);
    });
  });

  describe('fallback when env vars are not set in cloud mode', () => {
    beforeEach(() => {
      vi.resetModules();
      // Mock fetch to reject (tier 1 fails), and only set appMode to cloud
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network error')));
      vi.stubEnv('VITE_APP_MODE', 'cloud');
      // Do NOT set VITE_API_BASE_URL or VITE_WS_URL — should fall back to defaults
      mockWindowLocation('https://app.test.sprout.dev', 'https:', 'app.test.sprout.dev');
    });

    afterEach(() => {
      vi.unstubAllGlobals();
      vi.unstubAllEnvs();
      restoreWindowLocation();
      vi.resetModules();
    });

    it('installs CloudAdapter with fallback to localhost defaults', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter).not.toBeNull();
      expect(adapter!.name).toBe('foundry-cloud');
    });

    it('uses default WebSocket URL when VITE_WS_URL is not set', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      // Falls back to localhost defaults since VITE_WS_URL is not set
      expect(adapter!.getWebSocketURL()).toBe('ws://localhost:56000/ws');
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
        };
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve(serverConfig),
        } as any);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config).toEqual(serverConfig);
        expect(consoleLogSpy).toHaveBeenCalledWith(
          'bootstrap: fetched config from /api/bootstrap'
        );
      });

      it('caches the fetched config in getBootstrapConfig', async () => {
        const serverConfig = {
          apiBaseURL: 'http://server:8080',
          wsURL: 'ws://server:8080/ws',
          authMode: 'none',
          appMode: 'local',
          buildVersion: '1.0.0',
        };
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve(serverConfig),
        } as any);

        const { fetchRuntimeConfig, getBootstrapConfig } = await import(
          './bootstrapAdapter'
        );
        await fetchRuntimeConfig();

        const cached = getBootstrapConfig();
        expect(cached).toEqual(serverConfig);
      });
    });

    describe('Tier 2: VITE env vars fallback', () => {
      it('falls back to env vars when fetch rejects', async () => {
        fetchSpy.mockRejectedValue(new Error('network error'));
        vi.stubEnv('VITE_API_BASE_URL', 'http://env:9090');
        vi.stubEnv('VITE_WS_URL', 'ws://env:9090/ws');
        vi.stubEnv('VITE_AUTH_MODE', 'bearer');
        vi.stubEnv('VITE_APP_MODE', 'cloud');
        vi.stubEnv('VITE_BUILD_VERSION', '2.0.0');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://env:9090');
        expect(config.wsURL).toBe('ws://env:9090/ws');
        expect(config.authMode).toBe('bearer');
        expect(config.appMode).toBe('cloud');
        expect(config.buildVersion).toBe('2.0.0');
        expect(consoleWarnSpy).toHaveBeenCalledWith(
          expect.stringContaining('using VITE env vars'),
          expect.anything()
        );
      });

      it('falls back to env vars when server returns 500', async () => {
        fetchSpy.mockResolvedValue({
          ok: false,
          status: 500,
          json: () => Promise.reject(new Error('Internal Server Error')),
        } as any);
        vi.stubEnv('VITE_API_BASE_URL', 'http://env:9090');
        vi.stubEnv('VITE_WS_URL', 'ws://env:9090/ws');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://env:9090');
        expect(config.wsURL).toBe('ws://env:9090/ws');
        expect(consoleWarnSpy).toHaveBeenCalledWith(
          expect.stringContaining('using VITE env vars'),
          expect.anything()
        );
      });

      it('uses partial env vars with defaults for missing fields', async () => {
        fetchSpy.mockRejectedValue(new Error('no network'));
        vi.stubEnv('VITE_API_BASE_URL', 'http://partial:7777');
        vi.stubEnv('VITE_AUTH_MODE', 'bearer');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://partial:7777');
        expect(config.wsURL).toBe('ws://localhost:56000/ws'); // falls back to default
        expect(config.authMode).toBe('bearer');
        expect(config.appMode).toBe('local'); // falls back to default
        expect(config.buildVersion).toBe('dev'); // falls back to default
      });
    });

    describe('Tier 3: localhost defaults', () => {
      it('falls back to localhost defaults when fetch fails and no env vars', async () => {
        fetchSpy.mockRejectedValue(new Error('network error'));
        vi.stubEnv('VITE_API_BASE_URL', undefined);
        vi.stubEnv('VITE_WS_URL', undefined);
        vi.stubEnv('VITE_AUTH_MODE', undefined);
        vi.stubEnv('VITE_APP_MODE', undefined);
        vi.stubEnv('VITE_BUILD_VERSION', undefined);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://localhost:56000');
        expect(config.wsURL).toBe('ws://localhost:56000/ws');
        expect(config.authMode).toBe('none');
        expect(config.appMode).toBe('local');
        expect(config.buildVersion).toBe('dev');
        expect(consoleLogSpy).toHaveBeenCalledWith(
          'bootstrap: using localhost defaults'
        );
      });

      it('uses localhost defaults when server returns non-JSON and no env vars', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.reject(new Error('Unexpected token')),
        } as any);
        vi.stubEnv('VITE_API_BASE_URL', undefined);
        vi.stubEnv('VITE_WS_URL', undefined);
        vi.stubEnv('VITE_AUTH_MODE', undefined);
        vi.stubEnv('VITE_APP_MODE', undefined);
        vi.stubEnv('VITE_BUILD_VERSION', undefined);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://localhost:56000');
        expect(config.wsURL).toBe('ws://localhost:56000/ws');
        expect(consoleLogSpy).toHaveBeenCalledWith(
          'bootstrap: using localhost defaults'
        );
      });
    });

    describe('edge cases', () => {
      it('falls back to env vars when server returns non-JSON', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.reject(new Error('invalid json')),
        } as any);
        vi.stubEnv('VITE_API_BASE_URL', 'http://fallback:3000');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://fallback:3000');
        expect(consoleWarnSpy).toHaveBeenCalledWith(
          expect.stringContaining('using VITE env vars'),
          expect.anything()
        );
      });

      it('falls back when server returns JSON without apiBaseURL', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve({ foo: 'bar' }),
        } as any);
        vi.stubEnv('VITE_API_BASE_URL', 'http://fallback:3000');

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://fallback:3000');
        // fetchError is null here because JSON parsed OK but validation failed
        expect(consoleWarnSpy).toHaveBeenCalledWith(
          'bootstrap: using VITE env vars (fetch failed: %s)',
          null
        );
      });

      it('uses server config even when partially missing optional fields', async () => {
        fetchSpy.mockResolvedValue({
          json: () => Promise.resolve({ apiBaseURL: 'http://srv:1234' }),
        } as any);

        const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
        const config = await fetchRuntimeConfig();

        expect(config.apiBaseURL).toBe('http://srv:1234');
        expect(config.authMode).toBe('none'); // defaults
        expect(config.appMode).toBe('local'); // defaults
        expect(config.buildVersion).toBe('dev'); // defaults
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
      };
      fetchSpy.mockResolvedValue({
        json: () => Promise.resolve(serverConfig),
      } as any);

      const { fetchRuntimeConfig, getBootstrapConfig } = await import(
        './bootstrapAdapter'
      );
      await fetchRuntimeConfig();

      expect(getBootstrapConfig()).toEqual(serverConfig);
    });

    it('returns localhost defaults before fetchRuntimeConfig resolves', async () => {
      // Mock fetch to hang so the auto-run never completes
      fetchSpy.mockImplementation(
        () => new Promise(() => {})
      );

      const { getBootstrapConfig } = await import('./bootstrapAdapter');

      // The module-level fetchRuntimeConfig() is fire-and-forget and hasn't resolved
      expect(getBootstrapConfig()).toEqual({
        apiBaseURL: 'http://localhost:56000',
        wsURL: 'ws://localhost:56000/ws',
        authMode: 'none',
        appMode: 'local',
        buildVersion: 'dev',
      });
    });
  });
});
