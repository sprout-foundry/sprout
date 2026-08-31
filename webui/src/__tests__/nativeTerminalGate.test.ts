// @vitest-environment node
/**
 * R-3: pure gate-decision coverage for the Track R native-terminal deferral
 * leaf module (webui/src/services/nativeTerminal/index.ts). Mirrors
 * nativeFsDeferral.test.ts.
 *
 * Coverage:
 *   - resolveNativeTerminalGate (pure): every reason path, both flag states,
 *     malformed capabilities (non-object / null / undefined / missing fields).
 *   - structural detector: hasSproutStudioTerminalBridge +
 *     detectSproutStudioTerminal (no window / no SproutStudio / missing
 *     getCapabilities / good bridge).
 *   - cached async resolver nativeTerminalGate(): getCapabilities called
 *     exactly once across two calls; the getCapabilities-rejected path;
 *     unexpected-error never throws; and __resetNativeTerminalGateForTests
 *     clearing the cache.
 *
 * The resolver short-circuits to `native-terminal-disabled` when the
 * module-level compile-time flag is false. To exercise the bridge paths we
 * flip the flag with `vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL','1')` +
 * `vi.resetModules()` + a fresh dynamic import of the leaf module, and install
 * a fake `window.SproutStudio` (in the node environment `window` is undefined,
 * so we define it on globalThis). Each resolver case resets the gate cache in
 * beforeEach/afterEach so it never leaks.
 */

import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import {
  resolveNativeTerminalGate,
  hasSproutStudioTerminalBridge,
  detectSproutStudioTerminal,
  nativeTerminalGate,
  __resetNativeTerminalGateForTests,
} from '../services/nativeTerminal';
import type { SproutStudioCapabilities } from '../services/nativeTerminal';

/** A minimal usable terminal bridge: a getCapabilities spy only. */
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

const RATIFIED_TERMINAL = [{ portion: 'terminal', status: 'ratified' }];
const SEAM_ONLY_TERMINAL = [{ portion: 'terminal', status: 'seam-only' }];

// ── resolveNativeTerminalGate (pure) ─────────────────────────────────────

