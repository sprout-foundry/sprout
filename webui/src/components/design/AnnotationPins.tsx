/**
 * AnnotationPins — the pin layer over a rendered screen (SP-140-6 §6e,
 * drag-to-adjust per SP-140-7 §7e).
 *
 * Renders every annotation in the target's feedback file as a pin at its
 * normalized `at` coordinates (§4d: 0–1, resolution-independent), colored by
 * its critique-area, open annotations visually distinct from resolved ones.
 * A pin click focuses that annotation in the pane (`onSelect`). While
 * placement mode is armed (`placeMode`), the next click on the layer reports
 * the normalized point instead (`onPlace`).
 *
 * §7e drag: a pin is draggable — pointerdown on a pin starts the drag
 * (with pointer capture so fast pointers cannot outrun the small element);
 * pointerup on the layer reports the new normalized coordinates via
 * `onPinMove`, which the host persists to the feedback file. The gesture
 * maps to exactly one field — the coordinate pair — nothing else moves.
 *
 * Pure geometry + events; the feedback read/write stays with the pane
 * (DesignFeedbackResolution / the affordance). The layer is absolutely
 * positioned over the preview wrapper, which must be `position: relative`.
 */

import { MapPin } from 'lucide-react';
import { useRef, useState } from 'react';
import type { DesignFeedbackAnnotation } from '../../services/api/types';

export interface AnnotationPinsProps {
  /** The selected asset's workspace-relative path (the feedback target). */
  target: string;
  /** The annotations to pin (the target's feedback file's list). */
  annotations: DesignFeedbackAnnotation[];
  /** The annotation id focused in the pane, if any (highlight). */
  selectedId?: string | null;
  /** A pin was clicked — focus that annotation in the pane. */
  onSelect?: (id: string) => void;
  /** Placement mode armed: the next layer click reports its point. */
  placeMode?: boolean;
  /** A layer click in placement mode, normalized 0–1. */
  onPlace?: (at: { x: number; y: number }) => void;
  /** §7e: a dragged pin's new position, normalized 0–1. */
  onPinMove?: (id: string, at: { x: number; y: number }) => void;
}

/** The CSS class suffix for an annotation's area (slug-safe). */
function areaClass(area: string): string {
  return area ? ` pin-area-${area.replace(/[^a-z0-9-]/gi, '').toLowerCase()}` : '';
}

/** Normalized 0–1 for an axis, clamped against malformed values. */
function clamp01(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? Math.min(1, Math.max(0, value)) : 0.5;
}

function pct(value: number | undefined): string {
  return `${(clamp01(value) * 100).toFixed(2)}%`;
}

export default function AnnotationPins({
  target,
  annotations,
  selectedId,
  onSelect,
  placeMode = false,
  onPlace,
  onPinMove,
}: AnnotationPinsProps) {
  const layerRef = useRef<HTMLDivElement>(null);
  /** The pin being dragged, if any (pointer capture keeps events flowing). */
  const [draggingId, setDraggingId] = useState<string | null>(null);

  /** Normalized point for an event against the layer's box, or null. */
  const pointFrom = (event: React.PointerEvent | React.MouseEvent): { x: number; y: number } | null => {
    const rect = layerRef.current?.getBoundingClientRect();
    if (!rect || rect.width === 0 || rect.height === 0) return null;
    return {
      x: Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width)),
      y: Math.min(1, Math.max(0, (event.clientY - rect.top) / rect.height)),
    };
  };

  const handleLayerClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!placeMode || !onPlace) return;
    const point = pointFrom(event);
    if (point) onPlace(point);
  };

  const handleLayerPointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!draggingId) return;
    const id = draggingId;
    setDraggingId(null);
    const point = pointFrom(event);
    if (point) onPinMove?.(id, point);
  };

  // Drag completion lives on the PIN: pointer capture delivers pointerup /
  // pointercancel to the captured element, so an interrupted gesture clears
  // here rather than arming the layer for a stray commit later.
  const handlePinPointerUp = (event: React.PointerEvent<HTMLButtonElement>) => {
    if (!draggingId) return;
    const id = draggingId;
    setDraggingId(null);
    event.preventDefault(); // suppress the synthesized click after a drag
    const point = pointFrom(event);
    if (point) onPinMove?.(id, point);
  };

  const handlePinPointerCancel = () => {
    // An interrupted gesture (scroll, OS gesture) persists nothing.
    setDraggingId(null);
  };

  return (
    <div
      ref={layerRef}
      className={`design-pins${placeMode ? ' design-pins-placing' : ''}${draggingId ? ' design-pins-dragging' : ''}`}
      data-testid="design-pins"
      data-target={target}
      data-placing={placeMode}
      data-dragging={draggingId ?? ''}
      onClick={handleLayerClick}
      role={placeMode ? 'button' : undefined}
      aria-label={placeMode ? 'Click to place the annotation pin' : undefined}
    >
      {annotations.map((annotation) => (
        <button
          key={annotation.id}
          type="button"
          className={`design-pin${annotation.resolved ? ' design-pin-resolved' : ' design-pin-open'}${annotation.id === selectedId ? ' design-pin-selected' : ''}${areaClass(annotation.area)}`}
          style={{ left: pct(annotation.at?.x), top: pct(annotation.at?.y) }}
          data-testid={`design-pin-${annotation.id}`}
          data-area={annotation.area}
          data-resolved={annotation.resolved}
          title={`${annotation.area}: ${annotation.note}`}
          aria-label={`Annotation ${annotation.id} (${annotation.area})${annotation.resolved ? ', resolved' : ', open'}`}
          onClick={(event) => {
            // A pin click focuses the annotation; it must not also place a
            // new one when placement mode is armed. The click that ends a
            // drag is suppressed by preventDefault in handlePinPointerUp.
            event.stopPropagation();
            onSelect?.(annotation.id);
          }}
          onPointerDown={(event) => {
            if (!onPinMove) return;
            event.stopPropagation();
            (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
            setDraggingId(annotation.id);
          }}
          onPointerUp={handlePinPointerUp}
          onPointerCancel={handlePinPointerCancel}
        >
          <MapPin size={14} />
          <span className="design-pin-badge">{annotation.id}</span>
        </button>
      ))}
    </div>
  );
}
