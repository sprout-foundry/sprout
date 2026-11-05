/**
 * Platform-surface clients (runners, cloud tasks, cloud txn, full workspaces)
 * must reach the backend through clientFetch, so an embedding host whose
 * transport points at another origin receives the request instead of the page
 * origin. The real clientFetch runs here; only the active host and the
 * network are stubbed.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const activeHostRef = vi.hoisted(() => ({ value: null as unknown }));
vi.mock('../host/accessor', () => ({
  getActiveHost: () => activeHostRef.value,
}));
vi.mock('./apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
}));

import { WEBUI_CLIENT_ID_HEADER } from './clientSession';
import { getCloudTask, submitCloudTask } from './cloudTasks';
import { txnStatus } from './cloudTxn';
import { __resetFullWorkspaceForTests, probeFullWorkspaces, startFullWorkspace } from './fullWorkspace';
import { listRunners } from './runners';

const HOST_API = 'https://api.host.test';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('platform clients route through the host transport', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    // A resolved client id keeps clientFetch from issuing its recovery probe.
    window.sessionStorage.setItem('sprout.webuiClientId', 'client-test');
    activeHostRef.value = { transport: { apiBaseURL: HOST_API, wsURL: '', authMode: 'none' } };
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    activeHostRef.value = null;
    vi.unstubAllGlobals();
    __resetFullWorkspaceForTests();
  });

  function lastCall(): [string, RequestInit] {
    return fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit];
  }

  it('listRunners GETs the host backend with the session and client id', async () => {
    fetchMock.mockResolvedValue(jsonResponse([{ runner_id: 'r-1', name: 'Mac', status: 'online' }]));
    const runners = await listRunners();
    expect(runners.map((r) => r.runner_id)).toEqual(['r-1']);
    const [url, init] = lastCall();
    expect(url).toBe(`${HOST_API}/runners`);
    expect(init.method).toBe('GET');
    expect(init.credentials).toBe('include');
    expect(new Headers(init.headers).get(WEBUI_CLIENT_ID_HEADER)).toBe('client-test');
  });

  it('submitCloudTask POSTs to the host backend and keeps its headers', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ task_id: 't-1', status: 'pending' }, 201));
    const { task } = await submitCloudTask({ repo_url: 'https://github.com/o/r', prompt: 'p' });
    expect(task.task_id).toBe('t-1');
    const [url, init] = lastCall();
    expect(url).toBe(`${HOST_API}/api/tasks`);
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json');
  });

  it('getCloudTask GETs the host backend', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ task_id: 'a/b', status: 'running' }));
    await getCloudTask('a/b');
    expect(lastCall()[0]).toBe(`${HOST_API}/api/tasks/a%2Fb`);
  });

  it('txnStatus GETs the host backend', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ txn_id: 'x', status: 'run' }));
    await txnStatus('ws', 'x');
    expect(lastCall()[0]).toBe(`${HOST_API}/workspace/fly/ws/txn/x`);
  });

  it('full-workspace probe and launch hit the host backend, not the page origin', async () => {
    // A fresh body per call: the probe reads its response, then the launch
    // POST gets its own — a single shared Response body is one-shot.
    fetchMock.mockImplementation(() => Promise.resolve(jsonResponse({ status: 'starting' })));
    await probeFullWorkspaces();
    // The probe asks the typed availability endpoint (200 { available }); only
    // a definitive available:false hides the option — a non-boolean answer
    // like this keeps failing open, so the fetch is the assertion target.
    expect(fetchMock.mock.calls[0][0]).toBe(`${HOST_API}/workspace/availability`);
    // Launch POSTs the host-agnostic cloud create (host=cloud), not the
    // legacy /workspace/fly start route.
    await startFullWorkspace('https://github.com/o/r');
    const [url, init] = lastCall();
    expect(url).toBe(`${HOST_API}/workspace/txn`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toEqual({
      repo_url: 'https://github.com/o/r',
      host: 'cloud',
    });
  });

  it('without a host the same calls stay same-origin', async () => {
    activeHostRef.value = null;
    fetchMock.mockResolvedValue(jsonResponse([]));
    await listRunners();
    expect(lastCall()[0]).toBe('/runners');
  });
});
