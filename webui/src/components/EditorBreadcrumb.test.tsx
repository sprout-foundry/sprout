import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import EditorBreadcrumb, { type BreadcrumbSymbol } from './EditorBreadcrumb';
import { getEnclosingSymbols } from './GoToSymbolOverlay';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('lucide-react', () => ({
  ChevronRight: (props: any) => <svg data-testid="chevron-right" {...props} />,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

function renderBreadcrumb(
  props: Partial<{
    filePath: string;
    onNavigate?: (path: string) => void;
    symbols?: BreadcrumbSymbol[];
    onNavigateToSymbol?: (line: number) => void;
  }> = {},
) {
  const {
    filePath = 'src/components/App.tsx',
    onNavigate,
    symbols,
    onNavigateToSymbol,
  } = props;

  act(() => {
    root.render(
      <EditorBreadcrumb
        filePath={filePath}
        onNavigate={onNavigate}
        symbols={symbols}
        onNavigateToSymbol={onNavigateToSymbol}
      />,
    );
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

// ── Null/empty rendering ──

describe('EditorBreadcrumb null rendering', () => {
  test('returns null for virtual workspace paths starting with __workspace/', () => {
    renderBreadcrumb({ filePath: '__workspace/chat' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for empty string filePath', () => {
    renderBreadcrumb({ filePath: '' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for plain filename without directory separator', () => {
    renderBreadcrumb({ filePath: 'file.ts' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for single-directory path (only one segment after filtering)', () => {
    renderBreadcrumb({ filePath: 'src/' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for path with only one non-empty segment', () => {
    renderBreadcrumb({ filePath: 'src' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });
});

// ── Rendering breadcrumb segments ──

describe('EditorBreadcrumb segment rendering', () => {
  test('renders all segments for "src/components/App.tsx"', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[0].textContent).toBe('src');
    expect(segments[1].textContent).toBe('components');
    expect(segments[2].textContent).toBe('App.tsx');
  });

  test('renders the breadcrumb as a nav element with aria-label', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });
    const nav = container.querySelector('nav.editor-breadcrumb');
    expect(nav).not.toBeNull();
    expect(nav?.getAttribute('aria-label')).toBe('Breadcrumb');
  });

  test('renders an ol list inside the nav', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });
    const list = container.querySelector('.breadcrumb-list');
    expect(list).not.toBeNull();
    expect(list?.tagName.toLowerCase()).toBe('ol');
  });

  test('last segment has breadcrumb-segment-current class and aria-current', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments[2].classList.contains('breadcrumb-segment-current')).toBe(true);
    expect(segments[2].getAttribute('aria-current')).toBe('page');
  });

  test('non-current segments are rendered as buttons', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    // First two segments should be <button> elements
    expect(segments[0].tagName.toLowerCase()).toBe('button');
    expect(segments[1].tagName.toLowerCase()).toBe('button');
    // Last segment should be <span>
    expect(segments[2].tagName.toLowerCase()).toBe('span');
  });

  test('renders ChevronRight separators between segments', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const separators = container.querySelectorAll('.breadcrumb-separator');
    expect(separators).toHaveLength(2);
  });

  test('renders correct number of separators for a 2-segment path', () => {
    renderBreadcrumb({ filePath: 'src/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(2);

    const separators = container.querySelectorAll('.breadcrumb-separator');
    expect(separators).toHaveLength(1);
  });

  test('separators have aria-hidden="true"', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const separators = container.querySelectorAll('.breadcrumb-separator');
    separators.forEach((sep) => {
      expect(sep.getAttribute('aria-hidden')).toBe('true');
    });
  });
});

// ── Title attributes ──

describe('EditorBreadcrumb title attributes', () => {
  test('non-current segments have title showing path up to that segment', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments[0].getAttribute('title')).toBe('src');
    expect(segments[1].getAttribute('title')).toBe('src/components');
  });

  test('title for nested directory path with 4 levels', () => {
    renderBreadcrumb({
      filePath: 'src/features/auth/LoginForm.tsx',
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(4);
    expect(segments[0].getAttribute('title')).toBe('src');
    expect(segments[1].getAttribute('title')).toBe('src/features');
    expect(segments[2].getAttribute('title')).toBe('src/features/auth');
  });
});

// ── Click handling ──

describe('EditorBreadcrumb click handling', () => {
  test('clicking non-current segment calls onNavigate with correct path', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');

    act(() => {
      (segments[0] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(onNavigate).toHaveBeenCalledWith('src');

    act(() => {
      (segments[1] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledTimes(2);
    expect(onNavigate).toHaveBeenCalledWith('src/components');
  });

  test('clicking the current (last) span segment does NOT cause errors', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    const lastSegment = segments[2] as HTMLElement;

    // Last segment is a <span>, clicking it is safe
    act(() => {
      lastSegment.click();
    });

    expect(onNavigate).not.toHaveBeenCalled();
  });

  test('clicking segments when onNavigate is not provided does not throw', () => {
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');

    expect(() => {
      act(() => {
        (segments[0] as HTMLElement).click();
        (segments[1] as HTMLElement).click();
        (segments[2] as HTMLElement).click();
      });
    }).not.toThrow();
  });

  test('clicking all non-current segments with a 4-level path calls onNavigate correctly', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/features/auth/LoginForm.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(4);

    act(() => {
      (segments[0] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src');

    act(() => {
      (segments[1] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src/features');

    act(() => {
      (segments[2] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src/features/auth');

    // Current segment (span) — should not call
    act(() => {
      (segments[3] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledTimes(3);
  });

  test('keyboard activation with Enter key calls onNavigate', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    const firstSegment = segments[0] as HTMLElement;

    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true });
      firstSegment.dispatchEvent(event);
    });

    expect(onNavigate).toHaveBeenCalledWith('src');
  });
});

// ── Edge cases ──

describe('EditorBreadcrumb edge cases', () => {
  test('handles multiple consecutive slashes correctly', () => {
    renderBreadcrumb({ filePath: 'src//components///App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[0].textContent).toBe('src');
    expect(segments[1].textContent).toBe('components');
    expect(segments[2].textContent).toBe('App.tsx');
  });

  test('handles path starting with a slash (leading slash filtered out)', () => {
    renderBreadcrumb({ filePath: '/src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[0].textContent).toBe('src');
  });

  test('handles path with trailing slash (trailing empty string filtered out)', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx/' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[2].textContent).toBe('App.tsx');
    expect(segments[2].classList.contains('breadcrumb-segment-current')).toBe(true);
  });

  test('path with only two segments renders both', () => {
    renderBreadcrumb({ filePath: 'src/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(2);
    expect(segments[0].textContent).toBe('src');
    expect(segments[1].textContent).toBe('App.tsx');

    const separators = container.querySelectorAll('.breadcrumb-separator');
    expect(separators).toHaveLength(1);
  });
});

// ── Symbol breadcrumb rendering ──

describe('EditorBreadcrumb symbol rendering', () => {
  test('renders symbol segments with correct names and kind icons', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'myMethod', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments).toHaveLength(2);

    // First symbol: class with C icon
    expect(symbolSegments[0].textContent).toContain('MyClass');
    const icon0 = symbolSegments[0].querySelector('.breadcrumb-symbol-icon');
    expect(icon0?.textContent).toBe('C');

    // Second symbol: method with ƒ icon
    expect(symbolSegments[1].textContent).toContain('myMethod');
    const icon1 = symbolSegments[1].querySelector('.breadcrumb-symbol-icon');
    expect(icon1?.textContent).toBe('ƒ');
  });

  test('section separator renders between path and symbols', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'myFunc', line: 5, kind: 'function' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const sectionSep = container.querySelectorAll('.breadcrumb-symbol-section-separator');
    expect(sectionSep).toHaveLength(1);
    expect(sectionSep[0].getAttribute('aria-hidden')).toBe('true');
  });

  test('no section separator when only symbols (no path segments)', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'myFunc', line: 5, kind: 'function' },
    ];

    renderBreadcrumb({
      filePath: 'file.ts', // won't produce path segments
      symbols,
    });

    const sectionSep = container.querySelectorAll('.breadcrumb-symbol-section-separator');
    expect(sectionSep).toHaveLength(0);

    // Symbols should still render
    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments).toHaveLength(1);
  });

  test('last symbol has current styling', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'render', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments[0].classList.contains('breadcrumb-segment-current')).toBe(false);
    expect(symbolSegments[1].classList.contains('breadcrumb-segment-current')).toBe(true);
    expect(symbolSegments[1].getAttribute('aria-current')).toBe('page');
  });

  test('last symbol is a span (non-clickable) and first is a button', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'render', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments[0].tagName.toLowerCase()).toBe('button');
    expect(symbolSegments[1].tagName.toLowerCase()).toBe('span');
  });

  test('single symbol is rendered as current (span)', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'myFunc', line: 5, kind: 'function' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments).toHaveLength(1);
    expect(symbolSegments[0].classList.contains('breadcrumb-segment-current')).toBe(true);
    expect(symbolSegments[0].tagName.toLowerCase()).toBe('span');
  });

  test('undefined symbols does not render symbol section', () => {
    renderBreadcrumb({
      filePath: 'src/App.tsx',
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments).toHaveLength(0);
    const sectionSep = container.querySelectorAll('.breadcrumb-symbol-section-separator');
    expect(sectionSep).toHaveLength(0);
  });

  test('empty symbols array does not render symbol section', () => {
    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols: [],
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments).toHaveLength(0);
  });

  test('symbol separators between multiple symbols', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'method1', line: 20, kind: 'method' },
      { name: 'method2', line: 30, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    // The section separator between path and symbols plus 2 separators between 3 symbols
    const separators = container.querySelectorAll('.breadcrumb-separator');
    // Original path separators: 1 between src > App.tsx
    // Plus: section separator (1) + between symbols (2)  = 3 additional
    // But section separator uses breadcrumb-symbol-section-separator class
    const sectionSep = container.querySelectorAll('.breadcrumb-symbol-section-separator');
    expect(sectionSep).toHaveLength(1);

    // breadcrumb-separator count: path separators (1: src > App.tsx)
    // + symbol separators (2: MyClass > method1, method1 > method2) = 3
    expect(separators).toHaveLength(3);
  });

  test('symbol title attribute includes kind, name, and line', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'myFunc', line: 42, kind: 'function' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    expect(symbolSegments[0].getAttribute('title')).toBe('function myFunc:42');
  });
});

// ── Symbol click handling ──

describe('EditorBreadcrumb symbol click handling', () => {
  test('clicking non-current symbol calls onNavigateToSymbol with correct line', () => {
    const onNavigateToSymbol = jest.fn();
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'render', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
      onNavigateToSymbol,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    // Click the first symbol (class) — it's a button
    act(() => {
      (symbolSegments[0] as HTMLElement).click();
    });
    expect(onNavigateToSymbol).toHaveBeenCalledWith(10);
    expect(onNavigateToSymbol).toHaveBeenCalledTimes(1);
  });

  test('clicking current (last) symbol does NOT call onNavigateToSymbol', () => {
    const onNavigateToSymbol = jest.fn();
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'render', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
      onNavigateToSymbol,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    // The last symbol is a span — no onClick
    act(() => {
      (symbolSegments[1] as HTMLElement).click();
    });
    expect(onNavigateToSymbol).toHaveBeenCalledTimes(0);
  });

  test('clicking symbol when onNavigateToSymbol is undefined does not throw', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'render', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');

    expect(() => {
      act(() => {
        (symbolSegments[0] as HTMLElement).click();
      });
    }).not.toThrow();
  });

  test('keyboard Enter on symbol calls onNavigateToSymbol', () => {
    const onNavigateToSymbol = jest.fn();
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'render', line: 25, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      symbols,
      onNavigateToSymbol,
    });

    const symbolSegments = container.querySelectorAll('.breadcrumb-symbol');
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true });
      (symbolSegments[0] as HTMLElement).dispatchEvent(event);
    });

    expect(onNavigateToSymbol).toHaveBeenCalledWith(10);
  });
});

