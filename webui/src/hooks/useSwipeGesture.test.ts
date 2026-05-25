/**
 * Tests for useSwipeGesture
 *
 * Covers:
 * - Swipe left/right detection
 * - Threshold, maxVerticalDrift, maxDuration constraints
 * - enabled flag
 * - custom targetRef
 * - non-touch device guard
 * - multi-touch ignored (touchstart and touchmove)
 * - callback updates without re-attaching listeners
 * - cleanup on unmount
 * - touchmove updates end position
 * - edge cases (zero distance, vertical-only, etc.)
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeAll, beforeEach, afterEach } from 'vitest';

import { useSwipeGesture } from './useSwipeGesture';

// ---------------------------------------------------------------------------
// DOM setup / teardown
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Touch event helpers
// ---------------------------------------------------------------------------

/** Create a mock Touch object with the given coordinates. */
function mockTouch(x: number, y: number, identifier = 0, target: EventTarget = document): Touch {
  return { clientX: x, clientY: y, identifier, target } as unknown as Touch;
}

/**
 * Create a synthetic TouchEvent-like Event.
 * `changedTouches` can differ from `touches` (e.g. on touchend where
 * `touches` is empty but `changedTouches` contains the released finger).
 */
function createTouchEvent(
  type: string,
  touches: Touch[],
  eventTarget: EventTarget = document,
  changedTouches?: Touch[],
): Event {
  const event = new Event(type, { bubbles: true });
  Object.defineProperty(event, 'touches', { value: touches });
  Object.defineProperty(event, 'changedTouches', {
    value: changedTouches ?? touches,
  });
  return event;
}

/** Dispatch a touch event inside act() to avoid React warnings. */
function fireTouchEvent(
  type: string,
  touches: Touch[],
  eventTarget: EventTarget = document,
  changedTouches?: Touch[],
): void {
  act(() => {
    eventTarget.dispatchEvent(createTouchEvent(type, touches, eventTarget, changedTouches));
  });
}

/**
 * Fire a complete swipe gesture (touchstart → touchend) using the default
 * target (document) and standard single-finger semantics.
 */
function fireSwipe(
  startX: number,
  startY: number,
  endX: number,
  endY: number,
  eventTarget: EventTarget = document,
): void {
  fireTouchEvent('touchstart', [mockTouch(startX, startY)], eventTarget);
  fireTouchEvent('touchend', [], eventTarget, [mockTouch(endX, endY)]);
}

// ---------------------------------------------------------------------------
// Hook wrapper component
// ---------------------------------------------------------------------------

interface HookRunnerProps {
  onSwipeLeft?: () => void;
  onSwipeRight?: () => void;
  enabled?: boolean;
  threshold?: number;
  maxVerticalDrift?: number;
  maxDuration?: number;
  targetRef?: React.RefObject<HTMLElement | null>;
}

function HookRunner({
  onSwipeLeft,
  onSwipeRight,
  enabled,
  threshold,
  maxVerticalDrift,
  maxDuration,
  targetRef,
}: HookRunnerProps): JSX.Element {
  useSwipeGesture({ onSwipeLeft, onSwipeRight, enabled, threshold, maxVerticalDrift, maxDuration, targetRef });
  return createElement('div');
}

// ---------------------------------------------------------------------------
// Helpers to control 'ontouchstart' in window
// ---------------------------------------------------------------------------

let originalOntouchstart: boolean | undefined;

function enableTouchSupport(): void {
  if (originalOntouchstart === undefined) {
    originalOntouchstart = 'ontouchstart' in window;
  }
  Object.defineProperty(window, 'ontouchstart', {
    value: true,
    writable: true,
    configurable: true,
  });
}

function disableTouchSupport(): void {
  delete (window as any).ontouchstart;
}

function restoreTouchSupport(): void {
  if (originalOntouchstart === true) {
    enableTouchSupport();
  } else {
    disableTouchSupport();
  }
}

// ---------------------------------------------------------------------------
// Non-touch device
// ---------------------------------------------------------------------------

