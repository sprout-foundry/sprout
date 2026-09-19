import { describe, it, expect, vi } from 'vitest';
import {
  listAssets,
  readAsset,
  writeLayout,
  writeFeedback,
  buildInventory,
  parseFeedback,
  parseFlowText,
  parseFrames,
  parseTokensFile,
  designRootPath,
  DESIGN_DIR,
} from './designApi';
import type { FilesResponse, DesignFeedbackFile } from './types';

const ROOT = '/ws/demo';

function file(path: string) {
  return { path, modified: false };
}

/** A representative design/ tree as /api/files would return it. */
function designTree(): FilesResponse {
  return {
    message: 'success',
    files: [
      file(`${ROOT}/src/app.ts`),
      file(`${ROOT}/design`),
      file(`${ROOT}/design/README.md`),
      file(`${ROOT}/design/wireframes/login.svg`),
      file(`${ROOT}/design/wireframes/home.svg`),
      file(`${ROOT}/design/screens/login.html`),
      file(`${ROOT}/design/flows/checkout.mmd`),
      file(`${ROOT}/design/flows/checkout.layout.json`),
      file(`${ROOT}/design/tokens/color.tokens.json`),
      file(`${ROOT}/design/tokens/spacing.tokens.json`),
      file(`${ROOT}/design/feedback/login.json`),
      file(`${ROOT}/design/icons/arrow.svg`),
      file(`${ROOT}/design/notes.txt`),
    ],
  };
}

const README = ['# Manifest', '', 'frames:', '  desktop: 1440x900', '  mobile: 390x844'].join('\n');

const COLOR_TOKENS = JSON.stringify({
  color: {
    primary: { $type: 'color', $value: '#0055aa' },
    surface: { $type: 'color', $value: '#ffffff' },
  },
});

const SPACING_TOKENS = JSON.stringify({
  space: { sm: { $type: 'dimension', $value: '4px' } },
});

const CHECKOUT_MMD = [
  'flowchart LR',
  '  cart[Cart] --> pay{Payment}',
  '  pay --> done[Done]',
  '  %% a comment',
  '  classDef x fill:#fff',
].join('\n');

const FEEDBACK_JSON = JSON.stringify({
  target: 'wireframes/login.svg',
  status: 'changes-requested',
  resolution: '',
  annotations: [
    { id: 'a1', at: { x: 0.42, y: 0.18 }, area: 'hierarchy', note: 'CTA emphasis', resolved: false, created: 'x' },
    { id: 'a2', at: { x: 0.1, y: 0.2 }, area: 'contrast', note: 'too light', resolved: true, created: 'y' },
  ],
});

interface MockResult {
  fetchFn: ReturnType<typeof vi.fn>;
  readFn: ReturnType<typeof vi.fn>;
  calls: string[];
}

/** fetchFn answers /api/files; readFn answers /api/file with per-path text. */
function makeMocks(bodies: Record<string, string>): MockResult {
  const calls: string[] = [];
  const fetchFn = vi.fn(async (url: string) => {
    calls.push(url);
    return {
      ok: true,
      status: 200,
      json: async () => designTree(),
      text: async () => '',
    } as unknown as Response;
  });
  const readFn = vi.fn(async (url: string) => {
    calls.push(url);
    const decoded = decodeURIComponent(url.replace('/api/file?path=', ''));
    const body = bodies[decoded];
    if (body === undefined) return { ok: false, status: 404, text: async () => '' } as unknown as Response;
    return { ok: true, status: 200, text: async () => body } as unknown as Response;
  });
  return { fetchFn, readFn, calls };
}

function fullBodies(): Record<string, string> {
  return {
    'design/README.md': README,
    'design/flows/checkout.mmd': CHECKOUT_MMD,
    'design/tokens/color.tokens.json': COLOR_TOKENS,
    'design/tokens/spacing.tokens.json': SPACING_TOKENS,
    'design/feedback/login.json': FEEDBACK_JSON,
  };
}