// ── Path + symbol interaction ──

describe('EditorBreadcrumb path and symbol interaction', () => {
  test('last path segment becomes clickable when symbols are present', () => {
    const onNavigate = jest.fn();
    const symbols: BreadcrumbSymbol[] = [
      { name: 'myFunc', line: 5, kind: 'function' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      onNavigate,
      symbols,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    // Path segments: src, App.tsx — both should be buttons now
    expect(segments[0].tagName.toLowerCase()).toBe('button');
    expect(segments[1].tagName.toLowerCase()).toBe('button');
  });

  test('clicking last path segment calls onNavigate when symbols present', () => {
    const onNavigate = jest.fn();
    const symbols: BreadcrumbSymbol[] = [
      { name: 'myFunc', line: 5, kind: 'function' },
    ];

    renderBreadcrumb({
      filePath: 'src/App.tsx',
      onNavigate,
      symbols,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    // segments[1] is App.tsx (last path segment) — now should be clickable
    act(() => {
      (segments[1] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src/App.tsx');
  });

  test('total segments = path segments + symbol segments', () => {
    const symbols: BreadcrumbSymbol[] = [
      { name: 'MyClass', line: 10, kind: 'class' },
      { name: 'doWork', line: 20, kind: 'method' },
    ];

    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      symbols,
    });

    const allSegments = container.querySelectorAll('.breadcrumb-segment');
    // 3 path segments + 2 symbol segments = 5
    expect(allSegments).toHaveLength(5);
  });
});

// ── getEnclosingSymbols ──

describe('getEnclosingSymbols', () => {
  test('returns empty array for empty content', () => {
    const result = getEnclosingSymbols('', '.ts', 1);
    expect(result).toEqual([]);
  });

  test('returns the function when cursor is inside it', () => {
    const content = `function hello() {
  console.log("hi");
}
`;
    const result = getEnclosingSymbols(content, '.ts', 2);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('hello');
    expect(result[0].kind).toBe('function');
  });

  test('returns class and method when cursor is inside a class method', () => {
    const content = `class Foo {
  bar() {
    return 42;
  }
}`;
    const result = getEnclosingSymbols(content, '.ts', 3);
    expect(result).toHaveLength(2);
    expect(result[0].name).toBe('Foo');
    expect(result[0].kind).toBe('class');
    expect(result[1].name).toBe('bar');
    expect(result[1].kind).toBe('method');
  });

  test('returns empty when cursor is outside all symbols', () => {
    const content = `function hello() {
  console.log("hi");
}

// comment after function
`;
    const result = getEnclosingSymbols(content, '.ts', 5);
    expect(result).toEqual([]);
  });

  test('returns empty when cursor is before all symbols', () => {
    const content = `
function hello() {
  console.log("hi");
}`;
    const result = getEnclosingSymbols(content, '.ts', 1);
    expect(result).toEqual([]);
  });

  test('works with Go code', () => {
    const content = `package main

func Hello() {
\tfmt.Println("hello")
}

func World() {
\tfmt.Println("world")
}`;
    const result = getEnclosingSymbols(content, '.go', 3);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('Hello');
    expect(result[0].kind).toBe('method');
  });

  test('works with Python code (indentation-based)', () => {
    const content = `class Foo:
    def bar(self):
        return 42
`;
    // Python uses indent-based scoping; our brace-counting heuristic
    // will extend to end of file since there are no braces.
    // The cursor at line 3 is inside Foo (no braces → scope = end of file)
    const result = getEnclosingSymbols(content, '.py', 3);
    // Foo has no braces so scope extends to end of file → encloses line 3
    expect(result.length).toBeGreaterThanOrEqual(1);
    expect(result[0].name).toBe('Foo');
  });

  test('caps at 3 symbols', () => {
    const content = `class A {
  class B {
    class C {
      class D {
        class E {
        }
      }
    }
  }
}`;
    // All 5 classes should be found, but only 3 returned (cap)
    const result = getEnclosingSymbols(content, '.ts', 9);
    expect(result.length).toBeLessThanOrEqual(3);
  });

  test('handles 0-based cursor line correctly', () => {
    const content = `function hello() {
  console.log("hi");
}`;
    // cursorLine=1 (1-based) means cursor is on the function def line itself
    const result = getEnclosingSymbols(content, '.ts', 1);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('hello');
  });

  test('does not count braces inside strings', () => {
    const content = `function hello() {
  const s = "{not a brace}";
  console.log(s);
}
`;
    // The { and } inside the string should not affect scope detection
    const result = getEnclosingSymbols(content, '.ts', 3);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('hello');
  });

  test('does not count braces inside single-quoted strings', () => {
    const content = `function hello() {
  const s = '{also not}';
}
`;
    const result = getEnclosingSymbols(content, '.ts', 2);
    expect(result).toHaveLength(1);
  });

  test('does not count braces inside template literals', () => {
    const content = `function hello() {
  const s = \`{template}\`;
}`;
    const result = getEnclosingSymbols(content, '.ts', 2);
    expect(result).toHaveLength(1);
  });

  test('does not count braces inside multi-line template literals', () => {
    const content = `function hello() {
  const s = \`
    {
      key: \${value}
    }
  \`;
  return s;
}`;
    const result = getEnclosingSymbols(content, '.ts', 2);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('hello');
  });

  test('handles escaped quotes in single-quoted strings without corrupting scope', () => {
    const content = `function foo() {
  const x = '\\'';
  return x;
}
function bar() {
  return 42;
}`;
    // Cursor inside foo — scope should end at foo's closing brace (line 4), not extend into bar
    const result = getEnclosingSymbols(content, '.ts', 2);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('foo');
  });

  test('does not count braces inside line comments', () => {
    const content = `function hello() {
  // this is a {comment}
  return 1;
}
`;
    const result = getEnclosingSymbols(content, '.ts', 3);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('hello');
  });

  test('returns empty for cursorLine < 1', () => {
    const content = `function hello() {}\n`;
    expect(getEnclosingSymbols(content, '.ts', 0)).toEqual([]);
    expect(getEnclosingSymbols(content, '.ts', -1)).toEqual([]);
  });
});
