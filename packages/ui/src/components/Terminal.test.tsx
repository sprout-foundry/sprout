// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ── Mocks before importing the component ────────────────────────────────

vi.mock('./TerminalPane', () => {
  function MockTerminalPane({ sessionId, fontSize, isActive, themePack, createConnection, createWasmShell }: any) {
    return createElement('div', {
      className: 'mock-terminal-pane',
      'data-session-id': sessionId,
      'data-font-size': String(fontSize),
    }, sessionId);
  }
  return { __esModule: true, default: MockTerminalPane };
});

vi.mock('./TerminalTabBar', () => {
  function MockTerminalTabBar({ sessions, activeSessionId, onSwitch, onClose, onRename, onTogglePin, onCreate }: any) {
    return createElement('div', {
      className: 'mock-terminal-tab-bar',
      'data-active-session': activeSessionId,
      'data-session-count': String(sessions?.length ?? 0),
    },
      (sessions ?? []).map((s: any) =>
        createElement('span', {
          key: s.id,
          className: `mock-tab ${s.id === activeSessionId ? 'active-tab' : ''}`,
        }, s.name)
      )
    );
  }
  return { __esModule: true, default: MockTerminalTabBar };
});

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

import Terminal from './Terminal';

// ── Helpers ──────────────────────────────────────────────────────────────

let container: HTMLDivElement;
let root: Root;

const localStorageMock = (() => {
  let store: Record<string, string> = {};
  const mock = {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
  // Expose store externally so tests can reset it
  Object.defineProperty(mock, 'store', {
    get: () => store,
    set: (val: Record<string, string>) => { store = val; },
    configurable: true,
  });
  return mock;
})();

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  // @ts-expect-error — mock localStorage
  Object.defineProperty(window, 'localStorage', { value: localStorageMock, writable: true });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ── Tests: Terminal ──────────────────────────────────────────────────────

describe('Terminal', () => {
  let onToggleExpand: vi.Mock;

  beforeEach(() => {
    onToggleExpand = vi.fn();
    // Reset the localStorage store between tests
    (localStorageMock as any).store = {};
  });

  it('renders collapsed bar when isExpanded is false', () => {
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(
        createElement(Terminal, {
          isExpanded: false,
          onToggleExpand,
        })
      );
    });
    expect(container.querySelector('.terminal-collapsed-bar')).not.toBeNull();
    expect(container.querySelector('.terminal-collapsed-label')?.textContent).toBe('Terminal');
    expect(container.querySelector('.terminal-collapsed-hint')?.textContent).toBe('Click to expand');
  });

  it('renders collapsed bar when no isExpanded prop is provided', () => {
    act(() => {
      root.render(createElement(Terminal));
    });
    expect(container.querySelector('.terminal-collapsed-bar')).not.toBeNull();
  });

  it('expands terminal when collapsed bar is clicked', () => {
    act(() => {
      root.render(createElement(Terminal, { onToggleExpand }));
    });

    const collapsedBar = container.querySelector('.terminal-collapsed-bar');
    act(() => {
      collapsedBar?.click();
    });

    expect(container.querySelector('.terminal-container')).not.toBeNull();
    expect(onToggleExpand).toHaveBeenCalledWith(true);
  });

  it('renders expanded terminal when isExpanded is true', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    expect(container.querySelector('.terminal-container')).not.toBeNull();
    expect(container.querySelector('.terminal-header')).not.toBeNull();
    expect(container.querySelector('.terminal-panes-container')).not.toBeNull();
  });

  it('renders resize handle in expanded terminal', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    expect(container.querySelector('.terminal-resize-handle')).not.toBeNull();
  });

  it('calls onToggleExpand when collapse button is clicked', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    // Find the collapse button by its title
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let collapseBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Collapse terminal') {
        collapseBtn = btn as HTMLButtonElement;
        break;
      }
    }
    expect(collapseBtn).not.toBeNull();

    act(() => {
      collapseBtn?.click();
    });

    expect(onToggleExpand).toHaveBeenCalledWith(false);
  });

  it('zoom in button increments font size', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    // Find the initial font size from the mocked terminal pane
    const pane = container.querySelector('.mock-terminal-pane');
    const initialFontSize = pane?.getAttribute('data-font-size');
    expect(initialFontSize).toBe('13');

    // Click zoom in
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let zoomInBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Zoom in') {
        zoomInBtn = btn as HTMLButtonElement;
        break;
      }
    }
    expect(zoomInBtn).not.toBeNull();

    act(() => {
      zoomInBtn?.click();
    });

    const newFontSize = pane?.getAttribute('data-font-size');
    expect(newFontSize).toBe('14');
  });

  it('zoom out button decrements font size', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    const pane = container.querySelector('.mock-terminal-pane');
    expect(pane?.getAttribute('data-font-size')).toBe('13');

    const buttons = container.querySelectorAll('.terminal-header-btn');
    let zoomOutBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Zoom out') {
        zoomOutBtn = btn as HTMLButtonElement;
        break;
      }
    }
    expect(zoomOutBtn).not.toBeNull();

    act(() => {
      zoomOutBtn?.click();
    });

    expect(pane?.getAttribute('data-font-size')).toBe('12');
  });

  it('reset font size button resets to default', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    // Zoom in first
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let zoomInBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Zoom in') {
        zoomInBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      zoomInBtn?.click();
    });

    let pane = container.querySelector('.mock-terminal-pane');
    expect(pane?.getAttribute('data-font-size')).toBe('14');

    // Reset
    let resetBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Reset font size') {
        resetBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      resetBtn?.click();
    });

    pane = container.querySelector('.mock-terminal-pane');
    expect(pane?.getAttribute('data-font-size')).toBe('13');
  });

  it('toggle split horizontal creates second pane', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(1);

    // Click split horizontal
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let splitBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split horizontal') {
        splitBtn = btn as HTMLButtonElement;
        break;
      }
    }
    expect(splitBtn).not.toBeNull();

    act(() => {
      splitBtn?.click();
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(2);
    expect(container.querySelector('.terminal-split-horizontal')).not.toBeNull();
  });

  it('toggle split horizontal again removes split', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    // Enable split
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let splitBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split horizontal') {
        splitBtn = btn as HTMLButtonElement;
        break;
      }
    }

    act(() => {
      splitBtn?.click();
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(2);

    // Click again to remove
    act(() => {
      splitBtn?.click();
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(1);
    expect(container.querySelector('.terminal-split-horizontal')).toBeNull();
  });

  it('toggle split vertical creates second pane', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    const buttons = container.querySelectorAll('.terminal-header-btn');
    let splitBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split vertical') {
        splitBtn = btn as HTMLButtonElement;
        break;
      }
    }
    expect(splitBtn).not.toBeNull();

    act(() => {
      splitBtn?.click();
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(2);
    expect(container.querySelector('.terminal-split-vertical')).not.toBeNull();
  });

  it('font size is persisted to localStorage after zoom', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    // Zoom in
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let zoomInBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Zoom in') {
        zoomInBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      zoomInBtn?.click();
    });

    expect(localStorageMock.setItem).toHaveBeenCalledWith('sprout-terminal-font-size', '14');
  });

  it('terminal height is persisted to localStorage on mount', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    expect(localStorageMock.setItem).toHaveBeenCalledWith('sprout-terminal-height', '400');
  });
});
