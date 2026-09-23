/**
 * Sidecar coverage-gap tests (SP-140-3 §3b).
 *
 * `sidecar.test.ts` pins the happy paths: a fresh sidecar is reused, a drifted
 * hash regenerates, a malformed sidecar degrades. This file fills the branches
 * those tests leave open — the ones the canvas actually relies on when it
 * *changes its mind* about a layout:
 *
 * - the orientation override moving between two hints, which must re-lay-out
 *   the graph even though the `.mmd` bytes (and so `derivedFrom`) never moved;
 * - `derivedFrom` round-tripping through the upper-cased / padded spellings a
 *   hand-edited sidecar can carry;
 * - a sidecar whose `nodes` are well-formed but unusable (stale ids, NaN), and
 *   the guard that a sidecar covering an empty flow is still "complete";
 * - the defensive parse paths (array input, unusable `nodes`, non-string
 *   fields, `'null'` text) and the fact that every flattened point is a fresh
 *   object rather than a reference into the parsed value.
 */
import { describe, expect, it } from 'vitest';
import { parseFlowGraph } from './flowText';
import { layoutFlowGraph, resolveFlowDirection } from './layout';
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

describe('hashContent / derivedFromHash edge inputs', () => {
  it('treats null like empty input (the ?? guard)', () => {
    expect(hashContent(null as unknown as string)).toBe('811c9dc5');
    expect(derivedFromHash(null as unknown as string)).toBe(hashContent(''));
  });
});

describe('isLayoutStale recorded-hash forms', () => {
  it('ignores surrounding whitespace and case in the recorded hash', () => {
    const hash = derivedFromHash(FLOW);
    expect(isLayoutStale(FLOW, `  ${hash}  `)).toBe(false);
    expect(isLayoutStale(FLOW, hash.toUpperCase())).toBe(false);
    expect(isLayoutStale(FLOW, `  ${hash.toUpperCase()}  `)).toBe(false);
  });

  it('does not trim the flow content it hashes', () => {
    // The hash is over the bytes on disk: a trailing newline is a real edit.
    expect(isLayoutStale(FLOW, derivedFromHash(`${FLOW}\n`))).toBe(true);
  });
});

describe('resolveSidecarLayoutHint normalization', () => {
  it('trims a supplied hint before recording it', () => {
    expect(resolveSidecarLayoutHint(graphOf(FLOW), '  left-right  ')).toBe('left-right');
  });

  it('falls back to the declaration for a blank hint, not to the default', () => {
    // A recorded sidecar must keep agreeing with the orientation the agent
    // rendered, so an unusable hint falls back to the flow's own declaration.
    expect(resolveSidecarLayoutHint(graphOf(FLOW), '   ')).toBe('LR');
    expect(resolveSidecarLayoutHint(graphOf('flowchart BT\n  a --> b\n'), null)).toBe('BT');
  });

  it('records "" for a missing graph as well as an empty one', () => {
    expect(resolveSidecarLayoutHint(null as unknown as ReturnType<typeof graphOf>, 'top-down')).toBe('');
    expect(resolveSidecarLayoutHint(undefined as unknown as ReturnType<typeof graphOf>)).toBe('');
  });
});

