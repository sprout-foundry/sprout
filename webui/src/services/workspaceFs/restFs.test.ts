import { describe, expect, it } from 'vitest';
import { createRestFs } from './restFs';

// A fake of the files endpoint: GET, one directory per call, entries in the
// daemon shape with workspace-relative `relative` paths.
function filesEndpoint(tree: Record<string, Array<{ name: string; dir?: boolean; size?: number }>>) {
  const calls: string[] = [];
  const fetchFn = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://x');
    calls.push(`${init?.method ?? 'GET'} ${url.pathname}${url.search}`);
    const dir = url.searchParams.get('path') ?? '';
    const entries = tree[dir];
    if (!entries) return new Response(JSON.stringify({ error: 'failed_to_read_directory' }), { status: 500 });
    const files = entries.map((e) => ({
      name: e.name,
      relative: dir ? `${dir}/${e.name}` : e.name,
      is_dir: !!e.dir,
      size: e.size ?? 0,
    }));
    return new Response(JSON.stringify({ message: 'success', files }), { status: 200 });
  }) as typeof fetch;
  return { fetchFn, calls };
}

describe('restFs.list', () => {
  const tree = {
    '': [
      { name: 'repos', dir: true },
      { name: 'README.md', size: 3 },
    ],
    repos: [{ name: 'acme', dir: true }],
    'repos/acme': [{ name: 'web', dir: true }],
    'repos/acme/web': [{ name: 'index.ts', size: 9 }],
  };

  it('lists a directory level by level down to the depth asked for', async () => {
    const { fetchFn, calls } = filesEndpoint(tree);
    const listing = await createRestFs(fetchFn).list('repos', 2);

    expect(listing).toEqual({
      ok: true,
      files: [
        { path: 'repos/acme', size: 0, isDir: true },
        { path: 'repos/acme/web', size: 0, isDir: true },
      ],
    });
    expect(calls).toEqual(['GET /api/files?path=repos', 'GET /api/files?path=repos%2Facme']);
  });

  it('lists the workspace root without a path', async () => {
    const { fetchFn } = filesEndpoint(tree);
    const listing = await createRestFs(fetchFn).list('', 1);

    expect(listing.ok && listing.files.map((f) => f.path)).toEqual(['repos', 'README.md']);
  });

  it('fails when the directory itself cannot be listed', async () => {
    const { fetchFn } = filesEndpoint(tree);
    expect(await createRestFs(fetchFn).list('missing', 1)).toEqual({ ok: false, error: 'failed_to_read_directory' });
  });

  it('ignores entries outside the directory asked for', async () => {
    // A backend that answers a missing directory with the root's children.
    const rootForEverything = (async () =>
      new Response(
        JSON.stringify({ files: [{ name: 'README.md', relative: 'README.md', is_dir: false, size: 3 }] }),
      )) as typeof fetch;

    expect(await createRestFs(rootForEverything).list('repos', 1)).toEqual({ ok: true, files: [] });
  });
});