describe('designApi.listAssets', () => {
  it('inventories the design tree with per-kind rows', async () => {
    const { fetchFn, readFn } = makeMocks(fullBodies());
    const inv = await listAssets(fetchFn as unknown as typeof fetch, readFn as unknown as typeof fetch);

    expect(inv.exists).toBe(true);
    expect(inv.manifest).toMatchObject({ path: 'design/README.md', exists: true });
    expect(inv.wireframes.map((a) => a.name)).toEqual(['home.svg', 'login.svg']);
    expect(inv.flows.map((a) => a.name)).toEqual(['checkout.mmd']);
    expect(inv.layouts.map((a) => a.name)).toEqual(['checkout.layout.json']);
    expect(inv.tokenFiles.map((a) => a.name)).toEqual(['color.tokens.json', 'spacing.tokens.json']);
    expect(inv.feedback.map((f) => f.name)).toEqual(['login.json']);
    expect(inv.screens.map((a) => a.name)).toEqual(['login.html']);
    expect(inv.assets.every((a) => a.path.startsWith(`${DESIGN_DIR}/`))).toBe(true);
  });

  it('derives token group counts and flow node/edge counts', async () => {
    const { fetchFn, readFn } = makeMocks(fullBodies());
    const inv = await listAssets(fetchFn as unknown as typeof fetch, readFn as unknown as typeof fetch);

    expect(inv.tokenCount).toBe(3);
    expect(inv.tokenGroups).toEqual([
      { name: 'color.tokens.json', path: 'design/tokens/color.tokens.json', tokenCount: 2, types: ['color'] },
      {
        name: 'spacing.tokens.json',
        path: 'design/tokens/spacing.tokens.json',
        tokenCount: 1,
        types: ['dimension'],
      },
    ]);
    expect(inv.flowSummaries).toEqual([
      {
        name: 'checkout',
        path: 'design/flows/checkout.mmd',
        nodeCount: 3,
        edgeCount: 2,
        direction: 'LR',
      },
    ]);
  });

  it('parses manifest frames and feedback status/resolution counts', async () => {
    const { fetchFn, readFn } = makeMocks(fullBodies());
    const inv = await listAssets(fetchFn as unknown as typeof fetch, readFn as unknown as typeof fetch);

    expect(inv.manifest.frames).toEqual([
      { name: 'desktop', width: 1440, height: 900 },
      { name: 'mobile', width: 390, height: 844 },
    ]);
    expect(inv.feedback[0]).toMatchObject({
      path: 'design/feedback/login.json',
      status: 'changes-requested',
      annotationCount: 2,
      resolvedCount: 1,
    });
  });

  it('returns exists:false for a workspace without design/', async () => {
    const fetchFn = vi.fn(
      async () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({ message: 'success', files: [file(`${ROOT}/src/app.ts`)] }),
        }) as unknown as Response,
    );

    const inv = await listAssets(fetchFn as unknown as typeof fetch);
    expect(inv.exists).toBe(false);
    expect(inv.assets).toEqual([]);
    expect(inv.tokenCount).toBe(0);
  });

  it('returns exists:false for an empty design/ (only the directory itself)', async () => {
    const fetchFn = vi.fn(
      async () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({ message: 'success', files: [file(`${ROOT}/design`)] }),
        }) as unknown as Response,
    );

    const inv = await listAssets(fetchFn as unknown as typeof fetch);
    expect(inv.exists).toBe(false);
  });

  it('degrades to exists:false on a failed /api/files request', async () => {
    const fetchFn = vi.fn(async () => ({ ok: false, status: 500 }) as unknown as Response);
    const inv = await listAssets(fetchFn as unknown as typeof fetch);
    expect(inv.exists).toBe(false);
  });

  it('degrades to exists:false when /api/files throws', async () => {
    const fetchFn = vi.fn(async () => {
      throw new Error('network down');
    });
    const inv = await listAssets(fetchFn as unknown as typeof fetch);
    expect(inv.exists).toBe(false);
  });

  it('tolerates unreadable per-file content (empty counts, no throw)', async () => {
    const { fetchFn, readFn } = makeMocks({});
    const inv = await listAssets(fetchFn as unknown as typeof fetch, readFn as unknown as typeof fetch);

    expect(inv.exists).toBe(true);
    expect(inv.tokenCount).toBe(0);
    expect(inv.tokenGroups.map((g) => g.tokenCount)).toEqual([0, 0]);
    expect(inv.flowSummaries[0]).toMatchObject({ nodeCount: 0, edgeCount: 0, direction: '' });
  });

  it('requests the design files through the consent-aware read path override', async () => {
    const { fetchFn, readFn, calls } = makeMocks(fullBodies());
    await listAssets(fetchFn as unknown as typeof fetch, readFn as unknown as typeof fetch);

    expect(calls.some((c) => c.startsWith('/api/file?path='))).toBe(true);
    expect(calls.some((c) => c.includes(encodeURIComponent('design/flows/checkout.mmd')))).toBe(true);
  });
});

