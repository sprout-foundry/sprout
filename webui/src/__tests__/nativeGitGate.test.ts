// @vitest-environment node
/**
 * R-4: pure gate-decision coverage for the Track R native-git deferral leaf
 * module (webui/src/services/nativeGit/index.ts). Mirrors
 * nativeChatGate.test.ts.
 *
 * Coverage:
 *   - resolveNativeGitGate (pure): every reason path, both flag states,
 *     malformed capabilities (non-object / null / undefined / missing fields).
 *   - structural detector: hasSproutStudioGitBridge +
 *     detectSproutStudioGit (no window / no SproutStudio / missing
 *     getCapabilities / good bridge).
 *   - cached async resolver nativeGitGate(): getCapabilities called
 *     exactly once across two calls; the getCapabilities-rejected path;
 *     and __resetNativeGitGateForTests clearing the cache.
 *
 * The resolver short-circuits to `native-git-disabled` when the
 * module-level compile-time flag is false. To exercise the bridge paths we
 * flip the flag with `vi.stubEnv('VITE_SPROUT_NATIVE_GIT','1')` +
 * `vi.resetModules()` + a fresh dynamic import of the leaf module, and
 * install a fake `window.SproutStudio` (in the node environment `window` is
 * undefined, so we define it on globalThis). Each resolver case resets the
 * gate cache in beforeEach/afterEach so it never leaks.
 */

import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import {
  resolveNativeGitGate,
  hasSproutStudioGitBridge,
  detectSproutStudioGit,
  nativeGitGate,
  __resetNativeGitGateForTests,
} from '../services/nativeGit';
import type { SproutStudioCapabilities } from '../services/nativeGit';

/** A minimal usable git bridge: a getCapabilities spy only. */
function makeBridge(
  capabilities: Record<string, boolean>,
  excluded: Array<Record<string, unknown>> = [],
): { getCapabilities: ReturnType<typeof vi.fn> } {
  return {
    getCapabilities: vi.fn(
      async (): Promise<SproutStudioCapabilities> => ({
        schemaVersion: 1,
        capabilities,
        excluded: excluded as unknown as SproutStudioCapabilities['excluded'],
        manifestPresent: excluded.length > 0,
        servable: true,
      }),
    ),
  };
}

const RATIFIED_GIT = [{ portion: 'git', status: 'ratified' }];
const SEAM_ONLY_GIT = [{ portion: 'git', status: 'seam-only' }];

// ── resolveNativeGitGate (pure) ─────────────────────────────────────

