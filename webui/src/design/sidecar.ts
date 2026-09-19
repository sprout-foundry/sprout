/**
 * Layout-sidecar logic (SP-140-3 §3b, SP-140 invariants 2 & 6).
 *
 * The canvas owns derived position data only: `design/flows/<name>.layout.json`
 * is a sidecar `{nodes: {id: {x, y}}, layoutHint, derivedFrom}` where
 * `derivedFrom` is a content hash of the `.mmd` it was derived from. Editing
 * the flow drifts the hash; the canvas detects that and regenerates the layout
 * on next load.
 *
 * Pure module: hashing, staleness, and sidecar construction are plain
 * functions with no React, DOM, or I/O. The hash is a synchronous non-crypto
 * content hash (FNV-1a) rather than `crypto.subtle`: drift detection must be
 * synchronous, work identically in the browser and in tests, and — unlike
 * provenance/security hashes (`pkg/trace`, `pkg/embedding` use SHA-256) — it
 * is a local change-detector with no adversarial requirement. The sidecar
 * remains a derived artifact: a false "fresh" reading is recoverable because
 * the canvas can always re-derive from the `.mmd` (SP-140 invariant 2).
 */

import type { DesignLayoutSidecar } from '../services/api/types';
import type { FlowDimensions, FlowGraph, FlowPoint } from './layout';
import { collapseLayout, isPoint, layoutFlowGraph, normaliseLayoutHint, resolveFlowDirection } from './layout';

export type { DesignLayoutSidecar } from '../services/api/types';

/** FNV-1a 32-bit offset basis and prime. */
const FNV_OFFSET_BASIS = 0x811c9dc5;
const FNV_PRIME = 0x01000193;

/**
 * FNV-1a (32-bit) content hash of `text`, lowercase zero-padded hex.
 *
 * Deterministic and platform-independent: hashed over UTF-16 code units rather
 * than bytes, so the same `.mmd` text yields the same hash in every runtime.
 * Empty input yields the offset basis (`811c9dc5`), never ''.
 *
 * This is a 32-bit change-detector, not a provenance hash: it distinguishes
 * ordinary edits, and the sidecar is disposable derived state, so a
 * pathological collision costs one stale layout, never data (SP-140
 * invariant 2). Keep the width in mind if this value is ever compared across
 * tools — widen it (two 32-bit lanes) before it becomes a cross-tool contract.
 */
export function hashContent(text: string): string {
  const content = text ?? '';
  let hash = FNV_OFFSET_BASIS;
  for (let i = 0; i < content.length; i += 1) {
    // XOR then multiply with a 32-bit wrapping multiply (Math.imul keeps the
    // 32-bit lane; a plain `*` would lose precision past 2^53).
    hash ^= content.charCodeAt(i);
    hash = Math.imul(hash, FNV_PRIME);
  }
  return (hash >>> 0).toString(16).padStart(8, '0');
}

/** `derivedFrom` value for a flow's `.mmd` content. */
export function derivedFromHash(mmdText: string): string {
  return hashContent(mmdText);
}

/**
 * True when a recorded `derivedFrom` no longer matches the flow's content and
 * the layout must be regenerated. An empty `derivedFrom` is stale by
 * definition (an absent or hand-written sidecar carries no provenance).
 */
export function isLayoutStale(mmdText: string, derivedFrom: string | null | undefined): boolean {
  const recorded = String(derivedFrom ?? '').trim();
  if (!recorded) return true;
  return recorded.toLowerCase() !== derivedFromHash(mmdText);
}

export interface BuildSidecarOptions {
  /** Render-time orientation hint, recorded verbatim as `layoutHint` when set. */
  layoutHint?: string | null;
  /**
   * Positions to persist. Defaults to the positions derived from `graph`;
   * a re-derived layout is always collapsed against the graph so ids that
   * vanished upstream do not survive in the sidecar.
   */
  positions?: Record<string, FlowPoint>;
  /** Layout options used only when `positions` is omitted. */
  options?: Parameters<typeof layoutFlowGraph>[1];
}

/**
 * The orientation recorded in the sidecar: the verbatim render hint when one
 * was supplied, else the resolved mermaid keyword (the flow's own declaration,
 * or the top-down default). Always a mermaid keyword or '' — a flow with no
 * nodes has no orientation to record, so it always yields ''.
 */
export function resolveSidecarLayoutHint(graph: FlowGraph, layoutHint?: string | null): string {
  if (!graph || graph.nodes.length === 0) return '';
  const hint = String(layoutHint ?? '').trim();
  if (hint) return hint;
  return resolveFlowDirection(graph.direction, '');
}

/**
 * Construct the `design/flows/<name>.layout.json` sidecar for a flow: its
 * collapsed node positions, its layout hint, and the hash of the `.mmd` it was
 * derived from (SP-140 invariant 2).
 */
