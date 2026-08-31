// @vitest-environment jsdom
/**
 * R-2f: conditional-WASM-boot — the boot path must NEVER instantiate the
 * (hard-excluded) `wasmShell` module when NATIVE_FS_ENABLED is true, while
 * the default build (flag off) keeps today's exact behavior byte-identical.
 *
 * The compile-time flag is controlled exactly like the other native-FS tests
 * (nativeFsSidebar.test.tsx, nativeFsDeferral.test.ts):
 * `vi.stubEnv('VITE_SPROUT_NATIVE_FS','1')` + `vi.resetModules()` + a FRESH
 * dynamic import of the module under test, so the `NATIVE_FS_ENABLED`
 * constant baked into that import reflects the env.
 *
 * Coverage in this file:
 *   1. `CloudAdapter.preloadWasmShell()` — flag ON: resolves `false` without
 *      ever calling `initWasmShell`; flag OFF: calls `initWasmShell`
 *      (success and rejection paths).
 *   2. `CloudAdapter.fetch()` routing — flag ON: wasm-local endpoints
 *      (POST /api/query) and the dynamic decision endpoints
 *      (POST /api/edits/{id}/decision, /api/shell-approvals/{id}/decision)
 *      fall through to the standard Foundry proxy (rewritten URL, proxied
 *      fetch). Flag OFF + initWasmShell rejecting: the old
 *      fall-through-with-warn path still reaches the proxied fetch.
 *   3. `useWasmTerminalInput` — flag ON: the lifecycle effect
 *      short-circuits, `initWasmShell` is never called, and
 *      `wasmProvidedByShell === true` / `wasmLoading === false` /
 *      `wasmError === null`; flag OFF + disconnected: `initWasmShell` IS
 *      called (via a pre-populated xtermRef).
 *
 * NOTE (TerminalPane mount): `TerminalPane.tsx` renders the
 * "Terminal provided by the native shell" placeholder purely on the
 * `wasmProvidedByShell` flag returned by `useWasmTerminalInput` — a trivial
 * conditional with no logic of its own. Mounting the component is heavy
 * (real xterm mount + 7 hooks) and would add no behavioral coverage beyond
 * case 3, so the component mount is deliberately skipped. The hook-level
 * test above is the authoritative check for the R-2f boot behavior.
 *
 * `useAppInitialization` is likewise not exercised here: it delegates the
 * boot decision to `adapter.preloadWasmShell()`, which is covered in case 1.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import React, { useRef } from 'react';

type XTerm = import('@xterm/xterm').Terminal;

// ── Shared mocks ────────────────────────────────────────────────────────────

/**
 * The fake WASM shell returned by the mocked initWasmShell. Only the surface
 * CloudAdapter touches (writeFile/readFile/getCwd) is needed; the rest are
 * no-op stubs.
 */
function makeFakeWasmShell() {
  return {
    writeFile: vi.fn(() => ''),
    readFile: vi.fn(() => ({ content: '' })),
    getCwd: () => '/',
    executeCommand: vi.fn(() => ({ stdout: '', stderr: '', exitCode: 0 })),
    autoComplete: vi.fn(() => ({ completions: [] })),
    changeDir: vi.fn(() => ({ cwd: '/' })),
    listDir: vi.fn(() => ({ entries: [] })),
    deleteFile: vi.fn(() => ''),
    clearConversation: vi.fn(),
    stopAgent: vi.fn(),
  };
}

const initWasmShellMock = vi.hoisted(() => vi.fn());
const resetWasmShellMock = vi.hoisted(() => vi.fn());
vi.mock('../services/wasmShell', () => ({
  initWasmShell: (...args: unknown[]) => initWasmShellMock(...args),
  resetWasmShell: (...args: unknown[]) => resetWasmShellMock(...args),
}));

// clientSession: keep the module graph light and identity stable.
vi.mock('../services/clientSession', () => ({
  clientFetch: (...args: unknown[]) => args,
  appendClientIdToUrl: (u: string) => u,
  getProxyBase: () => '',
  getWebUIClientId: () => 'test-client',
  resolveWebUIClientId: async () => 'test-client',
  WEBUI_CLIENT_ID_HEADER: 'X-Sprout-Client-ID',
}));

