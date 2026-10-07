/**
 * host.14 Part B — a host shell's declared capabilities are restored through
 * the host (SP-160 §160b).
 *
 * A studio (`--native-fs`) dist ships a `capabilities.json` declaring
 * `supportsFolderPicker: true` / `supportsWorkspaceSwitching: true`, which its
 * shell reports to the webui through the installed adapter. The selected host
 * is the local host, whose own defaults leave `folderPicker` off; the entry
 * overlays the adapter's declared flags onto the host's capabilities so the
 * studio workspace gate (WorkspaceGateModal's studio variant) renders again.
 *
 * These tests exercise the seam the entry uses — `applyHostCapabilities` +
 * `upsertActiveHostCapabilities` — and assert the resulting host capabilities
 * and the `config/mode` bindings the gate reads.
 *
 * `localHost` is a module singleton shared across every import in one module
 * graph; a test that mutates its capabilities must not leak into another. Each
 * it() re-imports the whole chain after `vi.resetModules()`, snapshots the
 * host capabilities first, and restores them in a finally — so no scenario
 * observes another's mutation. (Vitest does not reset the imported module graph
 * between tests on its own, hence the explicit reset + restore.)
 */
import type { APIAdapter } from '@sprout/ui';
import type { SproutHost } from './types';

/** An adapter declaring exactly the given capability keys (others undefined). */
function adapterDeclaring(caps: Partial<APIAdapter>): APIAdapter {
  return {
    name: 'studio-shell',
    fetch: async () => new Response(),
    getWebSocketURL: () => null,
    requiresBackendHealthCheck: false,
    fileOpsViaAPI: false,
    showOnboarding: false,
    supportsSSH: false,
    supportsGit: true,
    supportsChat: true,
    supportsWorkspaceSwitching: false,
    supportsExport: false,
    supportsInstances: false,
    supportsLocalTerminal: false,
    supportsSettings: true,
    ...caps,
  };
}

/** Snapshot a host's capabilities so a mutation can be undone. */
function snapshotCaps(host: SproutHost): SproutHost['capabilities'] {
  return { ...host.capabilities };
}