describe('buildLayoutSidecar option handling', () => {
  it('records the supplied hint while the derived positions stay a pure function of the graph', () => {
    const graph = graphOf(FLOW);
    const sidecar = buildLayoutSidecar(graph, FLOW, { layoutHint: 'top-down' });
    expect(sidecar.layoutHint).toBe('top-down');
    // The derivation call receives no dagre options (the caller already chose
    // a hint), so the positions are a function of the graph alone — the
    // `resolveFlowLayout` regeneration path relies on exactly that. The hint is
    // still recorded, it just is not threaded into dagre here.
    expect(sidecar.nodes).toEqual(layoutFlowGraph(graph));
  });

  it('derives through the nested dagre options when the hint has not been resolved for us', () => {
    const graph = graphOf(FLOW);
    const throughOptions = buildLayoutSidecar(graph, FLOW, {
      layoutHint: 'top-down',
      options: { layoutHint: 'top-down' },
    });
    // A caller that only passes `options` gets the orientation it asked for...
    expect(throughOptions.nodes).not.toEqual(buildLayoutSidecar(graph, FLOW).nodes);
    // ...and a caller that resolved the hint itself gets the declared-orientation
    // positions instead, which is the arrangement the canvas draws.
    expect(buildLayoutSidecar(graph, FLOW, { layoutHint: 'top-down' }).nodes).toEqual(layoutFlowGraph(graph));

    // The nested options are honoured in general, not just for orientation.
    const tall = buildLayoutSidecar(graph, FLOW, { options: { defaultDimensions: { height: 400 } } });
    expect(tall.nodes.pay.y).toBeGreaterThan(buildLayoutSidecar(graph, FLOW).nodes.pay.y);
  });

  it('collapses an explicit `positions: {}` instead of re-deriving', () => {
    // `??` (not `||`): an explicit empty map is a real choice — the canvas has
    // no recorded drag positions yet and must not silently gain dagre ones.
    expect(buildLayoutSidecar(graphOf(FLOW), FLOW, { positions: {} }).nodes).toEqual({});
  });

  it('drops a NaN drag position rather than persisting a non-finite coordinate', () => {
    const sidecar = buildLayoutSidecar(graphOf(FLOW), FLOW, {
      positions: { cart: { x: Number.NaN, y: 0 }, pay: { x: 1, y: 2 }, done: { x: 3, y: 4 } },
    });
    expect(sidecar.nodes).toEqual({ pay: { x: 1, y: 2 }, done: { x: 3, y: 4 } });
  });
});

describe('parseLayoutSidecar defensive branches', () => {
  it('returns null for values that are not plain objects', () => {
    expect(parseLayoutSidecar(42)).toBeNull();
    expect(parseLayoutSidecar(false)).toBeNull();
    expect(parseLayoutSidecar(undefined)).toBeNull();
  });

  it('keeps an id whose point is unusable away from the other nodes', () => {
    const parsed = parseLayoutSidecar({
      nodes: { good: { x: 1, y: 2 }, bad: { x: null, y: 2 }, worse: 'nope', alsoBad: { x: 1 } },
      layoutHint: 'TB',
      derivedFrom: 'h',
    });
    expect(parsed).toEqual({ nodes: { good: { x: 1, y: 2 } }, layoutHint: 'TB', derivedFrom: 'h' });
  });

  it('degrades an unusable `nodes` container to {}', () => {
    expect(parseLayoutSidecar({ nodes: [1, 2] })).toEqual({ nodes: {}, layoutHint: '', derivedFrom: '' });
    expect(parseLayoutSidecar({ nodes: 'nope', layoutHint: 7, derivedFrom: null })).toEqual({
      nodes: {},
      layoutHint: '',
      derivedFrom: '',
    });
  });

  it('copies stored points instead of aliasing the parsed input', () => {
    const value = { nodes: { a: { x: 1, y: 2, extra: true } }, layoutHint: 'LR', derivedFrom: 'h' };
    const parsed = parseLayoutSidecar(value);
    expect(parsed).toEqual({ nodes: { a: { x: 1, y: 2 } }, layoutHint: 'LR', derivedFrom: 'h' });
    (value.nodes.a as { x: number }).x = 99;
    expect(parsed?.nodes.a).toEqual({ x: 1, y: 2 });
  });

  it('parses the JSON literal null to null, not to an empty sidecar', () => {
    expect(parseLayoutSidecarText('null')).toBeNull();
    expect(parseLayoutSidecarText('[]')).toBeNull();
    expect(parseLayoutSidecarText('"x"')).toBeNull();
  });
});

