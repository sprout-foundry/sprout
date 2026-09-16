/**
 * Layout derivation (SP-140-3 §3b) — mermaid flow source → dagre → node
 * positions.
 *
 * Pure module: no React, no DOM, no side effects, no I/O. Given a parsed flow
 * graph (or `.mmd` text) it returns plain `{ x, y }` coordinates that React
 * Flow consumes directly. The same dagre engine mermaid uses internally, so
 * canvas positions agree with the agent's `design_render` output.
 *
 * Orientation is resolved through `normaliseLayoutHint`, whose keyword table
 * mirrors `pkg/agent_tools/design_render_mermaid.go`'s `flowLayouts`: the
 * render-time `flow_layout` hint first, else the flow's own `flowchart <dir>`
 * declaration, else top-down (mermaid's own default). The sidecar's
 * `layoutHint` field records the resolved keyword so the canvas and the render
 * agree (SP-140-3 §3b AC).
 */

import dagre from 'dagre';
import type { DesignLayoutSidecar } from '../services/api/types';
import type { FlowEdge, FlowGraph, FlowNode } from './flowText';
import { parseFlowGraph } from './flowText';

export type { FlowEdge, FlowGraph, FlowNode } from './flowText';

/** A plain React Flow position — no library type leaks into the sidecar. */
export interface FlowPoint {
  x: number;
  y: number;
}

/** Node geometry used to reserve space in the layout. */
export interface FlowDimensions {
  width: number;
  height: number;
}

export interface FlowLayoutOptions {
  /** Render-time orientation hint (`flow_layout`), e.g. `left-right` or `LR`. */
  layoutHint?: string;
  /** Per-node geometry override; missing ids fall back to the default. */
  dimensions?: Record<string, Partial<FlowDimensions>>;
  /** Node dimensions when neither the override nor the default applies. */
  defaultDimensions?: Partial<FlowDimensions>;
}

/** Deterministic layout defaults — dagre rejects non-positive dimensions. */
export const DEFAULT_NODE_WIDTH = 180;
export const DEFAULT_NODE_HEIGHT = 100;
export const DEFAULT_NODE_SEPARATION = 40;
export const DEFAULT_RANK_SEPARATION = 64;
/** Mermaid's own default when a flow declares no direction. */
export const DEFAULT_DIRECTION = 'TB';

/**
 * `flow_layout` argument → mermaid direction keyword. Mirrors
 * `flowLayouts` in `pkg/agent_tools/design_render_mermaid.go` exactly, so a
 * hint the renderer accepts is a hint the canvas honors.
 */
const LAYOUT_HINTS: Record<string, string> = {
  'top-down': 'TB',
  td: 'TB',
  tb: 'TB',
  'bottom-up': 'BT',
  bt: 'BT',
  'left-right': 'LR',
  lr: 'LR',
  'right-left': 'RL',
  rl: 'RL',
};

const DIRECTIONS = new Set(['TB', 'TD', 'BT', 'LR', 'RL']);

/** Mermaid direction keyword for a layout hint, or '' when there is no override. */
export function normaliseLayoutHint(hint: string | null | undefined): string {
  return (
    LAYOUT_HINTS[
      String(hint ?? '')
        .trim()
        .toLowerCase()
    ] ?? ''
  );
}

/**
 * The orientation a flow should be laid out in: the override hint when it
 * resolves, else the source's own declaration, else `TB`. `TD` is an alias of
 * `TB` in mermaid and in dagre (`rankdir: 'TD'` lays out identically), so it
 * is normalized to one canonical spelling — that keeps the resolved keyword
 * comparable, which the staleness check depends on.
 */
export function resolveFlowDirection(direction: string | null | undefined, hint?: string | null): string {
  const override = normaliseLayoutHint(hint);
  if (override) return override;
  const declared = String(direction ?? '')
    .trim()
    .toUpperCase();
  if (!DIRECTIONS.has(declared)) return DEFAULT_DIRECTION;
  return declared === 'TD' ? DEFAULT_DIRECTION : declared;
}

/** True when `value` is a finite number (NaN/Infinity are not positions). */
function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

/** True when `value` is a usable `{x, y}` position. */
export function isPoint(value: unknown): value is FlowPoint {
  if (!value || typeof value !== 'object') return false;
  const point = value as { x?: unknown; y?: unknown };
  return isFiniteNumber(point.x) && isFiniteNumber(point.y);
}

/** Round to 1/1000 px so derived positions stay stable across platforms. */
function round(value: number): number {
  return Math.round(value * 1000) / 1000;
}

