/**
 * Mermaid flow-text extraction (SP-140-3 §3b) — the pure parser shared by the
 * layout derivation (`layout.ts`) and the Flow-correctness rules.
 *
 * It is the structural superset of the counting-only parser in
 * `services/api/designApi.ts` (`parseFlowText`) and mirrors the Go subset
 * parser in `pkg/design/flowchart.go`: the same declaration handling, the same
 * first-seen node order, the same connect-operator set (ordered longest-first
 * so `-->` never splits as `--`), and bracket-group awareness so an operator
 * inside a node label (`A[Step --> Process] --> B`) is not a separator.
 *
 * Differences from the Go parser are deliberate and additive: node labels are
 * extracted (the canvas renders labeled boxes), subgraph membership is tracked
 * so grouped nodes stay grouped, and unstyled node ids merely referenced in
 * edge statements are labeled with their id. The `.mmd` source is never
 * written by any consumer of this module (SP-140 invariant 3).
 */

/** A single mermaid flowchart node, in first-seen order. */
export interface FlowNode {
  /** Mermaid node identifier (e.g. `login`). */
  id: string;
  /** Bracket label, or the id itself when the flow declares no label. */
  label: string;
  /** Enclosing `subgraph` name, or '' when the node is not grouped. */
  group: string;
}

/** A single edge between two node ids. */
export interface FlowEdge {
  source: string;
  target: string;
  /** The connect operator as written (e.g. `-->`, `-.->`). */
  operator: string;
  /** Edge text from a `|label|` block, or ''. */
  label: string;
  /** True when `operator` is a directed arrow form. */
  directed: boolean;
}

/** The parsed shape of one mermaid flowchart source. */
export interface FlowGraph {
  /** Declared orientation keyword (TD/TB/BT/LR/RL), or '' when absent. */
  direction: string;
  nodes: FlowNode[];
  edges: FlowEdge[];
  /** Subgraph names in first-seen order. */
  groups: string[];
}

/**
 * Connect operators, longest-first. The order is load-bearing: the scanner
 * takes the first match at each position, so `-.->` must precede `-->`, and
 * `-->` must precede `--`.
 */
export const FLOW_OPERATORS: readonly string[] = [
  '-.->',
  '--o',
  '--x',
  'o--',
  'x--',
  '-->',
  '==>',
  '===',
  '---',
  '--',
  '==',
];

const LEADING_ID = /^[A-Za-z0-9_-]+/;

/** A `|edge text|` block written immediately after a connect operator. */
const EDGE_LABEL = /^\s*\|([^|]*)\|/;

/** Statement keywords that are never node/edge statements. */
const SKIP_KEYWORDS = ['classdef', 'class', 'style', 'linkstyle', 'click', 'direction'];

/** Direction keywords a `flowchart` declaration may carry. */
const DIRECTION_KEYWORDS = new Set(['TB', 'TD', 'BT', 'RL', 'LR']);

/** An empty graph — also the value returned for empty or unusable input. */
export function emptyFlowGraph(): FlowGraph {
  return { direction: '', nodes: [], edges: [], groups: [] };
}

/**
 * Split a statement into node segments and the operators between them, while
 * ignoring bracket groups (node labels may contain operator characters).
 */
export function splitFlowOperators(line: string): { segments: string[]; operators: string[] } {
  const segments: string[] = [];
  const operators: string[] = [];
  let current = '';
  let depth = 0;
  let i = 0;

  while (i < line.length) {
    const char = line[i];
    if (char === '[' || char === '(' || char === '{') {
      depth += 1;
      current += char;
      i += 1;
      continue;
    }
    if (char === ']' || char === ')' || char === '}') {
      depth = Math.max(0, depth - 1);
      current += char;
      i += 1;
      continue;
    }
    const operator = depth === 0 ? FLOW_OPERATORS.find((candidate) => line.startsWith(candidate, i)) : undefined;
    if (operator) {
      segments.push(current);
      operators.push(operator);
      current = '';
      i += operator.length;
      continue;
    }
    current += char;
    i += 1;
  }
  segments.push(current);
  return { segments, operators };
}

/** Extract the leading node id from a node reference segment. */
export function parseFlowNodeId(segment: string): string {
  return LEADING_ID.exec(segment.trim())?.[0] ?? '';
}

/**
 * Return the text inside the outermost bracket group that follows an id:
 * `[label]`, `(label)`, `{label}`, and nested variants such as `((label))`.
 * The group is matched by bracket identity (count of the same bracket), so
 * `((Cart))` yields `Cart` and `[a(b)c]` yields `a(b)c`.
 * Returns '' when the segment carries no bracket group.
 */
