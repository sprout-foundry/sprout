/**
 * Sidecar hash/staleness tests (SP-140-3 §3b, SP-140 invariant 2).
 *
 * The sidecar is `{nodes, layoutHint, derivedFrom}`; `derivedFrom` is the
 * content hash of the `.mmd` the positions were derived from. These tests pin
 * the hash's determinism, the drift detection the canvas relies on, and the
 * defensive parsing of sidecars found on disk.
 */
import { describe, expect, it } from 'vitest';
import { parseFlowGraph } from './flowText';
import { layoutFlowGraph } from './layout';
import {
  buildLayoutSidecar,
  derivedFromHash,
  hashContent,
  isLayoutStale,
  parseLayoutSidecar,
  parseLayoutSidecarText,
  resolveFlowLayout,
  resolveSidecarLayoutHint,
  sidecarStaleness,
} from './sidecar';

const FLOW = ['flowchart LR', '  cart[Cart] --> pay{Payment}', '  pay --> done((Done))'].join('\n');
const graphOf = (text: string) => parseFlowGraph(text);

describe('hashContent / derivedFromHash', () => {
  it('is deterministic for identical content', () => {
    expect(hashContent(FLOW)).toBe(hashContent(FLOW));
    expect(derivedFromHash(FLOW)).toBe(hashContent(FLOW));
  });

  it('changes when the content changes', () => {
    expect(hashContent(FLOW)).not.toBe(hashContent(`${FLOW}\n`));
    expect(hashContent('a --> b')).not.toBe(hashContent('a --> c'));
    expect(hashContent('ab')).not.toBe(hashContent('ba'));
  });

  it('is lowercase 8-digit hex, stable for known inputs', () => {
    expect(hashContent('')).toBe('811c9dc5');
    expect(hashContent('a')).toBe('e40c292c');
    expect(hashContent(FLOW)).toMatch(/^[0-9a-f]{8}$/);
  });

  it('handles empty and non-ASCII content without throwing', () => {
    expect(hashContent('')).toMatch(/^[0-9a-f]{8}$/);
    expect(hashContent('étape → fin ✓')).toMatch(/^[0-9a-f]{8}$/);
    expect(hashContent(undefined as unknown as string)).toBe(hashContent(''));
  });
});

describe('isLayoutStale', () => {
  it('is fresh when the recorded hash matches', () => {
    expect(isLayoutStale(FLOW, derivedFromHash(FLOW))).toBe(false);
    expect(isLayoutStale(FLOW, derivedFromHash(FLOW).toUpperCase())).toBe(false);
  });

  it('is stale when the flow content drifted', () => {
    expect(isLayoutStale(`${FLOW}\n  done --> cart`, derivedFromHash(FLOW))).toBe(true);
  });

  it('is stale when no hash was recorded', () => {
    expect(isLayoutStale(FLOW, '')).toBe(true);
    expect(isLayoutStale(FLOW, null)).toBe(true);
    expect(isLayoutStale(FLOW, undefined)).toBe(true);
    expect(isLayoutStale(FLOW, '   ')).toBe(true);
  });
});

describe('resolveSidecarLayoutHint', () => {
  it('records a supplied hint verbatim', () => {
    expect(resolveSidecarLayoutHint(graphOf(FLOW), 'left-right')).toBe('left-right');
    expect(resolveSidecarLayoutHint(graphOf(FLOW), 'RL')).toBe('RL');
  });

  it('falls back to the resolved mermaid keyword', () => {
    expect(resolveSidecarLayoutHint(graphOf(FLOW))).toBe('LR');
    expect(resolveSidecarLayoutHint(graphOf('flowchart TD\n  a --> b\n'))).toBe('TB');
    expect(resolveSidecarLayoutHint(graphOf('a --> b\n'))).toBe('TB');
  });

  it('records "" for a graph with no nodes, hint or not', () => {
    // No nodes means no orientation to record; a stray hint must not persist.
    expect(resolveSidecarLayoutHint(graphOf(''))).toBe('');
    expect(resolveSidecarLayoutHint(graphOf(''), 'top-down')).toBe('');
  });
});

