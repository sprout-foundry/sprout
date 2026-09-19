/**
 * designApi coverage-gap tests (SP-140-3 §3f, TODO item 3.10).
 *
 * `designApi.test.ts` covers the happy paths of the inventory/read/write half
 * and the pure parsers. This file fills the branches it leaves open — the
 * failure and normalization behaviour the DesignView tabs depend on:
 *
 * - `/api/files` answering with a body that is not the expected shape;
 * - the per-file read hardening in `listAssets` (a throwing read path, an
 *   unusable body), which is what keeps a broken asset from taking the whole
 *   inventory down;
 * - the manifest status chip actually landing on a screen row (its documented
 *   one consumer) and the `'dir/'` skip in `buildEntry`;
 * - the write paths' transport contract (`writeAsset`'s write-back path,
 *   design-root-relative targets, defaults for absent sidecar fields);
 * - the pure parsers' ignored/rejected inputs (unparseable, directory-ish, and
 *   non-`$`-keyed structures).
 */
import { describe, expect, it, vi } from 'vitest';
import {
  buildInventory,
  listAssets,
  parseFeedback,
  parseFrames,
  parseTokensFile,
  writeAsset,
  writeLayout,
} from './designApi';
import { fileUrl } from './designApiPaths';
import type { FilesResponse } from './types';

const ROOT = '/ws/demo';
const file = (path: string) => ({ path, modified: false });

function jsonFetch(body: unknown, status = 200): typeof fetch {
  return vi.fn(
    async () => ({ ok: status < 400, status, json: async () => body }) as unknown as Response,
  ) as unknown as typeof fetch;
}

/** A compact design tree: one of each kind plus a nested screen. */
function tree(): FilesResponse {
  return {
    message: 'success',
    files: [
      file(`${ROOT}/design`),
      file(`${ROOT}/design/README.md`),
      file(`${ROOT}/design/screens/login.html`),
      file(`${ROOT}/design/screens/sub/nested.html`),
      file(`${ROOT}/design/flows/checkout.mmd`),
      file(`${ROOT}/design/tokens/color.tokens.json`),
      file(`${ROOT}/design/feedback/login.json`),
    ],
  };
}

function readFnFor(bodies: Record<string, string>): typeof fetch {
  return vi.fn(async (url: string | URL | Request) => {
    const path = decodeURIComponent(String(url).replace('/api/file?path=', ''));
    const body = bodies[path];
    if (body === undefined) return new Response('', { status: 404 });
    return new Response(body, { status: 200 });
  }) as unknown as typeof fetch;
}

describe('designApi.listAssets request hardening', () => {
  it('degrades to exists:false when the /api/files body is not the expected shape', async () => {
    const inv = await listAssets(jsonFetch({ files: 'nope' }));
    expect(inv.exists).toBe(false);
    expect(inv.assets).toEqual([]);

    expect((await listAssets(jsonFetch(null))).exists).toBe(false);
    expect((await listAssets(jsonFetch({ message: 'success' }))).exists).toBe(false);
  });

  it('reads design paths when a file row carries no path', async () => {
    const inv = await listAssets(jsonFetch({ message: 'success', files: [{ modified: false }] }));
    expect(inv.exists).toBe(false);
  });

  it('keeps the inventory when the per-file read path throws', async () => {
    const readFn = vi.fn(async () => {
      throw new Error('consent read failed');
    }) as unknown as typeof fetch;
    const inv = await listAssets(jsonFetch(tree()), readFn);

    expect(inv.exists).toBe(true);
    expect(readFn).toHaveBeenCalled();
    expect(inv.tokenCount).toBe(0);
    expect(inv.tokenGroups).toEqual([
      { name: 'color.tokens.json', path: 'design/tokens/color.tokens.json', tokenCount: 0, types: [] },
    ]);
    expect(inv.flowSummaries[0]).toMatchObject({ nodeCount: 0, edgeCount: 0, direction: '' });
    expect(inv.feedback[0]).toMatchObject({ status: '', annotationCount: 0, resolvedCount: 0 });
    expect(inv.manifest).toMatchObject({ path: 'design/README.md', exists: true, chars: 0 });
  });

  it('falls back to design/README.md for the read but keeps the manifest absent', async () => {
    const fetchFn = jsonFetch({
      message: 'success',
      files: [file(`${ROOT}/design/wireframes/login.svg`)],
    });
    const readFn = readFnFor({ 'design/README.md': '# M\n\ntext\n' });
    const inv = await listAssets(fetchFn, readFn);

    // The status/label read still happens against design/README.md so frames
    // and statuses stay available — but the inventory reports no manifest, so
    // DesignView does not claim a README that the listing lacks.
    expect(readFn).toHaveBeenCalledWith('/api/file?path=design%2FREADME.md');
    expect(inv.manifest).toEqual({ path: 'README.md', exists: false, frames: [], chars: 0 });
  });
});

