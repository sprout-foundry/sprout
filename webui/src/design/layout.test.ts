/**
 * Layout-derivation tests (SP-140-3 §3b, AC: "dagre layout matches
 * design_render's orientation hints").
 *
 * The modules under test are pure, so everything here is a table-driven
 * assertion on plain data — no React, no DOM, no mocks.
 */
import { describe, expect, it } from 'vitest';
import { parseFlowGraph } from './flowText';
import {
  DEFAULT_DIRECTION,
  DEFAULT_NODE_HEIGHT,
  DEFAULT_NODE_WIDTH,
  collapseLayout,
  deriveLayout,
  expandLayout,
  isPoint,
  layoutFlowGraph,
  normaliseLayoutHint,
  resolveFlowDirection,
  staleLayoutIds,
} from './layout';

const DIAMOND = ['flowchart TB', '  a[Start] --> b[Review]', '  b --> c[Done]'].join('\n');
const TWO_UP = ['flowchart LR', '  x --> y', '  x --> z', '  y --> z'].join('\n');

const graphOf = (text: string) => parseFlowGraph(text);

describe('normaliseLayoutHint', () => {
  it('mirrors design_render flowLayouts keyword-for-keyword', () => {
    // pkg/agent_tools/design_render_mermaid.go`flowLayouts`.
    expect(normaliseLayoutHint('top-down')).toBe('TB');
    expect(normaliseLayoutHint('td')).toBe('TB');
    expect(normaliseLayoutHint('tb')).toBe('TB');
    expect(normaliseLayoutHint('bottom-up')).toBe('BT');
    expect(normaliseLayoutHint('bt')).toBe('BT');
    expect(normaliseLayoutHint('left-right')).toBe('LR');
    expect(normaliseLayoutHint('lr')).toBe('LR');
    expect(normaliseLayoutHint('right-left')).toBe('RL');
    expect(normaliseLayoutHint('rl')).toBe('RL');
  });

  it('is case- and whitespace-insensitive', () => {
    expect(normaliseLayoutHint(' LEFT-RIGHT ')).toBe('LR');
    expect(normaliseLayoutHint('LR')).toBe('LR');
  });

  it('accepts every flow_layout value design_render accepts, and no others', () => {
    // The 9 keys of pkg/agent_tools/design_render_mermaid.go`flowLayouts.
    const accepted = ['top-down', 'td', 'tb', 'bottom-up', 'bt', 'left-right', 'lr', 'right-left', 'rl'];
    for (const key of accepted) {
      expect(normaliseLayoutHint(key), `hint ${key} must resolve`).not.toBe('');
    }
    // The renderer falls back to the source direction for anything else; so
    // must the canvas, otherwise the two disagree on orientation.
    for (const rejected of ['', 'sideways', 'diagonal', 'top_down', 'td-ish']) {
      expect(normaliseLayoutHint(rejected), `hint ${rejected} must not resolve`).toBe('');
    }
  });

  it('returns "" for an absent or unknown hint (no override)', () => {
    expect(normaliseLayoutHint('')).toBe('');
    expect(normaliseLayoutHint('  ')).toBe('');
    expect(normaliseLayoutHint('sideways')).toBe('');
    expect(normaliseLayoutHint(null)).toBe('');
    expect(normaliseLayoutHint(undefined)).toBe('');
  });
});

describe('resolveFlowDirection', () => {
  it('prefers the render hint over the source declaration', () => {
    expect(resolveFlowDirection('LR', 'top-down')).toBe('TB');
    expect(resolveFlowDirection('TB', 'left-right')).toBe('LR');
  });

  it('falls back to the declaration, normalising TD to TB', () => {
    expect(resolveFlowDirection('LR', '')).toBe('LR');
    expect(resolveFlowDirection('TD', '')).toBe('TB');
    expect(resolveFlowDirection('bt', '')).toBe('BT');
  });

  it('falls back to top-down when neither is usable', () => {
    expect(resolveFlowDirection('', '')).toBe(DEFAULT_DIRECTION);
    expect(resolveFlowDirection('sideways', 'sideways')).toBe('TB');
    expect(resolveFlowDirection(null, null)).toBe('TB');
  });
});

