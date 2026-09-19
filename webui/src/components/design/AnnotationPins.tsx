/**
 * AnnotationPins — the pin layer over a rendered screen (SP-140-6 §6e).
 *
 * Renders every annotation in the target's feedback file as a pin at its
 * normalized `at` coordinates (§4d: 0–1, resolution-independent), colored by
 * its critique-area, open annotations visually distinct from resolved ones.
 * A pin click focuses that annotation in the pane (`onSelect`). While
 * placement mode is armed (`placeMode`), the next click on the layer reports
 * the normalized point instead (`onPlace`) — §6e's click-to-place replaces
 * the {0.5, 0.5} center default the §3e affordance wrote.
 *
 * Pure geometry + events; the feedback read/write stays with the pane
 * (DesignFeedbackResolution / the affordance). The layer is absolutely
 * positioned over the preview wrapper, which must be `position: relative`.
 */

import { MapPin } from 'lucide-react';
import { useRef } from 'react';
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
}

/** The CSS class suffix for an annotation's area (slug-safe). */
function areaClass(area: string): string {
  return area ? ` pin-area-${area.replace(/[^a-z0-9-]/gi, '').toLowerCase()}` : '';
}

/** Normalized 0–1 percent for an axis, clamped against malformed values. */
function pct(value: number | undefined): string {
  const v = typeof value === 'number' && Number.isFinite(value) ? Math.min(1, Math.max(0, value)) : 0.5;
  return `${(v * 100).toFixed(2)}%`;
}

export default function AnnotationPins({
  target,
  annotations,
  selectedId,
  onSelect,
  placeMode = false,
  onPlace,
}: AnnotationPinsProps) {
  const layerRef = useRef<HTMLDivElement>(null);

  const handleClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!placeMode || !onPlace) return;
    const rect = layerRef.current?.getBoundingClientRect();
    if (!rect || rect.width === 0 || rect.height === 0) return;
    const x = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    const y = Math.min(1, Math.max(0, (event.clientY - rect.top) / rect.height));
    onPlace({ x, y });
  };

  return (
    <div
      ref={layerRef}
      className={`design-pins${placeMode ? ' design-pins-placing' : ''}`}
      data-testid="design-pins"
      data-target={target}
      data-placing={placeMode}
      onClick={handleClick}
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
            // new one when placement mode is armed.
            event.stopPropagation();
            onSelect?.(annotation.id);
          }}
        >
          <MapPin size={14} />
          <span className="design-pin-badge">{annotation.id}</span>
        </button>
      ))}
    </div>
  );
}
