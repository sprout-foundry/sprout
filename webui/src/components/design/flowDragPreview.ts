/**
 * Mid-drag node preview for the flow canvas (SP-140-3 §3b).
 *
 * React Flow tracks a node's drag position with a side effect: it moves the
 * wrapper element instead of re-rendering the node component through React
 * (the "store, not state" design, so a 500-node graph stays at 60fps). A node
 * that renders imagery therefore keeps its React-provided element until the
 * drag ends — the SVG refuses to follow the cursor.
 *
 * This module closes that gap without giving up React Flow's drag internals.
 * While a drag is in flight a `pointermove` listener (installed by
 * `FlowsCanvas`) translates the node's own element by the raw pointer delta
 * from the position the drag started at. React Flow's own wrapper translation
 * is *not* the source of truth here: it is rounded to whole pixels and
 * overwritten by its next store update, so reading it back would make the
 * preview stutter.
 *
 * The state lives in module scope rather than React state on purpose: the
 * drag path must not re-render the canvas (that is exactly what React Flow
 * avoids), and every write is idempotent. `dragPreviewOf` returns the shared
 * object so the caller never has to copy it back in.
 */

import type { FlowPoint } from '../../design/layout';

/** Pointer-anchored drag state for one node, in pane coordinates. */
export interface NodeDragPreview {
  /** Node element the preview translates. */
  element: HTMLElement;
  /** Pointer position when the drag started (node coordinates). */
  origin: FlowPoint;
  /** Pointer position when the drag started (viewport coordinates). */
  pointer: FlowPoint;
  /** Pointer offset inside the node at drag start (pane coordinates). */
  left?: number;
  /** Pointer offset inside the node at drag start (pane coordinates). */
  top?: number;
}

const previews = new Map<string, NodeDragPreview>();

/** Shared preview record for a node, created on first use. */
export function dragPreviewOf(nodeId: string): NodeDragPreview | undefined {
  const existing = previews.get(nodeId);
  if (existing) return existing;
  const element = nodeElementById(nodeId);
  if (!element) return undefined;
  const preview: NodeDragPreview = { element, origin: { x: 0, y: 0 }, pointer: { x: 0, y: 0 } };
  previews.set(nodeId, preview);
  return preview;
}

/** Drop a node's preview state — called when its drag ends. */
export function clearDragPreview(nodeId?: string): void {
  if (nodeId) previews.delete(nodeId);
  else previews.clear();
}

/** Clear React Flow's leftover wrapper transform after a drag settles. */
export function resetNodeTransform(nodeId: string): void {
  const element = nodeElementById(nodeId);
  if (element && element.style.transform) element.style.transform = '';
}

/** Translate a node by the pointer delta since its drag started. */
export function applyDragPreview(nodeId: string, clientX: number, clientY: number): void {
  const preview = previews.get(nodeId);
  if (!preview) return;
  const dx = clientX - preview.pointer.x;
  const dy = clientY - preview.pointer.y;
  const left = preview.origin.x + dx;
  const top = preview.origin.y + dy;
  preview.element.style.transform = `translate(${left}px, ${top}px)`;
}

/** A node's own element: the child of React Flow's positioned wrapper. */
function nodeElementById(nodeId: string): HTMLElement | null {
  if (typeof document === 'undefined') return null;
  const wrapper = document.querySelector(`.react-flow__node[data-id="${escapeAttribute(nodeId)}"]`);
  return wrapper?.firstElementChild instanceof HTMLElement ? wrapper.firstElementChild : null;
}

/** Minimal escaping for an attribute-selector value. */
function escapeAttribute(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}
