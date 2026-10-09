/**
 * host.3 capability-resolution tests.
 *
 * Proves that the config/mode `supports*` bindings are driven by the active
 * host's capabilities when a host is set (via the entry point's
 * setActiveHost), and that they fall back to the adapter/mode defaults when
 * no host is active — the path `mode.test.ts` exercises. This is the
 * "capabilities replace the module-level flags" behavior with no behavior
 * change in either build.
 *
 * Each scenario resets the module registry ONCE, then dynamically imports
 * everything (the accessor, the adapter, config/mode) from that single reset,
 * so the accessor singleton they all share is the one the test configures.
 */
import type { SproutHost } from './types';

const ORIGINAL_ENV = process.env.VITE_SPROUT_MODE;

afterAll(() => {
  if (ORIGINAL_ENV === undefined) {
    delete process.env.VITE_SPROUT_MODE;
  } else {
    process.env.VITE_SPROUT_MODE = ORIGINAL_ENV;
  }
  vi.resetModules();
});

/** A host whose capabilities are each a distinct boolean, so any flag that
 * drifts away from the host's value is caught. */
function testHost(overrides: Record<string, boolean> = {}): SproutHost {
  return {
    transport: { apiBaseURL: '', wsURL: '', authMode: 'none' },
    navigation: { open() {} },
    notifications: { post() {} },
    capabilities: {
      ssh: true,
      git: false,
      chat: true,
      workspaceSwitching: true,
      folderPicker: true,
      export: false,
      instances: true,
      localTerminal: false,
      settings: true,
      automations: true,
      agentChanges: true,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: false,
      chatSessions: true,
      ...overrides,
    },
  };
}

describe('config/mode capability bindings read the active host', () => {
  it('resolves every supports* binding from host.capabilities when a host is set', async () => {
    vi.resetModules();
    const { setActiveHost } = await import('./accessor');
    setActiveHost(testHost());
    const mode = await import('../config/mode');

    expect(mode.supportsSSH).toBe(true);
    expect(mode.supportsGit).toBe(false);
    expect(mode.supportsChat).toBe(true);
    expect(mode.supportsWorkspaceSwitching).toBe(true);
    expect(mode.supportsFolderPicker).toBe(true);
    expect(mode.supportsExport).toBe(false);
    expect(mode.supportsInstances).toBe(true);
    expect(mode.supportsLocalTerminal).toBe(false);
    expect(mode.supportsSettings).toBe(true);
    expect(mode.supportsAutomations).toBe(true);
    expect(mode.supportsAgentChanges).toBe(true);
    expect(mode.supportsChatSessions).toBe(true);
  });

  it('a distinct host re-derives the bindings (host is the source of truth)', async () => {
    vi.resetModules();
    const { setActiveHost } = await import('./accessor');

    setActiveHost(testHost({ git: true, ssh: false, instances: false }));
    const first = await import('../config/mode');
    expect(first.supportsGit).toBe(true);
    expect(first.supportsSSH).toBe(false);
    expect(first.supportsInstances).toBe(false);

    // A second, different host produces a different set of binding values.
    vi.resetModules();
    const { setActiveHost: set2 } = await import('./accessor');
    set2(testHost({ git: false, ssh: true, instances: true }));
    const second = await import('../config/mode');
    expect(second.supportsGit).toBe(false);
    expect(second.supportsSSH).toBe(true);
    expect(second.supportsInstances).toBe(true);
  });

  it('host wins over an installed adapter (adapter refresh is a no-op with a host)', async () => {
    vi.resetModules();
    const { setActiveHost, getActiveHost } = await import('./accessor');
    const { installAdapter } = await import('../services/apiAdapter');
    // An adapter that disagrees with the host on several capabilities.
    installAdapter({
      name: 'conflicting-adapter',
      fetch: async () => new Response(),
      getWebSocketURL: () => null,
      requiresBackendHealthCheck: false,
      fileOpsViaAPI: true,
      showOnboarding: true,
      supportsSSH: true,
      supportsGit: true,
      supportsChat: true,
      supportsWorkspaceSwitching: true,
      supportsExport: true,
      supportsInstances: true,
      supportsLocalTerminal: true,
      supportsSettings: true,
    });
    const host = testHost({ settings: false });
    setActiveHost(host);
    const mode = await import('../config/mode');
    // The host's values hold; the adapter's conflicting values are ignored.
    expect(mode.supportsSettings).toBe(false);
    expect(mode.supportsGit).toBe(false);
    expect(mode.supportsSSH).toBe(true);
    expect(getActiveHost()?.capabilities.settings).toBe(false);
  });
});

describe('config/mode capability bindings fall back with no active host', () => {
  it('uses local-mode defaults when no host is set and no adapter is installed', async () => {
    delete process.env.VITE_SPROUT_MODE;
    vi.resetModules();
    const { getActiveHost } = await import('./accessor');
    expect(getActiveHost()).toBeNull();

    const mode = await import('../config/mode');
    expect(mode.supportsSSH).toBe(true);
    expect(mode.supportsGit).toBe(true);
    expect(mode.supportsChat).toBe(true);
    expect(mode.supportsWorkspaceSwitching).toBe(true);
    expect(mode.supportsFolderPicker).toBe(false);
    expect(mode.supportsExport).toBe(true);
    expect(mode.supportsInstances).toBe(false);
    expect(mode.supportsLocalTerminal).toBe(true);
    expect(mode.supportsSettings).toBe(true);
    expect(mode.supportsAutomations).toBe(true);
    expect(mode.supportsAgentChanges).toBe(true);
    expect(mode.supportsChatSessions).toBe(false);
  });

  it('uses local defaults in a cloud build with no host (no build-flag seeding)', async () => {
    process.env.VITE_SPROUT_MODE = 'cloud';
    vi.resetModules();
    const mode = await import('../config/mode');
    // host.8 removed the build-mode read: with no active host and no adapter
    // the bindings use the local defaults regardless of build. The hosted
    // build's cloud values come from cloudHost (pinned by hosts.test.ts).
    expect(mode.supportsSSH).toBe(true);
    expect(mode.supportsInstances).toBe(false);
    expect(mode.supportsWorkspaceSwitching).toBe(true);
    expect(mode.supportsLocalTerminal).toBe(true);
    expect(mode.supportsAutomations).toBe(true);
    expect(mode.supportsAgentChanges).toBe(true);
    delete process.env.VITE_SPROUT_MODE;
  });
});