describe('sidecarStaleness orientation-change branches', () => {
  it('flags a recorded hint that the current hint no longer resolves to', () => {
    // TB→LR is the "user changed flow_layout" case: same `.mmd`, same hash.
    const recorded = { nodes: {}, layoutHint: 'TB', derivedFrom: derivedFromHash(FLOW) };
    const status = sidecarStaleness(FLOW, recorded, graphOf(FLOW), 'left-right');
    expect(status).toEqual({
      stale: true,
      currentHash: derivedFromHash(FLOW),
      recordedHash: derivedFromHash(FLOW),
      orphaned: false,
      hintChanged: true,
    });
  });

  it('does not flag a recorded mermaid keyword that still matches the declaration', () => {
    // A sidecar written by the canvas (or another tool) records the resolved
    // keyword; with no override it must still count as fresh.
    const recorded = { nodes: {}, layoutHint: 'LR', derivedFrom: derivedFromHash(FLOW) };
    const status = sidecarStaleness(FLOW, recorded, graphOf(FLOW), '');
    expect(status.hintChanged).toBe(false);
    expect(status.stale).toBe(false);
  });

  it('never reports a hint change for a flow with no nodes', () => {
    const status = sidecarStaleness('', { nodes: {}, layoutHint: 'TB', derivedFrom: 'x' }, graphOf(''), 'left-right');
    expect(status.hintChanged).toBe(false);
    // ...but an empty `.mmd` still makes the sidecar orphaned and stale.
    expect(status.orphaned).toBe(true);
    expect(status.stale).toBe(true);
  });

  it('treats a whitespace-only recorded hint as absent, not as a change', () => {
    const status = sidecarStaleness(
      FLOW,
      { nodes: {}, layoutHint: '   ', derivedFrom: derivedFromHash(FLOW) },
      graphOf(FLOW),
      'left-right',
    );
    expect(status.hintChanged).toBe(false);
    expect(status.stale).toBe(false);
  });

  it('accepts an undefined `.mmd` as an empty flow and reports an orphan', () => {
    const status = sidecarStaleness(undefined, { nodes: {}, layoutHint: '', derivedFrom: '' }, graphOf(''));
    expect(status).toEqual({
      stale: true,
      currentHash: hashContent(''),
      recordedHash: '',
      orphaned: true,
      hintChanged: false,
    });
  });

  it('trims the recorded hash before reporting it', () => {
    const status = sidecarStaleness(
      FLOW,
      { nodes: {}, layoutHint: '', derivedFrom: ` ${derivedFromHash(FLOW)} ` },
      graphOf(FLOW),
    );
    expect(status.recordedHash).toBe(derivedFromHash(FLOW));
    expect(status.stale).toBe(false);
  });
});