export function buildLayoutSidecar(
  graph: FlowGraph,
  mmdText: string,
  layoutOptions: BuildSidecarOptions = {},
): DesignLayoutSidecar {
  const positions = layoutOptions.positions ?? layoutFlowGraph(graph, layoutOptions.options);
  return {
    nodes: collapseLayout(positions, graph),
    layoutHint: resolveSidecarLayoutHint(graph, layoutOptions.layoutHint),
    derivedFrom: derivedFromHash(mmdText),
  };
}

/**
 * Read a persisted sidecar defensively: a malformed sidecar (wrong types,
 * stray keys, non-finite coordinates) degrades to an empty layout rather than
 * crashing the canvas. Returns null when the value is not an object.
 */
export function parseLayoutSidecar(value: unknown): DesignLayoutSidecar | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  const nodes: Record<string, FlowPoint> = {};
  const rawNodes = record.nodes;
  if (rawNodes && typeof rawNodes === 'object' && !Array.isArray(rawNodes)) {
    for (const [id, point] of Object.entries(rawNodes as Record<string, unknown>)) {
      if (isPoint(point)) nodes[id] = { x: point.x, y: point.y };
    }
  }
  return {
    nodes,
    layoutHint: typeof record.layoutHint === 'string' ? record.layoutHint : '',
    derivedFrom: typeof record.derivedFrom === 'string' ? record.derivedFrom : '',
  };
}

/** Parse sidecar JSON text; unparseable text degrades to null. */
export function parseLayoutSidecarText(text: string): DesignLayoutSidecar | null {
  if (!text) return null;
  try {
    return parseLayoutSidecar(JSON.parse(text));
  } catch {
    return null;
  }
}

export interface SidecarStaleness {
  /** True when the sidecar's `derivedFrom` no longer matches the flow. */
  stale: boolean;
  /** The flow's current content hash. */
  currentHash: string;
  /** The hash the sidecar recorded ('' when absent/unreadable). */
  recordedHash: string;
  /** True when the sidecar exists but the `.mmd` does not (nothing to derive). */
  orphaned: boolean;
  /** True when the orientation changed since the layout was recorded. */
  hintChanged: boolean;
}

/**
 * Full staleness picture for a flow/sidecar pair. Stale when the hash drifts,
 * when no hash was recorded, or when the recorded orientation still resolves
 * to a different mermaid keyword than the current one — a changed
 * `flow_layout` hint must re-lay-out the graph even though the `.mmd` text is
 * untouched.
 */
export function sidecarStaleness(
  mmdText: string | null | undefined,
  sidecar: DesignLayoutSidecar | null | undefined,
  graph: FlowGraph,
  layoutHint?: string | null,
): SidecarStaleness {
  const text = mmdText ?? '';
  const recordedHash = String(sidecar?.derivedFrom ?? '').trim();
  const currentHash = derivedFromHash(text);
  const recorded = String(sidecar?.layoutHint ?? '').trim();
  const hintChanged =
    graph.nodes.length > 0 &&
    normaliseLayoutHint(recorded) !== '' &&
    normaliseLayoutHint(recorded) !== resolveFlowDirection(graph.direction, layoutHint);
  return {
    stale: text === '' || recordedHash.toLowerCase() !== currentHash || hintChanged,
    currentHash,
    recordedHash,
    orphaned: text === '' && !!sidecar,
    hintChanged,
  };
}

/**
 * Decide what the canvas renders for a flow: reuse the recorded positions when
 * the sidecar is present and fresh (and covers every current node), otherwise
 * a freshly derived layout.
 */
export interface ResolvedFlowLayout {
  /** Positions for every node of `graph`. */
  positions: Record<string, FlowPoint>;
  /** The sidecar to persist — unchanged when fresh, rebuilt when stale. */
  sidecar: DesignLayoutSidecar;
  /** True when the positions were re-derived this pass. */
  regenerated: boolean;
}

export function resolveFlowLayout(
  mmdText: string,
  graph: FlowGraph,
  sidecar: DesignLayoutSidecar | null | undefined,
  layoutHint?: string | null,
  dimensions?: Record<string, Partial<FlowDimensions>>,
): ResolvedFlowLayout {
  const stored = sidecar?.nodes ?? {};
  const covered = graph.nodes.every((node) => isPoint(stored[node.id]));
  const staleness = sidecarStaleness(mmdText, sidecar, graph, layoutHint);
  const reuse = !!sidecar && !staleness.stale && covered;
  if (reuse) {
    return {
      positions: collapseLayout(stored, graph),
      sidecar: sidecar as DesignLayoutSidecar,
      regenerated: false,
    };
  }
  return {
    // The dimensions are what the canvas actually renders (wireframe-derived
    // sizes), so dagre must lay out against them: with the defaults a node
    // drawing a tall wireframe overlapped its neighbour and the edge between
    // them collapsed to a zero-height path.
    positions: layoutFlowGraph(graph, { layoutHint: layoutHint ?? undefined, dimensions }),
    sidecar: buildLayoutSidecar(graph, mmdText, { layoutHint }),
    regenerated: true,
  };
}
