/**
 * hostAgentBackend — the host's agent backend, wired for a composed workspace.
 *
 * The item under test: a host's transport states where the agent runs. A wasm
 * backend installs the cloud adapter from the host (with the host project's
 * repo, not the `?repo=` parameter) and routes the in-browser agent's events
 * into the shared event bus. A daemon backend — and a host that names no
 * backend — needs no wiring here.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getAdapter, installAdapter } from '../services/apiAdapter';
import type { APIAdapter } from '../services/apiAdapter';
import { headlessHost } from '../host/HostProvider';
import type { SproutHost } from '../host';
import { WebSocketService } from '../services/websocket';
import { __resetStartupRepoImportForTests, importStartupRepo } from '../services/startupRepoImport';
import { resetHostAgentBackend, wireHostAgentBackend } from './hostAgentBackend';

const setAgentEventDispatcher = vi.fn();
vi.mock('../services/cloudWasmHandlers', () => ({
  setAgentEventDispatcher: (fn: unknown) => setAgentEventDispatcher(fn),
}));

/** A minimal adapter used to reset the singleton between tests. */
const NOOP_ADAPTER: APIAdapter = {
  name: 'test-noop',
  requiresBackendHealthCheck: false,
  fileOpsViaAPI: false,
  showOnboarding: false,
  supportsSSH: false,
  supportsGit: false,
  supportsChat: false,
  supportsWorkspaceSwitching: false,
  supportsExport: false,
  supportsInstances: false,
  supportsLocalTerminal: false,
  supportsSettings: false,
  fetch: async () => new Response('{}'),
} as unknown as APIAdapter;

function wasmHost(modelEndpoint = 'https://models.host.test/v1/chat'): SproutHost {
  return {
    ...headlessHost(),
    transport: {
      apiBaseURL: 'https://host.test/api',
      wsURL: 'wss://host.test/ws',
      authMode: 'none',
      agent: { kind: 'wasm', modelEndpoint },
    },
  };
}

function daemonHost(): SproutHost {
  return {
    ...headlessHost(),
    transport: {
      apiBaseURL: 'https://daemon.test',
      wsURL: 'wss://daemon.test/ws',
      authMode: 'none',
      agent: { kind: 'daemon', apiBaseURL: 'https://daemon.test', wsURL: 'wss://daemon.test/ws' },
    },
  };
}

beforeEach(async () => {
  installAdapter(NOOP_ADAPTER);
  setAgentEventDispatcher.mockClear();
  // Wiring a wasm host with a repo imports it; keep that off the network.
  const { CloudAdapter } = await import('../services/cloudAdapter');
  vi.spyOn(CloudAdapter.prototype, 'restoreRepo').mockResolvedValue({ success: true });
});

afterEach(() => {
  vi.restoreAllMocks();
  resetHostAgentBackend();
  __resetStartupRepoImportForTests();
});

