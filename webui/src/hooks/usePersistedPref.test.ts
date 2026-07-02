/**
 * usePersistedPref — tests for the localStorage-backed preference hooks
 * extracted from Terminal.tsx during SP-075-4h.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { useRef } from 'react';
import {
  usePersistedNumber,
  usePersistedBoolean,
  useOutsideClickDismiss,
} from './usePersistedPref';

// ---------------------------------------------------------------------------
// Harness component — lets us call hooks from tests.
// ---------------------------------------------------------------------------

function NumberHarness({
  storageKey,
  fallback,
  parse,
  clamp,
  onChange,
}: {
  storageKey: string;
  fallback: number;
  parse: (raw: string | null) => number;
  clamp: (value: number) => number;
  onChange?: (value: number, set: (next: number | ((p: number) => number)) => void) => void;
}) {
  const [value, setValue] = usePersistedNumber(storageKey, fallback, parse, clamp);
  onChange?.(value, setValue);
  return null;
}

function BooleanHarness({
  storageKey,
  fallback,
  onChange,
}: {
  storageKey: string;
  fallback: boolean;
  parse?: (raw: string | null, fallback: boolean) => boolean;
  onChange?: (value: boolean, set: (next: boolean | ((p: boolean) => boolean)) => void) => void;
}) {
  const [value, setValue] = usePersistedBoolean(
    storageKey,
    fallback,
    (raw, fb) => (raw === null ? fb : raw === 'true'),
  );
  onChange?.(value, setValue);
  return null;
}

function OutsideClickHarness({
  enabled,
  containerRef,
  onDismiss,
  onReady,
}: {
  enabled: boolean;
  containerRef: React.RefObject<HTMLDivElement | null>;
  onDismiss: () => void;
  onReady: () => void;
}) {
  useOutsideClickDismiss(enabled, containerRef, onDismiss);
  onReady();
  return createElement('div', { ref: containerRef, 'data-testid': 'harness' }, 'inside');
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  localStorage.clear();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  localStorage.clear();
});

describe('usePersistedNumber', () => {
  it('initializes with fallback when storage is empty', () => {
    let observed = -1;
    act(() => {
      root.render(
        createElement(NumberHarness, {
          storageKey: 'k1',
          fallback: 42,
          parse: (raw) => (raw ? Number(raw) : 42),
          clamp: (v) => v,
          onChange: (v) => {
            observed = v;
          },
        }),
      );
    });
    expect(observed).toBe(42);
  });

  it('initializes from localStorage when value is present', () => {
    localStorage.setItem('k1', '99');
    let observed = -1;
    act(() => {
      root.render(
        createElement(NumberHarness, {
          storageKey: 'k1',
          fallback: 42,
          parse: (raw) => (raw ? Number(raw) : 42),
          clamp: (v) => v,
          onChange: (v) => {
            observed = v;
          },
        }),
      );
    });
    expect(observed).toBe(99);
  });

  it('persists changes to localStorage on set', () => {
    let setValueRef: ((next: number | ((p: number) => number)) => void) | null = null;
    act(() => {
      root.render(
        createElement(NumberHarness, {
          storageKey: 'k1',
          fallback: 10,
          parse: (raw) => (raw ? Number(raw) : 10),
          clamp: (v) => v,
          onChange: (_v, set) => {
            setValueRef = set;
          },
        }),
      );
    });
    act(() => {
      setValueRef?.(77);
    });
    expect(localStorage.getItem('k1')).toBe('77');
  });

  it('clamps the value through the clamp function', () => {
    let observed = 0;
    act(() => {
      root.render(
        createElement(NumberHarness, {
          storageKey: 'k1',
          fallback: 0,
          parse: (raw) => (raw ? Number(raw) : 0),
          clamp: (v) => Math.max(0, Math.min(100, v)),
          onChange: (v) => {
            observed = v;
          },
        }),
      );
    });
    act(() => {
      // Re-render with new clamp output
    });
    let setValueRef: ((next: number | ((p: number) => number)) => void) | null = null;
    act(() => {
      root.render(
        createElement(NumberHarness, {
          storageKey: 'k1',
          fallback: 0,
          parse: (raw) => (raw ? Number(raw) : 0),
          clamp: (v) => Math.max(0, Math.min(100, v)),
          onChange: (v, set) => {
            observed = v;
            setValueRef = set;
          },
        }),
      );
    });
    act(() => {
      setValueRef?.(500);
    });
    expect(observed).toBe(100);
    expect(localStorage.getItem('k1')).toBe('100');
  });

  it('survives a localStorage quota error (does not throw)', () => {
    const setItemSpy = vi
      .spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new Error('quota');
      });
    let setValueRef: ((next: number | ((p: number) => number)) => void) | null = null;
    act(() => {
      root.render(
        createElement(NumberHarness, {
          storageKey: 'k1',
          fallback: 0,
          parse: (raw) => (raw ? Number(raw) : 0),
          clamp: (v) => v,
          onChange: (_v, set) => {
            setValueRef = set;
          },
        }),
      );
    });
    expect(() =>
      act(() => {
        setValueRef?.(1);
      }),
    ).not.toThrow();
    setItemSpy.mockRestore();
  });
});

describe('usePersistedBoolean', () => {
  it('initializes with fallback when storage is empty', () => {
    let observed: boolean | null = null;
    act(() => {
      root.render(
        createElement(BooleanHarness, {
          storageKey: 'b1',
          fallback: true,
          onChange: (v) => {
            observed = v;
          },
        }),
      );
    });
    expect(observed).toBe(true);
  });

  it('initializes from localStorage when present', () => {
    localStorage.setItem('b1', 'false');
    let observed: boolean | null = null;
    act(() => {
      root.render(
        createElement(BooleanHarness, {
          storageKey: 'b1',
          fallback: true,
          onChange: (v) => {
            observed = v;
          },
        }),
      );
    });
    expect(observed).toBe(false);
  });

  it('persists the boolean as "true" or "false"', () => {
    let setValueRef: ((next: boolean | ((p: boolean) => boolean)) => void) | null = null;
    act(() => {
      root.render(
        createElement(BooleanHarness, {
          storageKey: 'b1',
          fallback: false,
          onChange: (_v, set) => {
            setValueRef = set;
          },
        }),
      );
    });
    act(() => {
      setValueRef?.(true);
    });
    expect(localStorage.getItem('b1')).toBe('true');
    act(() => {
      setValueRef?.((prev) => !prev);
    });
    expect(localStorage.getItem('b1')).toBe('false');
  });
});

describe('useOutsideClickDismiss', () => {
  it('calls onDismiss when clicking outside the container', () => {
    const containerRef = { current: null as HTMLDivElement | null };
    const onDismiss = vi.fn();
    act(() => {
      root.render(
        createElement(OutsideClickHarness, {
          enabled: true,
          containerRef,
          onDismiss,
          onReady: () => {},
        }),
      );
    });

    // Simulate a click on the document body (outside the harness container)
    act(() => {
      const outside = document.createElement('div');
      document.body.appendChild(outside);
      outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      outside.remove();
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('does NOT call onDismiss when clicking inside the container', () => {
    const containerRef = { current: null as HTMLDivElement | null };
    const onDismiss = vi.fn();
    act(() => {
      root.render(
        createElement(OutsideClickHarness, {
          enabled: true,
          containerRef,
          onDismiss,
          onReady: () => {},
        }),
      );
    });
    const inside = container.querySelector('[data-testid="harness"]') as HTMLElement;
    expect(inside).not.toBeNull();
    act(() => {
      inside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('calls onDismiss when Escape is pressed', () => {
    const containerRef = { current: null as HTMLDivElement | null };
    const onDismiss = vi.fn();
    act(() => {
      root.render(
        createElement(OutsideClickHarness, {
          enabled: true,
          containerRef,
          onDismiss,
          onReady: () => {},
        }),
      );
    });
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('does not register listeners when enabled is false', () => {
    const containerRef = { current: null as HTMLDivElement | null };
    const onDismiss = vi.fn();
    act(() => {
      root.render(
        createElement(OutsideClickHarness, {
          enabled: false,
          containerRef,
          onDismiss,
          onReady: () => {},
        }),
      );
    });
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('removes listeners when unmounted', () => {
    const containerRef = { current: null as HTMLDivElement | null };
    const onDismiss = vi.fn();
    act(() => {
      root.render(
        createElement(OutsideClickHarness, {
          enabled: true,
          containerRef,
          onDismiss,
          onReady: () => {},
        }),
      );
    });
    act(() => {
      root.unmount();
    });
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(onDismiss).not.toHaveBeenCalled();
  });
});