describe('designApi.buildInventory', () => {
  it('classifies assets and ignores non-design files', () => {
    const inv = buildInventory(designTree());
    const paths = inv.assets.map((a) => a.path);
    expect(paths).not.toContain('src/app.ts');
    expect(paths).not.toContain('design/notes.txt');
    expect(paths).toContain('design/README.md');
    expect(paths).toContain('design/icons/arrow.svg');
  });

  it('returns an empty inventory for null input', () => {
    expect(buildInventory(null).exists).toBe(false);
    expect(buildInventory(undefined).tokenGroups).toEqual([]);
  });

  it('accepts design-relative paths from a flattened listing', () => {
    const inv = buildInventory({
      message: 'success',
      files: [file('design/wireframes/login.svg'), file('design/flows/a.mmd')],
    });
    expect(inv.exists).toBe(true);
    expect(inv.wireframes.map((a) => a.path)).toEqual(['design/wireframes/login.svg']);
  });
});

describe('designApi.readAsset', () => {
  it('reads an asset via the default fetch path (text body)', async () => {
    const fetchFn = vi.fn(async () => ({ ok: true, status: 200, text: async () => '<svg/>' }) as unknown as Response);
    const text = await readAsset(fetchFn as unknown as typeof fetch, 'wireframes/login.svg');

    expect(text).toBe('<svg/>');
    expect(fetchFn).toHaveBeenCalledWith(`/api/file?path=${encodeURIComponent('design/wireframes/login.svg')}`);
  });

  it('reads via an injected read function', async () => {
    const fetchFn = vi.fn();
    const readFn = vi.fn(
      async () => ({ ok: true, status: 200, text: async () => 'flowchart LR' }) as unknown as Response,
    );
    const text = await readAsset(fetchFn as unknown as typeof fetch, 'flows/a.mmd', readFn as unknown as typeof fetch);

    expect(text).toBe('flowchart LR');
    expect(fetchFn).not.toHaveBeenCalled();
    expect(readFn).toHaveBeenCalledWith(`/api/file?path=${encodeURIComponent('design/flows/a.mmd')}`);
  });

  it('returns an empty string for a missing asset (404)', async () => {
    const fetchFn = vi.fn(async () => ({ ok: false, status: 404 }) as unknown as Response);
    await expect(readAsset(fetchFn as unknown as typeof fetch, 'wireframes/nope.svg')).resolves.toBe('');
  });

  it('throws on other HTTP errors', async () => {
    const fetchFn = vi.fn(async () => ({ ok: false, status: 500 }) as unknown as Response);
    await expect(readAsset(fetchFn as unknown as typeof fetch, 'wireframes/login.svg')).rejects.toThrow(
      'Failed to read design asset: wireframes/login.svg',
    );
  });
});

describe('designApi.writeLayout', () => {
  it('writes the sidecar with nodes/layoutHint/derivedFrom', async () => {
    const fetchFn = vi.fn(
      async () => ({ ok: true, status: 200, json: async () => ({ success: true }) }) as unknown as Response,
    );

    const result = await writeLayout(fetchFn as unknown as typeof fetch, 'checkout', {
      nodes: { cart: { x: 10, y: 20 } },
      layoutHint: 'LR',
      derivedFrom: 'abc123',
    });

    expect(result.path).toBe('design/flows/checkout.layout.json');
    expect(fetchFn).toHaveBeenCalledTimes(1);
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe(`/api/file?path=${encodeURIComponent('design/flows/checkout.layout.json')}`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toEqual({
      content: JSON.stringify({ nodes: { cart: { x: 10, y: 20 } }, layoutHint: 'LR', derivedFrom: 'abc123' }, null, 2),
    });
  });

  it('strips a .mmd or .layout.json suffix from the name', async () => {
    const fetchFn = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }) as unknown as Response);
    const a = await writeLayout(fetchFn as unknown as typeof fetch, 'checkout.mmd', {
      nodes: {},
      layoutHint: '',
      derivedFrom: 'h',
    });
    const b = await writeLayout(fetchFn as unknown as typeof fetch, 'checkout.layout.json', {
      nodes: {},
      layoutHint: '',
      derivedFrom: 'h',
    });
    expect(a.path).toBe('design/flows/checkout.layout.json');
    expect(b.path).toBe('design/flows/checkout.layout.json');
  });

  it('normalizes missing sidecar fields to safe defaults', async () => {
    const fetchFn = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }) as unknown as Response);
    const result = await writeLayout(
      fetchFn as unknown as typeof fetch,
      'checkout',
      {} as unknown as Parameters<typeof writeLayout>[2],
    );
    expect(JSON.parse(result.content)).toEqual({ nodes: {}, layoutHint: '', derivedFrom: '' });
  });

  it('uses the injected write function and throws on failure', async () => {
    const fetchFn = vi.fn();
    const writeFn = vi.fn(async () => ({ ok: false, status: 500 }) as unknown as Response);
    await expect(
      writeLayout(
        fetchFn as unknown as typeof fetch,
        'checkout',
        { nodes: {}, layoutHint: '', derivedFrom: 'h' },
        writeFn as unknown as typeof fetch,
      ),
    ).rejects.toThrow('Failed to write layout sidecar: design/flows/checkout.layout.json');
    expect(fetchFn).not.toHaveBeenCalled();
  });
});