describe('resolveFlowLayout re-layout triggers', () => {
  it('regenerates when the hint moves from top-down to left-right with the same `.mmd`', () => {
    const graph = graphOf(FLOW);
    const recordedHint = 'top-down';
    const currentHint = 'left-right';
    const stale = buildLayoutSidecar(graph, FLOW, { layoutHint: recordedHint });
    // Same `.mmd`, same `derivedFrom`: only the orientation moved.
    expect(stale.layoutHint).toBe(recordedHint);
    expect(stale.derivedFrom).toBe(derivedFromHash(FLOW));

    const resolved = resolveFlowLayout(FLOW, graph, stale, currentHint);

    expect(resolved.regenerated).toBe(true);
    // Same content hash — the regeneration is driven purely by the orientation.
    expect(resolved.sidecar.derivedFrom).toBe(stale.derivedFrom);
    expect(resolved.sidecar.layoutHint).toBe(currentHint);
    expect(resolved.positions).toEqual(buildLayoutSidecar(graph, FLOW, { layoutHint: currentHint }).nodes);
    // The regenerated sidecar is fresh on the next load, so the re-layout is
    // a one-off rather than a per-render loop.
    expect(resolveFlowLayout(FLOW, graph, resolved.sidecar, currentHint).regenerated).toBe(false);
  });

  it('regenerates and drops the recorded keyword hint when the override is removed', () => {
    const graph = graphOf(FLOW);
    // Built without a hint, the sidecar records the declared keyword `LR`;
    // re-laying out with the `top-down` hint must not reuse it.
    const declared = buildLayoutSidecar(graph, FLOW);
    expect(declared.layoutHint).toBe('LR');

    const resolved = resolveFlowLayout(FLOW, graph, declared, 'top-down');

    expect(resolved.regenerated).toBe(true);
    expect(resolved.sidecar.layoutHint).toBe('top-down');
    expect(resolved.positions).not.toEqual(declared.nodes);
    expect(resolved.positions.pay.y).toBeGreaterThan(resolved.positions.cart.y);
    // ...and the keyword spelling of the same hint keeps it fresh afterwards.
    expect(resolveFlowLayout(FLOW, graph, resolved.sidecar, 'TB').regenerated).toBe(false);
  });

  it('re-derives positions that a stale sidecar only partially recorded', () => {
    const graph = graphOf(FLOW);
    const partial = { nodes: { cart: { x: 999, y: 999 } }, layoutHint: 'LR', derivedFrom: derivedFromHash(FLOW) };
    const resolved = resolveFlowLayout(FLOW, graph, partial, '');

    expect(resolved.regenerated).toBe(true);
    expect(resolved.positions).toEqual(layoutFlowGraph(graph));
    expect(resolved.positions.cart).not.toEqual({ x: 999, y: 999 });
    expect(resolved.sidecar.nodes).toEqual(layoutFlowGraph(graph));
  });

  it('re-derives around ids the sidecar kept after they left the flow', () => {
    const graph = graphOf(FLOW);
    const withGhost = {
      nodes: { ...layoutFlowGraph(graph), ghost: { x: 1, y: 1 } },
      layoutHint: 'LR',
      derivedFrom: derivedFromHash(FLOW),
    };
    const resolved = resolveFlowLayout(FLOW, graph, withGhost, '');

    // Every current node is covered and the hash matches, so the recorded
    // positions are reused; the vanished id is collapsed away.
    expect(resolved.regenerated).toBe(false);
    expect(resolved.positions).toEqual(layoutFlowGraph(graph));
    expect(resolved.positions).not.toHaveProperty('ghost');
  });

  it('treats a sidecar with no usable nodes as incomplete for a non-empty flow', () => {
    const graph = graphOf(FLOW);
    const blank = { nodes: {}, layoutHint: 'LR', derivedFrom: derivedFromHash(FLOW) };
    const resolved = resolveFlowLayout(FLOW, graph, blank, '');
    expect(resolved.regenerated).toBe(true);
    expect(resolved.positions).toEqual(layoutFlowGraph(graph));
  });

  it('regenerates even a single-node flow from a fresh but empty sidecar', () => {
    // `graph.nodes.every(...)` is vacuously true for zero nodes, so the reuse
    // guard has to come from the coverage check on the non-empty graph.
    const graph = graphOf('flowchart TB\n  solo[Solo]\n');
    const blank = { nodes: {}, layoutHint: 'TB', derivedFrom: derivedFromHash('flowchart TB\n  solo[Solo]\n') };
    const resolved = resolveFlowLayout('flowchart TB\n  solo[Solo]\n', graph, blank, '');
    expect(resolved.regenerated).toBe(true);
    expect(resolved.positions.solo).toEqual(layoutFlowGraph(graph).solo);
  });

  it('regenerates an empty-flow layout from scratch when there is no sidecar', () => {
    const resolved = resolveFlowLayout('', graphOf(''), undefined, 'top-down');
    expect(resolved.regenerated).toBe(true);
    expect(resolved.positions).toEqual({});
    // No nodes means no orientation to record, so the hint is dropped.
    expect(resolved.sidecar).toEqual({ nodes: {}, layoutHint: '', derivedFrom: derivedFromHash('') });
  });

  it('keeps the layout deterministic when the regeneration hint is unusable', () => {
    const graph = graphOf(FLOW);
    const resolved = resolveFlowLayout(FLOW, graph, null, 'sideways');
    // The unusable hint is not a mermaid keyword, so it resolves to the
    // declared orientation — the positions and the rendered layout agree.
    expect(resolveFlowDirection(graph.direction, 'sideways')).toBe('LR');
    expect(resolved.positions).toEqual(layoutFlowGraph(graph));
    expect(resolved.positions.pay.y).toBe(resolved.positions.cart.y);
  });
});
