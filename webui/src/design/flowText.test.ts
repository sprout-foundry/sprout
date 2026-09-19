/**
 * Flow-text extraction tests (SP-140-3 §3b).
 *
 * The parser feeds both the dagre layout and the Flow-correctness rules, so it
 * is pinned against `pkg/design/flowchart.go`'s semantics: the same operator
 * set, bracket-group awareness, first-seen node order, and keyword skipping.
 */
import { describe, expect, it } from 'vitest';
import {
  FLOW_OPERATORS,
  emptyFlowGraph,
  parseFlowGraph,
  parseFlowNodeId,
  parseFlowNodeLabel,
  splitFlowOperators,
} from './flowText';

const CHECKOUT = [
  '%% a comment line',
  'flowchart LR',
  '  cart[Cart] --> pay{Payment}',
  '  pay --> done((Done))',
  '',
  '  subgraph Auth',
  '    login[Login] --> home[Home]',
  '  end',
].join('\n');

describe('splitFlowOperators', () => {
  it('splits a chained statement into segments and operators', () => {
    expect(splitFlowOperators('a --> b --> c')).toEqual({
      segments: ['a ', ' b ', ' c'],
      operators: ['-->', '-->'],
    });
  });

  it('prefers the longest operator at each position', () => {
    // `-.->` must win over `--`, and `-->` over `--`.
    expect(splitFlowOperators('a -.-> b').operators).toEqual(['-.->']);
    expect(splitFlowOperators('a --- b').operators).toEqual(['---']);
    expect(splitFlowOperators('a ==> b').operators).toEqual(['==>']);
    expect(splitFlowOperators('a == b').operators).toEqual(['==']);
  });

  it('ignores operators inside bracket groups', () => {
    const { segments, operators } = splitFlowOperators('a[Step --> Process] --> b');
    expect(segments).toEqual(['a[Step --> Process] ', ' b']);
    expect(operators).toEqual(['-->']);
  });

  it('handles nested bracket groups', () => {
    expect(splitFlowOperators('a((x --> y)) --> b').operators).toEqual(['-->']);
  });

  it('returns a single segment when there is no operator', () => {
    expect(splitFlowOperators('justANode[label]')).toEqual({ segments: ['justANode[label]'], operators: [] });
  });

  it('pins the operator set longest-first (the scanner depends on the order)', () => {
    expect([...FLOW_OPERATORS]).toEqual(['-.->', '--o', '--x', 'o--', 'x--', '-->', '==>', '===', '---', '--', '==']);
  });
});

describe('parseFlowNodeId / parseFlowNodeLabel', () => {
  it('extracts the leading id', () => {
    expect(parseFlowNodeId('  login[Login]')).toBe('login');
    expect(parseFlowNodeId('  ')).toBe('');
    expect(parseFlowNodeId('with-dash_1[x]')).toBe('with-dash_1');
  });

  it('extracts square, round, curly, and nested-bracket labels', () => {
    expect(parseFlowNodeLabel('cart[Cart]')).toBe('Cart');
    expect(parseFlowNodeLabel('cart(Cart)')).toBe('Cart');
    expect(parseFlowNodeLabel('cart{Cart?}')).toBe('Cart?');
    // Nested groups keep their inner text: `((Cart))` is a circle shape whose
    // label is the parenthesised group, matching pkg/design/flowchart.go.
    expect(parseFlowNodeLabel('cart((Cart))')).toBe('(Cart)');
    expect(parseFlowNodeLabel('cart[a(b)c]')).toBe('a(b)c');
  });

  it('returns an empty label when the reference carries no bracket group', () => {
    expect(parseFlowNodeLabel('cart')).toBe('');
    expect(parseFlowNodeLabel('cart -->')).toBe('');
  });
});