export function parseFlowNodeLabel(segment: string): string {
  const id = parseFlowNodeId(segment);
  const rest = segment.trim().slice(id.length).trim();
  if (!rest) return '';
  const open = rest[0];
  const close = open === '[' ? ']' : open === '(' ? ')' : open === '{' ? '}' : '';
  if (!close) return '';
  let depth = 0;
  for (let i = 0; i < rest.length; i += 1) {
    if (rest[i] === open) depth += 1;
    else if (rest[i] === close) {
      depth -= 1;
      if (depth === 0) return rest.slice(1, i).trim();
    }
  }
  // Unclosed bracket group: best-effort, matching the Go parser.
  return rest.slice(1).trim();
}

/** Leading `subgraph <title>` name, quoted (possibly with spaces) or bare. */
function parseSubgraphName(tail: string): string {
  const trimmed = tail.trim();
  const quote = trimmed[0];
  if (quote === '"' || quote === "'") {
    const end = trimmed.indexOf(quote, 1);
    return end < 0 ? trimmed.slice(1).trim() : trimmed.slice(1, end).trim();
  }
  return trimmed.split(/\s+/)[0] ?? '';
}

interface MutableGraph {
  direction: string;
  order: string[];
  labels: Map<string, string>;
  groups: string[];
  groupOf: Map<string, string>;
  edges: FlowEdge[];
}

function addNode(graph: MutableGraph, id: string, label: string): void {
  if (!id) return;
  if (!graph.labels.has(id)) {
    graph.labels.set(id, label || id);
    graph.order.push(id);
    return;
  }
  // A later explicit label wins over the id placeholder from a bare reference.
  if (label && graph.labels.get(id) === id) graph.labels.set(id, label);
}

/** Parse a `|label|` edge-text prefix off the segment that follows an operator. */
function parseOperatorTail(segment: string): { id: string; label: string } {
  const trimmed = segment.trim();
  const match = EDGE_LABEL.exec(trimmed);
  if (match) {
    return { id: parseFlowNodeId(trimmed.slice(match[0].length)), label: match[1].trim() };
  }
  return { id: parseFlowNodeId(trimmed), label: '' };
}

function parseStatement(line: string, graph: MutableGraph): void {
  const { segments, operators } = splitFlowOperators(line);
  const tails = segments.map((segment, index) =>
    index === 0 ? { id: parseFlowNodeId(segment), label: '' } : parseOperatorTail(segment),
  );

  // A statement with operators needs an id on both ends; a bare statement
  // needs one node id. Anything else is unparseable and contributes nothing —
  // a half-written line must not inject phantom nodes (pkg/design/flowchart.go
  // reports the same lines as `BadLines`).
  const operable =
    operators.length === 0 ? !!tails[0].id : operators.every((_, i) => !!tails[i]?.id && !!tails[i + 1]?.id);
  if (!operable) return;

  tails.forEach((tail, index) => {
    if (!tail.id) return;
    addNode(graph, tail.id, parseFlowNodeLabel(segments[index]));
  });

  for (let i = 0; i < operators.length; i += 1) {
    const operator = operators[i];
    graph.edges.push({
      source: tails[i].id,
      target: tails[i + 1].id,
      operator,
      label: tails[i + 1].label,
      directed: operator.includes('>'),
    });
  }
}

/**
 * Parse a mermaid flowchart source into nodes, edges, groups, and direction.
 * Pure and total: malformed lines are skipped rather than throwing, so a
 * half-written `.mmd` still yields the structure the canvas can draw.
 */
export function parseFlowGraph(text: string): FlowGraph {
  if (!text) return emptyFlowGraph();

  const graph: MutableGraph = {
    direction: '',
    order: [],
    labels: new Map(),
    groups: [],
    groupOf: new Map(),
    edges: [],
  };
  let group = '';

  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line || line.startsWith('%%')) continue;
    const lower = line.toLowerCase();

    if (lower.startsWith('flowchart') || lower.startsWith('graph')) {
      const keyword = (line.split(/\s+/)[1] ?? '').toUpperCase();
      if (DIRECTION_KEYWORDS.has(keyword)) graph.direction = keyword;
      continue;
    }
    if (lower === 'end') {
      group = '';
      continue;
    }
    if (lower.startsWith('subgraph')) {
      const name = parseSubgraphName(line.slice('subgraph'.length));
      if (name && !graph.groups.includes(name)) graph.groups.push(name);
      group = name;
      continue;
    }
    if (SKIP_KEYWORDS.some((keyword) => lower.startsWith(keyword))) continue;

    const before = graph.order.length;
    parseStatement(line, graph);
    for (const id of graph.order.slice(before)) graph.groupOf.set(id, group);
  }

  const nodes: FlowNode[] = graph.order.map((id) => ({
    id,
    label: graph.labels.get(id) ?? id,
    group: graph.groupOf.get(id) ?? '',
  }));
  return { direction: graph.direction, nodes, edges: graph.edges, groups: graph.groups };
}
