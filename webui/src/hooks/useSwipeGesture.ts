import { useEffect, useRef } from 'react';

/**
 * Configuration options for the swipe gesture hook.
 */
export interface UseSwipeGestureOptions {
  /** Callback fired when the user swipes left (finger moves from right to left). */
  onSwipeLeft?: () => void;
  /** Callback fired when the user swipes right (finger moves from left to right). */
  onSwipeRight?: () => void;
  /** Minimum horizontal distance (px) required to register as a swipe. Default: 50. */
  threshold?: number;
  /** Maximum vertical drift (px) allowed before the gesture is discarded as a scroll. Default: 100. */
  maxVerticalDrift?: number;
  /** Maximum swipe duration (ms) before the gesture is discarded as a slow drag. Default: 500. */
  maxDuration?: number;
  /** Whether the gesture listeners are active. Default: true. */
  enabled?: boolean;
  /**
   * Optional ref to an element to attach listeners on.
   * When omitted, listeners are attached to `document`.
   */
  targetRef?: React.RefObject<HTMLElement | null>;
}

/**
 * Custom hook that attaches touch-event listeners to detect horizontal swipe
 * gestures and fires the appropriate callback.
 *
 * - Only activates on touch-enabled devices (`'ontouchstart' in window`).
 * - Ignores multi-finger touches (single finger only).
 * - Filters out gestures whose vertical displacement exceeds `maxVerticalDrift`
 *   so that normal scrolling is not accidentally treated as a swipe.
 * - Ignores gestures slower than `maxDuration` to avoid slow drags.
 * - Cleans up all listeners on unmount.
 *
 * Returns nothing — this is a side-effect-only hook.
 */
export function useSwipeGesture(options: UseSwipeGestureOptions = {}): void {
  const {
    onSwipeLeft,
    onSwipeRight,
    threshold = 50,
    maxVerticalDrift = 100,
    maxDuration = 500,
    enabled = true,
    targetRef,
  } = options;

  // Keep the latest callbacks in refs so the listener closures don't need to
  // be recreated every render.
  const onSwipeLeftRef = useRef(onSwipeLeft);
  const onSwipeRightRef = useRef(onSwipeRight);
  onSwipeLeftRef.current = onSwipeLeft;
  onSwipeRightRef.current = onSwipeRight;

  useEffect(() => {
    // Early-out: not a touch device or explicitly disabled.
    if (!('ontouchstart' in window) || !enabled) {
      return;
    }

    const target = targetRef?.current ?? document;
    if (!target) {
      return;
    }

    let startX = 0;
    let startY = 0;
    let startTime = 0;
    let active = false;

    const handleTouchStart = (e: TouchEvent) => {
      // Only handle single-finger touches.
      if (e.touches.length !== 1) {
        return;
      }

      const touch = e.touches[0];
      startX = touch.clientX;
      startY = touch.clientY;
      startTime = Date.now();
      active = true;
    };

    const handleTouchMove = (e: TouchEvent) => {
      if (!active) {
        return;
      }

      // If the user lifts extra fingers, bail out.
      if (e.touches.length !== 1) {
        active = false;
        return;
      }
    };

    const handleTouchEnd = (e: TouchEvent) => {
      if (!active) {
        return;
      }

      active = false;

      // The touchend event reports changedTouches (finger(s) that just lifted);
      // use the last changed touch to compute the end position.
      if (e.changedTouches.length === 0) {
        return;
      }

      const touch = e.changedTouches[0];
      const endX = touch.clientX;
      const endY = touch.clientY;
      const elapsed = Date.now() - startTime;

      // Discard slow drags.
      if (elapsed > maxDuration) {
        return;
      }

      const dx = startX - endX; // positive = left, negative = right
      const dy = Math.abs(startY - endY);

      // Discard if vertical drift is too large (likely a scroll, not a swipe).
      if (dy > maxVerticalDrift) {
        return;
      }

      // Discard if horizontal distance is below the threshold.
      if (Math.abs(dx) < threshold) {
        return;
      }

      if (dx > 0 && onSwipeLeftRef.current) {
        // Swiped left (started right of where the finger ended).
        onSwipeLeftRef.current();
      } else if (dx < 0 && onSwipeRightRef.current) {
        // Swiped right (started left of where the finger ended).
        onSwipeRightRef.current();
      }
    };

    const handleTouchCancel = () => {
      // Browser cancelled the touch gesture (incoming call, overlay, etc.).
      // Reset active so a stray touchend won't trigger a phantom swipe.
      active = false;
    };

    const addEventListener = (
      type: string,
      listener: (e: TouchEvent) => void,
      options?: AddEventListenerOptions,
    ) => {
      (target as EventTarget).addEventListener(type, listener as EventListener, options);
    };

    const removeEventListener = (type: string, listener: (e: TouchEvent) => void) => {
      (target as EventTarget).removeEventListener(type, listener as EventListener);
    };

    addEventListener('touchstart', handleTouchStart, { passive: true });
    addEventListener('touchmove', handleTouchMove, { passive: true });
    addEventListener('touchend', handleTouchEnd, { passive: true });
    addEventListener('touchcancel', handleTouchCancel, { passive: true });

    return () => {
      removeEventListener('touchstart', handleTouchStart);
      removeEventListener('touchmove', handleTouchMove);
      removeEventListener('touchend', handleTouchEnd);
      removeEventListener('touchcancel', handleTouchCancel);
    };
  }, [enabled, threshold, maxVerticalDrift, maxDuration, targetRef]);
}