describe('wireHostAgentBackend', () => {
  it('installs the cloud adapter for a wasm backend, carrying the host repo', async () => {
    const { CloudAdapter } = await import('../services/cloudAdapter');
    wireHostAgentBackend(wasmHost(), 'https://github.com/acme/widgets');
    // The install is async (dynamic import); let the microtask queue drain.
    await vi.waitFor(() => {
      expect(getAdapter()).toBeInstanceOf(CloudAdapter);
    });
    const adapter = getAdapter() as InstanceType<typeof CloudAdapter>;
    expect(adapter.getStartupRepo()).toBe('https://github.com/acme/widgets');
  });

  it('installs the cloud adapter with the host transport URLs', async () => {
    const { CloudAdapter } = await import('../services/cloudAdapter');
    wireHostAgentBackend(wasmHost(), undefined);
    await vi.waitFor(() => expect(getAdapter()).toBeInstanceOf(CloudAdapter));
    const adapter = getAdapter() as InstanceType<typeof CloudAdapter>;
    expect(adapter.getWebSocketURL()).toBe('wss://host.test/ws');
  });

  it('keeps a wasm host with empty transport URLs on its own origin (no daemon bootstrap URLs)', async () => {
    // '' is the same-origin sentinel: an embedding host that names no URL must
    // not inherit the daemon bootstrap's absolute URLs.
    const { CloudAdapter } = await import('../services/cloudAdapter');
    const host: SproutHost = {
      ...headlessHost(),
      transport: { apiBaseURL: '', wsURL: '', authMode: 'none', agent: { kind: 'wasm', modelEndpoint: '' } },
    };
    wireHostAgentBackend(host, undefined);
    await vi.waitFor(() => expect(getAdapter()).toBeInstanceOf(CloudAdapter));
    const adapter = getAdapter() as InstanceType<typeof CloudAdapter>;
    expect(adapter.getWebSocketURL()).toBe('');
  });

  it('does not install an adapter for a daemon backend (the transport already drives the calls)', () => {
    wireHostAgentBackend(daemonHost(), undefined);
    expect(getAdapter()).toBe(NOOP_ADAPTER);
  });

  it('does nothing for a host that names no backend', () => {
    wireHostAgentBackend({ ...headlessHost() }, undefined);
    expect(getAdapter()).toBe(NOOP_ADAPTER);
    expect(setAgentEventDispatcher).not.toHaveBeenCalled();
  });

  it('routes the in-browser agent events into the shared bus for a wasm backend', async () => {
    const deliverSpy = vi.spyOn(WebSocketService.getInstance(), 'deliverLocal').mockImplementation(() => undefined);

    wireHostAgentBackend(wasmHost(), undefined);
    await vi.waitFor(() => expect(setAgentEventDispatcher).toHaveBeenCalled());
    const installed = setAgentEventDispatcher.mock.calls.at(-1)?.[0] as (event: unknown) => void;
    expect(installed).toBeTypeOf('function');

    installed({ type: 'query_started', data: {} });
    expect(deliverSpy).toHaveBeenCalledWith({ type: 'query_started', data: {} });

    deliverSpy.mockRestore();
  });

  it('leaves the dispatcher installed (it is a process-lifetime singleton)', async () => {
    // The dispatcher is never cleared: the hosted app root installs the same
    // one, and clearing it on unmount would kill in-browser agent events for
    // whatever mounts next.
    wireHostAgentBackend(wasmHost(), undefined);
    await vi.waitFor(() => expect(setAgentEventDispatcher).toHaveBeenCalled());
    expect(setAgentEventDispatcher).not.toHaveBeenCalledWith(null);
  });

  it('reports a wasm backend connected only once its adapter is installed', async () => {
    const events = WebSocketService.getInstance();
    const statuses: unknown[] = [];
    const listener = (event: { type: string; data?: unknown }) => {
      if (event.type === 'connection_status') statuses.push(event.data);
    };
    events.onEvent(listener);
    const { CloudAdapter } = await import('../services/cloudAdapter');

    const wired = wireHostAgentBackend(wasmHost(), undefined);
    // Still installing: not connected, and the adapter is not in place yet.
    expect(events.isConnected()).toBe(false);
    expect(getAdapter()).toBe(NOOP_ADAPTER);

    await wired;
    expect(getAdapter()).toBeInstanceOf(CloudAdapter);
    expect(events.isConnected()).toBe(true);
    expect(statuses).toEqual([{ connected: true }]);
    events.removeEvent(listener);
  });

  it('leaves a daemon backend on the socket state (never reported connected without one)', async () => {
    const events = WebSocketService.getInstance();
    events.setLocalAgentState('ready');

    await wireHostAgentBackend(daemonHost(), undefined);

    expect(events.isConnected()).toBe(false);
  });

  it("imports the host project's repo once the adapter is installed, once per repo", async () => {
    const { CloudAdapter } = await import('../services/cloudAdapter');
    const restore = vi.spyOn(CloudAdapter.prototype, 'restoreRepo').mockResolvedValue({ success: true });
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);

    await wireHostAgentBackend(wasmHost(), 'https://github.com/acme/widgets');
    // A re-mount (or a reconnect re-running the wiring) does not import again.
    await wireHostAgentBackend(wasmHost(), 'https://github.com/acme/widgets');

    expect(restore).toHaveBeenCalledTimes(1);
    expect(restore).toHaveBeenCalledWith('https://github.com/acme/widgets');
  });

  it("keeps the bootstrap's startup repo from importing over the host project's", async () => {
    const { CloudAdapter } = await import('../services/cloudAdapter');
    vi.spyOn(CloudAdapter.prototype, 'restoreRepo').mockResolvedValue({ success: true });
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const bootstrapAdapter = { restoreRepo: vi.fn() };

    const wired = wireHostAgentBackend(wasmHost(), 'https://github.com/acme/widgets');
    // The bootstrap install lands while the composition's is still in flight.
    expect(importStartupRepo(bootstrapAdapter, 'https://github.com/other/deep-link', 'bootstrap')).toBeNull();
    await wired;

    expect(bootstrapAdapter.restoreRepo).not.toHaveBeenCalled();
  });

  it('releases the state on reset, so the next mount starts clean', async () => {
    const events = WebSocketService.getInstance();
    await wireHostAgentBackend(wasmHost(), 'https://github.com/acme/widgets');
    expect(events.isConnected()).toBe(true);

    resetHostAgentBackend();

    expect(events.isConnected()).toBe(false);
    const bootstrapAdapter = { restoreRepo: vi.fn().mockResolvedValue({ success: true }) };
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    await importStartupRepo(bootstrapAdapter, 'https://github.com/other/deep-link', 'bootstrap');
    expect(bootstrapAdapter.restoreRepo).toHaveBeenCalledTimes(1);
  });
});
