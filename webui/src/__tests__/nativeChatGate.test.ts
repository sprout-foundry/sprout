// @vitest-environment node
/**
 * R-4: pure gate-decision coverage for the Track R native-chat deferral leaf
 * module (webui/src/services/nativeChat/index.ts). Mirrors
 * nativeTerminalGate.test.ts.
 *
 * Coverage:
 *   - resolveNativeChatGate (pure): every reason path, both flag states,
 *     malformed capabilities (non-object / null / undefined / missing fields).
 *   - structural detector: hasSproutStudioChatBridge +
 *     detectSproutStudioChat (no window / no SproutStudio / missing
 *     getCapabilities / good bridge).
 *   - cached async resolver nativeChatGate(): getCapabilities called
 *     exactly once across two calls; the getCapabilities-rejected path;
 *     and __resetNativeChatGateForTests clearing the cache.
 *
 * The resolver short-circuits to `native-chat-disabled` when the
 * module-level compile-time flag is false. To exercise the bridge paths we
 * flip the flag with `vi.stubEnv('VITE_SPROUT_NATIVE_CHAT','1')` +
 * `vi.resetModules()` + a fresh dynamic import of the leaf module, and
 * install a fake `window.SproutStudio` (in the node environment `window` is
 * undefined, so we define it on globalThis). Each resolver case resets the
 * gate cache in beforeEach/afterEach so it never leaks.
 */

import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import {
  resolveNativeChatGate,
  hasSproutStudioChatBridge,
  detectSproutStudioChat,
  nativeChatGate,
  __resetNativeChatGateForTests,
} from '../services/nativeChat';
import type { SproutStudioCapabilities } from '../services/nativeChat';

/** A minimal usable chat bridge: a getCapabilities spy only. */
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

const RATIFIED_CHAT = [{ portion: 'chat', status: 'ratified' }];
const SEAM_ONLY_CHAT = [{ portion: 'chat', status: 'seam-only' }];

// ── resolveNativeChatGate (pure) ─────────────────────────────────────

