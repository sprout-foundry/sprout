// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import StatusBar from './StatusBar';
import type { CursorPosition } from './StatusBar';

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
  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('StatusBar', () => {
  it('renders as a footer element', () => {
    act(() => {
      root.render(createElement(StatusBar));
    });

    const footer = container.querySelector('footer');
    expect(footer).not.toBeNull();
    expect(footer?.classList.contains('statusbar')).toBe(true);
  });

  it('has aria-label="Editor status bar"', () => {
    act(() => {
      root.render(createElement(StatusBar));
    });

    const footer = container.querySelector('footer');
    expect(footer?.getAttribute('aria-label')).toBe('Editor status bar');
  });

  it('shows "No Git" when branch is not provided', () => {
    act(() => {
      root.render(createElement(StatusBar));
    });

    const gitItem = container.querySelector('.statusbar-item-git');
    expect(gitItem).not.toBeNull();
    expect(gitItem?.querySelector('.statusbar-text')?.textContent).toBe('No Git');
  });

  it('renders branch name when provided', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'main',
      }));
    });

    const gitItem = container.querySelector('.statusbar-item-git');
    expect(gitItem?.querySelector('.statusbar-text')?.textContent).toBe('main');
  });

  it('sets title attribute on git branch item', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'feature/login',
      }));
    });

    const gitItem = container.querySelector('.statusbar-item-git');
    expect(gitItem?.getAttribute('title')).toBe('Branch: feature/login');
  });

  it('sets title="Branch: unknown" when branch is not provided', () => {
    act(() => {
      root.render(createElement(StatusBar));
    });

    const gitItem = container.querySelector('.statusbar-item-git');
    expect(gitItem?.getAttribute('title')).toBe('Branch: unknown');
  });

  it('renders cursor position with 1-indexed values', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 0, column: 0 },
      }));
    });

    const cursorItem = container.querySelector('.statusbar-item-cursor');
    expect(cursorItem).not.toBeNull();
    expect(cursorItem?.textContent).toBe('Ln 1, Col 1');
  });

  it('renders cursor position with non-zero values', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 42, column: 10 },
      }));
    });

    const cursorItem = container.querySelector('.statusbar-item-cursor');
    expect(cursorItem?.textContent).toBe('Ln 43, Col 11');
  });

  it('does not render cursor position when not provided', () => {
    act(() => {
      root.render(createElement(StatusBar));
    });

    expect(container.querySelector('.statusbar-item-cursor')).toBeNull();
  });

  it('does not render cursor position when cursorPosition is incomplete', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        // @ts-expect-error — intentionally providing incomplete object to test validation
        cursorPosition: { line: 5 },
      }));
    });

    expect(container.querySelector('.statusbar-item-cursor')).toBeNull();
  });

  it('cursor position is aria-hidden to prevent screen reader spam', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 5, column: 10 },
      }));
    });

    const cursorItem = container.querySelector('.statusbar-item-cursor');
    expect(cursorItem?.getAttribute('aria-hidden')).toBe('true');
  });

  it('renders language when provided', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        language: 'TypeScript',
      }));
    });

    const langItem = container.querySelector('.statusbar-item-language');
    expect(langItem).not.toBeNull();
    expect(langItem?.textContent).toBe('TypeScript');
    expect(langItem?.getAttribute('title')).toBe('Language: TypeScript');
  });

  it('does not render language when not provided', () => {
    act(() => {
      root.render(createElement(StatusBar));
    });

    expect(container.querySelector('.statusbar-item-language')).toBeNull();
  });

  it('renders encoding with default UTF-8', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 0, column: 0 },
      }));
    });

    const encItem = container.querySelector('.statusbar-item-encoding');
    expect(encItem).not.toBeNull();
    expect(encItem?.textContent).toBe('UTF-8');
    expect(encItem?.getAttribute('title')).toBe('File encoding');
  });

  it('renders custom encoding', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        encoding: 'UTF-16',
      }));
    });

    const encItem = container.querySelector('.statusbar-item-encoding');
    expect(encItem?.textContent).toBe('UTF-16');
  });

  it('renders line ending with default LF', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 0, column: 0 },
      }));
    });

    const leItem = container.querySelector('.statusbar-item-line-ending');
    expect(leItem).not.toBeNull();
    expect(leItem?.textContent).toBe('LF');
    expect(leItem?.getAttribute('title')).toBe('Line ending format');
  });

  it('renders custom line ending', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        lineEnding: 'CRLF',
      }));
    });

    const leItem = container.querySelector('.statusbar-item-line-ending');
    expect(leItem?.textContent).toBe('CRLF');
  });

  it('renders indentation with default Spaces: 2', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 0, column: 0 },
      }));
    });

    const indItem = container.querySelector('.statusbar-item-indentation');
    expect(indItem).not.toBeNull();
    expect(indItem?.textContent).toBe('Spaces: 2');
    expect(indItem?.getAttribute('title')).toBe('Indentation');
  });

  it('renders custom indentation', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        indentation: 'Tab',
      }));
    });

    const indItem = container.querySelector('.statusbar-item-indentation');
    expect(indItem?.textContent).toBe('Tab');
  });

  it('renders leftItems when provided instead of default git item', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'main',
        leftItems: createElement('span', { 'data-testid': 'custom-left' }, 'Custom Left'),
      }));
    });

    expect(container.querySelector('[data-testid="custom-left"]')).not.toBeNull();
    // Default git item should NOT be present
    expect(container.querySelector('.statusbar-item-git')).toBeNull();
  });

  it('renders rightItems when provided instead of default right section', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        rightItems: createElement('span', { 'data-testid': 'custom-right' }, 'Custom Right'),
      }));
    });

    expect(container.querySelector('[data-testid="custom-right"]')).not.toBeNull();
    // Default right section items should NOT be present
    expect(container.querySelector('.statusbar-item-cursor')).toBeNull();
    expect(container.querySelector('.statusbar-item-language')).toBeNull();
  });

  it('applies custom className alongside statusbar', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        className: 'my-statusbar',
      }));
    });

    const footer = container.querySelector('footer');
    expect(footer?.classList.contains('statusbar')).toBe(true);
    expect(footer?.classList.contains('my-statusbar')).toBe(true);
  });

  it('hides right section when showRightSection is false', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'main',
        cursorPosition: { line: 0, column: 0 },
        language: 'Go',
        showRightSection: false,
      }));
    });

    expect(container.querySelector('.statusbar-right')).toBeNull();
    // Left section should still be visible
    expect(container.querySelector('.statusbar-left')).not.toBeNull();
  });

  it('shows right section when showRightSection is true (default)', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        cursorPosition: { line: 0, column: 0 },
      }));
    });

    expect(container.querySelector('.statusbar-right')).not.toBeNull();
  });

  it('does not show right section when showRightSection is true but no right-side props', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'main',
        showRightSection: true,
      }));
    });

    // Even with showRightSection=true, if there are no right-side props,
    // the right section should not render (the conditional check)
    expect(container.querySelector('.statusbar-right')).toBeNull();
  });

  it('renders all default right section items together', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'develop',
        cursorPosition: { line: 99, column: 20 },
        language: 'Rust',
        encoding: 'ASCII',
        lineEnding: 'CRLF',
        indentation: 'Spaces: 4',
      }));
    });

    expect(container.querySelector('.statusbar-left')).not.toBeNull();
    expect(container.querySelector('.statusbar-right')).not.toBeNull();

    const gitItem = container.querySelector('.statusbar-item-git');
    expect(gitItem?.querySelector('.statusbar-text')?.textContent).toBe('develop');

    const cursorItem = container.querySelector('.statusbar-item-cursor');
    expect(cursorItem?.textContent).toBe('Ln 100, Col 21');

    const langItem = container.querySelector('.statusbar-item-language');
    expect(langItem?.textContent).toBe('Rust');

    const encItem = container.querySelector('.statusbar-item-encoding');
    expect(encItem?.textContent).toBe('ASCII');

    const leItem = container.querySelector('.statusbar-item-line-ending');
    expect(leItem?.textContent).toBe('CRLF');

    const indItem = container.querySelector('.statusbar-item-indentation');
    expect(indItem?.textContent).toBe('Spaces: 4');
  });

  it('renders GitBranchIcon as SVG', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'main',
      }));
    });

    const gitItem = container.querySelector('.statusbar-item-git');
    const svg = gitItem?.querySelector('svg');
    expect(svg).not.toBeNull();
    expect(svg?.getAttribute('aria-hidden')).toBe('true');
  });

  it('renders left section div', () => {
    act(() => {
      root.render(createElement(StatusBar, {
        branch: 'main',
      }));
    });

    const leftSection = container.querySelector('.statusbar-left');
    expect(leftSection).not.toBeNull();
  });

  it('CursorPosition type is exported', () => {
    // Verify the type exists by using it
    const pos: CursorPosition = { line: 0, column: 0 };
    expect(pos.line).toBe(0);
    expect(pos.column).toBe(0);
  });
});
