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
    // transport); the dev port is never hardcoded.
    expect(localHost.transport).toEqual({ apiBaseURL: '', wsURL: '', authMode: 'none' });
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
    // The concrete URL is resolved at startup by the bootstrap adapter; the
    // constant records the policy, so it uses the same-origin sentinel with
    // bearer auth and does not hardcode a platform URL.
    expect(cloudHost.transport).toEqual({ apiBaseURL: '', wsURL: '', authMode: 'bearer' });
    expect(cloudHost.transport.authMode).toBe('bearer');
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
    });
  });

  it('leaves platform-provided values absent (shape, not values)', () => {
    // Identity, entitlements, chrome, and theme are host-provided at runtime
    // by a later item; the constant declares the shape, so these are absent.
    expect(cloudHost.user).toBeUndefined();
    expect(cloudHost.entitlements).toBeUndefined();
    expect(cloudHost.chrome).toBeUndefined();
    expect(cloudHost.theme).toBeUndefined();
  });
});