describe('resolveNativeChatGate (pure)', () => {
  it('inactive when the compile-time flag is off (even with a good bridge + caps)', () => {
    const d = resolveNativeChatGate(false, makeBridge({ chat: true }, RATIFIED_CHAT), {
      capabilities: { chat: true },
      excluded: RATIFIED_CHAT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('native-chat-disabled');
  });

  it('inactive when there is no usable bridge', () => {
    const d = resolveNativeChatGate(true, null, {
      capabilities: { chat: true },
      excluded: RATIFIED_CHAT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('no-bridge');
  });

  it('inactive on malformed capabilities (null / undefined / a non-object)', () => {
    const bridge = makeBridge({ chat: true }, RATIFIED_CHAT);
    // A truly non-object response (or null / undefined) is a malformed
    // capabilities response — a gate-fail, never a throw.
    expect(resolveNativeChatGate(true, bridge, null)).toEqual({ active: false, reason: 'malformed-capabilities' });
    expect(resolveNativeChatGate(true, bridge, undefined)).toEqual({
      active: false,
      reason: 'malformed-capabilities',
    });
    // A non-object (a string) is also malformed.
    expect(resolveNativeChatGate(true, bridge, 'x' as unknown)).toEqual({
      active: false,
      reason: 'malformed-capabilities',
    });
  });

  it('inactive when capabilities is an object but the capabilities key is missing ({}) → chat-not-declared', () => {
    // Mirrors the terminal leaf: `{}` IS an object (so not "malformed"), but
    // it has no `capabilities` key, so it falls through to the not-declared
    // step. (nativeTerminalGate documents the identical terminal behavior.)
    const bridge = makeBridge({ chat: true }, RATIFIED_CHAT);
    const d = resolveNativeChatGate(true, bridge, {});
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-declared');
  });

  it('inactive when the shell does not declare chat', () => {
    const bridge = makeBridge({ chat: false }, RATIFIED_CHAT);
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: false },
      excluded: RATIFIED_CHAT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-declared');
  });

  it('inactive when the capabilities.chat key is missing entirely', () => {
    const bridge = makeBridge({}, RATIFIED_CHAT);
    const d = resolveNativeChatGate(true, bridge, { capabilities: {}, excluded: RATIFIED_CHAT });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-declared');
  });

  it('chat truthy-but-not-boolean (e.g. "yes") FAILS the gate (strict-typing)', () => {
    const bridge = makeBridge({ chat: true }, RATIFIED_CHAT);
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: 'yes' as unknown as boolean },
      excluded: RATIFIED_CHAT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-declared');
  });

  it('inactive when the chat entry is seam-only (unratified)', () => {
    const d = resolveNativeChatGate(true, makeBridge({ chat: true }, SEAM_ONLY_CHAT), {
      capabilities: { chat: true },
      excluded: SEAM_ONLY_CHAT,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('inactive when capabilities.chat===true but excluded is empty', () => {
    const d = resolveNativeChatGate(true, makeBridge({ chat: true }, []), {
      capabilities: { chat: true },
      excluded: [],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('inactive when a non-chat portion is ratified (e.g. fs only)', () => {
    const d = resolveNativeChatGate(true, makeBridge({ chat: true }, [{ portion: 'fs', status: 'ratified' }]), {
      capabilities: { chat: true },
      excluded: [{ portion: 'fs', status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('active when chat is declared and ratified', () => {
    const d = resolveNativeChatGate(true, makeBridge({ chat: true }, RATIFIED_CHAT), {
      capabilities: { chat: true },
      excluded: RATIFIED_CHAT,
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

// ── resolveNativeChatGate — malformed / edge excluded[] shapes ───────

describe('resolveNativeChatGate — malformed / edge excluded[] shapes', () => {
  const bridge = makeBridge({ chat: true }, RATIFIED_CHAT);

  it('excluded is a string (not an array) → inactive (chat-not-ratified)', () => {
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: true },
      excluded: 'chat' as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('excluded is null → inactive (chat-not-ratified)', () => {
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: true },
      excluded: null as unknown as Array<Record<string, unknown>>,
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('excluded entry missing `portion` → inactive (chat-not-ratified)', () => {
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: true },
      excluded: [{ status: 'ratified' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('excluded entry missing `status` → inactive (chat-not-ratified)', () => {
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: true },
      excluded: [{ portion: 'chat' }],
    });
    expect(d.active).toBe(false);
    expect(d.reason).toBe('chat-not-ratified');
  });

  it('duplicate chat entries (one seam-only + one ratified) → ACTIVE (any ratified wins)', () => {
    // NOTE ON ACTUAL BEHAVIOR: the implementation uses Array.prototype.some,
    // so ANY ratified chat entry activates the gate even if a sibling is
    // seam-only. This asserts the first-match-wins consequence.
    const d = resolveNativeChatGate(true, bridge, {
      capabilities: { chat: true },
      excluded: [
        { portion: 'chat', status: 'seam-only' },
        { portion: 'chat', status: 'ratified' },
      ],
    });
    expect(d.active).toBe(true);
    expect(d.reason).toBe('active');
  });
});

// ── bridge structural detection ──────────────────────────────────────────────

describe('bridge structural detection', () => {
  it('detects a getCapabilities-only bridge and rejects partial / null / non-object', () => {
    expect(hasSproutStudioChatBridge({ getCapabilities: async () => ({}) })).toBe(true);
    expect(hasSproutStudioChatBridge({})).toBe(false);
    expect(hasSproutStudioChatBridge(null)).toBe(false);
    expect(hasSproutStudioChatBridge(undefined)).toBe(false);
    expect(hasSproutStudioChatBridge('x')).toBe(false);
  });

  it('detectSproutStudioChat returns null without window (node env)', () => {
    // In this file's node environment window is undefined unless a resolver
    // case installs it; restore the clean state before asserting.
    delete (globalThis as unknown as { window?: unknown }).window;
    expect(detectSproutStudioChat()).toBeNull();
  });

  it('detectSproutStudioChat returns the bridge when window.SproutStudio has getCapabilities', () => {
    const bridge = { getCapabilities: async () => ({}) };
    (globalThis as unknown as { window?: unknown }).window = { SproutStudio: bridge };
    try {
      expect(detectSproutStudioChat()).toBe(bridge);
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it('detectSproutStudioChat returns null when window.SproutStudio lacks getCapabilities', () => {
    (globalThis as unknown as { window?: unknown }).window = { SproutStudio: { readWorkspaceFile: async () => ({}) } };
    try {
      expect(detectSproutStudioChat()).toBeNull();
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });

  it('detectSproutStudioChat returns null when window.SproutStudio is absent', () => {
    (globalThis as unknown as { window?: unknown }).window = {};
    try {
      expect(detectSproutStudioChat()).toBeNull();
    } finally {
      delete (globalThis as unknown as { window?: unknown }).window;
    }
  });
});

// ── cached async resolver (flag ON — fresh dynamic import) ─────────────────
//
// These exercise the bridge paths of nativeChatGate() by flipping the
// compile-time flag per case and installing a fake window.SproutStudio. The
// gate cache is reset in beforeEach/afterEach so the shared module-level
// `gatePromise` never leaks between cases.

type GateModule = typeof import('../services/nativeChat');

let gate: GateModule;

async function loadGateModule(enabled: boolean): Promise<GateModule> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_CHAT', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_CHAT', '');
  vi.resetModules();
  return import('../services/nativeChat');
}

function installBridge(bridge: unknown): void {
  (globalThis as unknown as { window?: unknown }).window = { SproutStudio: bridge };
}

describe('nativeChatGate (cached resolver, flag ON)', () => {
  beforeEach(async () => {
    gate = await loadGateModule(true);
    gate.__resetNativeChatGateForTests();
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('getCapabilities is called exactly once across two nativeChatGate() calls', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { chat: true },
      excluded: RATIFIED_CHAT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    const d1 = await gate.nativeChatGate();
    const d2 = await gate.nativeChatGate();

    expect(d1).toEqual({ active: true, reason: 'active' });
    expect(d2).toEqual({ active: true, reason: 'active' });
    // The cached promise means getCapabilities runs ONCE for the lifetime.
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);
  });

  it('no bridge installed → { active: false, reason: "no-bridge" } (never throws)', async () => {
    installBridge(undefined); // window.SproutStudio absent
    const d = await gate.nativeChatGate();
    expect(d).toEqual({ active: false, reason: 'no-bridge' });
  });

  it('getCapabilities REJECTS → { active: false, reason: "getCapabilities-rejected" } (never throws)', async () => {
    const bridge = {
      getCapabilities: vi.fn(async () => {
        throw new Error('transport down');
      }),
    };
    installBridge(bridge);
    const d = await gate.nativeChatGate();
    expect(d).toEqual({ active: false, reason: 'getCapabilities-rejected' });
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);
  });

  it('seam-only chat entry → { active: false, reason: "chat-not-ratified" }', async () => {
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { chat: true },
      excluded: SEAM_ONLY_CHAT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: false,
    };
    installBridge({ getCapabilities: async () => caps });
    const d = await gate.nativeChatGate();
    expect(d).toEqual({ active: false, reason: 'chat-not-ratified' });
  });
});

describe('__resetNativeChatGateForTests clears the cache', () => {
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
      capabilities: { chat: true },
      excluded: RATIFIED_CHAT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    // Two calls share one cached promise → one getCapabilities.
    await gate.nativeChatGate();
    await gate.nativeChatGate();
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(1);

    // Reset → the next call re-detects + re-fetches.
    gate.__resetNativeChatGateForTests();
    const d = await gate.nativeChatGate();
    expect(d).toEqual({ active: true, reason: 'active' });
    expect(bridge.getCapabilities).toHaveBeenCalledTimes(2);
  });
});

// ── resolver default-build short-circuit (flag OFF) ───────────────────────

describe('nativeChatGate default-build short-circuit (flag OFF)', () => {
  beforeEach(async () => {
    gate = await loadGateModule(false);
    gate.__resetNativeChatGateForTests();
  });
  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window;
    vi.unstubAllEnvs();
  });

  it('flag OFF short-circuits to native-chat-disabled BEFORE touching the bridge', async () => {
    // Even with a full, ratifying bridge installed, the compile-time flag off
    // means the resolver never reaches getCapabilities — a dead branch.
    const caps: SproutStudioCapabilities = {
      schemaVersion: 1,
      capabilities: { chat: true },
      excluded: RATIFIED_CHAT as unknown as SproutStudioCapabilities['excluded'],
      manifestPresent: true,
      servable: true,
    };
    const bridge = { getCapabilities: vi.fn(async () => caps) };
    installBridge(bridge);

    const d = await gate.nativeChatGate();
    expect(d).toEqual({ active: false, reason: 'native-chat-disabled' });
    expect(bridge.getCapabilities).not.toHaveBeenCalled();
  });
});

// Housekeeping: reset the shared module-level gate cache so an import-order
// surprise cannot leak across the (separate) boot tests.
beforeAll(() => {
  __resetNativeChatGateForTests();
});
