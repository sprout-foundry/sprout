// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ── Mocks before importing the component ────────────────────────────────

vi.mock('./ContextMenu', () => {
  function MockContextMenu({ isOpen, children, onClose, x, y }: any) {
    if (!isOpen) return null;
    return createElement('div', {
      className: 'mock-context-menu',
      'data-x': String(x),
      'data-y': String(y),
    }, children);
  }
  return { __esModule: true, default: MockContextMenu };
});

import TerminalTabBar, { type TerminalSession, type AttachableSession } from './TerminalTabBar';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

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
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: TerminalTabBar
// ---------------------------------------------------------------------------

describe('TerminalTabBar', () => {
  const defaultSessions: TerminalSession[] = [
    { id: 's1', name: 'Session 1', is_pinned: false },
    { id: 's2', name: 'Session 2', is_pinned: false },
    { id: 's3', name: 'Session 3', is_pinned: true },
  ];

  const defaultProps = {
    sessions: defaultSessions,
    activeSessionId: 's1',
    onSwitch: vi.fn(),
    onCreate: vi.fn(),
    onClose: vi.fn(),
    onRename: vi.fn(),
    onTogglePin: vi.fn(),
  };

  it('renders tab bar with role=tablist', () => {
    act(() => {
      root.render(
        createElement(TerminalTabBar, { ...defaultProps })
      );
    });
    const bar = container.querySelector('.terminal-tab-bar');
    expect(bar).not.toBeNull();
    expect(bar?.getAttribute('role')).toBe('tablist');
  });

  it('renders sessions as tabs', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const tabs = container.querySelectorAll('.terminal-tab');
    expect(tabs).toHaveLength(3);
  });

  it('highlights active tab', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const tabs = container.querySelectorAll('.terminal-tab');
    expect(tabs[0]?.classList.contains('active')).toBe(true);
    expect(tabs[1]?.classList.contains('active')).toBe(false);
    expect(tabs[2]?.classList.contains('active')).toBe(false);
  });

  it('renders tab names', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const names = container.querySelectorAll('.terminal-tab-name');
    expect(Array.from(names).map((n) => n.textContent)).toEqual([
      'Session 1',
      'Session 2',
      'Session 3',
    ]);
  });

  it('calls onSwitch when tab is clicked', () => {
    const onSwitch = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onSwitch,
        })
      );
    });
    const tabs = container.querySelectorAll<HTMLElement>('.terminal-tab');
    act(() => {
      tabs[1]?.click();
    });
    expect(onSwitch).toHaveBeenCalledWith('s2');
  });

  it('renders new session button when onCreate is provided', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    expect(container.querySelector('.terminal-tab-new')).not.toBeNull();
  });

  it('calls onCreate when new session button is clicked', () => {
    const onCreate = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onCreate,
        })
      );
    });
    act(() => {
      (container.querySelector('.terminal-tab-new') as HTMLElement | null)?.click();
    });
    expect(onCreate).toHaveBeenCalled();
  });

  it('does not render new session button without onCreate', () => {
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions: defaultSessions,
          activeSessionId: 's1',
          onSwitch: vi.fn(),
          onClose: vi.fn(),
          onRename: vi.fn(),
        })
      );
    });
    expect(container.querySelector('.terminal-tab-new')).toBeNull();
  });

  it('close buttons only show when sessions.length > 1', () => {
    // With 2 sessions
    const twoSessions: TerminalSession[] = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: false },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          sessions: twoSessions,
          activeSessionId: 'a',
        })
      );
    });
    expect(container.querySelectorAll('.terminal-tab-close')).toHaveLength(2);

    // With 1 session
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          sessions: [{ id: 'single', name: 'Solo', is_pinned: false }],
          activeSessionId: 'single',
        })
      );
    });
    expect(container.querySelectorAll('.terminal-tab-close')).toHaveLength(0);
  });

  it('calls onClose when close button is clicked', () => {
    const onClose = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onClose,
        })
      );
    });
    const closeBtns = container.querySelectorAll<HTMLElement>('.terminal-tab-close');
    act(() => {
      closeBtns[1]?.click();
    });
    expect(onClose).toHaveBeenCalledWith('s2');
  });

  it('double-click starts rename mode', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      tabs[1]?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
    });
    // Should show input instead of name span for that tab
    const input = container.querySelector('.terminal-tab-rename-input');
    expect(input).not.toBeNull();
    expect((input as HTMLInputElement)?.value).toBe('Session 2');
  });

  it('Enter key commits rename', () => {
    const onRename = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onRename,
        })
      );
    });
    // Start renaming
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      tabs[1]?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
    });
    // Set new value and press Enter
    const input = container.querySelector('.terminal-tab-rename-input') as HTMLInputElement;
    act(() => {
      Object.defineProperty(input, 'value', { value: 'New Name' });
      input.dispatchEvent(new Event('change', { bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    });
    expect(onRename).toHaveBeenCalledWith('s2', 'New Name');
    expect(container.querySelector('.terminal-tab-rename-input')).toBeNull();
  });

  it('Escape key cancels rename', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    // Start renaming
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      tabs[1]?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true }));
    });
    expect(container.querySelector('.terminal-tab-rename-input')).not.toBeNull();

    // Press Escape
    const input = container.querySelector('.terminal-tab-rename-input') as HTMLInputElement;
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    });
    expect(container.querySelector('.terminal-tab-rename-input')).toBeNull();
  });

  it('context menu shows on right-click', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      });
      tabs[1]?.dispatchEvent(event);
    });
    expect(document.querySelector('.mock-context-menu')).not.toBeNull();
  });

  it('context menu rename triggers rename mode', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    // Right-click a tab to open context menu
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      });
      tabs[2]?.dispatchEvent(event);
    });
    // Click "Rename" in context menu
    const menu = document.querySelector('.mock-context-menu')!;
    const renameBtn = menu.querySelector<HTMLElement>('.context-menu-item');
    act(() => {
      renameBtn?.click();
    });
    // Should be in rename mode now
    expect(container.querySelector('.terminal-tab-rename-input')).not.toBeNull();
  });

  it('context menu close calls onClose', () => {
    const onClose = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onClose,
        })
      );
    });
    // Right-click a tab
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      });
      tabs[1]?.dispatchEvent(event);
    });
    // Click "Close Tab" in context menu (last item with label "Close Tab")
    const menu = document.querySelector('.mock-context-menu')!;
    const labels = menu.querySelectorAll('.menu-item-label');
    let closeBtn: HTMLButtonElement | null = null;
    for (const label of Array.from(labels)) {
      if (label.textContent === 'Close Tab') {
        closeBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      closeBtn?.click();
    });
    expect(onClose).toHaveBeenCalledWith('s2');
  });

  it('context menu close is disabled when only one session', () => {
    const onClose = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions: [{ id: 'single', name: 'Solo', is_pinned: false }],
          activeSessionId: 'single',
          onSwitch: vi.fn(),
          onClose,
          onRename: vi.fn(),
        })
      );
    });
    const tab = container.querySelector('.terminal-tab');
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      });
      tab?.dispatchEvent(event);
    });
    const menu = document.querySelector('.mock-context-menu')!;
    const labels = menu.querySelectorAll('.menu-item-label');
    let closeBtn: HTMLButtonElement | null = null;
    for (const label of Array.from(labels)) {
      if (label.textContent === 'Close Tab') {
        closeBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }
    expect(closeBtn?.disabled).toBe(true);
  });

  it('attachable sessions dropdown shows when provided', () => {
    const attachableSessions: AttachableSession[] = [
      { id: 'agent1', name: 'Agent 1', status: 'active' },
      { id: 'agent2', name: 'Agent 2', status: 'inactive' },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          attachableSessions,
        })
      );
    });
    expect(container.querySelector('.agent-sessions-btn')).not.toBeNull();
    // Dropdown should not be open by default
    expect(container.querySelector('.agent-sessions-menu')).toBeNull();
  });

  it('clicking agent sessions button opens dropdown', () => {
    const attachableSessions: AttachableSession[] = [
      { id: 'agent1', name: 'Agent 1', status: 'active' },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          attachableSessions,
        })
      );
    });
    act(() => {
      (container.querySelector('.agent-sessions-btn') as HTMLElement | null)?.click();
    });
    expect(container.querySelector('.agent-sessions-menu')).not.toBeNull();
    expect(container.querySelector('.agent-sessions-header')?.textContent).toBe('Agent Sessions');
  });

  it('inactive sessions are disabled in dropdown', () => {
    const onAttachSession = vi.fn();
    const attachableSessions: AttachableSession[] = [
      { id: 'agent1', name: 'Agent 1', status: 'active' },
      { id: 'agent2', name: 'Agent 2', status: 'inactive' },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          attachableSessions,
          onAttachSession,
        })
      );
    });
    // Open dropdown
    act(() => {
      (container.querySelector('.agent-sessions-btn') as HTMLElement | null)?.click();
    });
    const items = container.querySelectorAll<HTMLButtonElement>('.agent-sessions-item');
    expect(items[0]?.disabled).toBe(false);
    expect(items[1]?.disabled).toBe(true);
  });

  it('calls onAttachSession when active session is clicked', () => {
    const onAttachSession = vi.fn();
    const attachableSessions: AttachableSession[] = [
      { id: 'agent1', name: 'Agent 1', status: 'active' },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          attachableSessions,
          onAttachSession,
        })
      );
    });
    // Open dropdown
    act(() => {
      (container.querySelector('.agent-sessions-btn') as HTMLElement | null)?.click();
    });
    // Click the attach item
    const items = container.querySelectorAll<HTMLElement>('.agent-sessions-item');
    act(() => {
      items[0]?.click();
    });
    expect(onAttachSession).toHaveBeenCalledWith('agent1', 'Agent 1');
  });

  it('context menu toggle pin calls onTogglePin', () => {
    const onTogglePin = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onTogglePin,
        })
      );
    });
    // Right-click the pinned session
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      });
      tabs[2]?.dispatchEvent(event);
    });
    // Click "Unpin" in context menu
    const menu = document.querySelector('.mock-context-menu')!;
    const labels = menu.querySelectorAll('.menu-item-label');
    let pinBtn: HTMLButtonElement | null = null;
    for (const label of Array.from(labels)) {
      if (label.textContent === 'Unpin') {
        pinBtn = label.closest('button') as HTMLButtonElement;
        break;
      }
    }
    act(() => {
      pinBtn?.click();
    });
    expect(onTogglePin).toHaveBeenCalledWith('s3');
  });

  it('tab has correct aria-selected attribute', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const tabs = container.querySelectorAll('.terminal-tab');
    expect(tabs[0]?.getAttribute('aria-selected')).toBe('true');
    expect(tabs[1]?.getAttribute('aria-selected')).toBe('false');
  });

  it('renders with empty sessions array', () => {
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions: [],
          activeSessionId: '',
          onSwitch: vi.fn(),
          onClose: vi.fn(),
          onRename: vi.fn(),
        })
      );
    });
    expect(container.querySelector('.terminal-tab-bar')).not.toBeNull();
    expect(container.querySelectorAll('.terminal-tab')).toHaveLength(0);
  });
});
