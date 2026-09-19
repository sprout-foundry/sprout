/**
 * Canvas model derivation (SP-140-3 §3b) — pure functions between the parsed
 * flow graph / sidecar and everything `FlowsCanvas` renders.
 *
 * No React, no DOM, no I/O: these functions take plain data in and return
 * plain data out, which keeps the canvas's rules (wireframe pairing, node
 * sizing, edge labels, selection descriptions) unit-testable without mounting
 * React Flow.
 *
 * The canvas renders only *derived* state (SP-140 invariant 2): node positions
 * come from the item-3.4 layout derivation, and node imagery is matched from
 * the flow's own labels. Nothing here reads or writes a `.mmd`; the canvas
 * never authors flow semantics.
 */

import type { FlowGraph, FlowPoint } from '../../design/layout';
import type { DesignAssetEntry, DesignLayoutSidecar } from '../../services/api/types';
import { basename } from './designPaths';

/** Node box size for wireframe imagery, in canvas units. */
export const NODE_IMAGE_WIDTH = 208;

/**
 * Vertical room reserved for a node's caption: the label plus (when present)
 * the group line. Kept explicit so `layout.ts`'s dagre geometry and the
 * rendered box agree.
 */
export const NODE_CAPTION_HEIGHT = 34;

/** Smallest labeled box; also the fallback height for non-wireframe flows. */
export const NODE_BOX_MIN_HEIGHT = 44;

/** Gap between a node's top and its wireframe image. */
export const NODE_IMAGE_OFFSET = 6;

/** Horizontal and vertical padding inside a node box, in canvas units. */
export const NODE_PADDING_X = 8;
export const NODE_PADDING_Y = 4;

/** Mermaid's default flow direction, mirrored from `layout.ts`. */
const DEFAULT_DIRECTION = 'TB';

/** Size of a labeled box whose label never wraps past the image width. */
export function boxDimensionsForLabel(label: string): { width: number; height: number } {
  const lines = Math.max(1, Math.ceil((label ?? '').length / 34));
  return {
    width: NODE_IMAGE_WIDTH,
    height: Math.max(NODE_BOX_MIN_HEIGHT, lines * 18 + 2 * NODE_PADDING_Y + 6),
  };
}

/** Size of a node rendering `svgText` as imagery, or null when there is none. */
export function wireframeDimensions(svgText: string): { width: number; height: number } | null {
  const viewBox = parseSvgViewBox(svgText);
  if (!viewBox) return null;
  const scale = NODE_IMAGE_WIDTH / viewBox.width;
  const imageHeight = Math.max(1, Math.round(viewBox.height * scale));
  return { width: NODE_IMAGE_WIDTH, height: imageHeight + NODE_CAPTION_HEIGHT + NODE_IMAGE_OFFSET };
}

/**
 * `viewBox="minX minY width height"` from an SVG's opening tag, or null when
 * absent/unparseable. The canvas needs only the aspect ratio, so min-x/min-y
 * are ignored: an off-origin viewBox still scales to the node width.
 */
export function parseSvgViewBox(svgText: string): { width: number; height: number } | null {
  if (!svgText) return null;
  const match = /viewBox\s*=\s*["']([^"']+)["']/i.exec(svgText);
  if (!match) return null;
  const parts = match[1]
    .trim()
    .split(/[\s,]+/)
    .map((part) => Number(part));
  if (parts.length !== 4 || parts.some((value) => !Number.isFinite(value))) return null;
  const width = parts[2];
  const height = parts[3];
  if (width <= 0 || height <= 0) return null;
  return { width, height };
}

/** True when a node label names (or aliases) a wireframe asset. */
export function matchesWireframe(label: string, asset: DesignAssetEntry): boolean {
  const normalised = normaliseName(label);
  if (!normalised) return false;
  const assetName = normaliseName(basename(asset.path).replace(/\.svg$/i, ''));
  const assetKind = normaliseName(asset.kind);
  const assetPath = normaliseName(asset.path).replace(/\.svg$/i, '');
  return [assetName, assetKind, assetPath].some((candidate) => candidate !== '' && candidate === normalised);
}

/** Lowercase, separator-free form used for label/asset comparison. */
function normaliseName(value: string): string {
  return String(value ?? '')
    .toLowerCase()
    .replace(/\s+/g, '')
    .replace(/[/_\\-]+/g, '');
}

/**
 * Map every flow node id to a matching wireframe asset path ('' when the flow
 * has no matching imagery — those nodes render labeled boxes). The first asset
 * in inventory order wins, so pairing is deterministic.
 */
export function matchWireframeAssets(
  graph: FlowGraph,
  wireframes: DesignAssetEntry[] | null | undefined,
): Record<string, string> {
  const assets = wireframes ?? [];
  const pairs: Record<string, string> = {};
  for (const node of graph.nodes) {
    const asset = assets.find((candidate) => matchesWireframe(node.label, candidate));
    pairs[node.id] = asset?.path ?? '';
  }
  return pairs;
}

/** Per-node geometry for the dagre layout, derived from the flow's imagery. */
export function nodeDimensionsFor(
  graph: FlowGraph,
  wireframes: DesignAssetEntry[] | null | undefined,
  svgText: Record<string, string> | null | undefined,
): Record<string, { width: number; height: number }> {
  const pairs = matchWireframeAssets(graph, wireframes);
  const dimensions: Record<string, { width: number; height: number }> = {};
  for (const node of graph.nodes) {
    const text = svgText?.[pairs[node.id]] ?? '';
    dimensions[node.id] = wireframeDimensions(text) ?? boxDimensionsForLabel(node.label);
  }
  return dimensions;
}

/** Node ids in the graph, in flow order. */
export function nodeIds(graph: FlowGraph): string[] {
  return graph.nodes.map((node) => node.id);
}

/** The orientation keyword recorded in a sidecar, for the canvas status line. */
export function canvasOrientation(graph: FlowGraph, sidecar: DesignLayoutSidecar | null | undefined): string {
  const recorded = String(sidecar?.layoutHint ?? '').trim();
  if (recorded) return recorded;
  const declared = String(graph.direction ?? '').trim();
  return declared ? declared.toUpperCase() : DEFAULT_DIRECTION;
}

/** Handle the edge leaves from, matching `sourcePosition`/`targetPosition`. */
export function handleIds(direction: string): { source: string; target: string } {
  switch (String(direction ?? '').toUpperCase()) {
    case 'LR':
      return { source: 'right', target: 'left' };
    case 'RL':
      return { source: 'left', target: 'right' };
    case 'BT':
      return { source: 'bottom', target: 'top' };
    default:
      return { source: 'bottom', target: 'top' };
  }
}

/**
 * Where to park a node the flow gained after its layout was recorded. Placed
 * clear of the stored positions (one column to the right of the bounding box)
 * so a new node is visible rather than stacked on an existing one.
 */
export function parkingSpot(existing: FlowPoint[]): FlowPoint {
  if (existing.length === 0) return { x: 0, y: 0 };
  const maxX = Math.max(...existing.map((point) => point.x));
  const minY = Math.min(...existing.map((point) => point.y));
  return { x: maxX + NODE_IMAGE_WIDTH + 60, y: minY };
}

/**
 * Where a node sits in storage terms: a position the user dragged it to wins
 * over the derived one (sidecar or dagre). Used on node-drag stop, where the
 * canvas must not snap a hand-placed node back under the cursor.
 */
export function draggedPosition(
  flowNodeId: string,
  dragged: Record<string, FlowPoint>,
  positions: Record<string, FlowPoint>,
): FlowPoint | undefined {
  return dragged[flowNodeId] ?? positions[flowNodeId];
}
