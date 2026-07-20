/**
 * useEditorCursor.test.ts — Unit tests for the useEditorCursor hook.
 *
 * Covers:
 * - Initial state (selectionInfo is null)
 * - Cursor move with no selection (single empty range)
 * - Single non-empty selection (charCount, selectionCount)
 * - Multiple selections (total charCount, selectionCount)
 * - No buffer (null or undefined) — cursor not persisted, selection still updated
 * - selectionSet=false — no side effects
 * - setSelectionInfo callable externally (e.g., file load reset)
 * - Column calculation (head - line.from)
 * - Error handling: lineAt throws → no crash, updateBufferCursor not called
 */
// @ts-nocheck — mock ViewUpdate/EditorState objects don't fully implement
// the CodeMirror interfaces; targeted imports are used for vitest globals
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the static import of the module under test
// ---------------------------------------------------------------------------

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// Static import — Vitest hoists vi.mock above all imports automatically
import { debugLog } from '../utils/log';
import { useEditorCursor } from './useEditorCursor';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface MockRange {
  from: number;
  to: number;
  empty?: boolean;
}

interface MockLine {
  number: number;
  from: number;
  to: number;
}

function createMockDoc(lines: MockLine[]) {
  return {
    lineAt: vi.fn((pos: number) => {
      // Find the line that contains this position (pos >= from && pos < to)
      const line = lines.find((l) => pos >= l.from && pos < l.to);
      return line ?? { number: -1, from: -1, to: -1 };
    }),
  };
}

function createMockUpdate(
  options: {
    selectionSet?: boolean;
    head?: number;
    ranges?: MockRange[];
    docLines?: MockLine[];
    throwOnLineAt?: boolean;
    mainNull?: boolean;
  } = {},
) {
  const { selectionSet = true, head = 0, ranges, docLines, throwOnLineAt = false, mainNull = false } = options;

  const doc = docLines ? createMockDoc(docLines) : createMockDoc([{ number: 1, from: 0, to: Infinity }]);

  if (throwOnLineAt) {
    doc.lineAt = vi.fn(() => {
      throw new Error('Simulated lineAt error');
    });
  }

  const computedRanges = ranges ?? [{ from: head, to: head, empty: true }];

  return {
    selectionSet,
    state: {
      selection: {
        main: mainNull ? null : { head },
        ranges: computedRanges,
      },
      doc,
    },
  };
}

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Render the hook inside a minimal wrapper component so React effects fire.
 * Returns control handles for making assertions.
 */
function renderTestHook(
  options: {
    bufferId?: string;
    docLines?: MockLine[];
  } = {},
) {
  const updateBufferCursor = vi.fn();
  const { bufferId = 'buf-1', docLines } = options;

  const bufferRef = {
    current: {
      id: bufferId,
      file: { path: '/test/file.ts' },
    },
  };

  // Mock CodeMirror view API ref — the hook reads `cmViewApiRef.current?.isExternalUpdate()`.
  // The default `isExternalUpdate: () => false` lets the cursor-update path run normally;
  // tests that exercise the gate pass a different mock here.
  const cmViewApiRef = {
    current: {
      isExternalUpdate: () => false,
    },
  };

  let hookReturn: any = null;

  function HookWrapper() {
    hookReturn = useEditorCursor({
      bufferRef,
      updateBufferCursor,
      cmViewApiRef,
    });
    return null;
  }

  act(() => {
    root.render(createElement(HookWrapper));
  });

  return {
    getReturn: () => hookReturn,
    updateBufferCursor,
    bufferRef,
    docLines: docLines ?? [{ number: 1, from: 0, to: Infinity }],
  };
}

// ---------------------------------------------------------------------------
// Tests: Initial state
// ---------------------------------------------------------------------------

describe('initial state', () => {
  it('selectionInfo is null on mount', () => {
    const { getReturn } = renderTestHook();

    expect(getReturn().selectionInfo).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: handleCursorUpdate — cursor move with no selection
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — cursor move (no selection)', () => {
  it('calls updateBufferCursor with correct line and column for a single empty range', () => {
    const { getReturn, updateBufferCursor, docLines } = renderTestHook();
    const head = 50;

    const update = createMockUpdate({
      head,
      ranges: [{ from: head, to: head, empty: true }],
      docLines,
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).toHaveBeenCalledWith('buf-1', { line: 1, column: 50 });
  });

  it('sets selectionInfo to null for a single empty range', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      ranges: [{ from: 10, to: 10, empty: true }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(getReturn().selectionInfo).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: handleCursorUpdate — single non-empty selection
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — single selection', () => {
  it('calls updateBufferCursor with correct line and column', () => {
    const { getReturn, updateBufferCursor, docLines } = renderTestHook();

    const update = createMockUpdate({
      head: 30,
      ranges: [{ from: 10, to: 30, empty: false }],
      docLines,
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).toHaveBeenCalledWith('buf-1', { line: 1, column: 30 });
  });

  it('sets selectionInfo with correct charCount and selectionCount', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 30,
      ranges: [{ from: 10, to: 30, empty: false }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 20, selectionCount: 1 });
  });
});

// ---------------------------------------------------------------------------
// Tests: handleCursorUpdate — multiple selections
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — multiple selections', () => {
  it('calls updateBufferCursor with cursor at main selection head', () => {
    const { getReturn, updateBufferCursor, docLines } = renderTestHook();

    const update = createMockUpdate({
      head: 50,
      ranges: [
        { from: 10, to: 20, empty: false },
        { from: 50, to: 60, empty: false },
        { from: 100, to: 105, empty: false },
      ],
      docLines,
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).toHaveBeenCalledWith('buf-1', { line: 1, column: 50 });
  });

  it('sets selectionInfo with total charCount and selectionCount for all ranges', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 50,
      ranges: [
        { from: 10, to: 20, empty: false }, // 10 chars
        { from: 50, to: 60, empty: false }, // 10 chars
        { from: 100, to: 105, empty: false }, // 5 chars
      ],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 25, selectionCount: 3 });
  });
});