describe('layoutFlowGraph', () => {
  it('positions every node of the graph', () => {
    const positions = layoutFlowGraph(graphOf(DIAMOND));
    expect(Object.keys(positions)).toEqual(['a', 'b', 'c']);
    for (const point of Object.values(positions)) {
      expect(isPoint(point)).toBe(true);
    }
  });

  it('is deterministic across repeated calls', () => {
    expect(layoutFlowGraph(graphOf(DIAMOND))).toEqual(layoutFlowGraph(graphOf(DIAMOND)));
    expect(deriveLayout(DIAMOND)).toEqual(layoutFlowGraph(graphOf(DIAMOND)));
  });

  it('is pure: the input graph is not mutated', () => {
    const graph = graphOf(DIAMOND);
    const snapshot = JSON.stringify(graph);
    layoutFlowGraph(graph);
    expect(JSON.stringify(graph)).toBe(snapshot);
  });

  it('produces numeric {x, y} only — no dagre objects leak through', () => {
    const positions = layoutFlowGraph(graphOf(DIAMOND));
    for (const point of Object.values(positions)) {
      expect(Object.keys(point).sort()).toEqual(['x', 'y']);
      expect(typeof point.x).toBe('number');
      expect(typeof point.y).toBe('number');
      expect(Number.isFinite(point.x)).toBe(true);
    }
  });

  it('lays TB out strictly downward and LR out strictly sideward', () => {
    const down = layoutFlowGraph(graphOf(DIAMOND), { layoutHint: 'top-down' });
    expect(down.a.y).toBeLessThan(down.b.y);
    expect(down.b.y).toBeLessThan(down.c.y);
    expect(down.a.x).toBe(down.b.x);

    const across = layoutFlowGraph(graphOf(DIAMOND), { layoutHint: 'left-right' });
    expect(across.a.x).toBeLessThan(across.b.x);
    expect(across.b.x).toBeLessThan(across.c.x);
    expect(across.a.y).toBe(across.b.y);
  });

  it('reverses the rank order for BT and RL', () => {
    const bottomUp = layoutFlowGraph(graphOf(DIAMOND), { layoutHint: 'bottom-up' });
    expect(bottomUp.a.y).toBeGreaterThan(bottomUp.c.y);

    const rightLeft = layoutFlowGraph(graphOf(DIAMOND), { layoutHint: 'right-left' });
    expect(rightLeft.a.x).toBeGreaterThan(rightLeft.c.x);
  });

  it('gives TB and LR genuinely different layouts', () => {
    const nodes = graphOf(TWO_UP);
    expect(layoutFlowGraph(nodes, { layoutHint: 'top-down' })).not.toEqual(
      layoutFlowGraph(nodes, { layoutHint: 'left-right' }),
    );
  });

  it('uses the flow declaration when no hint overrides it', () => {
    const declaredLr = layoutFlowGraph(graphOf(TWO_UP));
    const tb = layoutFlowGraph(graphOf(TWO_UP), { layoutHint: 'top-down' });
    expect(declaredLr.y.x).toBeLessThan(declaredLr.z.x);
    expect(tb.y.y).toBeGreaterThan(tb.x.y);
  });

  it('defaults to top-down when no hint and no declaration', () => {
    const positions = layoutFlowGraph(graphOf('x --> y\n'));
    expect(positions.x.x).toBe(positions.y.x);
    expect(positions.y.y).toBeGreaterThan(positions.x.y);
  });

  it('separates parallel ranks by the reserved node height', () => {
    const positions = layoutFlowGraph(graphOf('flowchart TB\n  a --> b\n'));
    expect(positions.b.y - positions.a.y).toBeGreaterThan(DEFAULT_NODE_HEIGHT);
  });

  it('honours per-node dimensions and defaults', () => {
    const graph = graphOf('flowchart TB\n  a --> b\n');
    const wide = layoutFlowGraph(graph, { defaultDimensions: { width: 400, height: 300 } });
    const narrow = layoutFlowGraph(graph);
    expect(wide.b.y - wide.a.y).toBeGreaterThan(narrow.b.y - narrow.a.y);

    const perNode = layoutFlowGraph(graph, { dimensions: { a: { width: 20, height: 20 }, b: { height: 400 } } });
    expect(perNode.b.y - perNode.a.y).toBeGreaterThan(narrow.b.y - narrow.a.y);
  });

  it('ignores non-positive or non-finite dimension overrides', () => {
    const graph = graphOf('flowchart TB\n  a --> b\n');
    expect(layoutFlowGraph(graph, { defaultDimensions: { width: -5, height: Number.NaN } })).toEqual(
      layoutFlowGraph(graph, { defaultDimensions: { width: DEFAULT_NODE_WIDTH, height: DEFAULT_NODE_HEIGHT } }),
    );
  });

  it('drops edges whose endpoints are not in the graph', () => {
    const graph = graphOf('flowchart TB\n  a --> b\n');
    const dangling = {
      ...graph,
      edges: [...graph.edges, { source: 'a', target: 'ghost', operator: '-->', label: '', directed: true }],
    };
    expect(layoutFlowGraph(dangling)).toEqual(layoutFlowGraph(graph));
  });

  it('returns {} for an empty graph or a graph with no nodes', () => {
    expect(layoutFlowGraph(graphOf(''))).toEqual({});
    expect(layoutFlowGraph({ direction: '', nodes: [], edges: [], groups: [] })).toEqual({});
    expect(layoutFlowGraph(undefined as unknown as ReturnType<typeof graphOf>)).toEqual({});
  });

  it('handles a single node and a self-loop without hanging', () => {
    expect(layoutFlowGraph(graphOf('flowchart TB\n  solo[Solo]\n')).solo).toEqual({ x: 90, y: 50 });
    const loop = layoutFlowGraph(graphOf('flowchart TB\n  a[A] --> a\n'));
    expect(isPoint(loop.a)).toBe(true);
  });

  it('positions a cycle deterministically', () => {
    const cyclic = graphOf('flowchart TB\n  a --> b\n  b --> c\n  c --> a\n');
    expect(layoutFlowGraph(cyclic)).toEqual(layoutFlowGraph(cyclic));
  });

  it('rounds positions to 1/1000 px', () => {
    for (const point of Object.values(layoutFlowGraph(graphOf(TWO_UP)))) {
      expect(point.x).toBe(Math.round(point.x * 1000) / 1000);
      expect(point.y).toBe(Math.round(point.y * 1000) / 1000);
    }
  });
});