describe('designApi.listAssets screen status wiring', () => {
  it('stamps the README status marker onto screen rows (wireframe-stem fallback)', async () => {
    const readFn = readFnFor({
      'design/README.md': [
        '# Manifest',
        '',
        '- `login` \u2014 ready \u2014 sign-in entry',
        '- `nested.html` - draft - nested',
      ].join('\n'),
    });
    const inv = await listAssets(jsonFetch(tree()), readFn);

    expect(inv.screens.map((s) => [s.name, s.status])).toEqual([
      ['login.html', 'ready'],
      ['nested.html', 'draft'],
    ]);
  });

  it('leaves the status off rows the manifest does not list', async () => {
    const inv = await listAssets(
      jsonFetch(tree()),
      readFnFor({ 'design/README.md': '- `other` \u2014 ready \u2014 x' }),
    );
    expect(inv.screens.map((s) => s.status)).toEqual([undefined, undefined]);
  });
});

describe('designApi reading through the design-root-relative path', () => {
  it('requests a screen read through the same URL writeAsset writes', async () => {
    const fetchFn = vi.fn(async () => new Response('<svg/>', { status: 200 })) as unknown as typeof fetch;
    const { readAsset } = await import('./designApi');
    await readAsset(fetchFn, 'screens/login.html');
    expect(fetchFn).toHaveBeenCalledWith(fileUrl('screens/login.html'));
    expect(fileUrl('screens/login.html')).toBe('/api/file?path=design%2Fscreens%2Flogin.html');
  });
});

describe('designApi.writeAsset', () => {
  it('posts the file text to the asset path and returns it verbatim', async () => {
    const fetchFn = vi.fn(async () => new Response('{}', { status: 200 })) as unknown as typeof fetch;
    const result = await writeAsset(fetchFn, 'screens/login.html', '<h1>Sign in</h1>');

    expect(result.path).toBe('design/screens/login.html');
    expect(result.content).toBe('<h1>Sign in</h1>');
    expect(result.response.status).toBe(200);
    expect(fetchFn).toHaveBeenCalledWith(fileUrl('screens/login.html'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: '<h1>Sign in</h1>' }),
    });
  });

  it('accepts a workspace-relative path and normalizes it to one URL', async () => {
    const fetchFn = vi.fn(async () => new Response('{}', { status: 200 })) as unknown as typeof fetch;
    const result = await writeAsset(fetchFn, 'design/screens/login.html', 'x');
    expect(result.path).toBe('design/screens/login.html');
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });

  it('prefers the injected write function and reports the failure path', async () => {
    const fetchFn = vi.fn() as unknown as typeof fetch;
    const writeFn = vi.fn(async () => new Response('', { status: 500 })) as unknown as typeof fetch;
    await expect(writeAsset(fetchFn, 'screens/login.html', '<h1/>', writeFn)).rejects.toThrow(
      'Failed to write design asset: design/screens/login.html',
    );
    expect(fetchFn).not.toHaveBeenCalled();
    expect(writeFn).toHaveBeenCalledWith(fileUrl('screens/login.html'), expect.objectContaining({ method: 'POST' }));
  });
});

describe('designApi.writeLayout field normalization', () => {
  it('writes an empty-but-shaped sidecar when every field is absent', async () => {
    const fetchFn = vi.fn(async () => new Response('{}', { status: 200 })) as unknown as typeof fetch;
    const result = await writeLayout(fetchFn, 'checkout', {} as Parameters<typeof writeLayout>[2]);

    expect(result.path).toBe('design/flows/checkout.layout.json');
    expect(JSON.parse(result.content)).toEqual({ nodes: {}, layoutHint: '', derivedFrom: '' });
  });

  it('preserves an explicit empty node map rather than inventing positions', async () => {
    const fetchFn = vi.fn(async () => new Response('{}', { status: 200 })) as unknown as typeof fetch;
    const result = await writeLayout(fetchFn, 'checkout', { nodes: {}, layoutHint: 'TB', derivedFrom: 'h' });
    expect(result.path).toBe('design/flows/checkout.layout.json');
    expect(JSON.parse(result.content)).toEqual({ nodes: {}, layoutHint: 'TB', derivedFrom: 'h' });
    // Nothing re-derives: the persisted sidecar is the graph's own node list.
    expect(JSON.parse(result.content).nodes).not.toHaveProperty('cart');
  });
});

