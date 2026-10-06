/**
 * Tests for cloudTxnEscalate.runTxnCommand — the phase stream it reports to a
 * status UI. A run must always end with a terminal phase ("done" on success,
 * "error" on any failure) so an inline status/toast can clear; without it a
 * run that fails before "pulling" (e.g. a stall at "opening") left the status
 * pinned on screen forever.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { runTxnCommand } from './cloudTxnEscalate';
import { AUTO_HOST } from './escalationHost';

const WS = 'ws-1';
const TXN = 'txn-1';

/** JSON Response helper (fresh body per call — Response bodies are one-shot). */
function jsonResponse(body: unknown, init?: { status?: number }) {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Route table for the txn surface, keyed by `<method> <path>` where the path is
 * matched by suffix so txn ids and the backend segment don't matter. Return a
 * Response, a plain object (auto-JSON'd), or throw to fail a step.
 */
function routeFetch(routes: Record<string, () => Response>) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method ?? 'GET').toUpperCase();
    const path = url.split('?')[0];
    const key = Object.keys(routes).find((k) => {
      const [m, suffix] = k.split(' ');
      return m === method && path.endsWith(suffix);
    });
    if (!key) throw new Error(`unrouted ${method} ${path}`);
    return routes[key]();
  });
}

const OK_ROUTES: Record<string, () => Response> = {
  'GET /workspace/txn/resolve': () => jsonResponse({ workspace_id: WS, backend: 'fly' }),
  'POST /workspace/txn': () => jsonResponse({ workspace_id: WS, backend: 'fly' }),
  [`POST /workspace/fly/${WS}/txn`]: () => jsonResponse({ txn_id: TXN, status: 'push' }),
  [`POST /workspace/fly/${WS}/txn/${TXN}/push`]: () => jsonResponse({ files: [], bytes: 0 }),
  [`POST /workspace/fly/${WS}/txn/${TXN}/run`]: () =>
    jsonResponse({ stdout: 'ok\n', stderr: '', exit_code: 0, duration_ms: 1, timed_out: false, truncated: false }),
  [`POST /workspace/fly/${WS}/txn/${TXN}/pull`]: () => jsonResponse({ base: { git_sha: '', client: 'c' }, files: [] }),
  [`POST /workspace/fly/${WS}/txn/${TXN}/finish`]: () => jsonResponse({ status: 'done' }),
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('runTxnCommand phase stream', () => {
  it('emits opening → pushing → running → pulling → done on success', async () => {
    vi.stubGlobal('fetch', routeFetch(OK_ROUTES));
    const phases: string[] = [];
    const outcome = await runTxnCommand('https://github.com/a/b', 'echo ok', (p) => phases.push(p), AUTO_HOST);

    expect(outcome.result.stdout).toBe('ok\n');
    expect(phases).toEqual(['opening', 'pushing', 'running', 'pulling', 'done']);
  });

  it('emits a terminal "error" when the run fails before reaching pulling', async () => {
    // The workspace opens, but the run call fails — the phase must not stick on
    // "running" (the regression that pinned the status over the composer).
    vi.stubGlobal(
      'fetch',
      routeFetch({
        ...OK_ROUTES,
        [`POST /workspace/fly/${WS}/txn/${TXN}/run`]: () => jsonResponse({ error: 'boom' }, { status: 500 }),
      }),
    );
    const phases: string[] = [];
    await expect(
      runTxnCommand('https://github.com/a/b', 'echo ok', (p) => phases.push(p), AUTO_HOST),
    ).rejects.toThrow();
    expect(phases.at(-1)).toBe('error');
  });

  it('emits a terminal "error" when opening the workspace fails', async () => {
    // A stall/failure at "opening" is exactly the screenshot case: previously
    // the last emitted phase was "opening" and the status never cleared.
    vi.stubGlobal(
      'fetch',
      routeFetch({
        'GET /workspace/txn/resolve': () => jsonResponse({}, { status: 503 }),
        'POST /workspace/txn': () => jsonResponse({ error: 'workspaces are not available' }, { status: 503 }),
      }),
    );
    const phases: string[] = [];
    await expect(
      runTxnCommand('https://github.com/a/b', 'echo ok', (p) => phases.push(p), AUTO_HOST),
    ).rejects.toThrow();
    expect(phases).toEqual(['opening', 'error']);
  });
});
