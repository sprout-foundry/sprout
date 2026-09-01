// @vitest-environment jsdom
/**
 * R-4 boot guard — `useAppInitialization` hook level, git seam.
 *
 * `useAppInitialization.initApp()` wraps the three browser-git boot blocks
 * (configureBrowserGit, the agent git tool bridge, and the shell git
 * adapter) in `if (!NATIVE_GIT_ENABLED)` guards (see the
 * "Compile-time short-circuit (Track R --native-git)" comments in the hook).
 * A `--native-git` dist hard-excludes the git client API + boot wiring, so
 * the boot path must NOT wire browser git at all — no configureBrowserGit,
 * no registerGitToolGlobal / installGitToolBridge, no registerShellGitGlobal.
 * The default build (flag off) must keep today's exact behavior
 * byte-identical: all three git boot blocks run.
 *
 * The compile-time flag is controlled exactly like the sibling boot tests
 * (nativeFsBootInit.test.tsx, nativeTerminalBoot.test.tsx):
 * `vi.stubEnv('VITE_SPROUT_NATIVE_GIT','1')` + `vi.resetModules()` + a FRESH
 * dynamic import of the hook module, so the `NATIVE_GIT_ENABLED` constant
 * baked into `nativeGitFlag.ts` at import time reflects the env.
 * `VITE_SPROUT_MODE` is stubbed to `'cloud'` in every case so `isCloud`
 * bakes true — the git boot blocks only run on the cloud boot path.
 *
 * Coverage in this file:
 *   1. flag ON + cloud: NONE of the three git boot blocks run
 *      (configureBrowserGit / registerGitToolGlobal / installGitToolBridge /
 *      registerShellGitGlobal are all never called), while the non-git boot
 *      work (WASM preload state updates) still arrives — so the absence of
 *      git wiring is meaningful and not "nothing ran".
 *   2. flag OFF + cloud (default-build regression): the WASM preload resolves
 *      true with a live shell, so ALL THREE git boot blocks run —
 *      configureBrowserGit, registerGitToolGlobal + installGitToolBridge,
 *      and registerShellGitGlobal are each called exactly once.
 *
 * Harness: reuses the nativeFsBootInit pattern — the hook's `setState` is
 * spied and every updater is captured; the fake adapter's
 * `getWasmShell()` returns a live fake shell (with a
 * `wasm.SproutWasm.setToolExecutionHook`) so the inner `if (shell)` /
 * `if (wasmApi?.setToolExecutionHook)` branches of the git blocks are
 * actually reached in the flag-OFF case.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act, type RenderResult } from '@testing-library/react';
import type { EventsProvider } from '@sprout/events';
import type { AppState } from '../types/app';

// ── Mocks (hoisted — must resolve before the dynamic hook import) ───────────

/**
 * The fake adapter returned by the mocked `getAdapter`. `preloadWasmShell`
 * resolves true (the git boot blocks only run when the WASM preload
 * succeeds), and `getWasmShell` returns a live fake shell whose
 * `wasm.SproutWasm.setToolExecutionHook` presence is what lets the
 * agent-git-tool-bridge block reach `installGitToolBridge`.
 */
const fakeShell = vi.hoisted(() => ({
  writeFile: vi.fn(),
  readFile: vi.fn(() => ({ content: '' })),
  deleteFile: vi.fn(),
  wasm: {
    SproutWasm: {
      setToolExecutionHook: vi.fn(),
    },
  },
}));

