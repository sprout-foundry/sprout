// @ts-nocheck

import { createElement, act } from 'react';
import { createRoot } from 'react-dom/client';
import type { Root } from 'react-dom/client';
import { HotkeyProvider } from '../contexts/HotkeyContext';
import { NotificationProvider } from '../contexts/NotificationContext';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import type { HostNavigationIntent, SproutHost } from '../host/types';
import {
  __resetCommandHandlersForTests,
  claimAppShell,
  registerCommandHandler,
  registerEventHandler,
} from '../services/commandHandlers';
import { useHotkeyCommandHandler } from '../hooks/useHotkeyCommandHandler';
import MenuBar from './MenuBar';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// The File and Edit items follow the editor's state; tests set it here (null =
// rendered outside an editor).
let mockEditor: {
  activeBufferId: string | null;
  buffers: Map<string, { id: string; kind: string; isModified: boolean }>;
} | null = null;
vi.mock('../contexts/EditorManagerContext', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../contexts/EditorManagerContext')>()),
  useOptionalEditorManager: () => mockEditor,
}));

vi.mock('../services/api', () => {
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
  vi.clearAllMocks();
  // The menu suite runs as the standalone app, which handles every command;
  // the embed describe below resets this.
  claimAppShell();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  __resetCommandHandlersForTests();
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
let openedIntents: HostNavigationIntent[] = [];

function menuBarHost(): SproutHost {
  return {
    ...makeTestHost(),
    navigation: {
      open(intent) {
        openedIntents.push(intent);
      },
    },
  };
}

async function renderMenuBar(alongside?: ReturnType<typeof createElement>) {
  openedIntents = [];
  await act(async () => {
    root.render(
      createElement(
        HostProvider,
        { host: menuBarHost() },
        createElement(
          NotificationProvider,
          null,
          createElement(HotkeyProvider, null, createElement(MenuBar), alongside ?? null),
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

/** Mounts the app's sprout:hotkey command handler with inert callbacks. */
const noop = () => {};
function HotkeyCommands() {
  useHotkeyCommandHandler({
    onToggleCommandPalette: noop,
    onOpenCommandPalette: noop,
    onNewFile: noop,
    onToggleSidebar: noop,
    onToggleTerminal: noop,
    onPrimaryViewChange: noop,
    onFocusTabIndex: noop,
    onFocusPaneIndex: noop,
    onSplitRequest: noop,
    onCloseBuffer: noop,
    onCloseAllBuffers: noop,
    onCloseOtherBuffers: noop,
    onSaveAllBuffers: noop,
    onSwitchToBuffer: noop,
    onSwitchPane: noop,
    onTogglePinTab: noop,
    activeBufferId: null,
    activePaneId: null,
    buffers: new Map(),
  });
  return null;
}

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
      'Open File\u2026',
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
    const aboutItem = items.find((el) => el.textContent?.includes('About sprout'));
    expect(aboutItem).toBeDefined();

    // The "About sprout" item should not have a .menu-item-shortcut
    const shortcut = aboutItem!.querySelector('.menu-item-shortcut');
    expect(shortcut).toBeNull();
  });

  // ─── Toggle items ─────────────────────────────────────────────────

  function viewItem(label: string): HTMLElement {
    const item = getDropdownItems().find((el) => el.textContent?.includes(label));
    expect(item).toBeDefined();
    return item!;
  }

  test('toggle check marks read the editor settings the toggles change', async () => {
    localStorage.setItem('editor:minimap-enabled', 'false');
    localStorage.removeItem('editor:word-wrap-enabled');
    await renderMenuBar();
    openMenu(2); // View

    // Minimap explicitly off; word wrap defaults on; linked scrolling starts off.
    expect(viewItem('Toggle Minimap').querySelector('.menu-item-check')!.textContent).toBe('');
    expect(viewItem('Toggle Word Wrap').querySelector('.menu-item-check')!.textContent).toBe('✓');
    expect(viewItem('Toggle Linked Scrolling').querySelector('.menu-item-check')!.textContent).toBe('');

    localStorage.removeItem('editor:minimap-enabled');
  });

  test('toggle items reach the editor through the hotkey command handler', async () => {
    const editorToggles: string[] = [];
    const record = (e: Event) => editorToggles.push(e.type);
    for (const type of ['editor-toggle-word-wrap', 'editor-toggle-minimap', 'editor-toggle-linked-scroll']) {
      document.addEventListener(type, record);
    }
    await renderMenuBar(createElement(HotkeyCommands));

    for (const label of ['Toggle Word Wrap', 'Toggle Minimap', 'Toggle Linked Scrolling']) {
      openMenu(2); // View
      const item = viewItem(label);
      act(() => {
        item.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
    }

    expect(editorToggles).toEqual(['editor-toggle-word-wrap', 'editor-toggle-minimap', 'editor-toggle-linked-scroll']);
    for (const type of ['editor-toggle-word-wrap', 'editor-toggle-minimap', 'editor-toggle-linked-scroll']) {
      document.removeEventListener(type, record);
    }
  });

  // ─── Clicking menu items dispatches commands ─────────────────────

  test('clicking a menu item dispatches sprout:hotkey custom event with correct commandId', async () => {
    await renderMenuBar();
    openMenu(0); // File

    const handler = vi.fn();
    window.addEventListener('sprout:hotkey', handler);

    // Click "New File"
    const items = getDropdownItems();
    const newItem = items.find((el) => el.textContent?.includes('New File'))!;
    act(() => {
      newItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail.commandId).toBe('new_file');

    window.removeEventListener('sprout:hotkey', handler);
  });

  test('pressing Enter on a highlighted item dispatches the correct command', async () => {
    await renderMenuBar();
    openMenu(0); // File

    // "New File" should be highlighted at index 0
    const handler = vi.fn();
    window.addEventListener('sprout:hotkey', handler);

    act(() => {
      fireKeyDown('Enter');
    });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail.commandId).toBe('new_file');
    expect(getDropdown()).toBeNull(); // Menu closes after execution

    window.removeEventListener('sprout:hotkey', handler);
  });

  test('clicking "About sprout" shows an alert() with the real build version and closes the menu', async () => {
    const alertSpy = vi.spyOn(window, 'alert').mockImplementation(() => {});
    await renderMenuBar();
    openMenu(4); // Help

    const items = getDropdownItems();
    const aboutItem = items.find((el) => el.textContent?.includes('About sprout'))!;

    act(() => {
      aboutItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(alertSpy).toHaveBeenCalledTimes(1);
    // The version comes from the build/bootstrap (defaults to 'dev'), never a
    // hardcoded literal.
    expect(alertSpy.mock.calls[0][0]).toMatch(/^sprout WebUI\nVersion \S+\n\n/);
    expect(alertSpy.mock.calls[0][0]).not.toContain('Version 1.0.0');

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
      'Command Palette\u2026',
      'Toggle File Explorer',
      'Toggle Sidebar',
      'Toggle Terminal',
    ]);
  });

  test('Help menu shows "Keyboard Shortcuts", divider, "Report Issue", divider, and "About sprout"', async () => {
    await renderMenuBar();
    openMenu(4); // Help

    const items = getDropdownItems();
    const texts = items.map((el) => el.textContent?.trim());
    expect(texts).toEqual(['Keyboard Shortcuts', 'Export Diagnostics', 'Report Issue', 'About sprout']);

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

  test('clicking "Keyboard Shortcuts" dispatches sprout:open-hotkeys-config event', async () => {
    await renderMenuBar();
    openMenu(4); // Help

    const handler = vi.fn();
    window.addEventListener('sprout:open-hotkeys-config', handler);

    const items = getDropdownItems();
    const shortcutsItem = items.find((el) => el.textContent?.includes('Keyboard Shortcuts'))!;

    act(() => {
      shortcutsItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(handler).toHaveBeenCalledTimes(1);

    window.removeEventListener('sprout:open-hotkeys-config', handler);
  });

  test('clicking "Report Issue" dispatches the host reportBug intent', async () => {
    await renderMenuBar();
    openMenu(4); // Help

    const items = getDropdownItems();
    const reportItem = items.find((el) => el.textContent?.includes('Report Issue'))!;

    act(() => {
      reportItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // The component never hardcodes the repository URL: it asks the host to
    // resolve the `reportBug` intent (the local host opens the prefilled
    // public issue; a platform resolves it to its own support flow).
    expect(openedIntents.map((i) => i.type)).toEqual(['reportBug']);
  });
});

describe('MenuBar in an embed (no app shell)', () => {
  beforeEach(() => {
    __resetCommandHandlersForTests();
  });

  function dropdownLabels(): string[] {
    return getDropdownItems().map((el) => el.textContent ?? '');
  }

  test('renders nothing when no menu command has a handler', async () => {
    await renderMenuBar();
    expect(container.querySelector('.menu-bar')).toBeNull();
  });

  test('shows only the items something handles, and updates as handlers mount', async () => {
    await renderMenuBar();
    expect(container.querySelector('.menu-bar')).toBeNull();

    let unregister: () => void = () => {};
    act(() => {
      unregister = registerCommandHandler(['new_file', 'toggle_sidebar']);
    });
    expect(getMenuTitles().map((t) => t.textContent)).toEqual(['File', 'Edit', 'Help']);

    openMenu(0);
    expect(dropdownLabels()).toHaveLength(1);
    expect(dropdownLabels()[0]).toContain('New File');
    closeMenu(0);

    // Help keeps its self-handled items; Keyboard Shortcuts needs a listener.
    openMenu(2);
    expect(dropdownLabels().some((l) => l.includes('Keyboard Shortcuts'))).toBe(false);
    expect(dropdownLabels().some((l) => l.includes('Report Issue'))).toBe(true);
    closeMenu(2);

    let unregisterEvent: () => void = () => {};
    act(() => {
      unregisterEvent = registerEventHandler('sprout:open-hotkeys-config');
    });
    openMenu(2);
    expect(dropdownLabels().some((l) => l.includes('Keyboard Shortcuts'))).toBe(true);
    closeMenu(2);

    act(() => {
      unregister();
      unregisterEvent();
    });
    expect(container.querySelector('.menu-bar')).toBeNull();
  });
});

describe('File and Edit items follow the editor', () => {
  function item(label: string): HTMLButtonElement {
    const found = getDropdownItems().find((el) => el.textContent?.trim().startsWith(label));
    expect(found, label).toBeDefined();
    return found as HTMLButtonElement;
  }

  function enabledLabels(menuIndex: number): string[] {
    openMenu(menuIndex);
    const labels = getDropdownItems()
      .filter((el) => !(el as HTMLButtonElement).disabled)
      .map((el) => el.querySelector('.menu-item-label')?.textContent?.trim() ?? '');
    closeMenu(menuIndex);
    return labels;
  }

  afterEach(() => {
    mockEditor = null;
  });

  test('outside an editor only the items with no editor target are enabled', async () => {
    mockEditor = null;
    await renderMenuBar();
    expect(enabledLabels(0)).toEqual(['New File', 'Open File…']);
    expect(enabledLabels(1)).toEqual(['Command Palette…', 'Toggle File Explorer', 'Toggle Sidebar', 'Toggle Terminal']);

    // Disabled items take no action index, so the enabled ones after them
    // still run themselves.
    const hotkey = vi.fn();
    window.addEventListener('sprout:hotkey', hotkey);
    openMenu(1);
    expect(item('Undo').disabled).toBe(true);
    act(() => {
      item('Command Palette').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(hotkey).toHaveBeenCalledTimes(1);
    expect(hotkey.mock.calls[0][0].detail.commandId).toBe('command_palette');
    window.removeEventListener('sprout:hotkey', hotkey);
  });

  test('a clean text editor enables save, close and the editing commands', async () => {
    mockEditor = {
      activeBufferId: 'a',
      buffers: new Map([['a', { id: 'a', kind: 'file', isModified: false }]]),
    };
    await renderMenuBar();
    // Save All needs an unsaved buffer; Close Other needs a second one.
    expect(enabledLabels(0)).toEqual(['New File', 'Open File…', 'Save', 'Close Editor', 'Close All Editors']);
    expect(enabledLabels(1)).toHaveLength(12);
  });

  test('a non-text buffer can be closed but not saved or edited', async () => {
    mockEditor = {
      activeBufferId: 'd',
      buffers: new Map([
        ['d', { id: 'd', kind: 'diff', isModified: false }],
        ['b', { id: 'b', kind: 'file', isModified: true }],
      ]),
    };
    await renderMenuBar();
    expect(enabledLabels(0)).toEqual([
      'New File',
      'Open File…',
      'Save All',
      'Close Editor',
      'Close All Editors',
      'Close Other Editors',
    ]);
    expect(enabledLabels(1)).not.toContain('Undo');
  });

  test('clicks send the commands the shortcuts and command palette use', async () => {
    mockEditor = {
      activeBufferId: 'a',
      buffers: new Map([
        ['a', { id: 'a', kind: 'file', isModified: true }],
        ['b', { id: 'b', kind: 'file', isModified: false }],
      ]),
    };
    const editorEvents: string[] = [];
    const record = (e: Event) => editorEvents.push(e.type);
    const types = [
      'editor-save-current',
      'editor-undo',
      'editor-cut',
      'editor-copy',
      'editor-paste',
      'editor-find',
      'editor-find-replace',
      'editor-select-all',
    ];
    for (const type of types) document.addEventListener(type, record);
    const hotkeys: string[] = [];
    const recordHotkey = (e: Event) => hotkeys.push((e as CustomEvent).detail.commandId);
    window.addEventListener('sprout:hotkey', recordHotkey);
    await renderMenuBar(createElement(HotkeyCommands));

    const click = (menuIndex: number, label: string) => {
      openMenu(menuIndex);
      act(() => {
        item(label).dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
    };
    click(0, 'Save');
    click(0, 'Open File');
    click(1, 'Undo');
    click(1, 'Cut');
    click(1, 'Copy');
    click(1, 'Paste');
    click(1, 'Find and Replace');
    click(1, 'Find');
    click(1, 'Select All');
    click(1, 'Command Palette');

    expect(hotkeys).toEqual([
      'save_file',
      'quick_open',
      'undo',
      'editor_cut',
      'editor_copy',
      'editor_paste',
      'editor_replace',
      'editor_find',
      'editor_select_all',
      'command_palette',
    ]);
    expect(editorEvents).toEqual([
      'editor-save-current',
      'editor-undo',
      'editor-cut',
      'editor-copy',
      'editor-paste',
      'editor-find-replace',
      'editor-find',
      'editor-select-all',
    ]);
    for (const type of types) document.removeEventListener(type, record);
    window.removeEventListener('sprout:hotkey', recordHotkey);
  });
});