describe('non-touch device', () => {
  beforeEach(() => {
    disableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('does not attach event listeners when ontouchstart is not in window', () => {
    const spyAdd = vi.spyOn(document, 'addEventListener');

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: vi.fn(), onSwipeRight: vi.fn() }));
    });

    expect(spyAdd).not.toHaveBeenCalledWith('touchstart', expect.any(Function), expect.any(Object));
    spyAdd.mockRestore();
  });

  it('does not fire callbacks even when touch events are dispatched', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    fireSwipe(100, 200, 200, 200);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Swipe right detection
// ---------------------------------------------------------------------------

describe('swipe right detection', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('calls onSwipeRight when swiping from left to right', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // Swipe right: start at x=100, end at x=200 (dx = -100, |dx| > threshold 50)
    fireSwipe(100, 200, 200, 200);

    expect(onRight).toHaveBeenCalledTimes(1);
    expect(onLeft).not.toHaveBeenCalled();
  });

  it('calls onSwipeRight with diagonal swipe that exceeds threshold horizontally', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // Slight diagonal: start (100, 200), end (210, 230) — dx=-110, dy=30
    fireSwipe(100, 200, 210, 230);

    expect(onRight).toHaveBeenCalledTimes(1);
    expect(onLeft).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Swipe left detection
// ---------------------------------------------------------------------------

describe('swipe left detection', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('calls onSwipeLeft when swiping from right to left', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // Swipe left: start at x=200, end at x=100 (dx = 100, |dx| > threshold 50)
    fireSwipe(200, 200, 100, 200);

    expect(onLeft).toHaveBeenCalledTimes(1);
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Below threshold
// ---------------------------------------------------------------------------

describe('below threshold', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('does not fire any callback when swipe distance is below the default threshold (50px)', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // dx = 30, which is < 50 threshold
    fireSwipe(100, 200, 130, 200);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('does not fire when swipe distance is exactly 0', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    fireSwipe(100, 200, 100, 200);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Custom threshold
// ---------------------------------------------------------------------------

describe('custom threshold', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('requires swipe distance >= custom threshold before firing callback', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          threshold: 100,
        }),
      );
    });

    // dx = 80 < threshold 100 → no callback
    fireSwipe(100, 200, 180, 200);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('fires callback when swipe distance equals custom threshold exactly', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          threshold: 100,
        }),
      );
    });

    // dx = 100 == threshold → should fire (|dx| >= threshold)
    fireSwipe(100, 200, 200, 200);

    expect(onRight).toHaveBeenCalledTimes(1);
  });

  it('fires callback when swipe distance exceeds custom threshold', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          threshold: 100,
        }),
      );
    });

    // dx = 150 > threshold 100
    fireSwipe(100, 200, 250, 200);

    expect(onRight).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Vertical drift too high
// ---------------------------------------------------------------------------

describe('vertical drift too high', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('does not fire when vertical drift exceeds maxVerticalDrift', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          maxVerticalDrift: 50,
        }),
      );
    });

    // dx = 100 (passes threshold), dy = 80 (exceeds maxVerticalDrift 50)
    fireSwipe(100, 200, 200, 280);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('fires when vertical drift equals maxVerticalDrift exactly', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          maxVerticalDrift: 50,
        }),
      );
    });

    // dx = 100, dy = 50 (== maxVerticalDrift, should pass since dy <= maxVerticalDrift)
    fireSwipe(100, 200, 200, 250);

    expect(onRight).toHaveBeenCalledTimes(1);
  });

  it('fires when vertical drift is within maxVerticalDrift', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          maxVerticalDrift: 50,
        }),
      );
    });

    // dx = 100, dy = 30 (< maxVerticalDrift 50)
    fireSwipe(100, 200, 200, 230);

    expect(onRight).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Too slow (duration exceeded)
// ---------------------------------------------------------------------------

describe('too slow (duration exceeded)', () => {
  beforeEach(() => {
    enableTouchSupport();
    vi.useFakeTimers();
  });

  afterEach(() => {
    restoreTouchSupport();
    vi.useRealTimers();
  });

  it('does not fire when swipe takes longer than maxDuration', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          maxDuration: 300,
        }),
      );
    });

    fireTouchEvent('touchstart', [mockTouch(100, 200)]);

    // Advance time beyond maxDuration
    act(() => {
      vi.advanceTimersByTime(400);
    });

    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('fires when swipe completes within maxDuration', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          maxDuration: 300,
        }),
      );
    });

    fireTouchEvent('touchstart', [mockTouch(100, 200)]);

    // Advance time within maxDuration
    act(() => {
      vi.advanceTimersByTime(200);
    });

    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onRight).toHaveBeenCalledTimes(1);
  });

  it('does not fire when swipe exceeds maxDuration by exactly 1ms', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          maxDuration: 300,
        }),
      );
    });

    fireTouchEvent('touchstart', [mockTouch(100, 200)]);

    // elapsed > maxDuration (301 > 300) → should not fire
    act(() => {
      vi.advanceTimersByTime(301);
    });

    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Disabled via enabled=false
// ---------------------------------------------------------------------------

describe('disabled via enabled=false', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('does not attach listeners or fire callbacks when enabled=false', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    const spyAdd = vi.spyOn(document, 'addEventListener');

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          enabled: false,
        }),
      );
    });

    expect(spyAdd).not.toHaveBeenCalledWith('touchstart', expect.any(Function), expect.any(Object));
    spyAdd.mockRestore();

    fireSwipe(100, 200, 200, 200);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Custom targetRef
