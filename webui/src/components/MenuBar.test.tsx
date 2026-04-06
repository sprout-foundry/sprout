// @ts-nocheck

import { createElement } from 'react';
import { createRoot } from 'react-dom/client';
import type { Root } from 'react-dom/client';
import { act } from 'react';

import MenuBar from './MenuBar';
import { HotkeyProvider } from '../contexts/HotkeyContext';
import { NotificationProvider } from '../contexts/NotificationContext';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../services/api', () => {
  class MockApiService {
    private static instance: MockApiService;
    static getInstance() {
      if (!MockApiService.instance) MockApiService.instance = new MockApiService();
      return MockApiService.instance;
    }
    async getHotkeys() {
      return {
        hotkeys: [
          { key: 'Ctrl+N', command_id: 'new_file' },
          { key: 'Ctrl+P', command_id: 'quick_open' },
          { key: 'Ctrl+S', command_id: 'save_file', global: true },
          { key: 'Ctrl+Shift+S', command_id: 'save_all_files', global: true },
          { key: 'Ctrl+W', command_id: 'close_editor', global: false },
          { key: 'Ctrl+Shift+W', command_id: 'close_all_editors', global: true },
          { key: 'Ctrl+Alt+W', command_id: 'close_other_editors', global: true },
        ],
      };
    }
    async applyHotkeyPreset() {
      return {};
    }
  }
  return { ApiService: MockApiService };
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
  jest.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  // Clean up any portal dropdowns left on body
  document.querySelectorAll('.menu-bar-dropdown').forEach((el) => {
    if (el.parentNode) el.parentNode.removeChild(el);
  });
});

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/**
 * Renders the MenuBar wrapped in the required providers.
 * Returns async so callers can await hotkey loading.
 */
async function renderMenuBar() {
  await act(async () => {
    root.render(
      createElement(
        NotificationProvider,
        null,
        createElement(
          HotkeyProvider,
          null,
          createElement(MenuBar),
        ),
      ),
    );
  });
  // Let the hotkey provider's async loadHotkeys() settle
  await flushPromises();
}

/** Get the `.menu-bar` container element. */
function getMenuBar(): HTMLElement {
  return container.querySelector('.menu-bar')!;
}

/** Get all menu title buttons inside the bar. */
function getMenuTitles(): HTMLElement[] {
  return Array.from(getMenuBar().querySelectorAll('.menu-bar-title'));
}

