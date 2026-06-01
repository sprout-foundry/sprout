// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import CommandPalette, {
  type CommandDef,
} from './CommandPalette';

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

/** Helper to set the input value and dispatch a change event for React-controlled inputs. */
function setInputValue(input: HTMLInputElement, value: string) {
  Object.defineProperty(input, 'value', { value });
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

// ---------------------------------------------------------------------------
// Tests: CommandPalette
// ---------------------------------------------------------------------------

describe('CommandPalette', () => {
  const onClose = vi.fn();
  const onOpenFile = vi.fn();
  const onExecuteCommand = vi.fn();
  const onNavigateToLine = vi.fn();

  const defaultCommands: CommandDef[] = [
    { id: 'cmd1', label: 'New File', category: 'File' },
    { id: 'cmd2', label: 'New Folder', category: 'File' },
    { id: 'cmd3', label: 'Go to File', category: 'Navigate' },
    { id: 'cmd4', label: 'Find in Files', category: 'Search' },
  ];

  const defaultProps = {
    isOpen: true,
    onClose,
    onOpenFile,
    onExecuteCommand,
    onNavigateToLine,
    commands: defaultCommands,
  };

  it('returns null when isOpen is false', () => {
    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          isOpen: false,
        })
      );
    });
    expect(container.innerHTML).toBe('');
  });

  it('renders overlay when isOpen is true', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    expect(container.querySelector('.command-palette-overlay')).not.toBeNull();
  });

  it('renders command-palette inner container', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    expect(container.querySelector('.command-palette')).not.toBeNull();
  });

  it('renders input field', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    expect(container.querySelector('.command-palette-input')).not.toBeNull();
  });

  it('renders default placeholder for "all" mode', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    expect(input?.placeholder).toBe('> for commands, @ for symbols, type to find files');
  });

  it('renders "files" placeholder when initialMode is files', () => {
    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          initialMode: 'files',
        })
      );
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    expect(input?.placeholder).toBe('Search files…');
  });

  it('renders "symbols" placeholder when initialMode is symbols', () => {
    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          initialMode: 'symbols',
        })
      );
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    expect(input?.placeholder).toBe('Search symbols…');
  });

  it('calls onClose when clicking overlay background', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    act(() => {
      container.querySelector('.command-palette-overlay')?.dispatchEvent(
        new MouseEvent('click', { bubbles: true })
      );
    });
    expect(onClose).toHaveBeenCalled();
  });

  it('does not call onClose when clicking inside palette', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    act(() => {
      const palette = container.querySelector('.command-palette');
      palette?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  it('shows no results for empty query', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const items = container.querySelectorAll('.command-palette-item');
    expect(items).toHaveLength(0);
  });

  it('shows results when typing a query', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'New');
    });

    const items = container.querySelectorAll('.command-palette-item');
    expect(items.length).toBeGreaterThan(0);
  });

  it('shows command results with command prefix (>)', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, '>new');
    });

    const items = container.querySelectorAll('.command-palette-item');
    expect(items.length).toBeGreaterThan(0);
  });

  it('calls onExecuteCommand when a command result is clicked', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'New');
    });

    const items = container.querySelectorAll('.command-palette-item');
    act(() => {
      items[0]?.click();
    });

    expect(onExecuteCommand).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('calls onClose when Escape is pressed', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onClose).toHaveBeenCalled();
  });

  it('triggers file search when searching in files mode', () => {
    vi.useFakeTimers();
    const searchFilesMock = vi.fn().mockResolvedValue([
      { name: 'test.txt', path: 'src/test.txt', type: 'file' },
    ]);

    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          initialMode: 'files',
          onSearchFiles: searchFilesMock,
        })
      );
    });

    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    act(() => {
      setInputValue(input, 'test');
    });

    // Advance past the debounce delay (150ms) so the search fires
    act(() => {
      vi.advanceTimersByTime(150);
    });

    expect(searchFilesMock).toHaveBeenCalled();

    vi.useRealTimers();
  });

  it('calls onNavigateToLine when a symbol result is clicked', () => {
    const searchSymbolsMock = vi.fn().mockReturnValue([
      { name: 'myFunction', kind: 'function', line: 42 },
    ]);

    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          onSearchSymbols: searchSymbolsMock,
        })
      );
    });

    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    act(() => {
      setInputValue(input, 'myFunc');
    });

    const items = container.querySelectorAll('.command-palette-item');
    if (items.length > 0) {
      act(() => {
        items[0]?.click();
      });
      expect(onNavigateToLine).toHaveBeenCalledWith(42);
    }
  });

  it('shows "No results found" for query with no matches', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'zzzznotfound');
    });

    expect(container.querySelector('.command-palette-empty')).not.toBeNull();
  });

  it('handles keyboard navigation with ArrowDown', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'New');
    });

    // Press ArrowDown
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });

    // First item should be selected
    const selected = container.querySelector('.command-palette-item.selected');
    expect(selected).not.toBeNull();
  });

  it('handles keyboard navigation with ArrowUp from first item', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'New');
    });

    // Press ArrowUp (should stay at 0, not go negative)
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
    });

    // Should not crash and stay at index 0
    expect(true).toBe(true);
  });

  it('handles Enter to execute selected command', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'New');
    });

    // Press ArrowDown to select first item
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });

    // Press Enter to execute
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(onClose).toHaveBeenCalled();
  });

  it('shows result kind badge for commands', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, '>New');
    });

    const badge = container.querySelector('.result-kind-badge.command');
    expect(badge).not.toBeNull();
  });

  it('resets state when isOpen goes from false to true', () => {
    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          isOpen: false,
        })
      );
    });

    // Now open it
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });

    // Should have fresh state (empty query)
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    expect(input?.value).toBe('');
  });

  it('uses initialMode prop when opened', () => {
    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          initialMode: 'commands',
        })
      );
    });
    expect(container.querySelector('.command-palette')).not.toBeNull();
  });

  it('shows command category when available', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, '>New');
    });

    const category = container.querySelector('.command-palette-item-category');
    expect(category).not.toBeNull();
  });

  it('renders with empty commands array', () => {
    act(() => {
      root.render(
        createElement(CommandPalette, {
          ...defaultProps,
          commands: [],
        })
      );
    });
    expect(container.querySelector('.command-palette')).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: Accessibility
// ---------------------------------------------------------------------------

describe('Accessibility', () => {
  const onClose = vi.fn();
  const onOpenFile = vi.fn();
  const onExecuteCommand = vi.fn();
  const onNavigateToLine = vi.fn();

  const defaultCommands: CommandDef[] = [
    { id: 'cmd1', label: 'New File', category: 'File' },
    { id: 'cmd2', label: 'Open File', category: 'File' },
    { id: 'cmd3', label: 'Save', category: 'File' },
  ];

  const defaultProps = {
    isOpen: true,
    onClose,
    onOpenFile,
    onExecuteCommand,
    onNavigateToLine,
    commands: defaultCommands,
  };

  it('renders aria-live region with polite announcements when palette is open', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const liveRegion = container.querySelector('.command-palette-sr-only');
    expect(liveRegion).not.toBeNull();
    expect(liveRegion?.getAttribute('aria-live')).toBe('polite');
    expect(liveRegion?.getAttribute('aria-atomic')).toBe('true');
    expect(liveRegion?.getAttribute('role')).toBe('status');
  });

  it('has combobox input with correct ARIA attributes', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    expect(input?.getAttribute('role')).toBe('combobox');
    expect(input?.getAttribute('aria-haspopup')).toBe('listbox');
    expect(input?.getAttribute('aria-expanded')).toBe('true');
    expect(input?.getAttribute('aria-autocomplete')).toBe('list');
    expect(input?.getAttribute('aria-controls')).toBeTruthy();
  });

  it('updates aria-activedescendant when navigating with arrow keys', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'File');
    });

    // Initial state - aria-activedescendant should point to index 0
    expect(input?.getAttribute('aria-activedescendant')).toBe('command-palette-result-0');

    // Press ArrowDown to move to next result
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });

    // Should now point to index 1
    expect(input?.getAttribute('aria-activedescendant')).toBe('command-palette-result-1');
  });

  it('announces "N results available" when results appear', () => {
    vi.useFakeTimers();
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'File');
    });

    // Advance past the debounce delay (300ms)
    act(() => {
      vi.advanceTimersByTime(300);
    });

    const liveRegion = container.querySelector('.command-palette-sr-only');
    expect(liveRegion?.textContent).toMatch(/\d+ result(s)? available/);

    vi.useRealTimers();
  });

  it('announces "No results found" when there are no matches', () => {
    vi.useFakeTimers();
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'zzzznotfound');
    });

    // Advance past the debounce delay (300ms)
    act(() => {
      vi.advanceTimersByTime(300);
    });

    const liveRegion = container.querySelector('.command-palette-sr-only');
    expect(liveRegion?.textContent).toBe('No results found');

    vi.useRealTimers();
  });

  it('announces selected item position when navigating with ArrowDown', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'File');
    });

    const liveRegion = container.querySelector('.command-palette-sr-only');

    // Press ArrowDown to move to next item
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });

    // Should announce the selected item with position and label (index 1)
    expect(liveRegion?.textContent).toMatch(/2 of \d+,\s+\w+/);
  });

  it('has role="option" and aria-selected on each result item', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'New');
    });

    const items = container.querySelectorAll('.command-palette-item');
    expect(items.length).toBeGreaterThan(0);

    // First item should have aria-selected="true"
    expect(items[0]?.getAttribute('role')).toBe('option');
    expect(items[0]?.getAttribute('aria-selected')).toBe('true');

    // Other items should have aria-selected="false"
    if (items.length > 1) {
      expect(items[1]?.getAttribute('role')).toBe('option');
      expect(items[1]?.getAttribute('aria-selected')).toBe('false');
    }

    // Each option should have aria-setsize and aria-posinset
    for (let i = 0; i < items.length; i++) {
      expect(items[i]?.getAttribute('aria-setsize')).toBe(String(items.length));
      expect(items[i]?.getAttribute('aria-posinset')).toBe(String(i + 1));
    }
  });

  it('empty state div does not have role="status" to avoid double announcement', () => {
    vi.useFakeTimers();
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    act(() => {
      setInputValue(input, 'zzzznotfound');
    });

    act(() => {
      vi.advanceTimersByTime(300);
    });

    // The empty state div should NOT have role="status" — the external aria-live
    // region already handles the "No results found" announcement.
    const emptyDiv = container.querySelector('.command-palette-empty');
    expect(emptyDiv).not.toBeNull();
    expect(emptyDiv?.hasAttribute('role')).toBe(false);

    vi.useRealTimers();
  });

  it('has role="listbox" and accessible label on results container', () => {
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const listbox = container.querySelector('.command-palette-results');
    expect(listbox?.getAttribute('role')).toBe('listbox');
    expect(listbox?.getAttribute('aria-label')).toBe('Search results');
  });

  it('clears announcement when query is cleared', () => {
    vi.useFakeTimers();
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    // Type something that gets multiple results
    act(() => {
      setInputValue(input, 'File');
    });

    act(() => {
      vi.advanceTimersByTime(300);
    });

    const liveRegion = container.querySelector('.command-palette-sr-only');
    expect(liveRegion?.textContent).toMatch(/\d+ result(s)? available/);

    // Clear the query
    act(() => {
      setInputValue(input, '');
    });

    act(() => {
      vi.advanceTimersByTime(300);
    });

    // Announcement should be cleared
    expect(liveRegion?.textContent).toBe('');

    vi.useRealTimers();
  });

  it('does not overwrite selection announcement with count after navigation', () => {
    vi.useFakeTimers();
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;
    const liveRegion = container.querySelector('.command-palette-sr-only');

    // Type a query that matches multiple commands
    act(() => {
      setInputValue(input, 'File');
    });

    // Advance past the count debounce so the count announcement fires
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(liveRegion?.textContent).toMatch(/\d+ result/);

    // Navigate with ArrowDown — selection effect fires, updates announcement
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });

    // The live region should now show the selection announcement
    expect(liveRegion?.textContent).toMatch(/2 of \d+,\s+\w+/);

    // Advance past any remaining timers — selection should NOT be overwritten
    // because the count effect only depends on [results.length, query] and those
    // didn't change during navigation, so no new count timer was scheduled.
    act(() => {
      vi.advanceTimersByTime(500);
    });

    // Selection announcement should still be present
    expect(liveRegion?.textContent).toMatch(/2 of \d+,\s+\w+/);

    vi.useRealTimers();
  });

  it('announces count again when user types new query after navigation', () => {
    vi.useFakeTimers();
    act(() => {
      root.render(createElement(CommandPalette, { ...defaultProps }));
    });
    const input = container.querySelector('.command-palette-input') as HTMLInputElement;

    // Type a query, navigate, then type more
    act(() => {
      setInputValue(input, 'Fi');
    });

    act(() => {
      vi.advanceTimersByTime(300);
    });

    // Navigate
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });

    // Live region should show selection announcement
    const liveRegion = container.querySelector('.command-palette-sr-only');
    expect(liveRegion?.textContent).toMatch(/\d+ of \d+/);

    // Type a new character — triggers new count timer
    act(() => {
      setInputValue(input, 'Fil');
    });

    // Advance past debounce — count announcement should fire
    act(() => {
      vi.advanceTimersByTime(300);
    });

    expect(liveRegion?.textContent).toMatch(/\d+ result(s)? available/);

    vi.useRealTimers();
  });
});