// ---------------------------------------------------------------------------

describe('custom targetRef', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('attaches listeners to the referenced element instead of document', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();
    const targetRef = { current: container as HTMLDivElement };

    const spyDocAdd = vi.spyOn(document, 'addEventListener');
    const spyContainerAdd = vi.spyOn(container, 'addEventListener');

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          targetRef,
        }),
      );
    });

    // Should NOT attach to document
    expect(spyDocAdd).not.toHaveBeenCalledWith('touchstart', expect.any(Function), expect.any(Object));
    // Should attach to the target element
    expect(spyContainerAdd).toHaveBeenCalledWith('touchstart', expect.any(Function), expect.any(Object));
    expect(spyContainerAdd).toHaveBeenCalledWith('touchmove', expect.any(Function), expect.any(Object));
    expect(spyContainerAdd).toHaveBeenCalledWith('touchend', expect.any(Function), expect.any(Object));

    spyDocAdd.mockRestore();
    spyContainerAdd.mockRestore();
  });

  it('dispatches callbacks when touch events target the custom element', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();
    const targetRef = { current: container as HTMLDivElement };

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          targetRef,
        }),
      );
    });

    fireSwipe(100, 200, 200, 200, container);

    expect(onRight).toHaveBeenCalledTimes(1);
  });

  it('does not fire when touch events are dispatched on document instead of the target element', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();
    const targetRef = { current: container as HTMLDivElement };

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          targetRef,
        }),
      );
    });

    // Fire events on document — listeners are on container, not document
    fireSwipe(100, 200, 200, 200, document);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('removes listeners from the target element on unmount', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();
    const targetRef = { current: container as HTMLDivElement };

    const spyRemove = vi.spyOn(container, 'removeEventListener');

    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          targetRef,
        }),
      );
    });

    act(() => {
      root.unmount();
    });

    expect(spyRemove).toHaveBeenCalledWith('touchstart', expect.any(Function));
    expect(spyRemove).toHaveBeenCalledWith('touchmove', expect.any(Function));
    expect(spyRemove).toHaveBeenCalledWith('touchend', expect.any(Function));

    spyRemove.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Cleanup on unmount (default target = document)
// ---------------------------------------------------------------------------

describe('cleanup on unmount', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('removes all event listeners from document on unmount', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    const spyRemove = vi.spyOn(document, 'removeEventListener');

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    act(() => {
      root.unmount();
    });

    expect(spyRemove).toHaveBeenCalledWith('touchstart', expect.any(Function));
    expect(spyRemove).toHaveBeenCalledWith('touchmove', expect.any(Function));
    expect(spyRemove).toHaveBeenCalledWith('touchend', expect.any(Function));

    spyRemove.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Multi-touch ignored
// ---------------------------------------------------------------------------

describe('multi-touch ignored', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('ignores touchstart when more than one touch is present', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // Two-finger touchstart — active flag never set
    fireTouchEvent('touchstart', [mockTouch(100, 200, 0), mockTouch(150, 200, 1)]);
    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('cancels active swipe when a second finger touches during touchmove', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // Single-finger start → active = true
    fireTouchEvent('touchstart', [mockTouch(100, 200)]);

    // Second finger touches during move → active = false
    fireTouchEvent('touchmove', [mockTouch(150, 200, 0), mockTouch(180, 220, 1)]);

    // End — active is false, so nothing fires
    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });

  it('handles touchend with no changedTouches gracefully', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    fireTouchEvent('touchstart', [mockTouch(100, 200)]);

    // touchend with zero changedTouches — hook should not crash
    fireTouchEvent('touchend', [], document, []);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Callbacks update without re-attaching listeners
// ---------------------------------------------------------------------------

describe('callbacks update without re-attaching listeners', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('calls the latest onSwipeRight callback after re-render', () => {
    const callback1 = vi.fn();
    const callback2 = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: callback1, onSwipeRight: callback1 }));
    });

    // Re-render with new callbacks
    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: callback2, onSwipeRight: callback2 }));
    });

    fireSwipe(100, 200, 200, 200);

    // callback2 (the latest) should be called, not callback1
    expect(callback1).not.toHaveBeenCalled();
    expect(callback2).toHaveBeenCalledTimes(1);
  });

  it('calls the latest onSwipeLeft callback after re-render', () => {
    const callback1 = vi.fn();
    const callback2 = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: callback1, onSwipeRight: vi.fn() }));
    });

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: callback2, onSwipeRight: vi.fn() }));
    });

    // Swipe left: start at x=200, end at x=100
    fireSwipe(200, 200, 100, 200);

    expect(callback1).not.toHaveBeenCalled();
    expect(callback2).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Event listener registration on document (default target)
