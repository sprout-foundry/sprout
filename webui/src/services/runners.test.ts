import { afterEach, describe, expect, it, vi } from 'vitest';
import { isRunnerSelectable, listRunners, runnerModeLabel, type Runner } from './runners';

afterEach(() => {
  vi.unstubAllGlobals();
});

const MAC: Runner = {
  runner_id: 'r-1',
  name: 'MacBook',
  status: 'online',
  os: 'darwin',
  arch: 'arm64',
  capacity: 2,
  mode: 'native',
  sandbox: 'sandbox-exec',
  runner_version: '1.0.0',
  toolchains: ['go'],
};

describe('listRunners', () => {
  it('GETs /runners with the session and returns the runners', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify([MAC]), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    expect(await listRunners()).toEqual([MAC]);
    expect(fetchMock).toHaveBeenCalledWith('/runners', { method: 'GET', credentials: 'include' });
  });

  it('drops malformed entries', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify([MAC, { name: 'no id' }, null, 'x']), { status: 200 })),
    );
    expect(await listRunners()).toEqual([MAC]);
  });

  it.each([401, 404, 500])('returns [] on HTTP %i', async (status) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{"error":"x"}', { status })),
    );
    expect(await listRunners()).toEqual([]);
  });

  it('returns [] on a network failure or a non-array body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))),
    );
    expect(await listRunners()).toEqual([]);
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{"runners":[]}', { status: 200 })),
    );
    expect(await listRunners()).toEqual([]);
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('not json', { status: 200 })),
    );
    expect(await listRunners()).toEqual([]);
  });
});

describe('runner labels', () => {
  it('labels each isolation mode', () => {
    expect(runnerModeLabel({ mode: 'container', sandbox: '' })).toBe('container');
    expect(runnerModeLabel({ mode: 'native', sandbox: 'bwrap' })).toBe('native · bwrap');
    expect(runnerModeLabel({ mode: 'native', sandbox: '' })).toBe('native');
    expect(runnerModeLabel({ mode: 'bare-metal', sandbox: '' })).toBe('bare metal');
    expect(runnerModeLabel({ mode: '', sandbox: '' })).toBe('');
  });

  it('only offline runners are unselectable', () => {
    expect(isRunnerSelectable({ ...MAC, status: 'online' })).toBe(true);
    expect(isRunnerSelectable({ ...MAC, status: 'busy' })).toBe(true);
    expect(isRunnerSelectable({ ...MAC, status: 'offline' })).toBe(false);
  });
});
