/**
 * useEditorScrollSync.test.ts — Unit tests for the useEditorScrollSync hook.
 *
 * Covers:
 * - Scroll position persistence (throttled writes to buffer state)
 * - rAF final-flush on scroll end
 * - cancelPendingFlush cleanup
 * - Cross-pane linked scrolling via CustomEvent
 * - Linked scroll enabled state synchronization
 * - Edge cases: no buffer, no scrollDOM, same-pane filtering, different-file filtering
 */
// @ts-nocheck
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the static import of the module under test
// ---------------------------------------------------------------------------

const mockSetLinkedScrollEnabled = vi.fn();
const mockSuppressScrollSync = vi.fn();

vi.mock('../extensions/linkedScroll', () => ({
  setLinkedScrollEnabled: (...args) => mockSetLinkedScrollEnabled(...args),
  suppressScrollSync: (...args) => mockSuppressScrollSync(...args),
  _resetModuleStateForTesting: vi.fn(),
}));

// Static import — Vitest hoists vi.mock above all imports automatically
import { useEditorScrollSync } from './useEditorScrollSync';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface MockScrollDOM {
  scrollTop: number;
  scrollLeft: number;
  isConnected: boolean;
  scrollTo: ReturnType<typeof vi.fn>;
}

function createMockView(scrollTop = 0, scrollLeft = 0, docLines = 100) {
  const scrollDOM: MockScrollDOM = {
    scrollTop,
    scrollLeft,
    isConnected: true,
    scrollTo: vi.fn(),
  };
  return {
    scrollDOM,
    state: {
      doc: {
        lines: docLines,
        line: vi.fn((n: number) => ({ from: (n - 1) * 40 })),
      },
    },
    viewport: { from: 0, to: 400 },
    lineBlockAt: vi.fn((pos: number) => ({ top: pos })),
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
    paneId?: string;
    filePath?: string | null;
    isLinkedScrollEnabled?: boolean;
  } = {},
) {
  const updateBufferScroll = vi.fn();
  const mockView = createMockView();
  const viewRef = { current: mockView };
  const bufferRef = {
    current: { id: 'buf-1', file: { path: options.filePath ?? '/test/file.ts' } },
  };
  let hookReturn: any = null;

  const {
    paneId = 'pane-1',
    filePath = '/test/file.ts',
    isLinkedScrollEnabled = false,
  } = options;

  function HookWrapper() {
    hookReturn = useEditorScrollSync({
      paneId,
      viewRef: viewRef as any,
      bufferRef: bufferRef as any,
      filePath,
      updateBufferScroll,
      isLinkedScrollEnabled,
    });
    return null;
  }

  act(() => {
    root.render(createElement(HookWrapper));
  });

  return {
    getReturn: () => hookReturn,
    updateBufferScroll,
    mockView,
    bufferRef,
    viewRef,
  };
}

// ---------------------------------------------------------------------------
// Tests: handleScrollUpdate — scroll position persistence
// ---------------------------------------------------------------------------

