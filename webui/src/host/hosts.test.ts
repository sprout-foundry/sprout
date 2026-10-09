/**
 * Tests for the two built-in hosts' capability sets.
 *
 * The item under test: both hosts expose the capability set that matches
 * today's values. localHost mirrors the LOCAL defaults reported by
 * config/mode.ts (plus the spec-only flags, all on); cloudHost mirrors the
 * hosted capability set exposed by CloudAdapter (not mode.ts's cloud defaults).
 *
 * The host constants are stable (pre-built value objects) and are imported
 * statically. The mode/adapter modules are re-imported with the right
 * VITE_SPROUT_MODE, following config/mode.test.ts's pattern (reset the module
 * registry, then import with the env var set).
 */

import type { CloudAdapter } from '../services/cloudAdapter';
import { cloudHost } from './cloudHost';
import { localHost } from './localHost';

describe('localHost', () => {
  let modeModule;
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeAll(async () => {
    delete process.env.VITE_SPROUT_MODE;
    vi.resetModules();
    modeModule = await import('../config/mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('has no account and no entitlements', () => {
    expect(localHost.user).toBeNull();
    expect(localHost.entitlements).toBeUndefined();
  });

  it('uses a same-origin transport with no auth', () => {
    // '' is the same-origin sentinel (derived at runtime by the local
    // transport); the dev port is never hardcoded. The local host has no
    // outward platform surface, so its transport carries no platform URL.
    expect(localHost.transport).toEqual({
      apiBaseURL: '',
      wsURL: '',
      authMode: 'none',
      agent: { kind: 'daemon', apiBaseURL: '', wsURL: '' },
    });
    expect(localHost.transport.platformURL).toBeUndefined();
  });

  it('selects the local daemon as its agent backend', () => {
    // The local build's agent is the local daemon, reached at the transport's
    // own same-origin URLs. Stating the backend explicitly lets a host that
    // embeds the workspace read the choice off the contract.
    expect(localHost.transport.agent).toEqual({ kind: 'daemon', apiBaseURL: '', wsURL: '' });
  });

  it('opens the public repository issue for a reportBug intent (local host)', () => {
    // The local host has no support flow, so it opens the prefilled public
    // issue. The URL comes from the host-tree builder; the environment is
    // passed on the intent (collected outside the host tree).
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
    localHost.navigation.open({
      type: 'reportBug',
      environment: { version: 'v1.2.3', mode: 'local daemon', os: 'macOS', browser: 'TestBrowser' },
    });
    expect(openSpy).toHaveBeenCalledTimes(1);
    const [url, target, features] = openSpy.mock.calls[0];
    expect(String(url)).toContain('https://github.com/sprout-foundry/sprout/issues/new');
    expect(String(url)).toContain('labels=bug');
    expect(String(url)).toContain('v1.2.3');
    expect(target).toBe('_blank');
    expect(features).toBe('noopener,noreferrer');
    openSpy.mockRestore();
  });

  it('exposes the local-mode capability set (mode.ts local values)', () => {
    // Compare against mode.ts's LOCAL values read with VITE_SPROUT_MODE unset,
    // so a change to the local defaults is caught rather than frozen as a
    // literal. The spec-only flags (not yet in mode.ts) are all on for a full
    // local desktop build.
    expect(localHost.capabilities).toEqual({
      ssh: modeModule.supportsSSH,
      git: modeModule.supportsGit,
      chat: modeModule.supportsChat,
      workspaceSwitching: modeModule.supportsWorkspaceSwitching,
      folderPicker: modeModule.supportsFolderPicker,
      export: modeModule.supportsExport,
      instances: modeModule.supportsInstances,
      localTerminal: modeModule.supportsLocalTerminal,
      settings: modeModule.supportsSettings,
      automations: modeModule.supportsAutomations,
      agentChanges: modeModule.supportsAgentChanges,
      mcp: true,
      localModels: true,
      verification: true,
      serverGit: true,
      chatSessions: false,
    });
  });
});

describe('cloudHost', () => {
  let cloud: CloudAdapter;
  const originalEnv = process.env.VITE_SPROUT_MODE;

  beforeAll(async () => {
    delete process.env.VITE_SPROUT_MODE;
    vi.resetModules();
    const { CloudAdapter } = await import('../services/cloudAdapter');
    cloud = new CloudAdapter({
      apiBase: 'https://api.test.sprout.dev',
      wsUrl: 'wss://api.test.sprout.dev/ws',
    });
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.VITE_SPROUT_MODE;
    } else {
      process.env.VITE_SPROUT_MODE = originalEnv;
    }
    vi.resetModules();
  });

  it('records the cloud transport policy (bearer auth, same-origin-or-Foundry)', () => {
    // The concrete URL is resolved at startup by the bootstrap adapter and
    // recorded on this transport (platformURL); the constant declares the
    // policy, so it uses the same-origin sentinel with bearer auth and does not
    // hardcode a platform URL. The platform base is absent here because the
    // constant itself performs no bootstrap (importing the host fetches
    // nothing).
    expect(cloudHost.transport).toEqual({
      apiBaseURL: '',
      wsURL: '',
      authMode: 'bearer',
      agent: { kind: 'wasm', modelEndpoint: '' },
    });
    expect(cloudHost.transport.authMode).toBe('bearer');
    expect(cloudHost.transport.platformURL).toBeUndefined();
  });

  it('selects the in-browser agent as its backend (model endpoint resolved at runtime)', () => {
    // The hosted build runs the agent in the browser; the model endpoint is the
    // platform's managed model, resolved at runtime from the transport's
    // `modelEndpoint` (the constant declares the shape, so the endpoint is
    // empty here).
    expect(cloudHost.transport.agent).toEqual({ kind: 'wasm', modelEndpoint: '' });
  });

  it('exposes the CloudAdapter capability set (hosted source of truth)', () => {
    // Compare the eight CloudAdapter capability constants against a real
    // CloudAdapter so a change to the adapter is caught. The local-mode-only
    // and pre-configured spec-only flags are off for a hosted build.
    expect(cloudHost.capabilities).toEqual({
      ssh: cloud.supportsSSH,
      git: cloud.supportsGit,
      chat: cloud.supportsChat,
      workspaceSwitching: cloud.supportsWorkspaceSwitching,
      export: cloud.supportsExport,
      instances: cloud.supportsInstances,
      localTerminal: cloud.supportsLocalTerminal,
      settings: cloud.supportsSettings,
      folderPicker: false,
      automations: false,
      agentChanges: false,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: false,
      chatSessions: false,
    });
  });

  it('supplies the platform navigation surface (items + intent resolution)', () => {
    // host.6: the cloud host names the platform's pages and resolves intents to
    // them; Sprout components render the items and dispatch through open.
    expect(cloudHost.navigation.workItems?.map((i) => i.label)).toEqual(['Dashboard', 'Tasks', 'Workspaces']);
    expect(cloudHost.navigation.accountItems?.map((i) => i.label)).toEqual([
      'Dashboard',
      'Tasks',
      'Usage & billing',
      'Team',
      'Runners',
      'Settings',
    ]);
    expect(cloudHost.navigation.intentPath?.({ type: 'usage' })).toBe('/?from=editor#/account/billing');
  });

  it('resolves a task deep link by nav intent detail (the host owns the path)', () => {
    // The escalation toast asks for the task page by (generic) nav intent +
    // detail; the host resolves it, so no component hardcodes '#/tasks/'.
    expect(cloudHost.navigation.intentPath?.({ type: 'nav', id: 'tasks', detail: '42' })).toBe(
      '/?from=editor#/tasks/42',
    );
    expect(cloudHost.navigation.intentPath?.({ type: 'nav', id: 'repos', detail: 'acme/widgets' })).toBe(
      '/?from=editor#/repos/acme/widgets',
    );
  });

  it('resolves a reportBug intent to the platform support flow, not the public issue', () => {
    // The platform keeps its own support tickets: a reportBug intent resolves
    // to the platform's support page, never the public GitHub issue.
    const path = cloudHost.navigation.intentPath?.({ type: 'reportBug' });
    expect(path).toBe('/?from=editor#/support');
    expect(path).not.toContain('github.com');
  });

  it('supplies the platform GitHub surface as host data (account card + repo list)', () => {
    // GitHub is account-managed: the host supplies the account card (chrome) and
    // the connection/repo data, so the picker holds no platform call.
    expect(cloudHost.chrome?.githubAccount).toBeTruthy();
    expect(typeof cloudHost.github?.isConnected).toBe('function');
    expect(typeof cloudHost.github?.listRepos).toBe('function');
    expect(typeof cloudHost.github?.createRepo).toBe('function');
    // The embed decoration lives on the host side (PlatformHome holds no literal).
    expect(cloudHost.navigation.embedPagePath?.('/tasks')).toContain('/?embed=1');
    // The outward page URL carries the platform base but no embed decoration.
    expect(cloudHost.navigation.platformPagePath?.('/?from=editor#/admin')).toBe('/?from=editor#/admin');
  });

  it('leaves platform-provided identity/theme absent but supplies live entitlements', () => {
    // Identity and theme are host-provided at runtime by a later item; the
    // constant declares the shape, so these are absent. Entitlements are live:
    // the summary is resolved from the platform's billing status, so the object
    // exists with a resolver and (initially, in tests) no summary.
    expect(cloudHost.user).toBeUndefined();
    expect(cloudHost.theme).toBeUndefined();
    expect(typeof cloudHost.entitlements?.resolve).toBe('function');
    expect(cloudHost.entitlements?.usageSummary).toBeUndefined();
  });

  describe('the signOut intent', () => {
    const originalLocation = window.location;

    beforeEach(() => {
      // jsdom's location.href assignment is a no-op; stub it so the redirect is
      // observable and a throwing assignment can be simulated.
      Object.defineProperty(window, 'location', {
        configurable: true,
        writable: true,
        value: { ...originalLocation, href: '' },
      });
    });

    afterEach(() => {
      Object.defineProperty(window, 'location', { configurable: true, writable: true, value: originalLocation });
      vi.unstubAllGlobals();
    });

    it('posts the platform logout and lands on the login page', async () => {
      const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', fetchMock);

      await cloudHost.navigation.open({ type: 'signOut' });

      expect(fetchMock).toHaveBeenCalledWith('/webui/auth/logout', { method: 'POST', credentials: 'include' });
      expect(window.location.href).toBe('/login');
    });

    it('rejects when the logout POST fails, leaving the redirect untouched', async () => {
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')));

      await expect(cloudHost.navigation.open({ type: 'signOut' })).rejects.toThrow('network down');
      expect(window.location.href).toBe('');
    });

    it('does not treat a navigation failure as a sign-out failure', async () => {
      // The logout POST succeeds (the cookie is already cleared server-side);
      // a redirect the browser throttles must NOT surface as a failed sign-out
      // that would reopen the menu and claim the user is still signed in.
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 200 })));
      Object.defineProperty(window, 'location', {
        configurable: true,
        writable: true,
        value: {
          ...originalLocation,
          get href() {
            return '';
          },
          set href(_value: string) {
            throw new Error('navigation throttled');
          },
        },
      });

      await expect(cloudHost.navigation.open({ type: 'signOut' })).resolves.toBeUndefined();
    });
  });
});
