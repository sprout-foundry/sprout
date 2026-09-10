import { useCallback, useRef } from 'react';
import { UI_SCALE_FACTOR, type UIScale } from './useUIScale';

/** Read the live UI tier from <html> (falls back to default off-DOM). */
function readUIScale(): UIScale {
  try {
    const v = document.documentElement.getAttribute('data-ui-scale');
    if (v === 'compact' || v === 'large' || v === 'xlarge' || v === 'default') return v;
  } catch {
    /* SSR/tests */
  }
  return 'default';
}

export interface UseVerticalDragResizeArgs {
  currentHeight: number;
  onResize: (next: number) => void;
  /** Extra length (px) to subtract from the window height as the upper bound. */
  maxFactor?: number;
  /** Apply a final rounding step on mouse-up (defaults to true). */
  roundOnDrop?: boolean;
}

/**
 * Returns a React.MouseEvent handler that begins a vertical drag-resize on
 * `mousedown`. The delta is applied to `currentHeight`, clamped via
 * `maxFactor`, and pushed to `onResize`. Mouse-up rounds to integer pixels
 * (toggleable) and clears the document cursor/user-select side effects.
 *
 * SP-075-extension: extracted from Terminal.tsx to reduce single-file
 * complexity. No behavior change.
 */
export function useVerticalDragResize(args: UseVerticalDragResizeArgs): (e: React.MouseEvent) => void {
  const { currentHeight, onResize, maxFactor = 100, roundOnDrop = true } = args;

  // Track the latest value pushed through onResize so mouseup can round the
  // up-to-date state (not the stale closure value at hook-call time).
  const latestRef = useRef(currentHeight);
  latestRef.current = currentHeight;

  const handleMove = useCallback(
    (ev: MouseEvent, startY: number, startHeight: number) => {
      // P4.5: the terminal's drag handle receives SCREEN px, but the
      // persisted height is LOGICAL (painted = logical x ui-scale when
      // the terminal portal carries the paint scale). Divide the delta
      // by the live factor so drags track the pointer 1:1 and stored
      // values stay tier-independent.
      const scale = UI_SCALE_FACTOR[readUIScale()];
      const delta = (startY - ev.clientY) / scale;
      const next = startHeight + delta;
      const windowedMax = typeof window === 'undefined' ? Infinity : window.innerHeight / scale - maxFactor;
      const clamped = Math.max(0, Math.min(windowedMax, next));
      latestRef.current = clamped;
      onResize(clamped);
    },
    [onResize, maxFactor],
  );

  return useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startY = e.clientY;
      const startHeight = currentHeight;

      const onMove = (ev: MouseEvent) => handleMove(ev, startY, startHeight);

      const onUp = () => {
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        if (roundOnDrop) {
          // Round the latest applied value (not the stale closure value).
          const rounded = Math.round(latestRef.current);
          latestRef.current = rounded;
          onResize(rounded);
        }
      };

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'row-resize';
    },
    [currentHeight, onResize, roundOnDrop, handleMove],
  );
}
