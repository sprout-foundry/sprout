// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import TerminalTabBar from './TerminalTabBar';

// ---------------------------------------------------------------------------
// Mock ContextMenu: renders children into a simple div when isOpen
// ---------------------------------------------------------------------------

jest.mock('./ContextMenu', () => {
  return function MockContextMenu({ isOpen, children }: any) {
    if (!isOpen) return null;
    return <div data-testid="context-menu">{children}</div>;
  };
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const defaultSessions = [
  { id: 's1', name: 'Terminal 1' },
  { id: 's2', name: 'Terminal 2' },
  { id: 's3', name: 'Terminal 3' },
];

const defaultProps = {
  sessions: defaultSessions,
  activeSessionId: 's1',
  onSwitch: jest.fn(),
  onCreate: jest.fn(),
  onClose: jest.fn(),
  onRename: jest.fn(),
};

function renderWithProps(props = {}) {
  const merged = { ...defaultProps, ...props };
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(<TerminalTabBar {...merged} />);
  });

  return { container, root };
}

function getTabs(container: HTMLElement) {
  return Array.from(container.querySelectorAll('.terminal-tab'));
}

function _getTabButton(container: HTMLElement, sessionId: string) {
  return (
    container.querySelector(`.terminal-tab[title="${sessionId}"]`) ||
    // May have title set to session name, not id — find by text
    getTabs(container).find((tab) => tab.querySelector('.terminal-tab-name')?.textContent === sessionId)
  );
}

function getTabByName(container: HTMLElement, name: string) {
  return getTabs(container).find(
    (tab) => tab.getAttribute('title') === name || tab.querySelector('.terminal-tab-name')?.textContent === name,
  );
}

function getCloseButtons(container: HTMLElement) {
  return Array.from(container.querySelectorAll('.terminal-tab-close'));
}

function getRenameInput(container: HTMLElement) {
  return container.querySelector('.terminal-tab-rename-input') as HTMLInputElement | null;
}

function getContextMenu(): HTMLElement | null {
  return document.body.querySelector('[data-testid="context-menu"]');
}

function getContextMenuItem(label: string): HTMLButtonElement | null {
  const menu = getContextMenu();
  if (!menu) return null;
  const items = Array.from(menu.querySelectorAll('.context-menu-item'));
  return (items.find((item) => item.textContent?.includes(label)) as HTMLButtonElement) || null;
}

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

const _flushRAF = () =>
  act(async () => {
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await Promise.resolve();
  });

// ---------------------------------------------------------------------------
// Test Suite
// ---------------------------------------------------------------------------