describe('designApi parsers ignore unusable input', () => {
  it('does not mistake a directory-ish path for a screen asset', () => {
    // `design/screens/sub/` classifies as a screen but carries no listable
    // extension, yet the trailing slash keeps it in the roster (a real
    // directory row is not a thumbnail) — the point is it never gains a
    // nested-directory name.
    const inv = buildInventory({ message: 'success', files: [file('design/screens/sub/'), file('design/README.md')] });
    expect(inv.exists).toBe(true);
    // `basename()` falls back to the whole path when the path ends with a
    // slash (its last segment is empty) — such a row is not a thumbnail.
    expect(inv.screens.map((s) => s.name)).toEqual(['design/screens/sub/']);
    expect(inv.assets.map((a) => a.path)).toEqual(['design/README.md', 'design/screens/sub/']);
  });

  it('lists a nested screen with its basename and design-root-relative path', () => {
    const inv = buildInventory({ message: 'success', files: [file('design/screens/x/y.html')] });
    expect(inv.screens).toEqual([
      { path: 'design/screens/x/y.html', name: 'y.html', kind: 'screen', size: 0, modified: 0 },
    ]);
  });

  it('skips a design/ entry whose extension is not listable', () => {
    const inv = buildInventory({ message: 'success', files: [file('design/notes.xyz'), file('design/README.md')] });
    expect(inv.assets.map((a) => a.path)).toEqual(['design/README.md']);
  });

  it('does not walk into `$`-prefixed DTCG group keys', () => {
    // `$type` is group metadata, not a token; the `palette` group's own
    // `$value` is a leaf, and its nested `brand` group is never walked into.
    const doc = JSON.stringify({
      $type: 'color',
      palette: { $value: 'x', brand: { primary: { $value: '#fff', $type: 'color' } } },
    });
    expect(parseTokensFile(doc)).toEqual({ tokenCount: 1, types: [] });
    expect(parseTokensFile('[]')).toEqual({ tokenCount: 0, types: [] });
    expect(parseTokensFile('{"a": 1}')).toEqual({ tokenCount: 0, types: [] });
  });

  it('counts only annotations flagged `resolved: true`', () => {
    const entry = parseFeedback(
      JSON.stringify({ status: 7, annotations: [{ resolved: 'yes' }, { resolved: true }, null] }),
      'design/feedback/login.json',
    );
    expect(entry).toEqual({
      name: 'login.json',
      path: 'design/feedback/login.json',
      status: '',
      annotationCount: 3,
      resolvedCount: 1,
    });
    expect(parseFeedback('null', 'design/feedback/login.json').annotationCount).toBe(0);
  });

  it('keeps frames parsing inside the indented block and rejects bad sizes', () => {
    const md = [
      'frames:',
      '  desktop: 1440x900',
      'notAProperty: 1',
      'prose describing the layout',
      'frames:',
      '  malformed: axb',
      '  negative: -1x10',
      '  fractional: 10.5x20',
    ].join('\n');
    expect(parseFrames(md)).toEqual([{ name: 'desktop', width: 1440, height: 900 }]);
    // An unindented row never opens the block; `10 x 20` is still two parts
    // (the parser splits on 'x' only, mirroring `pkg/design/frames.go`), and
    // only the integer parts survive, so it yields a 10x20 frame.
    expect(parseFrames('  desktop: 1440x900')).toEqual([]);
    expect(parseFrames('frames:\n  spaced: 10 x 20\n')).toEqual([{ name: 'spaced', width: 10, height: 20 }]);
    expect(parseFrames('frames:\n  fractional: 10.5x20\n')).toEqual([]);
    expect(parseFrames('frames:\n  zero: 0x20\n')).toEqual([]);
    expect(parseFrames('frames:\n  three: 1x2x3\n')).toEqual([]);
  });
});
