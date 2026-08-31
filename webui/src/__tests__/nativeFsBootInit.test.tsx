// @vitest-environment jsdom
/**
 * R-2f boot guard — `useAppInitialization` hook level.
 *
 * `useAppInitialization.initApp()` wraps the entire cloud WASM-preload block
 * in `if (!NATIVE_FS_ENABLED) { ... }` (see the "Compile-time short-circuit
 * (R-2f)" comment in the hook). A `--native-fs` dist hard-excludes the
 * wasmShell module, so the boot path must NOT touch the adapter WASM preload
 * at all — no `preloadWasmShell()` call, no `wasmLoading: true` state update,
 * no `wasmError` state. The default build (flag off) must keep today's exact
 * behavior byte-identical.
 *
 * The compile-time flag is controlled exactly like the other native-FS tests
 * (nativeFsBoot.test.tsx, nativeFsSidebar.test.tsx):
 * `vi.stubEnv('VITE_SPROUT_NATIVE_FS','1')` + `vi.resetModules()` + a FRESH
 * dynamic import of the hook module, so the `NATIVE_FS_ENABLED` constant
 * baked into `nativeFsFlag.ts` at import time reflects the env.
 * `VITE_SPROUT_MODE` is stubbed to `'cloud'` in every case so `isCloud`
 * bakes true — the guard only matters on the cloud boot path.
 *
 * Coverage in this file (the nativeFsBoot.test.tsx header explicitly defers
 * the hook to this file — it only covers CloudAdapter + useWasmTerminalInput):
 *   1. flag ON + cloud mode: `adapter.preloadWasmShell` is NEVER called; no
 *      captured `setState` updater ever produces `wasmLoading === true` or
 *      a `wasmError` field.
 *   2. flag OFF + cloud mode (default-build regression):
 *      `adapter.preloadWasmShell` IS called exactly once, and the
 *      `wasmLoading: true` state update still arrives.
 *   3. flag OFF + cloud mode, preload resolves true: the
 *      `{ wasmReady: true, wasmLoading: false }` state update arrives.
 *
 * Harness: the hook's `setState` is spied (the hook only ever calls it with
 * functional updaters) and every updater is captured into an array. The
 * resulting state sequence is reconstructed by folding the updaters over a
 * running `prev`, which is what the test assertions inspect.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act, type RenderResult } from '@testing-library/react';
import type { EventsProvider } from '@sprout/events';
import type { AppState } from '../types/app';

// ── Mocks (hoisted — must resolve before the dynamic hook import) ───────────

/**
 * The fake adapter returned by the mocked `getAdapter`. `preloadWasmShell`
 * resolves true by default so the flag-OFF "preload resolves true" path is
 * the baseline; each test can reconfigure it if needed.
 */
const fakeAdapter = vi.hoisted(() => ({
  preloadWasmShell: vi.fn(async () => true),
  getWasmShell: vi.fn(() => null),
}));

vi.mock('../services/apiAdapter', () => ({
  getAdapter: () => fakeAdapter,
  installAdapter: vi.fn(),
  hasAdapter: () => true,
  requiresBackendHealthCheck: () => false,
  ADAPTER_INSTALLED_EVENT: 'sprout:adapter-installed',
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      getStats: async () => ({ provider: '', model: '', stats: {} }),
      getFiles: async () => ({ files: [] }),
      getWorkspace: async () => ({}),
      setWorkspace: async () => ({}),
      getSessions: async () => ({ sessions: [], current_session_id: '' }),
      restoreSession: async () => ({ messages: [] }),
    }),
  },
}));

vi.mock('../services/serviceWorkerRegistration', () => ({
  registerServiceWorker: vi.fn(),
}));

vi.mock('../services/clientSession', () => ({
  getTabWorkspacePath: () => '',
}));

vi.mock('../bootstrapAdapter', () => ({
  fetchRuntimeConfig: async () => ({ appMode: 'cloud', user: { id: 'u' } }),
}));

// Flag-OFF preload path dynamic-imports these; mocked so the imports are
// cheap no-ops and no real module graph (isomorphic-git, handlers, …) loads.
const setAgentEventDispatcherMock = vi.hoisted(() => vi.fn());
const listAllVfsFilesMock = vi.hoisted(() => vi.fn(async () => []));
vi.mock('../services/cloudWasmHandlers', () => ({
  setAgentEventDispatcher: (...args: unknown[]) => setAgentEventDispatcherMock(...args),
  listAllVfsFiles: (...args: unknown[]) => listAllVfsFilesMock(...args),
}));

const configureBrowserGitMock = vi.hoisted(() => vi.fn());
vi.mock('../services/browserGit', () => ({
  configureBrowserGit: (...args: unknown[]) => configureBrowserGitMock(...args),
}));

const registerGitToolGlobalMock = vi.hoisted(() => vi.fn());
const installGitToolBridgeMock = vi.hoisted(() => vi.fn());
vi.mock('../services/agentGitToolBridge', () => ({
  registerGitToolGlobal: (...args: unknown[]) => registerGitToolGlobalMock(...args),
  installGitToolBridge: (...args: unknown[]) => installGitToolBridgeMock(...args),
}));

const registerShellGitGlobalMock = vi.hoisted(() => vi.fn());
vi.mock('../services/shellGitAdapter', () => ({
  registerShellGitGlobal: (...args: unknown[]) => registerShellGitGlobalMock(...args),
}));

// Keep boot-path log output quiet and out of the dependency graph
// (the real useLog needs the NotificationContext provider).
vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  useLog: () => ({
    debug: vi.fn(),
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  }),
}));

// ── Harness ─────────────────────────────────────────────────────────────────

type HookModule = typeof import('../hooks/useAppInitialization');
type StateUpdater = (prev: any) => Partial<Record<string, unknown>>;