describe('resolveNativeTerminalGate (pure)', () => {
  it('inactive when the compile-time flag is off (even with a good bridge + caps)', () => {
    const d = resolveNativeTerminalGate(false, makeBridge({ terminal: true }, RATIFIED_TERMINAL), {
      capabilities: { terminal: true },
      excluded: RATIFIED_TERMINAL,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('native-terminal-disabled');
  });

  it('inactive when there is no usable bridge', () => {
    const d = resolveNativeTerminalGate(true, null, {
      capabilities: { terminal: true },
      excluded: RATIFIED_TERMINAL,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('no-bridge');
  });

  it('inactive on malformed capabilities (null / undefined / a non-object)', () => {
    const bridge = makeBridge({ terminal: true }, RATIFIED_TERMINAL);
    // A truly non-object response (or null / undefined) is a malformed
    // capabilities response — a gate-fail, never a throw.
    expect(resolveNativeTerminalGate(true, bridge, null)).toEqual({ active: false, reason: 'malformed-capabilities' });
    expect(resolveNativeTerminalGate(true, bridge, undefined)).toEqual({
      active: false,
      reason: 'malformed-capabilities',
    });
    // A non-object (a string) is also malformed.
    expect(resolveNativeTerminalGate(true, bridge, 'x' as unknown)).toEqual({
      active: false,
      reason: 'malformed-capabilities',
    });
  });

  it('inactive when capabilities is an object but the capabilities key is missing ({}) → terminal-not-declared', () => {
    // Mirrors the FS leaf: `{}` IS an object (so not "malformed"), but it has
    // no `capabilities` key, so it falls through to the not-declared step.
    // (nativeFsDeferral documents the identical fs behavior.)
    const bridge = makeBridge({ terminal: true }, RATIFIED_TERMINAL);
    const d = resolveNativeTerminalGate(true, bridge, {});
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-declared');
  });

  it('inactive when the shell does not declare terminal', () => {
    const bridge = makeBridge({ terminal: false }, RATIFIED_TERMINAL);
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: false },
      excluded: RATIFIED_TERMINAL,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-declared');
  });

  it('inactive when the capabilities.terminal key is missing entirely', () => {
    const bridge = makeBridge({}, RATIFIED_TERMINAL);
    const d = resolveNativeTerminalGate(true, bridge, { capabilities: {}, excluded: RATIFIED_TERMINAL });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-declared');
  });

  it('terminal truthy-but-not-boolean (e.g. "yes") FAILS the gate (strict-typing)', () => {
    const bridge = makeBridge({ terminal: true }, RATIFIED_TERMINAL);
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: 'yes' as unknown as boolean },
      excluded: RATIFIED_TERMINAL,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-declared');
  });

  it('inactive when the terminal entry is seam-only (unratified)', () => {
    const d = resolveNativeTerminalGate(true, makeBridge({ terminal: true }, SEAM_ONLY_TERMINAL), {
      capabilities: { terminal: true },
      excluded: SEAM_ONLY_TERMINAL,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('inactive when capabilities.terminal===true but excluded is empty', () => {
    const d = resolveNativeTerminalGate(true, makeBridge({ terminal: true }, []), {
      capabilities: { terminal: true },
      excluded: [],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('inactive when a non-terminal portion is ratified (e.g. fs only)', () => {
    const d = resolveNativeTerminalGate(true, makeBridge({ terminal: true }, [{ portion: 'fs', status: 'ratified' }]), {
      capabilities: { terminal: true },
      excluded: [{ portion: 'fs', status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('active when terminal is declared and ratified', () => {
    const d = resolveNativeTerminalGate(true, makeBridge({ terminal: true }, RATIFIED_TERMINAL), {
      capabilities: { terminal: true },
      excluded: RATIFIED_TERMINAL,
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

// ── resolveNativeTerminalGate — malformed / edge excluded[] shapes ───────

describe('resolveNativeTerminalGate — malformed / edge excluded[] shapes', () => {
  const bridge = makeBridge({ terminal: true }, RATIFIED_TERMINAL);

  it('excluded is a string (not an array) → inactive (terminal-not-ratified)', () => {
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: true },
      excluded: 'terminal' as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('excluded is null → inactive (terminal-not-ratified)', () => {
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: true },
      excluded: null as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('excluded entry missing `portion` → inactive (terminal-not-ratified)', () => {
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: true },
      excluded: [{ status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('excluded entry missing `status` → inactive (terminal-not-ratified)', () => {
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: true },
      excluded: [{ portion: 'terminal' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('terminal-not-ratified');
  });

  it('duplicate terminal entries (one seam-only + one ratified) → ACTIVE (first ratified wins)', () => {
    // NOTE ON ACTUAL BEHAVIOR: the implementation uses Array.prototype.some, so
    // ANY ratified terminal entry activates the gate even if a sibling is
    // seam-only. This asserts the first-match-wins consequence.
    const d = resolveNativeTerminalGate(true, bridge, {
      capabilities: { terminal: true },
      excluded: [
        { portion: 'terminal', status: 'seam-only' },
        { portion: 'terminal', status: 'ratified' },
      ],
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

// ── bridge structural detection ────────────────────────────────────────────

describe('bridge structural detection', () => {
  it('detects a getCapabilities-only bridge and rejects partial / null / non-object', () => {
    expect(hasSproutStudioTerminalBridge({ getCapabilities: async () => ({}) })).toBe(true);
    expect(hasSproutStudioTerminalBridge({})).toBe(false);
    expect(hasSproutStudioTerminalBridge(null)).toBe(false);
    expect(hasSproutStudioTerminalBridge(undefined)).toBe(false);
    expect(hasSproutStudioTerminalBridge('x')).toBe(false);
  });

  it('detectSproutStudioTerminal returns null without window (node env)', () => {
    // In this file's node environment window is undefined unless a resolver
    // case installs it; restore the clean state before asserting.
    delete (globalThis as unknown as { window?: unknown }).window;
    expect(detectSproutStudioTerminal()).toBeNull();
  });

  it('detectSproutStudioTerminal returns the bridge when window.SproutStudio has getCapabilities', () => {
    const bridge = { getCapabilities: async () => ({}) };
    (globalThis as unknown as { window?: unknown }).window = { SproutStudio: bridge };
    try {
      expect(detectSproutStudioTerminal()).toBe(bridge);
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it('detectSproutStudioTerminal returns null when window.SproutStudio lacks getCapabilities', () => {
    (globalThis as unknown as { window?: unknown }).window = { SproutStudio: { readWorkspaceFile: async () => ({}) } };
    try {
      expect(detectSproutStudioTerminal()).toBeNull();
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it('detectSproutStudioTerminal returns null when window.SproutStudio is absent', () => {
    (globalThis as unknown as { window?: unknown }).window = {};
    try {
      expect(detectSproutStudioTerminal()).toBeNull();
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });
});

// ── cached async resolver (flag ON — fresh dynamic import) ─────────────────
//
// These exercise the bridge paths of nativeTerminalGate() by flipping the
// compile-time flag per case and installing a fake window.SproutStudio. The
// gate cache is reset in beforeEach/afterEach so the shared module-level
// `gatePromise` never leaks between cases.

type GateModule = typeof import('../services/nativeTerminal');

let gate: GateModule;

async function loadGateModule(enabled: boolean): Promise<GateModule> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_TERMINAL', '');
  vi.resetModules();
  return import('../services/nativeTerminal');
}

function installBridge(bridge: unknown): void {
  (globalThis as unknown as { window?: unknown }).window = { SproutStudio: bridge };
}

describe('nativeTerminalGate (cached resolver, flag ON)', () => {
  let gate: GateModule;

  beforeEach(async () => {
    gate = await loadGateModule(true);
    gate.__resetNativeTerminalGateForTests();
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('getCapabilities is called exactly once across two nativeTerminalGate() calls', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { terminal: true },
      excluded: RATIFIED_TERMINAL as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    const d1 = await gate.nativeTerminalGate();
    const d2 = await gate.nativeTerminalGate();

    expect(d1).toEqual({ active: true, reason: 'active' });
    expect(d2).toEqual({ active: true, reason: 'active' });
    // The cached promise means getCapabilities runs ONCE for the lifetime.
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);
  });

  it('no bridge installed → { active: false, reason: "no-bridge" } (never throws)', async () => {
    installBridge(undefined); // window.SproutStudio absent
    const d = await gate.nativeTerminalGate();
    expect(d).toEqual({ active: false, reason: 'no-bridge' });
  });

  it('getCapabilities REJECTS → { active: false, reason: "getCapabilities-rejected" } (never throws)', async () => {
    const bridge = {
      getCapabilities: vi.fn(async () => {
        throw new Error('transport down');
      }),
    };
    installBridge(bridge);
    const d = await gate.nativeTerminalGate();
    expect(d).toEqual({ active: false, reason: 'getCapabilities-rejected' });
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);
  });

  it('seam-only terminal entry → { active: false, reason: "terminal-not-ratified" }', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { terminal: true },
      excluded: SEAM_ONLY_TERMINAL as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: false,
    };
    installBridge({ getCapabilities: async () => caps });
    const d = await gate.nativeTerminalGate();
    expect(d).toEqual({ active: false, reason: 'terminal-not-ratified' });
  });
});

describe('__resetNativeTerminalGateForTests clears the cache', () => {
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
      capabilities: { terminal: true },
      excluded: RATIFIED_TERMINAL as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    // Two calls share one cached promise → one getCapabilities.
    await gate.nativeTerminalGate();
    await gate.nativeTerminalGate();
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);

    // Reset → the next call re-detects + re-fetches.
    gate.__resetNativeTerminalGateForTests();
    const d = await gate.nativeTerminalGate();
    expect(d).toEqual({ active: true, reason: 'active' });
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(2);
  });
});

// ── resolver default-build short-circuit (flag OFF) ───────────────────────

describe('nativeTerminalGate default-build short-circuit (flag OFF)', () => {
  beforeEach(async () => {
    gate = await loadGateModule(false);
    gate.__resetNativeTerminalGateForTests();
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('flag OFF short-circuits to native-terminal-disabled BEFORE touching the bridge', async () => {
    // Even with a full, ratifying bridge installed, the compile-time flag off
    // means the resolver never reaches getCapabilities — a dead branch.
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { terminal: true },
      excluded: RATIFIED_TERMINAL as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    const d = await gate.nativeTerminalGate();
    expect(d).toEqual({ active: false, reason: 'native-terminal-disabled' });
    expect(bridge.getCapabilities).not.toHaveBeenCalled();
  });
});

// Housekeeping: reset the shared module-level gate cache so an import-order
// surprise cannot leak across the (separate) boot tests.
beforeAll(() => {
  __resetNativeTerminalGateForTests();
});