describe('host shell capabilities are restored through the host', () => {
  it('overlays the adapter-declared flags onto the host capabilities', async () => {
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const before = snapshotCaps(localHost);
    try {
      // The plain local build: folderPicker off, workspaceSwitching on.
      expect(localHost.capabilities.folderPicker).toBe(false);
      expect(localHost.capabilities.workspaceSwitching).toBe(true);

      const adapter = adapterDeclaring({ supportsFolderPicker: true, supportsWorkspaceSwitching: true });
      const result = applyHostCapabilities(localHost, adapter);

      // Same host object, mutated in place (the provider and accessor observe
      // the same instance), with the declared flags applied.
      expect(result).toBe(localHost);
      expect(localHost.capabilities.folderPicker).toBe(true);
      expect(localHost.capabilities.workspaceSwitching).toBe(true);
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('overlays a declared flag over a differing host default (adapter is authoritative)', async () => {
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const before = snapshotCaps(localHost);
    try {
      // A shell adapter declaring localTerminal off must win over localHost's
      // on — the adapter is the authoritative declaration for every key it
      // carries, not only the studio-gate pair.
      expect(localHost.capabilities.localTerminal).toBe(true);
      applyHostCapabilities(localHost, adapterDeclaring({ supportsLocalTerminal: false }));
      expect(localHost.capabilities.localTerminal).toBe(false);
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('a plain local build (no adapter) keeps today’s values', async () => {
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const before = snapshotCaps(localHost);
    try {
      // A plain local build installs no adapter, so the entry's
      // applyHostCapabilities(host, null) is a no-op and the host keeps its
      // own defaults: folderPicker off, workspaceSwitching on.
      applyHostCapabilities(localHost, null);
      expect(localHost.capabilities.folderPicker).toBe(false);
      expect(localHost.capabilities.workspaceSwitching).toBe(true);
      expect(localHost.capabilities).toEqual(before);
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('an explicitly-declared false also applies (the adapter is authoritative)', async () => {
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const before = snapshotCaps(localHost);
    try {
      applyHostCapabilities(
        localHost,
        adapterDeclaring({ supportsFolderPicker: false, supportsWorkspaceSwitching: false }),
      );
      expect(localHost.capabilities.folderPicker).toBe(false);
      expect(localHost.capabilities.workspaceSwitching).toBe(false);
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('does not touch capability fields the adapter has no key for', async () => {
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const before = snapshotCaps(localHost);
    try {
      // The spec-only flags (mcp, localModels, verification, serverGit) have no
      // adapter key, so they stay whatever the host declared.
      applyHostCapabilities(localHost, adapterDeclaring({ supportsFolderPicker: true }));
      expect(localHost.capabilities.mcp).toBe(before.mcp);
      expect(localHost.capabilities.localModels).toBe(before.localModels);
      expect(localHost.capabilities.verification).toBe(before.verification);
      expect(localHost.capabilities.serverGit).toBe(before.serverGit);
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('re-syncs config/mode’s bindings when a declared capability lands on the live host', async () => {
    // The two steps the entry performs after the adapter installs: apply the
    // adapter's declared flags to the active host, then upsert
    // (HOST_UPDATED_EVENT + capability hooks) so config/mode re-derives from
    // host.capabilities — the bindings the gate reads.
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const { setActiveHost, upsertActiveHostCapabilities } = await import('./accessor');
    const before = snapshotCaps(localHost);
    try {
      setActiveHost(localHost);
      // config/mode's module-scope seed already read the host's local defaults.
      const mode = await import('../config/mode');
      expect(mode.supportsFolderPicker).toBe(false);

      applyHostCapabilities(
        localHost,
        adapterDeclaring({ supportsFolderPicker: true, supportsWorkspaceSwitching: true }),
      );
      upsertActiveHostCapabilities();

      // The gate reads these live bindings; the studio variant now renders.
      expect(mode.supportsFolderPicker).toBe(true);
      expect(mode.supportsWorkspaceSwitching).toBe(true);
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('the entry’s adapter-installed path restores the studio flags end to end', async () => {
    // Mirrors index.tsx: the shell installs its adapter (firing
    // ADAPTER_INSTALLED_EVENT), the entry applies the declared flags to the
    // active host and upserts, so config/mode's live bindings flip.
    vi.resetModules();
    const { localHost } = await import('./localHost');
    const { installAdapter, getAdapter, ADAPTER_INSTALLED_EVENT } = await import('../services/apiAdapter');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const { setActiveHost, upsertActiveHostCapabilities } = await import('./accessor');
    const before = snapshotCaps(localHost);
    try {
      setActiveHost(localHost);
      const mode = await import('../config/mode');
      expect(mode.supportsFolderPicker).toBe(false);

      // The entry's listener: on adapter install, merge what it declares.
      const onAdapterInstalled = () => {
        const adapter = getAdapter();
        if (!adapter) return;
        applyHostCapabilities(localHost, adapter);
        upsertActiveHostCapabilities();
      };
      window.addEventListener(ADAPTER_INSTALLED_EVENT, onAdapterInstalled);
      try {
        installAdapter(adapterDeclaring({ supportsFolderPicker: true, supportsWorkspaceSwitching: true }));
        expect(mode.supportsFolderPicker).toBe(true);
        expect(mode.supportsWorkspaceSwitching).toBe(true);
      } finally {
        window.removeEventListener(ADAPTER_INSTALLED_EVENT, onAdapterInstalled);
      }
    } finally {
      Object.assign(localHost.capabilities, before);
    }
  });

  it('leaves the cloud host unaffected', async () => {
    vi.resetModules();
    const { cloudHost } = await import('./cloudHost');
    const { applyHostCapabilities } = await import('./applyHostCapabilities');
    const before = snapshotCaps(cloudHost);
    try {
      // The cloud host's own flags: no workspace switching, no folder picker.
      expect(cloudHost.capabilities.workspaceSwitching).toBe(false);
      expect(cloudHost.capabilities.folderPicker).toBe(false);

      // A cloud adapter declares both off, so applying it is a no-op on the
      // cloud host (it never gains a folder picker / switching).
      applyHostCapabilities(
        cloudHost,
        adapterDeclaring({
          supportsFolderPicker: false,
          supportsWorkspaceSwitching: false,
          supportsInstances: true,
          supportsSSH: false,
        }),
      );
      expect(cloudHost.capabilities.workspaceSwitching).toBe(false);
      expect(cloudHost.capabilities.folderPicker).toBe(false);
      // Untouched fields keep their cloud values.
      expect(cloudHost.capabilities.instances).toBe(before.instances);
      expect(cloudHost.capabilities.automations).toBe(false);
    } finally {
      Object.assign(cloudHost.capabilities, before);
    }
  });
});