/**
 * The freshly-imported hook (assigned by loadHook after vi.resetModules()).
 * Module scope in the TEST file is never reset, so the harness and the
 * test bodies always see the same variable.
 */
let hookFn: HookModule['useAppInitialization'];

/** Every `setState` updater the hook emits, in order. */
const capturedStateUpdates: StateUpdater[] = [];

/**
 * Reconstructs the state sequence by folding updaters over a running prev
 * (the hook only calls `setState` with functional updaters).
 */
function resultingStates(updates: StateUpdater[]): Array<Record<string, unknown>> {
  let prev: Record<string, unknown> = {};
  const states: Array<Record<string, unknown>> = [];
  for (const updater of updates) {
    prev = { ...prev, ...(updater(prev) ?? {}) };
    states.push({ ...prev });
  }
  return states;
}

/** Stable eventsProvider stub — identity must not change across renders. */
const eventsProviderStub = {
  connect: vi.fn(),
  onEvent: vi.fn(),
  onReconnect: vi.fn(),
  removeEvent: vi.fn(),
} as unknown as EventsProvider;

function BootHarness() {
  const setSpy = (updater: (prev: AppState) => Partial<AppState>) => {
    capturedStateUpdates.push(updater as unknown as StateUpdater);
  };
  hookFn({
    eventsProvider: eventsProviderStub,
    handleEvent: vi.fn(),
    connectionTimeoutRef: { current: null },
    loadChatSessions: vi.fn(),
    setRecentFiles: vi.fn(),
    setIsMobile: vi.fn(),
    setIsTablet: vi.fn(),
    setState: setSpy,
    handleReconnect: vi.fn(),
  });
  return null;
}

/**
 * Fresh import of the hook module with the given flag value. `VITE_SPROUT_MODE`
 * is always 'cloud' so `isCloud` bakes true; the native-FS flag is '1' for the
 * ON case, unset for the OFF case (default-build regression).
 */
async function loadHook(flagOn: boolean): Promise<HookModule> {
  if (flagOn) {
    vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
    vi.stubEnv('VITE_SPROUT_NATIVE_FS', '1');
  } else {
    vi.unstubAllEnvs();
    vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
  }
  vi.resetModules();
  const mod = await import('../hooks/useAppInitialization');
  hookFn = mod.useAppInitialization;
  return mod;
}

/**
 * Flushes the microtask queue several times so the
 * fetchRuntimeConfig().then(initApp) → preloadPromise.then(ready) → dynamic
 * import chains all settle. Real timers are kept (the hook's 5s stats
 * interval is harmless inside a fast test; unmount clears it).
 */
async function flushMicrotasks(rounds = 8): Promise<void> {
  await act(async () => {
    for (let i = 0; i < rounds; i++) {
      await Promise.resolve();
    }
  });
}

beforeEach(() => {
  vi.unstubAllEnvs();
  vi.clearAllMocks();
  capturedStateUpdates.length = 0;
});

// ── 1. Flag ON + cloud: the boot guard short-circuits ──────────────────────

describe('useAppInitialization — R-2f guard ACTIVE (--native-fs dist)', () => {
  it('flag ON + cloud: preloadWasmShell is never called and no wasmLoading/wasmError state updates are emitted', async () => {
    await loadHook(true);

    const result: RenderResult = render(<BootHarness />);
    await flushMicrotasks();
    result.unmount();

    // The guard: no WASM preload touch at all.
    expect(fakeAdapter.preloadWasmShell).not.toHaveBeenCalled();

    // Sanity: the hook DID run its non-WASM boot work (stats load), so the
    // absence of wasm state below is meaningful and not "nothing ran".
    const states = resultingStates(capturedStateUpdates);
    expect(states.length).toBeGreaterThan(0);
    for (const state of states) {
      expect(state.wasmLoading, 'no state update may set wasmLoading').toBeUndefined();
      expect(state.wasmError, 'no state update may set wasmError').toBeUndefined();
      expect(state.wasmReady, 'no state update may set wasmReady').toBeUndefined();
    }
  });
});

// ── 2. Flag OFF + cloud: default-build regression ──────────────────────────

describe('useAppInitialization — R-2f guard INACTIVE (default build)', () => {
  it('flag OFF + cloud: preloadWasmShell IS called exactly once', async () => {
    await loadHook(false);

    const { unmount } = await act(async () => render(<BootHarness />));
    await flushMicrotasks();
    unmount();

    expect(fakeAdapter.preloadWasmShell).toHaveBeenCalledTimes(1);

    // Default-build regression: the boot still announces the loading state.
    const states = resultingStates(capturedStateUpdates);
    expect(
      states.some((s) => s.wasmLoading === true),
      'wasmLoading:true state update must arrive',
    ).toBe(true);
  });
});

// ── 3. Flag OFF + cloud, preload resolves true ─────────────────────────────

describe('useAppInitialization — default build, preload success path', () => {
  it('flag OFF + cloud, preload resolves true: { wasmReady: true, wasmLoading: false } state update arrives', async () => {
    fakeAdapter.preloadWasmShell = vi.fn(async () => true);
    await loadHook(false);

    const { unmount } = await act(async () => render(<BootHarness />));
    await flushMicrotasks();
    unmount();

    expect(fakeAdapter.preloadWasmShell).toHaveBeenCalledTimes(1);
    const states = resultingStates(capturedStateUpdates);
    expect(
      states.some((s) => s.wasmReady === true && s.wasmLoading === false),
      'wasmReady:true / wasmLoading:false state update must arrive',
    ).toBe(true);
  });
});

// ── Housekeeping ────────────────────────────────────────────────────────────
// (RTL auto-cleanup handles unmounting; no manual cleanup needed.)