/** Open a dropdown by clicking the menu title at the given index. */
function openMenu(index: number) {
  const titles = getMenuTitles();
  act(() => {
    titles[index].dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

/** Close the currently-open menu by clicking the same title again. */
function closeMenu(index: number) {
  const titles = getMenuTitles();
  act(() => {
    titles[index].dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

/** Get the dropdown portal element on document.body (or null). */
function getDropdown(): HTMLElement | null {
  return document.querySelector('.menu-bar-dropdown');
}

/** Get all actionable menu items (buttons) in the dropdown. */
function getDropdownItems(): HTMLElement[] {
  const dd = getDropdown();
  return dd ? Array.from(dd.querySelectorAll('.context-menu-item')) : [];
}

/** Get all divider elements in the dropdown. */
function getDropdownDividers(): HTMLElement[] {
  const dd = getDropdown();
  return dd ? Array.from(dd.querySelectorAll('.context-menu-divider')) : [];
}

/** Dispatch a keyboard event on window inside act(). */
function fireKeyDown(key: string, opts: Partial<KeyboardEventInit> = {}) {
  act(() => {
    window.dispatchEvent(
      new KeyboardEvent('keydown', {
        key,
        bubbles: true,
        cancelable: true,
        ...opts,
      }),
    );
  });
}

/** Dispatch a keyup event on window inside act(). */
function fireKeyUp(key: string, opts: Partial<KeyboardEventInit> = {}) {
  act(() => {
    window.dispatchEvent(
      new KeyboardEvent('keyup', {
        key,
        ...opts,
      }),
    );
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MenuBar', () => {
  // ─── Basic rendering ──────────────────────────────────────────────

  test('renders without crashing', async () => {
    await renderMenuBar();
    expect(getMenuBar()).not.toBeNull();
  });

  test('renders a .menu-bar container with role="menubar"', async () => {
    await renderMenuBar();
    const bar = getMenuBar();
    expect(bar.getAttribute('role')).toBe('menubar');
  });

  test('renders 5 menu title buttons: File, Edit, View, Terminal, Help', async () => {
    await renderMenuBar();
    const titles = getMenuTitles();
    expect(titles.length).toBe(5);

    const labels = titles.map((t) => t.textContent?.trim());
    expect(labels).toEqual(['File', 'Edit', 'View', 'Terminal', 'Help']);
  });

  test('menu titles have correct ARIA attributes', async () => {
    await renderMenuBar();
    const titles = getMenuTitles();
    for (const title of titles) {
      expect(title.getAttribute('role')).toBe('menuitem');
      expect(title.getAttribute('aria-haspopup')).toBe('menu');
    }
  });

  // ─── Dropdown open/close ──────────────────────────────────────────

  test('clicking File title opens a dropdown', async () => {
    await renderMenuBar();
    expect(getDropdown()).toBeNull();

    openMenu(0); // File

    const dd = getDropdown();
    expect(dd).not.toBeNull();
  });

  test('clicking the same title again closes the dropdown', async () => {
    await renderMenuBar();
    openMenu(0);
    expect(getDropdown()).not.toBeNull();

    closeMenu(0);
    expect(getDropdown()).toBeNull();
  });

  test('dropdown is a portal on document.body', async () => {
    await renderMenuBar();
    openMenu(0);

    const dd = getDropdown();
    expect(dd).not.toBeNull();
    expect(dd!.parentElement).toBe(document.body);
  });

  test('dropdown has role="menu"', async () => {
    await renderMenuBar();
    openMenu(0);

    const dd = getDropdown();
    expect(dd!.getAttribute('role')).toBe('menu');
  });

  test('only one dropdown can be open at a time', async () => {
    await renderMenuBar();
    openMenu(0); // File
    expect(getDropdown()).not.toBeNull();

    openMenu(1); // Edit — should replace the File dropdown
    const dd = getDropdown();
    expect(dd).not.toBeNull();
    // The dropdown's aria-label should now be "Edit menu"
    expect(dd!.getAttribute('aria-label')).toBe('Edit menu');
  });

  // ─── File menu items ──────────────────────────────────────────────

  test('File menu shows correct items with labels', async () => {
    await renderMenuBar();
    openMenu(0);

    const items = getDropdownItems();
    // Extract just the label text (excluding the shortcut span)
    const texts = items.map((el) => {
      const label = el.querySelector('.menu-item-label');
      return label ? label.textContent?.trim() : el.textContent?.trim();
    });

    expect(texts).toEqual([
      'New File',
      'Open File...',
      'Save',
      'Save All',
      'Close Editor',
      'Close All Editors',
      'Close Other Editors',
    ]);
  });

  test('File menu has 1 divider (after Save All)', async () => {
    await renderMenuBar();
    openMenu(0);

    const dividers = getDropdownDividers();
    expect(dividers.length).toBe(1);
  });

  test('dropdown items have role="menuitem"', async () => {
    await renderMenuBar();
    openMenu(0);

    const items = getDropdownItems();
    for (const item of items) {
      expect(item.getAttribute('role')).toBe('menuitem');
    }
  });

  test('divider items have class "context-menu-divider" and role="separator"', async () => {
    await renderMenuBar();
    openMenu(0);

    const dividers = getDropdownDividers();
    expect(dividers.length).toBeGreaterThanOrEqual(1);
    for (const d of dividers) {
      expect(d.classList.contains('context-menu-divider')).toBe(true);
      expect(d.getAttribute('role')).toBe('separator');
    }
  });

  // ─── Keyboard shortcuts display ───────────────────────────────────

  test('menu items display keyboard shortcuts in .menu-item-shortcut elements', async () => {
    await renderMenuBar();
    openMenu(0); // File

    const dd = getDropdown()!;
    const shortcuts = Array.from(dd.querySelectorAll('.menu-item-shortcut'));
    expect(shortcuts.length).toBeGreaterThan(0);

    const shortcutTexts = shortcuts.map((s) => s.textContent?.trim());
    // "New File" → Ctrl+N, "Save" → Ctrl+S, "Save All" → Ctrl+Shift+S
    expect(shortcutTexts).toContain('Ctrl+N');
    expect(shortcutTexts).toContain('Ctrl+S');
    expect(shortcutTexts).toContain('Ctrl+Shift+S');
  });

  test('"about" item does not show a shortcut even though it has a commandId', async () => {
    await renderMenuBar();
    openMenu(4); // Help

    const dd = getDropdown()!;
    const items = getDropdownItems();
    const aboutItem = items.find((el) => el.textContent?.includes('About ledit'));
    expect(aboutItem).toBeDefined();

    // The "About ledit" item should not have a .menu-item-shortcut
    const shortcut = aboutItem!.querySelector('.menu-item-shortcut');
    expect(shortcut).toBeNull();
  });

  // ─── Toggle items ─────────────────────────────────────────────────

  test('toggle item "Toggle Minimap" shows checkmark when editor:minimap-enabled is true', async () => {
    localStorage.setItem('editor:minimap-enabled', 'true');
    await renderMenuBar();
    openMenu(2); // View

    const items = getDropdownItems();
    const minimapItem = items.find(
      (el) => el.textContent?.includes('Toggle Minimap'),
    );
    expect(minimapItem).toBeDefined();

    const check = minimapItem!.querySelector('.menu-item-check');
    expect(check).not.toBeNull();
    expect(check!.textContent).toBe('✓');

    localStorage.removeItem('editor:minimap-enabled');
  });

  test('toggle item "Toggle Minimap" hides checkmark when editor:minimap-enabled is false', async () => {
    localStorage.setItem('editor:minimap-enabled', 'false');
    await renderMenuBar();
    openMenu(2); // View

    const items = getDropdownItems();
    const minimapItem = items.find(
      (el) => el.textContent?.includes('Toggle Minimap'),
    );
    expect(minimapItem).toBeDefined();

    const check = minimapItem!.querySelector('.menu-item-check');
    expect(check).not.toBeNull();
    expect(check!.textContent).toBe('');

    localStorage.removeItem('editor:minimap-enabled');
  });

  test('toggle item "Toggle Minimap" hides checkmark when localStorage key is absent', async () => {
    localStorage.removeItem('editor:minimap-enabled');
    await renderMenuBar();
    openMenu(2); // View

    const items = getDropdownItems();
    const minimapItem = items.find(
      (el) => el.textContent?.includes('Toggle Minimap'),
    );
    expect(minimapItem).toBeDefined();

    const check = minimapItem!.querySelector('.menu-item-check');
    expect(check).not.toBeNull();
    expect(check!.textContent).toBe('');
  });

  // ─── Clicking menu items dispatches commands ─────────────────────

  test('clicking a menu item dispatches ledit:hotkey custom event with correct commandId', async () => {
    await renderMenuBar();
    openMenu(0); // File

    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    // Click "New File"
    const items = getDropdownItems();
    const newItem = items.find((el) => el.textContent?.includes('New File'))!;
    act(() => {
      newItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail.commandId).toBe('new_file');

    window.removeEventListener('ledit:hotkey', handler);
  });

  test('pressing Enter on a highlighted item dispatches the correct command', async () => {
    await renderMenuBar();
    openMenu(0); // File

    // "New File" should be highlighted at index 0
    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    act(() => {
      fireKeyDown('Enter');
    });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail.commandId).toBe('new_file');
    expect(getDropdown()).toBeNull(); // Menu closes after execution

    window.removeEventListener('ledit:hotkey', handler);
  });

  test('clicking "About ledit" shows an alert() and closes the menu', async () => {
    const alertSpy = jest.spyOn(window, 'alert').mockImplementation(() => {});
    await renderMenuBar();
    openMenu(4); // Help

    const items = getDropdownItems();
    const aboutItem = items.find((el) => el.textContent?.includes('About ledit'))!;

    act(() => {
      aboutItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(alertSpy).toHaveBeenCalledTimes(1);
    expect(alertSpy).toHaveBeenCalledWith(
      'ledit WebUI\nVersion 1.0.0\n\nA modern, keyboard-accessible code editor.',
    );

    // Menu should be closed after clicking
    expect(getDropdown()).toBeNull();

    alertSpy.mockRestore();
  });

  // ─── Close on outside click ───────────────────────────────────────

  test('clicking outside the dropdown closes it', async () => {
    await renderMenuBar();
    openMenu(0);
    expect(getDropdown()).not.toBeNull();

    act(() => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(getDropdown()).toBeNull();
  });

  // ─── Escape key ───────────────────────────────────────────────────

  test('Escape key closes an open dropdown', async () => {
    await renderMenuBar();
    openMenu(0);
    expect(getDropdown()).not.toBeNull();

    fireKeyDown('Escape');

    expect(getDropdown()).toBeNull();
  });

  // ─── Alt+mnemonic ─────────────────────────────────────────────────

  test('Alt+E opens the Edit menu via mnemonic', async () => {
    await renderMenuBar();
    expect(getDropdown()).toBeNull();

    // Fire Alt keydown then E keydown (as in Alt+E)
    fireKeyDown('E', { altKey: true });

    const dd = getDropdown();
    expect(dd).not.toBeNull();
    expect(dd!.getAttribute('aria-label')).toBe('Edit menu');
  });

  test('Alt+F opens the File menu via mnemonic', async () => {
    await renderMenuBar();

    fireKeyDown('F', { altKey: true });

    const dd = getDropdown();
    expect(dd).not.toBeNull();
    expect(dd!.getAttribute('aria-label')).toBe('File menu');
  });

  test('Alt+V opens the View menu via mnemonic', async () => {
    await renderMenuBar();

    fireKeyDown('V', { altKey: true });

    const dd = getDropdown();
    expect(dd).not.toBeNull();
    expect(dd!.getAttribute('aria-label')).toBe('View menu');
  });

  test('Alt+T opens the Terminal menu via mnemonic', async () => {
    await renderMenuBar();

    fireKeyDown('T', { altKey: true });

    const dd = getDropdown();
    expect(dd).not.toBeNull();
    expect(dd!.getAttribute('aria-label')).toBe('Terminal menu');
  });

  test('Alt+H opens the Help menu via mnemonic', async () => {
    await renderMenuBar();

    fireKeyDown('H', { altKey: true });

    const dd = getDropdown();
    expect(dd).not.toBeNull();
    expect(dd!.getAttribute('aria-label')).toBe('Help menu');
  });

  test('pressing the same Alt+mnemonic again closes the menu', async () => {
    await renderMenuBar();

    fireKeyDown('F', { altKey: true });
    expect(getDropdown()).not.toBeNull();

    fireKeyDown('F', { altKey: true });
    expect(getDropdown()).toBeNull();
  });

  // ─── Arrow key navigation ─────────────────────────────────────────

  test('ArrowRight cycles to the next menu', async () => {
    await renderMenuBar();
    openMenu(0); // File
    expect(getDropdown()!.getAttribute('aria-label')).toBe('File menu');

    fireKeyDown('ArrowRight');
    expect(getDropdown()!.getAttribute('aria-label')).toBe('Edit menu');

    fireKeyDown('ArrowRight');
    expect(getDropdown()!.getAttribute('aria-label')).toBe('View menu');
  });

  test('ArrowLeft cycles to the previous menu (wrapping)', async () => {
    await renderMenuBar();
    openMenu(0); // File

    fireKeyDown('ArrowLeft');
    // Wraps to Help (index 4)
    expect(getDropdown()!.getAttribute('aria-label')).toBe('Help menu');
  });

  test('ArrowDown highlights the next actionable item', async () => {
    await renderMenuBar();
    openMenu(0); // File

    // Initial activeItemIndex is 0
    fireKeyDown('ArrowDown');

    const dd = getDropdown()!;
    const items = dd.querySelectorAll('.context-menu-item');
    // The second item should now be selected
    expect(items[1]!.classList.contains('selected')).toBe(true);
  });

  test('ArrowUp highlights the previous actionable item', async () => {
    await renderMenuBar();
    openMenu(0); // File

    // Move down once to index 1
    fireKeyDown('ArrowDown');
    // Move up back to index 0
    fireKeyDown('ArrowUp');

    const dd = getDropdown()!;
    const items = dd.querySelectorAll('.context-menu-item');
    expect(items[0]!.classList.contains('selected')).toBe(true);
    expect(items[1]!.classList.contains('selected')).toBe(false);
  });

  // ─── Hover changes active menu while a menu is open ───────────────
  // NOTE: React 18 uses root-level event delegation and does NOT properly
  // handle mouseenter (which doesn't bubble) in jsdom's limited event
  // system. The hover-to-switch behavior is verified indirectly by the
  // ArrowRight/ArrowLeft navigation tests and the click-to-switch tests
  // above. Direct mouseenter-to-switch testing only works in real browsers.

  test('hovering a title when no menu is open does NOT open a menu', async () => {
    await renderMenuBar();
    expect(getDropdown()).toBeNull();

    const titles = getMenuTitles();
    act(() => {
      titles[0].dispatchEvent(new MouseEvent('mouseenter'));
    });

    expect(getDropdown()).toBeNull();
  });

  // ─── ARIA expanded state ──────────────────────────────────────────

  test('menu title has aria-expanded="true" when its dropdown is open', async () => {
    await renderMenuBar();

    openMenu(0);
    const titles = getMenuTitles();
    expect(titles[0].getAttribute('aria-expanded')).toBe('true');
  });

  test('menu title has aria-expanded="false" (absent) when dropdown is closed', async () => {
    await renderMenuBar();

    const titles = getMenuTitles();
    // When no menu is open, aria-expanded should be "false" (React renders
    // booleans as strings for aria-expanded)
    expect(titles[0].getAttribute('aria-expanded')).toBe('false');
  });

  // ─── Different menus have correct content ─────────────────────────

  test('View menu shows toggle items with .menu-item-check span', async () => {
    await renderMenuBar();
    openMenu(2); // View

    const dd = getDropdown()!;
    const checkSpans = dd.querySelectorAll('.menu-item-check');
    // View menu has three toggle items: Toggle Word Wrap, Toggle Minimap, Toggle Linked Scrolling
    expect(checkSpans.length).toBe(3);
  });

  test('Edit menu shows correct items', async () => {
    await renderMenuBar();
    openMenu(1); // Edit

    const items = getDropdownItems();
    const texts = items.map((el) => el.textContent?.trim());
    expect(texts).toEqual([
      'Undo',
      'Redo',
      'Cut',
      'Copy',
      'Paste',
      'Find',
      'Find and Replace',
      'Select All',
      'Command Palette...',
      'Toggle File Explorer',
      'Toggle Sidebar',
      'Toggle Terminal',
    ]);
  });

  test('Help menu shows "Keyboard Shortcuts", divider, "Report Issue", divider, and "About ledit"', async () => {
    await renderMenuBar();
    openMenu(4); // Help

    const items = getDropdownItems();
    const texts = items.map((el) => el.textContent?.trim());
    expect(texts).toEqual([
      'Keyboard Shortcuts',
      'Report Issue',
      'About ledit',
    ]);

    const dividers = getDropdownDividers();
    expect(dividers.length).toBe(2);
  });

  // ─── Mnemonic underlining (showMnemonics) ────────────────────────

  test('mnemonics are shown (first letter underlined) when Alt+mnemonic is pressed', async () => {
    await renderMenuBar();

    // Press Alt+F — this triggers showMnemonics=true in the component
    fireKeyDown('F', { altKey: true });

    const titles = getMenuTitles();
    // The first character of the File title should be underlined
    const html = titles[0].innerHTML;
    expect(html).toContain('<u>F</u>');

    // Close the menu first (Alt+F again)
    fireKeyDown('F', { altKey: true });

    // Release Alt — this sets showMnemonics=false
    fireKeyUp('Alt');

    // Mnemonics should no longer be shown
    const htmlAfter = titles[0].innerHTML;
    expect(htmlAfter).not.toContain('<u>');
  });

  test('clicking "Keyboard Shortcuts" dispatches ledit:open-hotkeys-config event', async () => {
    await renderMenuBar();
    openMenu(4); // Help

    const handler = jest.fn();
    window.addEventListener('ledit:open-hotkeys-config', handler);

    const items = getDropdownItems();
    const shortcutsItem = items.find(el => el.textContent?.includes('Keyboard Shortcuts'))!;

    act(() => {
      shortcutsItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(handler).toHaveBeenCalledTimes(1);

    window.removeEventListener('ledit:open-hotkeys-config', handler);
  });

  test('clicking "Report Issue" opens the GitHub issues URL', async () => {
    const openSpy = jest.spyOn(window, 'open').mockImplementation(() => null);
    await renderMenuBar();
    openMenu(4); // Help

    const items = getDropdownItems();
    const reportItem = items.find(el => el.textContent?.includes('Report Issue'))!;

    act(() => {
      reportItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const expectedUrl = 'https://github.com/alantheprice/ledit/issues/new';
    expect(openSpy).toHaveBeenCalledWith(expectedUrl, '_blank', 'noopener,noreferrer');

    openSpy.mockRestore();
  });
});