const fakeAdapter = vi.hoisted(() => ({
  preloadWasmShell: vi.fn(async () => true),
  getWasmShell: vi.fn(() => fakeShell),
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

// Cloud-mode dynamic imports the hook reaches (git + agent-dispatcher);
// mocked so the real module graph (isomorphic-git, wasm handlers, …) never
// loads and each git boot block's entry function is a spy we can assert on.
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

/** Every `setState` updater the hook emits, in order. */
const capturedStateUpdates: StateUpdater[] = [];

/** Reconstruct the state sequence by folding updaters over a running prev. */
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

let hookFn: HookModule['useAppInitialization'];

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
 * Fresh import of the hook with the given native-GIT flag value.
 * `VITE_SPROUT_MODE` is always 'cloud' so `isCloud` bakes true (the git boot
 * blocks are cloud-only). The native-FS flag stays unset in every case so
 * the `!NATIVE_FS_ENABLED` wrapper around the git blocks is a pass-through
 * — this file isolates the GIT guard, not the FS one.
 */
async function loadHook(gitFlagOn: boolean): Promise<HookModule> {
  if (gitFlagOn) {
    vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
    vi.stubEnv('VITE_SPROUT_NATIVE_GIT', '1');
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
 * Wait until `predicate` is true (or a hard timeout). The boot path chains
 * several async hops (fetchRuntimeConfig → initApp → preloadWasmShell →
 * nested dynamic imports cloudWasmHandlers → browserGit / agentGitToolBridge
 * / shellGitAdapter), and the exact microtask depth of those dynamic imports
 * in vitest is not fixed — a plain fixed-round flush is brittle. Polling a
 * concrete outcome (the spy / state) is the robust approach: it returns as
 * soon as the behavior we assert on has actually landed.
 */
async function waitFor(predicate: () => boolean, { timeoutMs = 2000, stepMs = 5 } = {}): Promise<void> {
  const start = Date.now();
  while (!predicate()) {
    if (Date.now() - start > timeoutMs) {
      throw new Error(`waitFor timed out after ${timeoutMs}ms — the awaited boot behavior did not land`);
    }
    await act(async () => {
      await new Promise((r) => setTimeout(r, stepMs));
    });
  }
}

beforeEach(() => {
  vi.unstubAllEnvs();
  vi.clearAllMocks();
  capturedStateUpdates.length = 0;
  fakeAdapter.preloadWasmShell = vi.fn(async () => true);
  fakeAdapter.getWasmShell = vi.fn(() => fakeShell);
  fakeShell.wasm.SproutWasm.setToolExecutionHook = vi.fn();
});

// ── 1. Flag ON + cloud: the git boot guard short-circuits ──────────────────

describe('useAppInitialization — R-4 git guard ACTIVE (--native-git dist)', () => {
  it('flag ON + cloud: NONE of the three git boot blocks run (no browser git wiring at all)', async () => {
    await loadHook(true);

    const result: RenderResult = render(<BootHarness />);
    // Settle once the non-git WASM preload state has arrived — the git blocks
    // (if any ran) would have fired in the same ready-branch.
    await waitFor(() => resultingStates(capturedStateUpdates).some((s) => s.wasmReady === true));
    // One extra settle so any (wrongly-occurring) git wiring would have fired.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    result.unmount();

    // The guard: no browser-git wiring of any kind.
    expect(configureBrowserGitMock, 'configureBrowserGit must never be called').not.toHaveBeenCalled();
    expect(registerGitToolGlobalMock, 'registerGitToolGlobal must never be called').not.toHaveBeenCalled();
    expect(installGitToolBridgeMock, 'installGitToolBridge must never be called').not.toHaveBeenCalled();
    expect(registerShellGitGlobalMock, 'registerShellGitGlobal must never be called').not.toHaveBeenCalled();

    // The non-git WASM import work never touches browser git either.
    expect(listAllVfsFilesMock).not.toHaveBeenCalled();

    // Sanity: the hook DID run its non-git boot work (WASM preload state),
    // so the absence of git wiring above is meaningful and not "nothing ran".
    const states = resultingStates(capturedStateUpdates);
    expect(
      states.some((s) => s.wasmLoading === true),
      'wasmLoading:true state update must still arrive (non-git boot work ran)',
    ).toBe(true);
    expect(
      states.some((s) => s.wasmReady === true && s.wasmLoading === false),
      'wasmReady:true / wasmLoading:false state update must still arrive',
    ).toBe(true);
  });
});

// ── 2. Flag OFF + cloud: default-build regression (all three blocks run) ───

describe('useAppInitialization — R-4 git guard INACTIVE (default build)', () => {
  it('flag OFF + cloud: all three git boot blocks run exactly once (today behavior)', async () => {
    await loadHook(false);

    const { unmount } = await act(async () => render(<BootHarness />));
    // Settle once block 1 (configureBrowserGit) has landed; blocks 2 + 3 are
    // independent parallel dynamic-import chains that settle around the
    // same time — one extra settle so they cannot race the assertions.
    await waitFor(() => configureBrowserGitMock.mock.calls.length >= 1);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    unmount();

    // Block 1: browser-native git configured with the VFS callbacks.
    expect(configureBrowserGitMock).toHaveBeenCalledTimes(1);
    const gitConfig = configureBrowserGitMock.mock.calls[0][0] as Record<string, unknown>;
    expect(gitConfig.name).toBe('Browser IDE');
    expect(typeof gitConfig.readVfsFiles).toBe('function');

    // Block 2: agent git tool bridge (global + the WASM hook install).
    expect(registerGitToolGlobalMock).toHaveBeenCalledTimes(1);
    expect(installGitToolBridgeMock).toHaveBeenCalledTimes(1);

    // Block 3: shell git adapter backs the WASM `git` command.
    expect(registerShellGitGlobalMock).toHaveBeenCalledTimes(1);

    // Default-build regression: the WASM preload state still arrives.
    const states = resultingStates(capturedStateUpdates);
    expect(states.some((s) => s.wasmReady === true && s.wasmLoading === false)).toBe(true);
  });
});

// ── Housekeeping ────────────────────────────────────────────────────────────
// (RTL auto-cleanup handles unmounting; no manual cleanup needed.)
