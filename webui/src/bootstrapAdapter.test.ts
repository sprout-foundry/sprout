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
  describe('cloud mode (VITE_SPROUT_MODE=cloud)', () => {
    beforeEach(() => {
      vi.resetModules();
      // Set cloud mode before importing bootstrapAdapter
      process.env.VITE_SPROUT_MODE = 'cloud';
      process.env.VITE_FOUNDRY_API_URL = 'https://foundry.test.sprout.dev/api';
      process.env.VITE_FOUNDRY_WS_URL = 'wss://foundry.test.sprout.dev/ws';
      mockWindowLocation('https://app.test.sprout.dev', 'https:', 'app.test.sprout.dev');
    });

    afterEach(() => {
      delete process.env.VITE_SPROUT_MODE;
      delete process.env.VITE_FOUNDRY_API_URL;
      delete process.env.VITE_FOUNDRY_WS_URL;
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
      delete process.env.VITE_SPROUT_MODE;
      delete process.env.VITE_FOUNDRY_API_URL;
      delete process.env.VITE_FOUNDRY_WS_URL;
      mockWindowLocation('http://localhost:3000', 'http:', 'localhost:3000');
    });

    afterEach(() => {
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
      process.env.VITE_SPROUT_MODE = 'cloud';
      // Do NOT set VITE_FOUNDRY_API_URL or VITE_FOUNDRY_WS_URL
      delete process.env.VITE_FOUNDRY_API_URL;
      delete process.env.VITE_FOUNDRY_WS_URL;
      mockWindowLocation('https://app.test.sprout.dev', 'https:', 'app.test.sprout.dev');
    });

    afterEach(() => {
      delete process.env.VITE_SPROUT_MODE;
      restoreWindowLocation();
      vi.resetModules();
    });

    it('installs CloudAdapter with fallback to window.location.origin', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      expect(adapter).not.toBeNull();
      expect(adapter!.name).toBe('foundry-cloud');
    });

    it('uses window.location-based WebSocket URL as fallback', async () => {
      await import('./bootstrapAdapter');

      const { getAdapter } = await import('./services/apiAdapter');
      const adapter = getAdapter();
      // bootstrapAdapter derives wsUrl from window.location when VITE_FOUNDRY_WS_URL is not set
      expect(adapter!.getWebSocketURL()).toBe('wss://app.test.sprout.dev/ws');
    });
  });
});