// cloudWasmHandlers: heavy handlers replaced with stubs. When flag OFF the
// CloudAdapter calls these with the mocked shell; they resolve a 200
// Response so the request is served "in the browser" and never reaches the
// proxied fetch — which is exactly what the flag-OFF assertions rely on.
const handleWasmLocalMock = vi.hoisted(() => vi.fn());
vi.mock('../services/cloudWasmHandlers', () => ({
  handleWasmLocal: (...args: unknown[]) => handleWasmLocalMock(...args),
  handleWasmEditDecision: vi.fn(
    () =>
      new Response(JSON.stringify({ ok: true, decision: 'edit' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
  ),
  handleWasmShellApprovalDecision: vi.fn(
    () =>
      new Response(JSON.stringify({ ok: true, decision: 'approval' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
  ),
  trackFileWrite: vi.fn(),
  jsonOk: (data: unknown) =>
    new Response(JSON.stringify({ message: 'success', ...data }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  jsonError: (message: string, status = 400) =>
    new Response(JSON.stringify({ message }), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
}));

// cloudSessionHandlers: keep it trivial so the real registry + proxy routes
// stay in the graph untouched.
vi.mock('../services/cloudSessionHandlers', () => ({
  handleCloudSessionsEndpoint: vi.fn(() => null),
}));

/** Stubs the global fetch and returns the recorded calls. */
function stubGlobalFetch(): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn(
    async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

/** Fresh import of CloudAdapter with the given compile-time flag value. */
async function loadCloudAdapter(enabled: boolean) {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_FS', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_FS', '');
  vi.resetModules();
  const mod = await import('../services/cloudAdapter');
  return mod.CloudAdapter;
}

beforeEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  initWasmShellMock.mockReset();
  handleWasmLocalMock.mockReset();
});

// ── 1. preloadWasmShell ─────────────────────────────────────────────────────

describe('CloudAdapter.preloadWasmShell — R-2f boot short-circuit', () => {
  it('flag ON: resolves false without ever calling initWasmShell', async () => {
    const CloudAdapter = await loadCloudAdapter(true);
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    const result = await adapter.preloadWasmShell();

    expect(result).toBe(false);
    expect(initWasmShellMock).not.toHaveBeenCalled();
    expect(adapter.getWasmShell()).toBeNull();
  });

  it('flag OFF: calls initWasmShell and resolves true on success', async () => {
    const CloudAdapter = await loadCloudAdapter(false);
    initWasmShellMock.mockResolvedValue(makeFakeWasmShell());
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    const result = await adapter.preloadWasmShell();

    expect(result).toBe(true);
    expect(initWasmShellMock).toHaveBeenCalledTimes(1);
    expect(adapter.getWasmShell()).not.toBeNull();
  });

  it('flag OFF: initWasmShell rejection resolves false (cached failure, no crash)', async () => {
    const CloudAdapter = await loadCloudAdapter(false);
    initWasmShellMock.mockRejectedValue(new Error('wasm unsupported'));
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    const result = await adapter.preloadWasmShell();

    expect(result).toBe(false);
    expect(initWasmShellMock).toHaveBeenCalledTimes(1);
    expect(adapter.getWasmShell()).toBeNull();
  });
});

// ── 2. fetch() routing ──────────────────────────────────────────────────────

describe('CloudAdapter.fetch — wasm-local routing under the flag', () => {
  const PROXIED_QUERY_URL = 'https://api.test/api/query';
  const PROXIED_EDIT_URL = 'https://api.test/api/edits/abc/decision';
  const PROXIED_APPROVAL_URL = 'https://api.test/api/shell-approvals/abc/decision';

  it('flag ON: POST /api/query falls through to the standard proxy', async () => {
    const CloudAdapter = await loadCloudAdapter(true);
    const fetchMock = stubGlobalFetch();
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    const res = await adapter.fetch('/api/query', {
      method: 'POST',
      body: JSON.stringify({ query: 'hello' }),
      headers: { 'Content-Type': 'application/json' },
    });

    expect(res.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toBe(PROXIED_QUERY_URL);
    // The WASM interception must not have been touched.
    expect(initWasmShellMock).not.toHaveBeenCalled();
    expect(handleWasmLocalMock).not.toHaveBeenCalled();
  });

  it('flag ON: edit-decision falls through to the standard proxy', async () => {
    const CloudAdapter = await loadCloudAdapter(true);
    const fetchMock = stubGlobalFetch();
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    await adapter.fetch('/api/edits/abc/decision', {
      method: 'POST',
      body: JSON.stringify({ approved: true }),
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toBe(PROXIED_EDIT_URL);
    expect(initWasmShellMock).not.toHaveBeenCalled();
  });

  it('flag ON: shell-approval-decision falls through to the standard proxy', async () => {
    const CloudAdapter = await loadCloudAdapter(true);
    const fetchMock = stubGlobalFetch();
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    await adapter.fetch('/api/shell-approvals/abc/decision', {
      method: 'POST',
      body: JSON.stringify({ decisions: {} }),
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toBe(PROXIED_APPROVAL_URL);
    expect(initWasmShellMock).not.toHaveBeenCalled();
  });

  it('flag ON: proxied fetch carries the client header + credentials', async () => {
    const CloudAdapter = await loadCloudAdapter(true);
    const fetchMock = stubGlobalFetch();
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    await adapter.fetch('/api/query', {
      method: 'POST',
      body: '{}',
      headers: { 'Content-Type': 'application/json' },
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe(PROXIED_QUERY_URL);
    expect(init.credentials).toBe('include');
    expect(init.headers.get('X-Sprout-Client-ID')).toBe('test-client');
  });

  it('flag OFF + initWasmShell rejecting: wasm-local fall-through-with-warn still reaches the proxy', async () => {
    const CloudAdapter = await loadCloudAdapter(false);
    const fetchMock = stubGlobalFetch();
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    initWasmShellMock.mockRejectedValue(new Error('wasm unsupported'));
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    await adapter.fetch('/api/query', {
      method: 'POST',
      body: JSON.stringify({ query: 'hello' }),
    });

    // Old behavior: ensureWasmShell was attempted, failed, and the request
    // fell through to the standard proxy with a console.warn.
    expect(initWasmShellMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toBe(PROXIED_QUERY_URL);
    expect(warnSpy).toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it('flag OFF + working shell: wasm-local endpoint is served in-browser (no proxy)', async () => {
    const CloudAdapter = await loadCloudAdapter(false);
    const fetchMock = stubGlobalFetch();
    initWasmShellMock.mockResolvedValue(makeFakeWasmShell());
    handleWasmLocalMock.mockResolvedValue(
      new Response(JSON.stringify({ ok: true, files: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const adapter = new CloudAdapter({ apiBase: 'https://api.test', wsUrl: 'wss://x/ws' });

    const res = await adapter.fetch('/api/query', {
      method: 'POST',
      body: JSON.stringify({ query: 'hello' }),
    });

    // The WASM handler served the request — the proxy was never reached.
    expect(res.status).toBe(200);
    expect(handleWasmLocalMock).toHaveBeenCalledTimes(1);
    expect(initWasmShellMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

// ── 3. useWasmTerminalInput ─────────────────────────────────────────────────

type HookModule = typeof import('../hooks/useWasmTerminalInput');

/**
 * Shared capture slot. The harness writes the hook's return value into
 * `current` on each render; the test reads it back. Passing the same plain
 * object (structurally a MutableRefObject) — rather than a fresh useRef per
 * test — keeps the write side and the read side on one object.
 */
const captured: React.MutableRefObject<HookModule['UseWasmTerminalInputReturn'] | null> = {
  current: null,
};

function WasmHookHarness(props: {
  isActive: boolean;
  isConnected: boolean;
  captured: React.MutableRefObject<HookModule['UseWasmTerminalInputReturn'] | null>;
}) {
  // Pre-populated ref: the hook only reads xtermRef.current, so a plain
  // object works and guarantees the null-check (term retry loop) never fires.
  // The stub term provides write/writeln because the flag-OFF success path
  // writes the shell banner.
  const xtermRef = useRef<XTerm | null>({ write: vi.fn(), writeln: vi.fn() } as unknown as XTerm);
  const ret = useWasmHook({ xtermRef, isActive: props.isActive, isConnected: props.isConnected });
  props.captured.current = ret;
  return null;
}

// Loaded dynamically per flag state; kept in a hoisted-safe function because
// it must be re-imported after vi.resetModules().
let useWasmHook: HookModule['default'];

async function loadHook(enabled: boolean): Promise<HookModule['default']> {
  if (enabled) vi.stubEnv('VITE_SPROUT_NATIVE_FS', '1');
  else vi.stubEnv('VITE_SPROUT_NATIVE_FS', '');
  vi.resetModules();
  const mod = await import('../hooks/useWasmTerminalInput');
  return mod.default;
}

describe('useWasmTerminalInput — R-2f effect short-circuit', () => {
  beforeEach(() => {
    captured.current = null;
    initWasmShellMock.mockReset();
  });

  it('flag ON: effect short-circuits — no init, wasmProvidedByShell=true, no loading/error', async () => {
    useWasmHook = await loadHook(true);

    await act(async () => {
      render(<WasmHookHarness isActive={true} isConnected={false} captured={captured} />);
    });
    // Flush any microtasks the (short-circuited) effect might have queued.
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook, 'hook result must have been captured').toBeTruthy();
    expect(hook!.wasmProvidedByShell).toBe(true);
    expect(hook!.wasmLoading).toBe(false);
    expect(hook!.wasmError).toBeNull();
    expect(hook!.wasmActive).toBe(false);
    expect(initWasmShellMock).not.toHaveBeenCalled();
  });

  it('flag OFF: disconnected + active → initWasmShell IS called', async () => {
    useWasmHook = await loadHook(false);
    initWasmShellMock.mockResolvedValue(makeFakeWasmShell());

    await act(async () => {
      render(<WasmHookHarness isActive={true} isConnected={false} captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook, 'hook result must have been captured').toBeTruthy();
    expect(hook!.wasmProvidedByShell).toBe(false);
    expect(initWasmShellMock).toHaveBeenCalledTimes(1);
  });

  it('flag OFF + isActive=false: effect skips (no init, matches today behavior)', async () => {
    useWasmHook = await loadHook(false);
    initWasmShellMock.mockResolvedValue(makeFakeWasmShell());

    await act(async () => {
      render(<WasmHookHarness isActive={false} isConnected={false} captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook).toBeTruthy();
    expect(initWasmShellMock).not.toHaveBeenCalled();
  });
});

// ── Housekeeping ────────────────────────────────────────────────────────────
// (RTL auto-cleanup handles unmounting; no manual cleanup needed.)