describe('orientation parity with design_render', () => {
  // Each case is (flow_layout hint the agent passes to design_render,
  // expected mermaid keyword) from pkg/agent_tools/design_render_mermaid.go.
  const cases: Array<[string, string]> = [
    ['top-down', 'TB'],
    ['bottom-up', 'BT'],
    ['left-right', 'LR'],
    ['right-left', 'RL'],
  ];

  it.each(cases)('resolveFlowDirection honors the %s hint as %s', (hint, keyword) => {
    expect(resolveFlowDirection('LR', hint)).toBe(keyword);
  });

  it.each(cases)('rank direction matches the keyword for a two-node flow', (_hint, keyword) => {
    const positions = layoutFlowGraph(graphOf('flowchart LR\n  a --> b\n'), { layoutHint: _hint });
    const [first, second] = [positions.a, positions.b];
    switch (keyword) {
      case 'TB':
        expect(first.x).toBe(second.x);
        expect(second.y).toBeGreaterThan(first.y);
        break;
      case 'BT':
        expect(first.x).toBe(second.x);
        expect(second.y).toBeLessThan(first.y);
        break;
      case 'LR':
        expect(second.x).toBeGreaterThan(first.x);
        expect(first.y).toBe(second.y);
        break;
      default: // RL
        expect(second.x).toBeLessThan(first.x);
        expect(first.y).toBe(second.y);
    }
  });
});