// ---------------------------------------------------------------------------
// Tests: handleCursorUpdate — no buffer
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — no buffer', () => {
  it('does NOT call updateBufferCursor when bufferRef.current is null', () => {
    const { getReturn, updateBufferCursor, bufferRef } = renderTestHook();
    bufferRef.current = null;

    const update = createMockUpdate({ head: 10, ranges: [{ from: 10, to: 10, empty: true }] });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).not.toHaveBeenCalled();
  });

  it('does NOT call updateBufferCursor when bufferRef.current is undefined', () => {
    const { getReturn, updateBufferCursor, bufferRef } = renderTestHook();
    bufferRef.current = undefined;

    const update = createMockUpdate({ head: 10, ranges: [{ from: 10, to: 10, empty: true }] });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).not.toHaveBeenCalled();
  });

  it('still updates selectionInfo when bufferRef.current is null', () => {
    const { getReturn, bufferRef } = renderTestHook();
    bufferRef.current = null;

    const update = createMockUpdate({
      head: 30,
      ranges: [{ from: 10, to: 30, empty: false }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 20, selectionCount: 1 });
  });
});

// ---------------------------------------------------------------------------
// Tests: handleCursorUpdate — selectionSet=false
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — selectionSet=false', () => {
  it('does NOT call updateBufferCursor when selectionSet is false', () => {
    const { getReturn, updateBufferCursor } = renderTestHook();

    const update = createMockUpdate({
      selectionSet: false,
      head: 10,
      ranges: [{ from: 10, to: 10, empty: true }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).not.toHaveBeenCalled();
  });

  it('does NOT change selectionInfo when selectionSet is false', () => {
    const { getReturn } = renderTestHook();

    // First set some selection state
    act(() => {
      getReturn().handleCursorUpdate(
        createMockUpdate({
          head: 30,
          ranges: [{ from: 10, to: 30, empty: false }],
        }),
      );
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 20, selectionCount: 1 });

    // Now fire a non-selection-change update — state must NOT change
    act(() => {
      getReturn().handleCursorUpdate(
        createMockUpdate({
          selectionSet: false,
          head: 100,
          ranges: [{ from: 100, to: 100, empty: true }],
        }),
      );
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 20, selectionCount: 1 });
  });
});

// ---------------------------------------------------------------------------
// Tests: setSelectionInfo external reset
// ---------------------------------------------------------------------------

describe('setSelectionInfo — external reset', () => {
  it('calling setSelectionInfo externally updates state', () => {
    const { getReturn } = renderTestHook();

    expect(getReturn().selectionInfo).toBeNull();

    act(() => {
      getReturn().setSelectionInfo({ charCount: 42, selectionCount: 1 });
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 42, selectionCount: 1 });
  });

  it('calling setSelectionInfo with null clears selection state', () => {
    const { getReturn } = renderTestHook();

    // Set some state first
    act(() => {
      getReturn().setSelectionInfo({ charCount: 42, selectionCount: 1 });
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 42, selectionCount: 1 });

    // Reset to null (simulates file load)
    act(() => {
      getReturn().setSelectionInfo(null);
    });

    expect(getReturn().selectionInfo).toBeNull();
  });

  it('setSelectionInfo accepts a functional updater', () => {
    const { getReturn } = renderTestHook();

    act(() => {
      getReturn().setSelectionInfo({ charCount: 10, selectionCount: 1 });
    });

    act(() => {
      getReturn().setSelectionInfo((prev) => (prev ? { ...prev, charCount: 0 } : null));
    });

    expect(getReturn().selectionInfo).toEqual({ charCount: 0, selectionCount: 1 });
  });
});