describe('parseFlowGraph', () => {
  it('extracts direction, nodes, edges, and groups', () => {
    const graph = parseFlowGraph(CHECKOUT);
    expect(graph.direction).toBe('LR');
    expect(graph.nodes.map((n) => n.id)).toEqual(['cart', 'pay', 'done', 'login', 'home']);
    expect(graph.edges).toEqual([
      { source: 'cart', target: 'pay', operator: '-->', label: '', directed: true },
      { source: 'pay', target: 'done', operator: '-->', label: '', directed: true },
      { source: 'login', target: 'home', operator: '-->', label: '', directed: true },
    ]);
    expect(graph.groups).toEqual(['Auth']);
  });

  it('labels nodes with their bracket text, falling back to the id', () => {
    const graph = parseFlowGraph('flowchart TB\n  cart[Cart] --> pay\n  pay{Payment}\n');
    expect(graph.nodes).toEqual([
      { id: 'cart', label: 'Cart', group: '' },
      { id: 'pay', label: 'Payment', group: '' },
    ]);
  });

  it('keeps the first explicit label when a node is redefined', () => {
    const graph = parseFlowGraph('flowchart TB\n  a --> b\n  a[First]\n  a[Second]\n');
    expect(graph.nodes.find((n) => n.id === 'a')?.label).toBe('First');
  });

  it('tracks subgraph membership and clears it at end', () => {
    const graph = parseFlowGraph(
      ['flowchart TB', '  solo[Solo]', '  subgraph Auth', '    login[Login]', '  end', '  after[After]'].join('\n'),
    );
    const group = (id: string) => graph.nodes.find((n) => n.id === id)?.group;
    expect(group('solo')).toBe('');
    expect(group('login')).toBe('Auth');
    expect(group('after')).toBe('');
  });

  it('reads quoted subgraph titles, including ones containing spaces', () => {
    const graph = parseFlowGraph('flowchart TB\n  subgraph "Sign-up flow"\n    a --> b\n  end\n');
    expect(graph.groups).toEqual(['Sign-up flow']);
    expect(graph.nodes.every((n) => n.group === 'Sign-up flow')).toBe(true);
  });

  it('takes edge labels from |label| blocks', () => {
    const graph = parseFlowGraph('flowchart TB\n  a -->|submit| b\n  b -.->|retry| a\n');
    expect(graph.edges.map((e) => [e.operator, e.label, e.directed])).toEqual([
      ['-->', 'submit', true],
      ['-.->', 'retry', true],
    ]);
  });

  it('extracts chained edges in one statement', () => {
    const graph = parseFlowGraph('flowchart TB\n  a --> b --> c\n');
    expect(graph.nodes.map((n) => n.id)).toEqual(['a', 'b', 'c']);
    expect(graph.edges.map((e) => `${e.source}>${e.target}`)).toEqual(['a>b', 'b>c']);
  });

  it('skips comments, styling keywords, and the declaration', () => {
    const graph = parseFlowGraph(
      [
        '%% note',
        'flowchart TD',
        '  a[A]',
        '  classDef big fill:#fff',
        '  style a stroke-width:2px',
        '  click a href',
      ].join('\n'),
    );
    expect(graph.nodes.map((n) => n.id)).toEqual(['a']);
    expect(graph.direction).toBe('TD');
  });

  it('reads the direction from a graph declaration and rejects non-keywords', () => {
    expect(parseFlowGraph('graph RL\n  a --> b\n').direction).toBe('RL');
    expect(parseFlowGraph('flowchart\n  a --> b\n').direction).toBe('');
    expect(parseFlowGraph('flowchart sideway\n  a --> b\n').direction).toBe('');
    expect(parseFlowGraph('graph lr\n  a --> b\n').direction).toBe('LR');
  });

  it('ignores malformed statements instead of throwing', () => {
    const graph = parseFlowGraph('flowchart TB\n  --> b\n  a -->\n  c[d]\n');
    expect(graph.nodes.map((n) => n.id)).toEqual(['c']);
    expect(graph.edges).toEqual([]);
  });

  it('returns an empty graph for empty or blank input', () => {
    expect(parseFlowGraph('')).toEqual(emptyFlowGraph());
    expect(parseFlowGraph('\n\n  \n')).toEqual({ direction: '', nodes: [], edges: [], groups: [] });
  });

  it('is pure: repeated parses of the same input are deep-equal', () => {
    expect(parseFlowGraph(CHECKOUT)).toEqual(parseFlowGraph(CHECKOUT));
  });
});
