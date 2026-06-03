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
    { id: 's3', name: 'Session 3', is_pinned: false },
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
    const pinnedSessions: TerminalSession[] = [
      { id: 's1', name: 'Session 1', is_pinned: false },
      { id: 's2', name: 'Session 2', is_pinned: false },
      { id: 's3', name: 'Session 3', is_pinned: true },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          sessions: pinnedSessions,
          onTogglePin,
        })
      );
    });
    // The pinned session sorts to the front, so s3 is at index 0.
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      });
      tabs[0]?.dispatchEvent(event);
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

  // ──────────────────────────────────────────────────────────────────────
  // Pinning
  // ──────────────────────────────────────────────────────────────────────

  it('renders a pin icon on pinned tabs', () => {
    const sessions: TerminalSession[] = [
      { id: 's1', name: 'Session 1', is_pinned: false },
      { id: 's2', name: 'Pinned', is_pinned: true },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions,
          activeSessionId: 's1',
          onSwitch: vi.fn(),
          onClose: vi.fn(),
          onRename: vi.fn(),
        })
      );
    });
    const pinIcons = container.querySelectorAll('.terminal-tab-pin-icon');
    expect(pinIcons).toHaveLength(1);
    // The pin icon sits inside the pinned tab.
    expect(pinIcons[0]?.closest('.terminal-tab')?.classList.contains('pinned')).toBe(true);
  });

  it('sorts pinned tabs to the front while preserving relative order', () => {
    const sessions: TerminalSession[] = [
      { id: 's1', name: 'Session 1', is_pinned: false },
      { id: 's2', name: 'Session 2', is_pinned: true },
      { id: 's3', name: 'Session 3', is_pinned: false },
      { id: 's4', name: 'Session 4', is_pinned: true },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions,
          activeSessionId: 's1',
          onSwitch: vi.fn(),
          onClose: vi.fn(),
          onRename: vi.fn(),
        })
      );
    });
    const names = Array.from(container.querySelectorAll('.terminal-tab-name')).map(
      (n) => n.textContent,
    );
    // Pinned first (s2, s4 — preserved order), then unpinned (s1, s3).
    expect(names).toEqual(['Session 2', 'Session 4', 'Session 1', 'Session 3']);
  });

  it('hides the close X on pinned tabs', () => {
    const sessions: TerminalSession[] = [
      { id: 's1', name: 'Session 1', is_pinned: false },
      { id: 's2', name: 'Session 2', is_pinned: true },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions,
          activeSessionId: 's1',
          onSwitch: vi.fn(),
          onClose: vi.fn(),
          onRename: vi.fn(),
        })
      );
    });
    const closes = container.querySelectorAll('.terminal-tab-close');
    expect(closes).toHaveLength(1);
    // The remaining close button sits inside the unpinned tab.
    expect(closes[0]?.closest('.terminal-tab')?.classList.contains('pinned')).toBe(false);
  });

  it('context menu Close is disabled on pinned tab', () => {
    const onClose = vi.fn();
    const sessions: TerminalSession[] = [
      { id: 's1', name: 'Session 1', is_pinned: false },
      { id: 's2', name: 'Pinned', is_pinned: true },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions,
          activeSessionId: 's1',
          onSwitch: vi.fn(),
          onClose,
          onRename: vi.fn(),
          onTogglePin: vi.fn(),
        })
      );
    });
    // The pinned tab sorts to the front.
    const tabs = container.querySelectorAll('.terminal-tab');
    act(() => {
      tabs[0]?.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 0, clientY: 0 }),
      );
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
    act(() => {
      closeBtn?.click();
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  // ──────────────────────────────────────────────────────────────────────
  // allowCloseLastTab
  // ──────────────────────────────────────────────────────────────────────

  it('does not show close X on the sole tab by default', () => {
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions: [{ id: 'only', name: 'Solo', is_pinned: false }],
          activeSessionId: 'only',
          onSwitch: vi.fn(),
          onClose: vi.fn(),
          onRename: vi.fn(),
        })
      );
    });
    expect(container.querySelectorAll('.terminal-tab-close')).toHaveLength(0);
  });

  it('shows close X on the sole tab when allowCloseLastTab is true', () => {
    const onClose = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions: [{ id: 'only', name: 'Solo', is_pinned: false }],
          activeSessionId: 'only',
          onSwitch: vi.fn(),
          onClose,
          onRename: vi.fn(),
          allowCloseLastTab: true,
        })
      );
    });
    const closes = container.querySelectorAll<HTMLElement>('.terminal-tab-close');
    expect(closes).toHaveLength(1);
    act(() => {
      closes[0]?.click();
    });
    expect(onClose).toHaveBeenCalledWith('only');
  });

  // ──────────────────────────────────────────────────────────────────────
  // Middle-click close
  // ──────────────────────────────────────────────────────────────────────

  it('middle-click on a tab calls onClose', () => {
    const onClose = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onClose,
        })
      );
    });
    const tabs = container.querySelectorAll<HTMLElement>('.terminal-tab');
    act(() => {
      tabs[1]?.dispatchEvent(
        new MouseEvent('auxclick', { bubbles: true, cancelable: true, button: 1 }),
      );
    });
    expect(onClose).toHaveBeenCalledWith('s2');
  });

  it('middle-click on a pinned tab is ignored', () => {
    const onClose = vi.fn();
    const sessions: TerminalSession[] = [
      { id: 's1', name: 'Session 1', is_pinned: false },
      { id: 's2', name: 'Pinned', is_pinned: true },
    ];
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions,
          activeSessionId: 's1',
          onSwitch: vi.fn(),
          onClose,
          onRename: vi.fn(),
        })
      );
    });
    // Pinned sorts to position 0.
    const tabs = container.querySelectorAll<HTMLElement>('.terminal-tab');
    act(() => {
      tabs[0]?.dispatchEvent(
        new MouseEvent('auxclick', { bubbles: true, cancelable: true, button: 1 }),
      );
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  it('middle-click on the sole tab is ignored when allowCloseLastTab is false', () => {
    const onClose = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          sessions: [{ id: 'only', name: 'Solo', is_pinned: false }],
          activeSessionId: 'only',
          onSwitch: vi.fn(),
          onClose,
          onRename: vi.fn(),
        })
      );
    });
    const tab = container.querySelector('.terminal-tab') as HTMLElement;
    act(() => {
      tab?.dispatchEvent(
        new MouseEvent('auxclick', { bubbles: true, cancelable: true, button: 1 }),
      );
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  // ──────────────────────────────────────────────────────────────────────
  // Activity indicator
  // ──────────────────────────────────────────────────────────────────────

  it('renders an activity dot on inactive sessions in the activity set', () => {
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          activitySessionIds: new Set(['s2', 's3']),
        })
      );
    });
    const dots = container.querySelectorAll('.terminal-tab-activity-dot');
    // s1 is active so its dot is suppressed; s2 and s3 are inactive + dirty.
    expect(dots).toHaveLength(2);
  });

  it('suppresses the activity dot on the active session even if listed', () => {
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          activitySessionIds: new Set(['s1', 's2']),
        })
      );
    });
    const dots = container.querySelectorAll('.terminal-tab-activity-dot');
    // s1 is active, so only s2's dot renders.
    expect(dots).toHaveLength(1);
    const s1Tab = container.querySelectorAll('.terminal-tab')[0];
    expect(s1Tab?.querySelector('.terminal-tab-activity-dot')).toBeNull();
  });

  it('renders no activity dots when the set is omitted', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    expect(container.querySelectorAll('.terminal-tab-activity-dot')).toHaveLength(0);
  });

  // ──────────────────────────────────────────────────────────────────────
  // Keyboard navigation
  // ──────────────────────────────────────────────────────────────────────

  it('ArrowRight on the active tab switches to the next tab', () => {
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
      tabs[0]?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }),
      );
    });
    expect(onSwitch).toHaveBeenCalledWith('s2');
  });

  it('ArrowLeft on the first tab wraps to the last', () => {
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
      tabs[0]?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }),
      );
    });
    expect(onSwitch).toHaveBeenCalledWith('s3');
  });

  it('Home jumps to the first tab, End jumps to the last', () => {
    const onSwitch = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          activeSessionId: 's2',
          onSwitch,
        })
      );
    });
    const tabs = container.querySelectorAll<HTMLElement>('.terminal-tab');
    act(() => {
      tabs[1]?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Home', bubbles: true, cancelable: true }),
      );
    });
    expect(onSwitch).toHaveBeenLastCalledWith('s1');
    act(() => {
      tabs[1]?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'End', bubbles: true, cancelable: true }),
      );
    });
    expect(onSwitch).toHaveBeenLastCalledWith('s3');
  });

  it('non-navigation keys do not call onSwitch', () => {
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
      tabs[0]?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true }),
      );
    });
    expect(onSwitch).not.toHaveBeenCalled();
  });

  it('arrow nav is suppressed while a tab is being renamed', () => {
    const onSwitch = vi.fn();
    act(() => {
      root.render(
        createElement(TerminalTabBar, {
          ...defaultProps,
          onSwitch,
        })
      );
    });
    // Enter rename mode on tab 0
    const tabs = container.querySelectorAll<HTMLElement>('.terminal-tab');
    act(() => {
      tabs[0]?.dispatchEvent(
        new MouseEvent('dblclick', { bubbles: true, cancelable: true }),
      );
    });
    expect(container.querySelector('.terminal-tab-rename-input')).not.toBeNull();

    // ArrowRight should be inert while renaming.
    act(() => {
      tabs[0]?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }),
      );
    });
    expect(onSwitch).not.toHaveBeenCalled();
  });

  // ──────────────────────────────────────────────────────────────────────
  // ARIA tabIndex (only the active tab is in the tab order)
  // ──────────────────────────────────────────────────────────────────────

  it('only the active tab has tabIndex=0', () => {
    act(() => {
      root.render(createElement(TerminalTabBar, { ...defaultProps }));
    });
    const tabs = container.querySelectorAll<HTMLElement>('.terminal-tab');
    expect(tabs[0]?.tabIndex).toBe(0);
    expect(tabs[1]?.tabIndex).toBe(-1);
    expect(tabs[2]?.tabIndex).toBe(-1);
  });
});
