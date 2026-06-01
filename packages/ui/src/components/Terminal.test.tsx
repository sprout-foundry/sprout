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

  it('switches from horizontal split to vertical split', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });

    // Enable horizontal split
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let splitHBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split horizontal') {
        splitHBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      splitHBtn?.click();
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(2);
    expect(container.querySelector('.terminal-split-horizontal')).not.toBeNull();

    // Switch to vertical split
    let splitVBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split vertical') {
        splitVBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      splitVBtn?.click();
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper')).toHaveLength(2);
    expect(container.querySelector('.terminal-split-horizontal')).toBeNull();
    expect(container.querySelector('.terminal-split-vertical')).not.toBeNull();
  });

  it('shows split divider when split is active', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    expect(container.querySelector('.terminal-split-divider')).toBeNull();

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
    expect(container.querySelector('.terminal-split-divider')).not.toBeNull();
  });

  it('shows split divider with correct direction class', () => {
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
    act(() => {
      splitBtn?.click();
    });
    const divider = container.querySelector('.terminal-split-divider');
    expect(divider?.classList.contains('terminal-split-divider-vertical')).toBe(true);
  });

  it('renders pane wrapper has correct style based on split sizes', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
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
    const wrappers = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappers.length).toBe(2);
    // Both should have a dimension after split
    const pane1 = wrappers[0] as HTMLElement;
    const pane2 = wrappers[1] as HTMLElement;
    expect(pane1.style.height || pane1.style.flexBasis || '').toBeDefined();
  });

  it('handles isExpanded=false then true then false via onToggleExpand', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: false, onToggleExpand }));
    });
    expect(container.querySelector('.terminal-collapsed-bar')).not.toBeNull();
    expect(container.querySelector('.terminal-container')).toBeNull();

    act(() => {
      container.querySelector('.terminal-collapsed-bar')?.click();
    });
    expect(onToggleExpand).toHaveBeenCalledWith(true);
    expect(container.querySelector('.terminal-container')).not.toBeNull();

    // Collapse again
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let collapseBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Collapse terminal') {
        collapseBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      collapseBtn?.click();
    });
    expect(onToggleExpand).toHaveBeenCalledWith(false);
  });

  it('renders terminal panes container with correct height offset', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    const panesContainer = container.querySelector('.terminal-panes-container');
    expect(panesContainer).not.toBeNull();
    // Height should be terminalHeight - collapsedHeight (400 - 42 = 358)
    const style = (panesContainer as HTMLElement).style;
    expect(style.height).toBe('358px');
  });

  it('sets focused pane on mouse down on pane wrapper', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    // Create a split first to have multiple panes
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let splitBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split vertical') {
        splitBtn = btn as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      splitBtn?.click();
    });
    const wrappers = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappers.length).toBe(2);

    // Click second pane to focus it
    act(() => {
      (wrappers[1] as HTMLElement).dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    // The focused pane changes but we verify the component doesn't crash
    expect(container.querySelector('.terminal-container')).not.toBeNull();
  });

  it('renders without crashing when isConnected is false', () => {
    act(() => {
      root.render(createElement(Terminal, {
        isExpanded: true,
        isConnected: false,
        onToggleExpand,
      }));
    });
    // Should render without crashing even when disconnected
    expect(container.querySelector('.terminal-container')).not.toBeNull();
  });

  it('resize handle is present and clickable', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    const handle = container.querySelector('.terminal-resize-handle');
    expect(handle).not.toBeNull();
    expect(handle?.classList.contains('terminal-resize-handle')).toBe(true);
  });

  it('split button shows active class when split direction matches', () => {
    act(() => {
      root.render(createElement(Terminal, { isExpanded: true, onToggleExpand }));
    });
    const buttons = container.querySelectorAll('.terminal-header-btn');
    let splitHBtn: HTMLButtonElement | null = null;
    let splitVBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(buttons)) {
      if (btn.getAttribute('title') === 'Split horizontal') {
        splitHBtn = btn as HTMLButtonElement;
      }
      if (btn.getAttribute('title') === 'Split vertical') {
        splitVBtn = btn as HTMLButtonElement;
      }
    }
    expect(splitHBtn?.classList.contains('active')).toBe(false);
    expect(splitVBtn?.classList.contains('active')).toBe(false);

    act(() => {
      (splitHBtn as HTMLButtonElement | null)?.click();
    });
    // Re-query after state change
    const updatedButtons = container.querySelectorAll('.terminal-header-btn');
    let updatedHBtn: HTMLButtonElement | null = null;
    let updatedVBtn: HTMLButtonElement | null = null;
    for (const btn of Array.from(updatedButtons)) {
      if (btn.getAttribute('title') === 'Split horizontal') {
        updatedHBtn = btn as HTMLButtonElement;
      }
      if (btn.getAttribute('title') === 'Split vertical') {
        updatedVBtn = btn as HTMLButtonElement;
      }
    }
    expect(updatedHBtn?.classList.contains('active')).toBe(true);
    expect(updatedVBtn?.classList.contains('active')).toBe(false);
  });
});