describe('deriveLayout', () => {
  it('derives positions straight from .mmd text', () => {
    const positions = deriveLayout(DIAMOND, { layoutHint: 'left-right' });
    expect(Object.keys(positions)).toEqual(['a', 'b', 'c']);
    expect(positions.a.x).toBeLessThan(positions.c.x);
  });

  it('returns {} for an empty source', () => {
    expect(deriveLayout('')).toEqual({});
  });
});

describe('isPoint', () => {
  it('accepts finite numeric pairs only', () => {
    expect(isPoint({ x: 0, y: 0 })).toBe(true);
    expect(isPoint({ x: Number.NaN, y: 1 })).toBe(false);
    expect(isPoint({ x: Number.POSITIVE_INFINITY, y: 1 })).toBe(false);
    expect(isPoint({ x: '1', y: 2 })).toBe(false);
    expect(isPoint({ x: 1 })).toBe(false);
    expect(isPoint(null)).toBe(false);
  });
});

describe('staleLayoutIds', () => {
  it('lists ids that vanished from the expanded graph, in layout order', () => {
    expect(staleLayoutIds({ a: { x: 0, y: 0 }, gone: { x: 1, y: 1 }, b: { x: 2, y: 2 } }, ['a', 'b'])).toEqual([
      'gone',
    ]);
  });

  it('returns [] for an absent layout or a fully covered graph', () => {
    expect(staleLayoutIds(null, ['a'])).toEqual([]);
    expect(staleLayoutIds({ a: { x: 0, y: 0 } }, ['a', 'b'])).toEqual([]);
  });
});

describe('expandLayout', () => {
  it('returns recorded positions for every node', () => {
    const graph = graphOf(DIAMOND);
    const stored = { a: { x: 10, y: 20 }, b: { x: 30, y: 40 }, c: { x: 50, y: 60 } };
    expect(expandLayout(graph, { nodes: stored })).toEqual(stored);
  });

  it('falls back to a derived position for a node the sidecar predates', () => {
    const graph = graphOf(DIAMOND);
    const expanded = expandLayout(graph, { nodes: { a: { x: 10, y: 20 } } });
    expect(expanded.a).toEqual({ x: 10, y: 20 });
    expect(isPoint(expanded.b)).toBe(true);
    expect(isPoint(expanded.c)).toBe(true);
    expect(expanded.b).toEqual(layoutFlowGraph(graph).b);
  });

  it('treats a malformed stored coordinate as missing', () => {
    const graph = graphOf('flowchart TB\n  a --> b\n');
    const expanded = expandLayout(graph, { nodes: { a: { x: Number.NaN, y: 0 } } as never });
    expect(expanded.a).toEqual(layoutFlowGraph(graph).a);
  });

  it('derives everything when there is no sidecar', () => {
    const graph = graphOf(DIAMOND);
    expect(expandLayout(graph, null)).toEqual(layoutFlowGraph(graph));
  });

  it('returns {} for an empty graph', () => {
    expect(expandLayout(graphOf(''), { nodes: { a: { x: 1, y: 1 } } })).toEqual({});
  });
});

describe('collapseLayout', () => {
  it('drops ids no longer in the flow and orders by node order', () => {
    const graph = graphOf('flowchart TB\n  b --> a\n');
    const collapsed = collapseLayout({ a: { x: 1, y: 1 }, gone: { x: 9, y: 9 }, b: { x: 2, y: 2 } }, graph);
    expect(Object.keys(collapsed)).toEqual(['b', 'a']);
  });

  it('rounds and skips unusable coordinates', () => {
    const graph = graphOf('flowchart TB\n  a --> b\n');
    const collapsed = collapseLayout({ a: { x: 1.23456, y: 2.00004 }, b: undefined as never }, graph);
    expect(collapsed).toEqual({ a: { x: 1.235, y: 2 } });
  });
});