describe('buildLayoutSidecar', () => {
  it('constructs the {nodes, layoutHint, derivedFrom} contract', () => {
    const graph = graphOf(FLOW);
    const sidecar = buildLayoutSidecar(graph, FLOW, { layoutHint: 'left-right' });
    expect(Object.keys(sidecar).sort()).toEqual(['derivedFrom', 'layoutHint', 'nodes']);
    expect(sidecar.layoutHint).toBe('left-right');
    expect(sidecar.derivedFrom).toBe(derivedFromHash(FLOW));
    expect(Object.keys(sidecar.nodes)).toEqual(['cart', 'pay', 'done']);
    expect(sidecar.nodes).toEqual(layoutFlowGraph(graph, { layoutHint: 'left-right' }));
  });

  it('is deterministic', () => {
    expect(buildLayoutSidecar(graphOf(FLOW), FLOW, { layoutHint: 'top-down' })).toEqual(
      buildLayoutSidecar(graphOf(FLOW), FLOW, { layoutHint: 'top-down' }),
    );
  });

  it('collapses supplied drag positions against the current flow', () => {
    const graph = graphOf(FLOW);
    const sidecar = buildLayoutSidecar(graph, FLOW, {
      positions: { cart: { x: 5, y: 6 }, gone: { x: 1, y: 1 }, pay: { x: 7, y: 8 }, done: { x: 9, y: 10 } },
    });
    expect(sidecar.nodes).toEqual({ cart: { x: 5, y: 6 }, pay: { x: 7, y: 8 }, done: { x: 9, y: 10 } });
  });

  it('produces an empty-node sidecar for an empty flow', () => {
    expect(buildLayoutSidecar(graphOf(''), '')).toEqual({ nodes: {}, layoutHint: '', derivedFrom: '811c9dc5' });
  });

  it('round-trips through JSON with the exact on-disk key order', () => {
    const sidecar = buildLayoutSidecar(graphOf('flowchart TB\n  a --> b\n'), 'flowchart TB\n  a --> b\n');
    expect(JSON.stringify(sidecar)).toBe(
      '{"nodes":{"a":{"x":90,"y":50},"b":{"x":90,"y":214}},"layoutHint":"TB","derivedFrom":"' +
        derivedFromHash('flowchart TB\n  a --> b\n') +
        '"}',
    );
  });
});

describe('parseLayoutSidecar', () => {
  it('reads a well-formed sidecar', () => {
    const value = { nodes: { a: { x: 1, y: 2 } }, layoutHint: 'LR', derivedFrom: 'abc' };
    expect(parseLayoutSidecar(value)).toEqual(value);
  });

  it('degrades malformed fields instead of throwing', () => {
    expect(parseLayoutSidecar(null)).toBeNull();
    expect(parseLayoutSidecar([])).toBeNull();
    expect(parseLayoutSidecar('nope')).toBeNull();
    expect(parseLayoutSidecar({})).toEqual({ nodes: {}, layoutHint: '', derivedFrom: '' });
    expect(parseLayoutSidecar({ nodes: { a: { x: 'x', y: 2 }, b: { x: 3, y: 4 } }, layoutHint: 7 })).toEqual({
      nodes: { b: { x: 3, y: 4 } },
      layoutHint: '',
      derivedFrom: '',
    });
  });

  it('parses sidecar text and tolerates unparseable JSON', () => {
    expect(parseLayoutSidecarText('{"nodes":{},"layoutHint":"TB","derivedFrom":"h"}')).toEqual({
      nodes: {},
      layoutHint: 'TB',
      derivedFrom: 'h',
    });
    expect(parseLayoutSidecarText('{not json')).toBeNull();
    expect(parseLayoutSidecarText('')).toBeNull();
  });
});

describe('sidecarStaleness', () => {
  it('reports fresh when hash and orientation agree', () => {
    const stale = sidecarStaleness(FLOW, buildLayoutSidecar(graphOf(FLOW), FLOW), graphOf(FLOW));
    expect(stale).toEqual({
      stale: false,
      currentHash: derivedFromHash(FLOW),
      recordedHash: derivedFromHash(FLOW),
      orphaned: false,
      hintChanged: false,
    });
  });

  it('reports stale when the .mmd drifted', () => {
    const recorded = { nodes: {}, layoutHint: 'LR', derivedFrom: derivedFromHash(FLOW) };
    const status = sidecarStaleness(`${FLOW}\n  done --> cart`, recorded, graphOf(FLOW));
    expect(status.stale).toBe(true);
    expect(status.recordedHash).toBe(derivedFromHash(FLOW));
  });

  it('reports stale when the orientation hint changed without a text edit', () => {
    const recorded = { nodes: {}, layoutHint: 'TB', derivedFrom: derivedFromHash(FLOW) };
    const status = sidecarStaleness(FLOW, recorded, graphOf(FLOW), 'left-right');
    expect(status.hintChanged).toBe(true);
    expect(status.stale).toBe(true);

    const same = sidecarStaleness(FLOW, { ...recorded, layoutHint: 'left-right' }, graphOf(FLOW), 'left-right');
    expect(same.hintChanged).toBe(false);
    expect(same.stale).toBe(false);
  });

  it('does not flag an empty recorded hint as a change', () => {
    const status = sidecarStaleness(
      FLOW,
      { nodes: {}, layoutHint: '', derivedFrom: derivedFromHash(FLOW) },
      graphOf(FLOW),
    );
    expect(status.hintChanged).toBe(false);
    expect(status.stale).toBe(false);
  });

  it('flags an orphaned sidecar (no .mmd to derive from)', () => {
    const status = sidecarStaleness('', buildLayoutSidecar(graphOf(FLOW), FLOW), graphOf(''));
    expect(status.orphaned).toBe(true);
    expect(status.stale).toBe(true);
  });

  it('is stale when no sidecar was loaded at all', () => {
    expect(sidecarStaleness(FLOW, null, graphOf(FLOW)).stale).toBe(true);
  });
});