// ---------------------------------------------------------------------------
// Tests: Column calculation
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — column calculation', () => {
  it('computes column as head - line.from (0-based offset within line)', () => {
    const { getReturn, updateBufferCursor } = renderTestHook({
      docLines: [
        { number: 1, from: 0, to: 40 },
        { number: 2, from: 40, to: 90 },
        { number: 3, from: 90, to: 150 },
      ],
    });

    // Cursor at pos 65, which is line 2 (from=40 to=90)
    const update = createMockUpdate({
      head: 65,
      ranges: [{ from: 65, to: 65, empty: true }],
      docLines: [
        { number: 1, from: 0, to: 40 },
        { number: 2, from: 40, to: 90 },
        { number: 3, from: 90, to: 150 },
      ],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // column = 65 - 40 = 25
    expect(updateBufferCursor).toHaveBeenCalledWith('buf-1', { line: 2, column: 25 });
  });

  it('computes column 0 when cursor is at line start', () => {
    const { getReturn, updateBufferCursor } = renderTestHook({
      docLines: [
        { number: 1, from: 0, to: 40 },
        { number: 2, from: 40, to: 90 },
      ],
    });

    const update = createMockUpdate({
      head: 40,
      ranges: [{ from: 40, to: 40, empty: true }],
      docLines: [
        { number: 1, from: 0, to: 40 },
        { number: 2, from: 40, to: 90 },
      ],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // column = 40 - 40 = 0
    expect(updateBufferCursor).toHaveBeenCalledWith('buf-1', { line: 2, column: 0 });
  });
});

// ---------------------------------------------------------------------------
// Tests: Error handling
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — error handling', () => {
  it('does not crash when lineAt throws', () => {
    const { getReturn, updateBufferCursor } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      ranges: [{ from: 10, to: 10, empty: true }],
      throwOnLineAt: true,
    });

    expect(() => {
      act(() => {
        getReturn().handleCursorUpdate(update);
      });
    }).not.toThrow();
  });

  it('does NOT call updateBufferCursor when lineAt throws', () => {
    const { getReturn, updateBufferCursor } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      ranges: [{ from: 10, to: 10, empty: true }],
      throwOnLineAt: true,
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).not.toHaveBeenCalled();
  });

  it('calls debugLog when lineAt throws', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      ranges: [{ from: 10, to: 10, empty: true }],
      throwOnLineAt: true,
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(debugLog).toHaveBeenCalled();
  });

  it('still updates selectionInfo when lineAt throws (selection processing is outside try/catch)', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 30,
      ranges: [{ from: 10, to: 30, empty: false }],
      throwOnLineAt: true,
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // Selection info update is outside the try/catch block
    expect(getReturn().selectionInfo).toEqual({ charCount: 20, selectionCount: 1 });
  });

  it('does not call updateBufferCursor when selection.main is null', () => {
    const { getReturn, updateBufferCursor } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      mainNull: true,
      ranges: [{ from: 10, to: 10, empty: true }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(updateBufferCursor).not.toHaveBeenCalled();
  });

  it('still updates selectionInfo when selection.main is null', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      mainNull: true,
      ranges: [{ from: 10, to: 10, empty: true }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(getReturn().selectionInfo).toBeNull();
  });

  it('does NOT call debugLog on successful cursor update', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({ head: 10, ranges: [{ from: 10, to: 10, empty: true }] });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    expect(debugLog).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Edge cases
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — edge cases', () => {
  it('handles a single range with empty=true but from !== to', () => {
    // This is an edge case: empty=true but the range has different from/to.
    // The hook checks `!ranges[0].empty`, so it should set selectionInfo to null.
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      ranges: [{ from: 5, to: 15, empty: true }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // empty=true means it falls through to the "no selection" branch
    expect(getReturn().selectionInfo).toBeNull();
  });

  it('handles zero-length selection (from === to, empty=false)', () => {
    // Another edge case: from === to but empty is explicitly false.
    // The hook computes charCount = to - from = 0.
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 10,
      ranges: [{ from: 10, to: 10, empty: false }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // !empty is true, so it goes into the single-selection branch
    // charCount = 10 - 10 = 0
    expect(getReturn().selectionInfo).toEqual({ charCount: 0, selectionCount: 1 });
  });

  it('handles multiple ranges where some are empty', () => {
    const { getReturn } = renderTestHook();

    const update = createMockUpdate({
      head: 50,
      ranges: [
        { from: 10, to: 20, empty: false }, // 10 chars
        { from: 50, to: 50, empty: true }, // 0 chars
        { from: 100, to: 105, empty: false }, // 5 chars
      ],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // 3 ranges → total chars = 10 + 0 + 5 = 15
    expect(getReturn().selectionInfo).toEqual({ charCount: 15, selectionCount: 3 });
  });
});

// ---------------------------------------------------------------------------
// Tests: lineAt caching optimization
// ---------------------------------------------------------------------------

describe('handleCursorUpdate — lineAt call count', () => {
  it('calls lineAt only once per update (cached in lineObj)', () => {
    const { getReturn } = renderTestHook({
      docLines: [{ number: 1, from: 0, to: Infinity }],
    });

    const update = createMockUpdate({
      head: 42,
      ranges: [{ from: 42, to: 42, empty: true }],
    });

    act(() => {
      getReturn().handleCursorUpdate(update);
    });

    // lineAt should be called exactly once (cached in lineObj)
    expect(update.state.doc.lineAt).toHaveBeenCalledTimes(1);
  });
});
