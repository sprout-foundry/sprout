import { describe, expect, it } from 'vitest';
import { instantiateStarter, listStarters } from './startersApi';

const respond = (status: number, body: unknown) =>
  (async () => new Response(JSON.stringify(body), { status })) as unknown as typeof fetch;

describe('listStarters', () => {
  it('parses the backend starter list', async () => {
    const result = await listStarters(
      respond(200, {
        starters: [
          { id: 'fixture', version: '0.1.0', files: 4, has_manifest: true },
          { id: 'web-app', version: '1.2.0', files: 12, has_manifest: true },
        ],
      }),
    );
    expect(result).toEqual([
      { id: 'fixture', version: '0.1.0', files: 4, has_manifest: true },
      { id: 'web-app', version: '1.2.0', files: 12, has_manifest: true },
    ]);
  });

  it('returns an empty list when the body has no starters array', async () => {
    await expect(listStarters(respond(200, {}))).resolves.toEqual([]);
  });

  it('drops malformed entries', async () => {
    await expect(
      listStarters(respond(200, { starters: [null, { id: '' }, { id: 'ok', version: '1', files: 1 }] })),
    ).resolves.toEqual([{ id: 'ok', version: '1', files: 1, has_manifest: false }]);
  });

  it('throws on a server error', async () => {
    await expect(listStarters(respond(500, { error: 'starter_list_failed' }))).rejects.toThrow('starter_list_failed');
  });
});

describe('instantiateStarter', () => {
  it('POSTs the right body and parses the 200', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const fetchFn: typeof fetch = (url, init) => {
      calls.push([String(url), init]);
      return Promise.resolve(
        new Response(
          JSON.stringify({
            root: '/home/alice/myproj',
            starter: 'fixture',
            files: 4,
            manifest: {
              starter: { id: 'fixture', version: '0.1.0' },
              build: 'npm run build',
              dev_port: 3000,
              routes: ['/'],
            },
          }),
          { status: 200 },
        ),
      );
    };

    const result = await instantiateStarter(fetchFn, {
      starter: 'fixture',
      path: '/home/alice/myproj',
      name: 'myproj',
    });

    expect(calls[0][0]).toBe('/api/starters/instantiate');
    const init = calls[0][1]!;
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      starter: 'fixture',
      path: '/home/alice/myproj',
      name: 'myproj',
    });

    expect(result).toEqual({
      root: '/home/alice/myproj',
      starter: 'fixture',
      files: 4,
      manifest: {
        starter: { id: 'fixture', version: '0.1.0' },
        build: 'npm run build',
        dev_port: 3000,
        routes: ['/'],
      },
    });
  });

  it('omits optional manifest fields when absent', async () => {
    const fetchFn: typeof fetch = () =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            root: '/x',
            starter: 's',
            files: 0,
            manifest: { starter: { id: 's', version: '1' } },
          }),
          { status: 200 },
        ),
      );
    const result = await instantiateStarter(fetchFn, { starter: 's', path: '/x' });
    expect(result.manifest).toEqual({ starter: { id: 's', version: '1' } });
    expect(result.manifest.build).toBeUndefined();
  });

  it('throws on an unknown starter (404)', async () => {
    await expect(
      instantiateStarter(respond(404, { error: 'unknown starter "nope"', code: 'unknown_starter' }), {
        starter: 'nope',
        path: '/x',
      }),
    ).rejects.toThrow('unknown starter "nope"');
  });
});