describe('resolveFlowLayout', () => {
  it('reuses a fresh, complete sidecar without regenerating', () => {
    const graph = graphOf(FLOW);
    const sidecar = buildLayoutSidecar(graph, FLOW, { layoutHint: 'left-right' });
    const resolved = resolveFlowLayout(FLOW, graph, sidecar, 'left-right');
    expect(resolved.regenerated).toBe(false);
    expect(resolved.sidecar).toBe(sidecar);
    expect(resolved.positions).toEqual(sidecar.nodes);
  });

  it('regenerates on hash drift and records the new hash', () => {
    const graph = graphOf(FLOW);
    const stale = buildLayoutSidecar(graph, FLOW, { layoutHint: 'left-right' });
    const edited = `${FLOW}\n  done --> cart`;
    const resolved = resolveFlowLayout(edited, graph, stale, 'left-right');
    expect(resolved.regenerated).toBe(true);
    expect(resolved.sidecar.derivedFrom).toBe(derivedFromHash(edited));
    expect(resolved.sidecar.layoutHint).toBe('left-right');
    expect(resolved.positions).toEqual(layoutFlowGraph(graph, { layoutHint: 'left-right' }));
  });

  it('regenerates when the sidecar omits a current node', () => {
    const graph = graphOf(FLOW);
    const sidecar = { nodes: { cart: { x: 1, y: 2 } }, layoutHint: 'LR', derivedFrom: derivedFromHash(FLOW) };
    const resolved = resolveFlowLayout(FLOW, graph, sidecar, '');
    expect(resolved.regenerated).toBe(true);
    expect(Object.keys(resolved.positions)).toEqual(['cart', 'pay', 'done']);
    expect(resolved.sidecar.derivedFrom).toBe(derivedFromHash(FLOW));
  });

  it('derives a layout when there is no sidecar', () => {
    const graph = graphOf(FLOW);
    const resolved = resolveFlowLayout(FLOW, graph, null, 'top-down');
    expect(resolved.regenerated).toBe(true);
    expect(resolved.positions).toEqual(layoutFlowGraph(graph, { layoutHint: 'top-down' }));
    expect(resolved.sidecar.layoutHint).toBe('top-down');
  });

  it('preserves dragged positions on a fresh sidecar', () => {
    const graph = graphOf(FLOW);
    const dragged = { cart: { x: 1, y: 1 }, pay: { x: 2, y: 2 }, done: { x: 3, y: 3 } };
    const sidecar = { nodes: dragged, layoutHint: 'LR', derivedFrom: derivedFromHash(FLOW) };
    expect(resolveFlowLayout(FLOW, graph, sidecar, '').positions).toEqual(dragged);
  });
});

describe('sidecar ↔ designApi interface', () => {
  it('serializes to exactly the shape writeLayout persists', async () => {
    // designApi.writeLayout writes `JSON.stringify(payload, null, 2)` of
    // `{nodes, layoutHint, derivedFrom}`. The constructed sidecar must be that
    // payload verbatim — the canvas hands it straight to writeLayout.
    const { writeLayout } = await import('../services/api/designApi');
    const sidecar = buildLayoutSidecar(graphOf(FLOW), FLOW, { layoutHint: 'left-right' });
    let body = '';
    let url = '';
    const fetchFn = (async (input: RequestInfo | URL, init?: RequestInit) => {
      url = String(input);
      body = String(init?.body ?? '');
      return new Response('{}', { status: 200 });
    }) as unknown as typeof fetch;

    await writeLayout(fetchFn, 'checkout', sidecar);

    expect(url).toBe('/api/file?path=design%2Fflows%2Fcheckout.layout.json');
    expect(JSON.parse(JSON.parse(body).content)).toEqual(sidecar);
    expect(JSON.stringify(JSON.parse(JSON.parse(body).content))).toBe(JSON.stringify(sidecar));
  });

  it('round-trips a built sidecar through parseLayoutSidecarText', () => {
    const sidecar = buildLayoutSidecar(graphOf(FLOW), FLOW, { layoutHint: 'top-down' });
    expect(parseLayoutSidecarText(JSON.stringify(sidecar, null, 2))).toEqual(sidecar);
    // A sidecar written by the canvas is fresh when read back.
    const reread = parseLayoutSidecarText(JSON.stringify(sidecar, null, 2));
    expect(sidecarStaleness(FLOW, reread, graphOf(FLOW), 'top-down').stale).toBe(false);
  });
});