function nodeSize(node: FlowNode, options: FlowLayoutOptions): FlowDimensions {
  const override = options.dimensions?.[node.id] ?? {};
  const fallback = options.defaultDimensions ?? {};
  const width = override.width ?? fallback.width ?? DEFAULT_NODE_WIDTH;
  const height = override.height ?? fallback.height ?? DEFAULT_NODE_HEIGHT;
  return {
    width: isFiniteNumber(width) && width > 0 ? width : DEFAULT_NODE_WIDTH,
    height: isFiniteNumber(height) && height > 0 ? height : DEFAULT_NODE_HEIGHT,
  };
}

/** Edges whose endpoints are both present, in source order. */
function drawableEdges(graph: FlowGraph): FlowEdge[] {
  const ids = new Set(graph.nodes.map((node) => node.id));
  return graph.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target));
}

/**
 * Headless dagre layout for a parsed flow graph.
 *
 * Nodes are seeded in first-seen order with `nodesep`/`ranksep` separation;
 * dagre's coordinates are centre-based (mermaid reads them the same way), so
 * they are emitted unchanged. Deterministic: the same graph, options, and
 * dagre version always produce the same map.
 */
export function layoutFlowGraph(graph: FlowGraph, options: FlowLayoutOptions = {}): Record<string, FlowPoint> {
  const positions: Record<string, FlowPoint> = {};
  if (!graph || graph.nodes.length === 0) return positions;

  const g = new dagre.graphlib.Graph({ multigraph: true });
  g.setGraph({
    rankdir: resolveFlowDirection(graph.direction, options.layoutHint),
    nodesep: DEFAULT_NODE_SEPARATION,
    ranksep: DEFAULT_RANK_SEPARATION,
  });
  g.setDefaultEdgeLabel(() => ({}));

  for (const node of graph.nodes) {
    const { width, height } = nodeSize(node, options);
    g.setNode(node.id, { width, height });
  }
  for (const edge of drawableEdges(graph)) {
    g.setEdge(edge.source, edge.target, {}, `${edge.source}->${edge.target}:${edge.operator}:${edge.label}`);
  }
  dagre.layout(g);

  for (const node of graph.nodes) {
    const laid = g.node(node.id) as { x?: unknown; y?: unknown } | undefined;
    if (!laid || !isFiniteNumber(laid.x) || !isFiniteNumber(laid.y)) continue;
    positions[node.id] = { x: round(laid.x), y: round(laid.y) };
  }
  return positions;
}

/** Convenience wrapper: `.mmd` text straight to positions. */
export function deriveLayout(text: string, options: FlowLayoutOptions = {}): Record<string, FlowPoint> {
  return layoutFlowGraph(parseFlowGraph(text), options);
}

/**
 * Node ids whose layout should be discarded, in the given order: ids that
 * vanished from the expanded graph (deleted or renamed upstream). Specified
 * ids always survive and keep their order, so the canvas can re-derive around
 * hand-placed nodes.
 */
export function staleLayoutIds(layout: Record<string, FlowPoint> | null | undefined, expandedIds: string[]): string[] {
  if (!layout) return [];
  const current = new Set(expandedIds);
  return Object.keys(layout).filter((id) => !current.has(id));
}

/**
 * Expand a collapsed sidecar into positions for every node of `graph`.
 * A node with no drawn position — a flow that gained a node since the layout
 * was recorded — falls back to its dagre position, keeping the canvas
 * renderable instead of dropping the node.
 */
export function expandLayout(
  graph: FlowGraph,
  sidecar: Pick<DesignLayoutSidecar, 'nodes'> | null | undefined,
  options: FlowLayoutOptions = {},
): Record<string, FlowPoint> {
  const stored = sidecar?.nodes ?? {};
  const missing = graph.nodes.some((node) => !isPoint(stored[node.id]));
  const fallback = missing ? layoutFlowGraph(graph, options) : {};
  const expanded: Record<string, FlowPoint> = {};
  for (const node of graph.nodes) {
    const point = stored[node.id];
    expanded[node.id] = isPoint(point) ? { x: point.x, y: point.y } : (fallback[node.id] ?? { x: 0, y: 0 });
  }
  return expanded;
}

/**
 * Collapse positions to the sidecar's compact form: drop ids that are no
 * longer in the flow, round, and order by the flow's node order so a rewritten
 * sidecar produces a minimal, deterministic diff.
 */
export function collapseLayout(position: Record<string, FlowPoint>, graph: FlowGraph): Record<string, FlowPoint> {
  const collapsed: Record<string, FlowPoint> = {};
  for (const node of graph.nodes) {
    const point = position[node.id];
    if (!isPoint(point)) continue;
    collapsed[node.id] = { x: round(point.x), y: round(point.y) };
  }
  return collapsed;
}