// ---------------------------------------------------------------------------

describe('event listener registration', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('attaches touchstart, touchmove, and touchend listeners to document by default', () => {
    const spyAdd = vi.spyOn(document, 'addEventListener');

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: vi.fn(), onSwipeRight: vi.fn() }));
    });

    expect(spyAdd).toHaveBeenCalledWith('touchstart', expect.any(Function), expect.any(Object));
    expect(spyAdd).toHaveBeenCalledWith('touchmove', expect.any(Function), expect.any(Object));
    expect(spyAdd).toHaveBeenCalledWith('touchend', expect.any(Function), expect.any(Object));

    spyAdd.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// touchmove updates end position
// ---------------------------------------------------------------------------

describe('touchmove and touchend interaction', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('uses changedTouches from touchend for final position regardless of touchmove', () => {
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeRight: onRight, onSwipeLeft: vi.fn() }));
    });

    // Start at 100, move to 250, end at 250
    fireTouchEvent('touchstart', [mockTouch(100, 200)]);
    fireTouchEvent('touchmove', [mockTouch(250, 200)]);
    fireTouchEvent('touchend', [], document, [mockTouch(250, 200)]);

    expect(onRight).toHaveBeenCalledTimes(1);
  });

  it('ignores touchmove when not currently swiping', () => {
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeRight: onRight }));
    });

    // Fire touchmove without prior touchstart — active is false, so ignored
    fireTouchEvent('touchmove', [mockTouch(200, 200)]);
    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onRight).not.toHaveBeenCalled();
  });

  it('ignores touchend when not currently swiping (no prior touchstart)', () => {
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeRight: onRight }));
    });

    fireTouchEvent('touchend', [], document, [mockTouch(200, 200)]);

    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Vertical swipes (default horizontal direction)
// ---------------------------------------------------------------------------

describe('vertical swipe behavior', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('does not fire callbacks for pure vertical swipes', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // Pure vertical swipe (dx=0, dy=150)
    fireSwipe(200, 100, 200, 250);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Only one of left/right fires at a time
// ---------------------------------------------------------------------------

describe('swipe direction exclusivity', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('only fires onSwipeLeft, not onSwipeRight, for a left swipe', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    fireSwipe(300, 200, 100, 200);

    expect(onLeft).toHaveBeenCalledTimes(1);
    expect(onRight).not.toHaveBeenCalled();
  });

  it('only fires onSwipeRight, not onSwipeLeft, for a right swipe', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    fireSwipe(100, 200, 300, 200);

    expect(onRight).toHaveBeenCalledTimes(1);
    expect(onLeft).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// enabled toggling (true → false → true)
// ---------------------------------------------------------------------------

describe('enabled toggling', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('correctly handles enabled: true → false → true re-renders', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    // enabled: true — swipe should fire
    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          enabled: true,
        }),
      );
    });

    fireSwipe(100, 200, 200, 200);
    expect(onRight).toHaveBeenCalledTimes(1);

    // Re-render with enabled: false — swipe should NOT fire
    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          enabled: false,
        }),
      );
    });

    fireSwipe(100, 200, 200, 200);
    expect(onRight).toHaveBeenCalledTimes(1); // still 1, no new call

    // Re-render with enabled: true — swipe should fire again
    act(() => {
      root.render(
        createElement(HookRunner, {
          onSwipeLeft: onLeft,
          onSwipeRight: onRight,
          enabled: true,
        }),
      );
    });

    fireSwipe(100, 200, 200, 200);
    expect(onRight).toHaveBeenCalledTimes(2);
  });
});

// ---------------------------------------------------------------------------
// touchcancel resets active state
// ---------------------------------------------------------------------------

describe('touchcancel resets active state', () => {
  beforeEach(() => {
    enableTouchSupport();
  });

  afterEach(() => {
    restoreTouchSupport();
  });

  it('prevents phantom swipe when touchcancel fires before touchend', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();

    act(() => {
      root.render(createElement(HookRunner, { onSwipeLeft: onLeft, onSwipeRight: onRight }));
    });

    // touchstart → active = true
    fireTouchEvent('touchstart', [mockTouch(100, 200)]);

    // touchcancel → active = false (browser interrupted: incoming call, overlay, etc.)
    fireTouchEvent('touchcancel', []);

    // touchend at a position that would normally trigger a swipe — should NOT fire
    fireTouchEvent('touchend', [], document, [mockTouch(250, 200)]);

    expect(onLeft).not.toHaveBeenCalled();
    expect(onRight).not.toHaveBeenCalled();
  });
});