describe('designApi.writeFeedback', () => {
  const feedback: DesignFeedbackFile = {
    target: 'wireframes/login.svg',
    status: 'changes-requested',
    resolution: '',
    annotations: [
      {
        id: 'a1',
        at: { x: 0.42, y: 0.18 },
        area: 'hierarchy',
        note: 'CTA emphasis',
        resolved: false,
        created: '2026-09-15T10:36:47Z',
      },
    ],
  };

  it('writes design/feedback/<target>.json with resolution + resolved fields', async () => {
    const fetchFn = vi.fn(
      async () => ({ ok: true, status: 200, json: async () => ({ success: true }) }) as unknown as Response,
    );

    const result = await writeFeedback(fetchFn as unknown as typeof fetch, 'login', feedback);

    expect(result.path).toBe('design/feedback/login.json');
    const [url, init] = fetchFn.mock.calls[0];
    expect(url).toBe(`/api/file?path=${encodeURIComponent('design/feedback/login.json')}`);
    expect(init.method).toBe('POST');
    const payload = JSON.parse(JSON.parse(String(init.body)).content);
    expect(payload).toEqual(feedback);
    expect(payload).toHaveProperty('resolution', '');
    expect(payload.annotations[0]).toHaveProperty('resolved', false);
  });

  it('defaults target to the argument and normalizes partial annotations', async () => {
    const fetchFn = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }) as unknown as Response);

    const result = await writeFeedback(fetchFn as unknown as typeof fetch, 'login.svg', {
      annotations: [{ note: 'hi' }],
    } as unknown as DesignFeedbackFile);

    const payload = JSON.parse(result.content);
    expect(payload.target).toBe('login.svg');
    expect(payload.status).toBe('');
    expect(payload.resolution).toBe('');
    expect(payload.annotations[0]).toEqual({
      id: '',
      at: { x: 0, y: 0 },
      area: '',
      note: 'hi',
      resolved: false,
      created: '',
    });
  });

  it('strips a trailing .json from the target', async () => {
    const fetchFn = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }) as unknown as Response);
    const result = await writeFeedback(fetchFn as unknown as typeof fetch, 'login.json', feedback);
    expect(result.path).toBe('design/feedback/login.json');
  });

  it('throws with the feedback path on HTTP error', async () => {
    const fetchFn = vi.fn(async () => ({ ok: false, status: 403 }) as unknown as Response);
    await expect(writeFeedback(fetchFn as unknown as typeof fetch, 'login', feedback)).rejects.toThrow(
      'Failed to write feedback: design/feedback/login.json',
    );
  });
});

describe('designApi parsers', () => {
  it('parseTokensFile counts DTCG leaves and collects types', () => {
    expect(parseTokensFile(COLOR_TOKENS)).toEqual({ tokenCount: 2, types: ['color'] });
    expect(parseTokensFile('not json')).toEqual({ tokenCount: 0, types: [] });
    expect(parseTokensFile('')).toEqual({ tokenCount: 0, types: [] });
  });

  it('parseFlowText extracts direction, nodes and edges (mermaid subset)', () => {
    expect(parseFlowText(CHECKOUT_MMD)).toEqual({ nodeCount: 3, edgeCount: 2, direction: 'LR' });
    expect(parseFlowText('')).toEqual({ nodeCount: 0, edgeCount: 0, direction: '' });
  });

  it('parseFrames reads the manifest frames block only', () => {
    expect(parseFrames(README)).toEqual([
      { name: 'desktop', width: 1440, height: 900 },
      { name: 'mobile', width: 390, height: 844 },
    ]);
    expect(parseFrames('# no frames here')).toEqual([]);
  });

  it('parseFeedback tolerates malformed JSON', () => {
    expect(parseFeedback('{', 'design/feedback/bad.json')).toMatchObject({
      path: 'design/feedback/bad.json',
      status: '',
      annotationCount: 0,
      resolvedCount: 0,
    });
  });

  it('designRootPath keeps design/ relative paths stable', () => {
    expect(designRootPath('wireframes/login.svg')).toBe('design/wireframes/login.svg');
    expect(designRootPath('design/flows/a.mmd')).toBe('design/flows/a.mmd');
    expect(designRootPath('/design/flows/a.mmd')).toBe('design/flows/a.mmd');
  });
});