describe('resolveNativeGitGate (pure)', () => {
  it('inactive when the compile-time flag is off (even with a good bridge + caps)', () => {
    const d = resolveNativeGitGate(false, makeBridge({ git: true }, RATIFIED_GIT), {
      capabilities: { git: true },
      excluded: RATIFIED_GIT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('native-git-disabled');
  });

  it('inactive when there is no usable bridge', () => {
    const d = resolveNativeGitGate(true, null, {
      capabilities: { git: true },
      excluded: RATIFIED_GIT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('no-bridge');
  });

  it('inactive on malformed capabilities (null / undefined / a non-object)', () => {
    const bridge = makeBridge({ git: true }, RATIFIED_GIT);
    // A truly non-object response (or null / undefined) is a malformed
    // capabilities response — a gate-fail, never a throw.
    expect(resolveNativeGitGate(true, bridge, null)).toEqual({ active: false, reason: 'malformed-capabilities' });
    expect(resolveNativeGitGate(true, bridge, undefined)).toEqual({
      active: false,
      reason: 'malformed-capabilities',
    });
    // A non-object (a string) is also malformed.
    expect(resolveNativeGitGate(true, bridge, 'x' as unknown)).toEqual({
      active: false,
      reason: 'malformed-capabilities',
    });
  });

  it('inactive when capabilities is an object but the capabilities key is missing ({}) → git-not-declared', () => {
    // Mirrors the chat leaf: `{}` IS an object (so not "malformed"), but
    // it has no `capabilities` key, so it falls through to the not-declared
    // step. (nativeChatGate documents the identical chat behavior.)
    const bridge = makeBridge({ git: true }, RATIFIED_GIT);
    const d = resolveNativeGitGate(true, bridge, {});
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-declared');
  });

  it('inactive when the shell does not declare git', () => {
    const bridge = makeBridge({ git: false }, RATIFIED_GIT);
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: false },
      excluded: RATIFIED_GIT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-declared');
  });

  it('inactive when the capabilities.git key is missing entirely', () => {
    const bridge = makeBridge({}, RATIFIED_GIT);
    const d = resolveNativeGitGate(true, bridge, { capabilities: {}, excluded: RATIFIED_GIT });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-declared');
  });

  it('git truthy-but-not-boolean (e.g. "yes") FAILS the gate (strict-typing)', () => {
    const bridge = makeBridge({ git: true }, RATIFIED_GIT);
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: 'yes' as unknown as boolean },
      excluded: RATIFIED_GIT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-declared');
  });

  it('inactive when the git entry is seam-only (unratified)', () => {
    const d = resolveNativeGitGate(true, makeBridge({ git: true }, SEAM_ONLY_GIT), {
      capabilities: { git: true },
      excluded: SEAM_ONLY_GIT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('inactive when capabilities.git===true but excluded is empty', () => {
    const d = resolveNativeGitGate(true, makeBridge({ git: true }, []), {
      capabilities: { git: true },
      excluded: [],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('inactive when a non-git portion is ratified (e.g. fs only)', () => {
    const d = resolveNativeGitGate(true, makeBridge({ git: true }, [{ portion: 'fs', status: 'ratified' }]), {
      capabilities: { git: true },
      excluded: [{ portion: 'fs', status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('active when git is declared and ratified', () => {
    const d = resolveNativeGitGate(true, makeBridge({ git: true }, RATIFIED_GIT), {
      capabilities: { git: true },
      excluded: RATIFIED_GIT,
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

// ── resolveNativeGitGate — malformed / edge excluded[] shapes ───────

describe('resolveNativeGitGate — malformed / edge excluded[] shapes', () => {
  const bridge = makeBridge({ git: true }, RATIFIED_GIT);

  it('excluded is a string (not an array) → inactive (git-not-ratified)', () => {
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: true },
      excluded: 'git' as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('excluded is null → inactive (git-not-ratified)', () => {
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: true },
      excluded: null as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('excluded entry missing `portion` → inactive (git-not-ratified)', () => {
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: true },
      excluded: [{ status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('excluded entry missing `status` → inactive (git-not-ratified)', () => {
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: true },
      excluded: [{ portion: 'git' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('git-not-ratified');
  });

  it('duplicate git entries (one seam-only + one ratified) → ACTIVE (any ratified wins)', () => {
    // NOTE ON ACTUAL BEHAVIOR: the implementation uses Array.prototype.some,
    // so ANY ratified git entry activates the gate even if a sibling is
    // seam-only. This asserts the first-match-wins consequence.
    const d = resolveNativeGitGate(true, bridge, {
      capabilities: { git: true },
      excluded: [
        { portion: 'git', status: 'seam-only' },
        { portion: 'git', status: 'ratified' },
      ],
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

// ── bridge structural detection ──────────────────────────────────────────────

describe('bridge structural detection', () => {
  it('detects a getCapabilities-only bridge and rejects partial / null / non-object', () => {
    expect(hasSproutStudioGitBridge({ getCapabilities: async () => ({}) })).toBe(true);
    expect(hasSproutStudioGitBridge({})).toBe(false);
    expect(hasSproutStudioGitBridge(null)).toBe(false);
    expect(hasSproutStudioGitBridge(undefined)).toBe(false);
    expect(hasSproutStudioGitBridge('x')).toBe(false);
  });

  it('detectSproutStudioGit returns null without window (node env)', () => {
    // In this file's node environment window is undefined unless a resolver
    // case installs it; restore the clean state before asserting.
    delete (globalThis as unknown as { window?: unknown }).window;
    expect(detectSproutStudioGit()).toBeNull();
  });

  it('detectSproutStudioGit returns the bridge when window.SproutStudio has getCapabilities', () => {
    const bridge = { getCapabilities: async () => ({}) };
    (globalThis as unknown as { window?: unknown }).window = { SproutStudio: bridge };
    try {
      expect(detectSproutStudioGit()).toBe(bridge);
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it('detectSproutStudioGit returns null when window.SproutStudio lacks getCapabilities', () => {
    (globalThis as unknown as { window?: unknown }).window = { SproutStudio: { readWorkspaceFile: async () => ({}) } };
    try {
      expect(detectSproutStudioGit()).toBeNull();
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it('detectSproutStudioGit returns null when window.SproutStudio is absent', () => {
    (globalThis as unknown as { window?: unknown }).window = {};
    try {
      expect(detectSproutStudioGit()).toBeNull();
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });
});

// ── cached async resolver (flag ON — fresh dynamic import) ─────────────────
//
// These exercise the bridge paths of nativeGitGate() by flipping the
// compile-time flag per case and installing a fake window.SproutStudio. The
// gate cache is reset in beforeEach/afterEach so the shared module-level
// `gatePromise` never leaks between cases.

type GateModule = typeof import('../services/nativeGit');

let gate: GateModule;

async function loadGateModule(enabled: boolean): Promise<GateModule> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_GIT', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_GIT', '');
  vi.resetModules();
  return import('../services/nativeGit');
}

function installBridge(bridge: unknown): void {
  (globalThis as unknown as { window?: unknown }).window = { SproutStudio: bridge };
}

describe('nativeGitGate (cached resolver, flag ON)', () => {
  beforeEach(async () => {
    gate = await loadGateModule(true);
    gate.__resetNativeGitGateForTests();
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('getCapabilities is called exactly once across two nativeGitGate() calls', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { git: true },
      excluded: RATIFIED_GIT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    const d1 = await gate.nativeGitGate();
    const d2 = await gate.nativeGitGate();

    expect(d1).toEqual({ active: true, reason: 'active' });
    expect(d2).toEqual({ active: true, reason: 'active' });
    // The cached promise means getCapabilities runs ONCE for the lifetime.
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);
  });

  it('no bridge installed → { active: false, reason: "no-bridge" } (never throws)', async () => {
    installBridge(undefined); // window.SproutStudio absent
    const d = await gate.nativeGitGate();
    expect(d).toEqual({ active: false, reason: 'no-bridge' });
  });

  it('getCapabilities REJECTS → { active: false, reason: "getCapabilities-rejected" } (never throws)', async () => {
    const bridge = {
      getCapabilities: vi.fn(async () => {
        throw new Error('transport down');
      }),
    };
    installBridge(bridge);
    const d = await gate.nativeGitGate();
    expect(d).toEqual({ active: false, reason: 'getCapabilities-rejected' });
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);
  });

  it('seam-only git entry → { active: false, reason: "git-not-ratified" }', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { git: true },
      excluded: SEAM_ONLY_GIT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: false,
    };
    installBridge({ getCapabilities: async () => caps });
    const d = await gate.nativeGitGate();
    expect(d).toEqual({ active: false, reason: 'git-not-ratified' });
  });
});

describe('__resetNativeGitGateForTests clears the cache', () => {
  beforeEach(async () => {
    gate = await loadGateModule(true);
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('a fresh getCapabilities call fires after a reset (no longer cached)', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { git: true },
      excluded: RATIFIED_GIT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    // Two calls share one cached promise → one getCapabilities.
    await gate.nativeGitGate();
    await gate.nativeGitGate();
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);

    // Reset → the next call re-detects + re-fetches.
    gate.__resetNativeGitGateForTests();
    const d = await gate.nativeGitGate();
    expect(d).toEqual({ active: true, reason: 'active' });
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(2);
  });
});

// ── resolver default-build short-circuit (flag OFF) ───────────────────────

describe('nativeGitGate default-build short-circuit (flag OFF)', () => {
  beforeEach(async () => {
    gate = await loadGateModule(false);
    gate.__resetNativeGitGateForTests();
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('flag OFF short-circuits to native-git-disabled BEFORE touching the bridge', async () => {
    // Even with a full, ratifying bridge installed, the compile-time flag off
    // means the resolver never reaches getCapabilities — a dead branch.
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { git: true },
      excluded: RATIFIED_GIT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    const d = await gate.nativeGitGate();
    expect(d).toEqual({ active: false, reason: 'native-git-disabled' });
    expect(bridge.getCapabilities).not.toHaveBeenCalled();
  });
});

// Housekeeping: reset the shared module-level gate cache so an import-order
// surprise cannot leak across the (separate) boot tests.
beforeAll(() => {
  __resetNativeGitGateForTests();
});
