// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockFreeze = jest.fn();
const mockResume = jest.fn();
const mockGetInstance = jest.fn();
const mockFreezeAll = jest.fn();
const mockResumeAll = jest.fn();

jest.mock('../services/websocket', () => {
  class MockWebSocketService {
    static getInstance = mockGetInstance;
    freeze = mockFreeze;
    resume = mockResume;
    connect = jest.fn();
    disconnect = jest.fn();
  }
  return { WebSocketService: MockWebSocketService };
});

jest.mock('../services/terminalWebSocket', () => {
  class MockTerminalWebSocketService {
    static createInstance = jest.fn();
    static getInstance = jest.fn();
    static freezeAll = mockFreezeAll;
    static resumeAll = mockResumeAll;
    static registerInstance = jest.fn();
    static unregisterInstance = jest.fn();
    static instances = new Set();
    freeze = jest.fn();
    resume = jest.fn();
    connect = jest.fn();
    disconnect = jest.fn();
  }
  return { TerminalWebSocketService: MockTerminalWebSocketService };
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  jest.useFakeTimers();
  jest.clearAllMocks();

  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);

  // Reset document visibility to visible
  Object.defineProperty(document, 'visibilityState', {
    value: 'visible',
    writable: true,
    configurable: true,
  });

  // Default mock: getInstance returns an object with freeze/resume
  mockGetInstance.mockReturnValue({ freeze: mockFreeze, resume: mockResume });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  jest.useRealTimers();
});

function fireVisibilityChange(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', {
    value: state,
    writable: true,
    configurable: true,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

/** Wrapper component that invokes the usePageVisibility hook. */
function HookRunner() {
  const { usePageVisibility } = require('./usePageVisibility');
  usePageVisibility();
  return null;
}

// ---------------------------------------------------------------------------
// Tests: isPageVisible
// ---------------------------------------------------------------------------

describe('isPageVisible', () => {
  let isPageVisible: () => boolean;

  beforeAll(() => {
    const mod = require('./usePageVisibility');
    isPageVisible = mod.isPageVisible;
  });

  it('returns true when document is visible', () => {
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      writable: true,
      configurable: true,
    });
    expect(isPageVisible()).toBe(true);
  });

  it('returns false when document is hidden', () => {
    Object.defineProperty(document, 'visibilityState', {
      value: 'hidden',
      writable: true,
      configurable: true,
    });
    expect(isPageVisible()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Tests: usePageVisibility hook
// ---------------------------------------------------------------------------

describe('usePageVisibility', () => {
  it('does not call freeze/resume on mount with visible page', () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockResume).not.toHaveBeenCalled();
    expect(mockFreezeAll).not.toHaveBeenCalled();
    expect(mockResumeAll).not.toHaveBeenCalled();
  });

  it('calls freeze on WebSocketService and freezeAll on TerminalWebSocketService after page hides', () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    fireVisibilityChange('hidden');

    // Not called immediately (debounce)
    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockFreezeAll).not.toHaveBeenCalled();

    // Advance past the debounce window (500ms)
    act(() => {
      jest.advanceTimersByTime(500);
    });

    expect(mockFreeze).toHaveBeenCalledTimes(1);
    expect(mockFreezeAll).toHaveBeenCalledTimes(1);
  });

  it('calls resume on WebSocketService and resumeAll on TerminalWebSocketService after page becomes visible', () => {
    // Start hidden
    Object.defineProperty(document, 'visibilityState', {
      value: 'hidden',
      writable: true,
      configurable: true,
    });

    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    fireVisibilityChange('visible');

    expect(mockResume).not.toHaveBeenCalled();
    expect(mockResumeAll).not.toHaveBeenCalled();

    act(() => {
      jest.advanceTimersByTime(500);
    });

    expect(mockResume).toHaveBeenCalledTimes(1);
    expect(mockResumeAll).toHaveBeenCalledTimes(1);
  });

  it('debounces rapid visibility toggles and only executes the most recent state', () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    // Rapid toggle: visible → hidden → visible within 100ms
    fireVisibilityChange('hidden');
    act(() => {
      jest.advanceTimersByTime(100);
    });
    fireVisibilityChange('visible');

    // Still in debounce window — nothing executed yet
    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockResume).not.toHaveBeenCalled();

    // Advance past debounce window
    act(() => {
      jest.advanceTimersByTime(500);
    });

    // Only resume should have been called (the final state was visible)
    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockResume).toHaveBeenCalledTimes(1);
    expect(mockResumeAll).toHaveBeenCalledTimes(1);
  });

  it('does not execute stale freeze if page became visible during debounce', () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    // Hide the page
    fireVisibilityChange('hidden');
    // Wait 200ms
    act(() => {
      jest.advanceTimersByTime(200);
    });
    // Show the page again
    fireVisibilityChange('visible');

    // Advance past the debounce — the page is now visible
    act(() => {
      jest.advanceTimersByTime(500);
    });

    // The stale freeze should have been skipped — only resume should execute
    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockResume).toHaveBeenCalledTimes(1);
    expect(mockResumeAll).toHaveBeenCalledTimes(1);
  });

  it('does not call anything after unmount (cleanup)', () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    fireVisibilityChange('hidden');
    act(() => {
      root.unmount();
    });

    // Advance timers after unmount — the mountedRef guard should prevent execution
    act(() => {
      jest.advanceTimersByTime(500);
    });

    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockFreezeAll).not.toHaveBeenCalled();
  });

  it('removes the visibilitychange event listener on unmount', () => {
    const spy = jest.spyOn(document, 'removeEventListener');

    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });
    act(() => {
      root.unmount();
    });

    expect(spy).toHaveBeenCalledWith('visibilitychange', expect.any(Function));
    spy.mockRestore();
  });

  it('handles multiple rapid toggle cycles and only executes final state', () => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(HookRunner));
    });

    // Rapid cycle: visible → hidden → visible → hidden → visible
    fireVisibilityChange('hidden');
    act(() => {
      jest.advanceTimersByTime(100);
    });
    fireVisibilityChange('visible');
    act(() => {
      jest.advanceTimersByTime(100);
    });
    fireVisibilityChange('hidden');
    act(() => {
      jest.advanceTimersByTime(100);
    });
    fireVisibilityChange('visible');

    // Advance past debounce window
    act(() => {
      jest.advanceTimersByTime(500);
    });

    // Only resume should execute (final state was visible)
    expect(mockFreeze).not.toHaveBeenCalled();
    expect(mockResume).toHaveBeenCalledTimes(1);
    expect(mockFreezeAll).not.toHaveBeenCalled();
    expect(mockResumeAll).toHaveBeenCalledTimes(1);
  });
});