describe('TerminalTabBar', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    // Reset all mock call history from defaultProps
    defaultProps.onSwitch.mockClear();
    defaultProps.onCreate.mockClear();
    defaultProps.onClose.mockClear();
    defaultProps.onRename.mockClear();
  });

  afterEach(() => {
    if (root) {
      act(() => {
        root.unmount();
      });
    }
    if (container) {
      container.remove();
    }
    // Clean up any portal elements left by ContextMenu mock
    document.querySelectorAll('[data-testid="context-menu"]').forEach((el) => el.remove());
    jest.clearAllMocks();
  });

  // ── 1. Renders tabs ──────────────────────────────────────

  it('renders the correct number of tabs matching sessions array', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tabs = getTabs(container);
    expect(tabs.length).toBe(3);
  });

  it('renders session names in tabs', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const names = Array.from(container.querySelectorAll('.terminal-tab-name')).map((el) => el.textContent);
    expect(names).toContain('Terminal 1');
    expect(names).toContain('Terminal 2');
    expect(names).toContain('Terminal 3');
  });

  it('renders a single tab for a single session', () => {
    const view = renderWithProps({
      sessions: [{ id: 's1', name: 'Main' }],
      activeSessionId: 's1',
    });
    container = view.container;
    root = view.root;

    expect(getTabs(container).length).toBe(1);
  });

  // ── 2. Active tab styling ────────────────────────────────

  it('adds "active" CSS class to the active tab', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const activeTab = getTabByName(container, 'Terminal 1');
    expect(activeTab?.classList.contains('active')).toBe(true);
  });

  it('does NOT add "active" CSS class to inactive tabs', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const inactiveTab = getTabByName(container, 'Terminal 2');
    expect(inactiveTab?.classList.contains('active')).toBe(false);
  });

  it('updates active tab when activeSessionId changes', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    // s1 is active initially
    let tab1 = getTabByName(container, 'Terminal 1');
    expect(tab1?.classList.contains('active')).toBe(true);

    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(<TerminalTabBar {...defaultProps} activeSessionId="s2" />);
    });

    // Now s2 should be active
    tab1 = getTabByName(container, 'Terminal 1');
    expect(tab1?.classList.contains('active')).toBe(false);

    const tab2 = getTabByName(container, 'Terminal 2');
    expect(tab2?.classList.contains('active')).toBe(true);
  });

  // ── 3. Tab click calls onSwitch ─────────────────────────

  it('calls onSwitch with correct session id when a tab is clicked', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    expect(tab2).toBeTruthy();

    act(() => {
      tab2!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(defaultProps.onSwitch).toHaveBeenCalledTimes(1);
    expect(defaultProps.onSwitch).toHaveBeenCalledWith('s2');
  });

  it('does not call onSwitch when clicking the active tab (just still calls it — component always)', () => {
    // The component always calls onSwitch regardless of active state,
    // which is standard behavior — clicking the active tab is a no-op
    // for the consumer
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab1 = getTabByName(container, 'Terminal 1');
    act(() => {
      tab1!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(defaultProps.onSwitch).toHaveBeenCalledWith('s1');
  });

  // ── 4. Close button visibility ───────────────────────────

  it('does NOT render close buttons when only 1 session exists', () => {
    const singleSessionProps = {
      sessions: [{ id: 's1', name: 'Solo' }],
      activeSessionId: 's1',
      onSwitch: jest.fn(),
      onCreate: jest.fn(),
      onClose: jest.fn(),
      onRename: jest.fn(),
    };

    const view = renderWithProps(singleSessionProps);
    container = view.container;
    root = view.root;

    const closeButtons = getCloseButtons(container);
    expect(closeButtons.length).toBe(0);
  });

  it('renders close buttons when 2+ sessions exist', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const closeButtons = getCloseButtons(container);
    expect(closeButtons.length).toBe(3);
  });

  // ── 5. Close button calls onClose ────────────────────────

  it('calls onClose with correct session id when close button is clicked', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    // Find close button inside the Terminal 2 tab
    const tab2 = getTabByName(container, 'Terminal 2');
    const closeBtn = tab2?.querySelector('.terminal-tab-close');
    expect(closeBtn).toBeTruthy();

    act(() => {
      closeBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(defaultProps.onClose).toHaveBeenCalledTimes(1);
    expect(defaultProps.onClose).toHaveBeenCalledWith('s2');
  });

  it('close button click does NOT propagate to tab (onSwitch not called)', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    const closeBtn = tab2?.querySelector('.terminal-tab-close');

    act(() => {
      closeBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // onSwitch should NOT have been called (stopPropagation)
    expect(defaultProps.onSwitch).not.toHaveBeenCalled();
    // Only onClose should fire
    expect(defaultProps.onClose).toHaveBeenCalledTimes(1);
  });

  // ── 6. Add button calls onCreate ─────────────────────────

  it('renders the new session (+) button', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const newBtn = container.querySelector('.terminal-tab-new');
    expect(newBtn).toBeTruthy();
  });

  it('calls onCreate when the + button is clicked', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const newBtn = container.querySelector('.terminal-tab-new');
    act(() => {
      newBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(defaultProps.onCreate).toHaveBeenCalledTimes(1);
  });

  // ── 7. Double-click rename ───────────────────────────────

  it('enters rename mode when a tab is double-clicked', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    expect(tab2).toBeTruthy();

    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);
    expect(renameInput).toBeTruthy();
  });

  it('prepopulates rename input with current session name', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab3 = getTabByName(container, 'Terminal 3');
    act(() => {
      tab3!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);
    expect(renameInput?.value).toBe('Terminal 3');
  });

  it('hides close button on the tab being renamed', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    // Before rename: 3 close buttons
    expect(getCloseButtons(container).length).toBe(3);

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    // After rename: 2 close buttons (renaming tab loses its close btn)
    expect(getCloseButtons(container).length).toBe(2);
  });

  it('hides the tab name when in rename mode', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    // The tab name span should no longer be visible for the renaming tab
    // Since the input replaces the name span
    const nameSpans = tab2!.querySelectorAll('.terminal-tab-name');
    expect(nameSpans.length).toBe(0);
  });

  // ── 8. Rename commit on Enter ────────────────────────────

  it('commits rename when Enter is pressed', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);
    expect(renameInput).toBeTruthy();

    // Simulate user typing a new name
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
    act(() => {
      nativeInputValueSetter!.call(renameInput, 'New Name');
      renameInput!.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Press Enter
    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(defaultProps.onRename).toHaveBeenCalledTimes(1);
    expect(defaultProps.onRename).toHaveBeenCalledWith('s2', 'New Name');
  });

  it('exits rename mode after Enter commit', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    // Commit
    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    // Rename input should be gone
    expect(getRenameInput(container)).toBeNull();
    // Name span should be back
    expect(tab2!.querySelector('.terminal-tab-name')).toBeTruthy();
  });

  // ── 9. Rename cancel on Escape ───────────────────────────

  it('cancels rename when Escape is pressed', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    // Type some text
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
    act(() => {
      nativeInputValueSetter!.call(renameInput, 'Modified Name');
      renameInput!.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Press Escape
    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });

    // onRename should NOT have been called
    expect(defaultProps.onRename).not.toHaveBeenCalled();
    // Rename mode should be exited
    expect(getRenameInput(container)).toBeNull();
  });

  it('preserves original name after cancel', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
    act(() => {
      nativeInputValueSetter!.call(renameInput, 'Modified Name');
      renameInput!.dispatchEvent(new Event('input', { bubbles: true }));
    });

    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });

    // Tab name should still show original
    expect(tab2!.querySelector('.terminal-tab-name')?.textContent).toBe('Terminal 2');
  });

  it('does not commit rename if the new name is empty', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    // Clear the input
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
    act(() => {
      nativeInputValueSetter!.call(renameInput, '');
      renameInput!.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Press Enter
    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    // onRename should NOT be called for empty name
    expect(defaultProps.onRename).not.toHaveBeenCalled();
    // Rename mode should be exited
    expect(getRenameInput(container)).toBeNull();
  });

  it('does not commit rename if name is whitespace only', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
    act(() => {
      nativeInputValueSetter!.call(renameInput, '   ');
      renameInput!.dispatchEvent(new Event('input', { bubbles: true }));
    });

    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(defaultProps.onRename).not.toHaveBeenCalled();
    expect(getRenameInput(container)).toBeNull();
  });

  it('trims whitespace from rename value before committing', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
    act(() => {
      nativeInputValueSetter!.call(renameInput, '  Trimmed Name  ');
      renameInput!.dispatchEvent(new Event('input', { bubbles: true }));
    });

    act(() => {
      renameInput!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(defaultProps.onRename).toHaveBeenCalledWith('s2', 'Trimmed Name');
  });

  it('commits rename on blur (clicking outside the input)', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    let renameInput = getRenameInput(container);

    // Set the value on the input so React's onChange picks up 'Blurred Name'
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    act(() => {
      nativeInputValueSetter.call(renameInput, 'Blurred Name');
      renameInput!.dispatchEvent(new Event('change', { bubbles: true }));
    });

    // Re-fetch the input after potential re-render
    renameInput = getRenameInput(container);

    // Use tabIndex + focus to programmatically remove focus, triggering real blur
    const tabBar = container.querySelector('.terminal-tab-bar') as HTMLElement;
    tabBar.setAttribute('tabindex', '0');

    act(() => {
      renameInput!.focus();
      tabBar.focus(); // Moving focus away from input triggers real blur
    });

    await flushPromises();

    expect(defaultProps.onRename).toHaveBeenCalledWith('s2', 'Blurred Name');
    expect(getRenameInput(container)).toBeNull();
  });

  it('clicking the rename input does NOT switch tabs', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');
    act(() => {
      tab2!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });

    const renameInput = getRenameInput(container);

    act(() => {
      renameInput!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // onSwitch should not have been triggered by clicking the rename input
    expect(defaultProps.onSwitch).not.toHaveBeenCalled();
  });

  // ── 10. Context menu ─────────────────────────────────────

  it('shows context menu on right-click on a tab', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');

    act(() => {
      const evt = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 200,
        clientY: 100,
      });
      tab2!.dispatchEvent(evt);
    });

    await flushPromises();

    const menu = getContextMenu();
    expect(menu).toBeTruthy();
  });

  it('context menu contains "Rename" item', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab1 = getTabByName(container, 'Terminal 1');

    act(() => {
      tab1!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 100, clientY: 50 }),
      );
    });

    await flushPromises();

    const renameItem = getContextMenuItem('Rename');
    expect(renameItem).toBeTruthy();
  });

  it('context menu contains "Close Tab" item', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab1 = getTabByName(container, 'Terminal 1');

    act(() => {
      tab1!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 100, clientY: 50 }),
      );
    });

    await flushPromises();

    const closeItem = getContextMenuItem('Close Tab');
    expect(closeItem).toBeTruthy();
  });

  it('context menu Rename action enters rename mode for correct tab', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab3 = getTabByName(container, 'Terminal 3');

    act(() => {
      tab3!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 300, clientY: 100 }),
      );
    });

    await flushPromises();

    const renameItem = getContextMenuItem('Rename');
    expect(renameItem).toBeTruthy();

    act(() => {
      renameItem!.click();
    });

    await flushPromises();

    // Rename input should appear, prepopulated with "Terminal 3"
    const renameInput = getRenameInput(container);
    expect(renameInput).toBeTruthy();
    expect(renameInput?.value).toBe('Terminal 3');
  });

  it('context menu Close Tab action calls onClose for correct session', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab2 = getTabByName(container, 'Terminal 2');

    act(() => {
      tab2!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 200, clientY: 100 }),
      );
    });

    await flushPromises();

    const closeItem = getContextMenuItem('Close Tab');
    expect(closeItem).toBeTruthy();

    act(() => {
      closeItem!.click();
    });

    await flushPromises();

    expect(defaultProps.onClose).toHaveBeenCalledTimes(1);
    expect(defaultProps.onClose).toHaveBeenCalledWith('s2');
  });

  // ── 11. Context menu close disabled ──────────────────────

  it('"Close Tab" is disabled when only 1 session', async () => {
    const singleSessionProps = {
      sessions: [{ id: 's1', name: 'Solo' }],
      activeSessionId: 's1',
      onSwitch: jest.fn(),
      onCreate: jest.fn(),
      onClose: jest.fn(),
      onRename: jest.fn(),
    };

    const view = renderWithProps(singleSessionProps);
    container = view.container;
    root = view.root;

    const tab1 = getTabByName(container, 'Solo');

    act(() => {
      tab1!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 100, clientY: 50 }),
      );
    });

    await flushPromises();

    const closeItem = getContextMenuItem('Close Tab');
    expect(closeItem).toBeTruthy();
    expect(closeItem?.disabled).toBe(true);
    expect(closeItem?.classList.contains('disabled')).toBe(true);
  });

  it('"Close Tab" is enabled when 2+ sessions exist', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab1 = getTabByName(container, 'Terminal 1');

    act(() => {
      tab1!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 100, clientY: 50 }),
      );
    });

    await flushPromises();

    const closeItem = getContextMenuItem('Close Tab');
    expect(closeItem).toBeTruthy();
    expect(closeItem?.disabled).toBe(false);
    expect(closeItem?.classList.contains('disabled')).toBe(false);
  });

  // ── Additional edge cases ────────────────────────────────

  it('context menu on the tab bar itself (not a tab) is prevented', () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tabBar = container.querySelector('.terminal-tab-bar');
    expect(tabBar).toBeTruthy();

    const evt = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
    });
    tabBar!.dispatchEvent(evt);

    // The tab bar's onContextMenu calls preventDefault and stopPropagation
    // The context menu should NOT appear since we didn't target a specific tab
    expect(getContextMenu()).toBeNull();
  });

  it('renders dividers between context menu items', async () => {
    const view = renderWithProps();
    container = view.container;
    root = view.root;

    const tab1 = getTabByName(container, 'Terminal 1');

    act(() => {
      tab1!.dispatchEvent(
        new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 100, clientY: 50 }),
      );
    });

    await flushPromises();

    const menu = getContextMenu();
    expect(menu).toBeTruthy();
    const dividers = menu!.querySelectorAll('.context-menu-divider');
    expect(dividers.length).toBe(1);
  });
});
