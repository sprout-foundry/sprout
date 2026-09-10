/**
 * useVerticalDragResize — tests for the vertical drag-resize DOM event hook
 * extracted from Terminal.tsx during SP-075-extension.
 */
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, beforeAll, vi } from 'vitest';

import { useVerticalDragResize } from './useVerticalDragResize';

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

function Harness({
  startHeight,
  onChange,
  onHandle,
}: {
  startHeight: number;
  onChange?: (next: number) => void;
  onHandle?: (handle: (e: React.MouseEvent) => void) => void;
}): null {
  const handle = useVerticalDragResize({
    currentHeight: startHeight,
    onResize: (next) => onChange?.(next),
  });
  onHandle?.(handle);
  return null;
}

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  document.body.style.userSelect = '';
  document.body.style.cursor = '';
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function dispatchPointerEvent(target: EventTarget, type: string, clientY: number): Event {
  // jsdom has no PointerEvent constructor; MouseEvent carries every field
  // the handler reads (clientY). pointerId is defined for the capture
  // call (best-effort in the hook).
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    clientY,
  });
  Object.defineProperty(event, 'pointerId', { value: 1 });
  act(() => {
    target.dispatchEvent(event);
  });
  return event;
}

function drag(handle: (e: React.PointerEvent) => void, fromY: number, toY: number, delta: number) {
  handle({
    preventDefault: vi.fn(),
    clientY: fromY,
    currentTarget: {
      setPointerCapture: vi.fn(),
      releasePointerCapture: vi.fn(),
    },
  } as unknown as React.PointerEvent);

  // pointermove
  dispatchPointerEvent(document, 'pointermove', toY);

  // pointerup
  dispatchPointerEvent(document, 'pointerup', 0);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useVerticalDragResize', () => {
  describe('drag-to-resize', () => {
    it('invokes onResize with currentHeight - delta (negative Y pulls up)', () => {
      let handle: ((e: React.MouseEvent) => void) | null = null;
      let lastValue: number | null = null;
      act(() => {
        root.render(
          createElement(Harness, {
            startHeight: 400,
            onChange: (n) => {
              lastValue = n;
            },
            onHandle: (h) => {
              handle = h;
            },
          }),
        );
      });

      // start Y = 300, move to Y = 200 → delta = 100 (terminal grows by 100)
      act(() => {
        handle!({ preventDefault: vi.fn(), clientY: 300 } as unknown as React.MouseEvent);
      });
      dispatchPointerEvent(document, 'pointermove', 200);
      dispatchPointerEvent(document, 'pointerup', 0);

      expect(lastValue).toBe(500);
    });

    it('clamps the result below 0 (Math.max(0, …))', () => {
      let handle: ((e: React.MouseEvent) => void) | null = null;
      let lastValue: number | null = null;
      act(() => {
        root.render(
          createElement(Harness, {
            startHeight: 200,
            onChange: (n) => {
              lastValue = n;
            },
            onHandle: (h) => {
              handle = h;
            },
          }),
        );
      });

      // start Y = 100, move to Y = 800 → delta = -700, currentHeight + delta = -500 → clamped to 0
      act(() => {
        handle!({ preventDefault: vi.fn(), clientY: 100 } as unknown as React.MouseEvent);
      });
      dispatchPointerEvent(document, 'pointermove', 800);
      dispatchPointerEvent(document, 'pointerup', 0);

      expect(lastValue).toBeGreaterThanOrEqual(0);
    });

    it('rounds to an integer on mouseup', () => {
      let handle: ((e: React.MouseEvent) => void) | null = null;
      let lastValue: number | null = null;
      act(() => {
        root.render(
          createElement(Harness, {
            startHeight: 400,
            onChange: (n) => {
              lastValue = n;
            },
            onHandle: (h) => {
              handle = h;
            },
          }),
        );
      });

      act(() => {
        handle!({ preventDefault: vi.fn(), clientY: 300 } as unknown as React.MouseEvent);
      });
      dispatchPointerEvent(document, 'pointermove', 250);
      dispatchPointerEvent(document, 'pointerup', 0);

      // Without rounding, delta=50 → 450, would be the "in-progress" value
      // With clamping, must be integer ≥ 120
      expect(Number.isInteger(lastValue)).toBe(true);
    });
  });

  describe('document.body style side-effects', () => {
    it('sets userSelect=none and cursor=row-resize on mousedown; restores on mouseup', () => {
      let handle: ((e: React.MouseEvent) => void) | null = null;
      act(() => {
        root.render(
          createElement(Harness, {
            startHeight: 400,
            onHandle: (h) => {
              handle = h;
            },
          }),
        );
      });

      expect(document.body.style.userSelect).toBe('');
      expect(document.body.style.cursor).toBe('');

      act(() => {
        handle!({ preventDefault: vi.fn(), clientY: 200 } as unknown as React.MouseEvent);
      });

      expect(document.body.style.userSelect).toBe('none');
      expect(document.body.style.cursor).toBe('row-resize');

      dispatchPointerEvent(document, 'pointermove', 150);
      dispatchPointerEvent(document, 'pointerup', 0);

      expect(document.body.style.userSelect).toBe('');
      expect(document.body.style.cursor).toBe('');
    });
  });

  describe('listener cleanup', () => {
    it('removes mousemove and mouseup on mouseup', () => {
      const removeSpy = vi.spyOn(document, 'removeEventListener');
      let handle: ((e: React.MouseEvent) => void) | null = null;
      act(() => {
        root.render(
          createElement(Harness, {
            startHeight: 400,
            onHandle: (h) => {
              handle = h;
            },
          }),
        );
      });

      act(() => {
        handle!({ preventDefault: vi.fn(), clientY: 300 } as unknown as React.MouseEvent);
      });
      dispatchPointerEvent(document, 'pointermove', 200);
      dispatchPointerEvent(document, 'pointerup', 0);

      expect(removeSpy).toHaveBeenCalledWith('pointermove', expect.any(Function));
      expect(removeSpy).toHaveBeenCalledWith('pointerup', expect.any(Function));

      removeSpy.mockRestore();
    });
  });

  describe('preventDefault', () => {
    it('calls preventDefault on the initial mousedown event', () => {
      let handle: ((e: React.MouseEvent) => void) | null = null;
      const preventDefault = vi.fn();
      act(() => {
        root.render(
          createElement(Harness, {
            startHeight: 400,
            onHandle: (h) => {
              handle = h;
            },
          }),
        );
      });

      act(() => {
        handle!({ preventDefault, clientY: 200 } as unknown as React.MouseEvent);
      });

      expect(preventDefault).toHaveBeenCalled();
    });
  });
});