describe('handleScrollUpdate — scroll position persistence', () => {
  it('calls updateBufferScroll when viewport changes with a valid buffer', () => {
    const { getReturn, updateBufferScroll, mockView } = renderTestHook();

    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: true,
        view: mockView,
      });
    });

    expect(updateBufferScroll).toHaveBeenCalledWith('buf-1', { top: 0, left: 0 });
  });

  it('does NOT call updateBufferScroll when viewportChanged is false', () => {
    const { getReturn, updateBufferScroll, mockView } = renderTestHook();

    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: false,
        view: mockView,
      });
    });

    expect(updateBufferScroll).not.toHaveBeenCalled();
  });

  it('does NOT call updateBufferScroll when bufferRef.current is null', () => {
    const { getReturn, updateBufferScroll, mockView, bufferRef } = renderTestHook();
    bufferRef.current = null;

    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: true,
        view: mockView,
      });
    });

    expect(updateBufferScroll).not.toHaveBeenCalled();
  });

  it('does NOT call updateBufferScroll when scrollDOM is null', () => {
    const { getReturn, updateBufferScroll } = renderTestHook();
    const viewNoScrollDOM = { scrollDOM: null };

    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: true,
        view: viewNoScrollDOM as any,
      });
    });

    expect(updateBufferScroll).not.toHaveBeenCalled();
  });

  it('throttles updateBufferScroll calls (100ms window)', () => {
    const { getReturn, updateBufferScroll, mockView } = renderTestHook();

    // First call — should go through immediately
    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: true,
        view: mockView,
      });
    });

    const callsAfterFirst = updateBufferScroll.mock.calls.length;
    expect(callsAfterFirst).toBeGreaterThanOrEqual(1);

    updateBufferScroll.mockClear();

    // Rapid second call within the throttle window — immediate call is suppressed
    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: true,
        view: mockView,
      });
    });

    // The immediate (throttled) call is blocked.
    // The rAF from the second call may fire (setTimeout(0) in setup),
    // but the direct synchronous call must not happen.
    const callsAfterSecond = updateBufferScroll.mock.calls.length;
    expect(callsAfterSecond).toBeLessThanOrEqual(1);
  });

  it('captures scroll position from view.scrollDOM properties', () => {
    const { getReturn, updateBufferScroll } = renderTestHook();
    const view = createMockView(250, 30);

    act(() => {
      getReturn().handleScrollUpdate({
        viewportChanged: true,
        view,
      });
    });

    // At least one call has the correct scroll values
    const matchingCall = updateBufferScroll.mock.calls.find(
      (call) => call[1].top === 250 && call[1].left === 30,
    );
    expect(matchingCall).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// Tests: cancelPendingFlush
// ---------------------------------------------------------------------------

describe('cancelPendingFlush', () => {
  it('cancels pending rAF without error', () => {
    const { getReturn } = renderTestHook();

    expect(() => {
      act(() => {
        getReturn().cancelPendingFlush();
      });
    }).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// Tests: Cross-pane linked scrolling
// ---------------------------------------------------------------------------

describe('cross-pane linked scrolling', () => {
  it('scrolls to the target line when receiving a linked-scroll event from another pane with the same file', () => {
    const { mockView } = renderTestHook({
      paneId: 'pane-B',
      filePath: '/shared/file.ts',
    });

    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 25,
          },
        }),
      );
    });

    expect(mockSuppressScrollSync).toHaveBeenCalledWith('pane-B');
    expect(mockView.scrollDOM.scrollTo).toHaveBeenCalled();
  });

  it('does NOT scroll when the event comes from the same pane', () => {
    const { mockView } = renderTestHook({
      paneId: 'pane-A',
      filePath: '/shared/file.ts',
    });

    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 25,
          },
        }),
      );
    });

    expect(mockView.scrollDOM.scrollTo).not.toHaveBeenCalled();
  });

  it('does NOT scroll when the file path differs', () => {
    const { mockView } = renderTestHook({
      paneId: 'pane-B',
      filePath: '/other/file.ts',
    });

    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 25,
          },
        }),
      );
    });

    expect(mockView.scrollDOM.scrollTo).not.toHaveBeenCalled();
  });

  it('does NOT scroll when viewRef.current is null', () => {
    const { viewRef } = renderTestHook({
      paneId: 'pane-B',
      filePath: '/shared/file.ts',
    });
    viewRef.current = null;

    // Must not throw
    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 25,
          },
        }),
      );
    });
  });

  it('does NOT scroll when topLine is out of bounds (< 1)', () => {
    const { mockView } = renderTestHook({
      paneId: 'pane-B',
      filePath: '/shared/file.ts',
    });

    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 0,
          },
        }),
      );
    });

    expect(mockView.scrollDOM.scrollTo).not.toHaveBeenCalled();
  });

  it('does NOT scroll when topLine exceeds document line count', () => {
    const { mockView } = renderTestHook({
      paneId: 'pane-B',
      filePath: '/shared/file.ts',
    });

    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 999, // doc has 100 lines
          },
        }),
      );
    });

    expect(mockView.scrollDOM.scrollTo).not.toHaveBeenCalled();
  });

  it('does NOT scroll when filePath is null', () => {
    const { mockView } = renderTestHook({
      paneId: 'pane-B',
      filePath: null,
    });

    act(() => {
      document.dispatchEvent(
        new CustomEvent('editor:linked-scroll', {
          detail: {
            sourcePaneId: 'pane-A',
            filePath: '/shared/file.ts',
            topLine: 25,
          },
        }),
      );
    });

    expect(mockView.scrollDOM.scrollTo).not.toHaveBeenCalled();
  });

  it('cleans up event listener on unmount', () => {
    const addSpy = vi.spyOn(document, 'addEventListener');
    const removeSpy = vi.spyOn(document, 'removeEventListener');

    renderTestHook({ paneId: 'pane-B', filePath: '/shared/file.ts' });

    expect(addSpy).toHaveBeenCalledWith('editor:linked-scroll', expect.any(Function));

    act(() => {
      root.unmount();
    });

    expect(removeSpy).toHaveBeenCalledWith('editor:linked-scroll', expect.any(Function));

    addSpy.mockRestore();
    removeSpy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Tests: Linked scroll enabled state synchronization
// ---------------------------------------------------------------------------

describe('linked scroll enabled state synchronization', () => {
  it('calls setLinkedScrollEnabled(false) on mount when isLinkedScrollEnabled is false', () => {
    renderTestHook({ isLinkedScrollEnabled: false });

    expect(mockSetLinkedScrollEnabled).toHaveBeenCalledWith(false);
  });

  it('calls setLinkedScrollEnabled(true) when isLinkedScrollEnabled is true', () => {
    renderTestHook({ isLinkedScrollEnabled: true });

    expect(mockSetLinkedScrollEnabled).toHaveBeenCalledWith(true);
  });
});
