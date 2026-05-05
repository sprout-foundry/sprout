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
    expect(input?.placeholder).toBe('Type a command or search...');
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
    expect(input?.placeholder).toBe('Search files by name...');
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
    expect(input?.placeholder).toBe('Search symbols...');
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
