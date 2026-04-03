// @ts-nocheck

import React from 'react';
import ReactDOM from 'react-dom';
import { act } from 'react-dom/test-utils';
import { extractSymbols } from './GoToSymbolOverlay';
import GoToSymbolOverlay from './GoToSymbolOverlay';

// ── JSDOM polyfills and compat ───────────────────────────────────────────

// jsdom does not implement Element.prototype.scrollIntoView
// React 18 compat: suppress createRoot warning for ReactDOM.render
const originalError = console.error;
beforeAll(() => {
  Element.prototype.scrollIntoView = jest.fn();
  console.error = (...args: any[]) => {
    if (typeof args[0] === 'string' && args[0].includes('ReactDOM.render is no longer supported')) return;
    originalError.call(console, ...args);
  };
});
afterAll(() => {
  console.error = originalError;
});

// ── Helpers ──────────────────────────────────────────────────────────────

/** Create a DOM node, render the component, and return helpers. */
function renderOverlay(props: {
  visible?: boolean;
  content?: string;
  fileExtension?: string;
  onSelectSymbol?: (line: number) => void;
  onClose?: () => void;
}) {
  const container = document.createElement('div');
  document.body.appendChild(container);

  const {
    visible = true,
    content = '',
    fileExtension = '.go',
    onSelectSymbol = jest.fn(),
    onClose = jest.fn(),
  } = props;

  let component: GoToSymbolOverlay | null = null;

  act(() => {
    component = ReactDOM.render(
      <GoToSymbolOverlay
        visible={visible}
        content={content}
        fileExtension={fileExtension}
        onSelectSymbol={onSelectSymbol}
        onClose={onClose}
      />,
      container,
    );
  });

  // React 18 createRoot returns a different API; ReactDOM.render returns
  // the component instance for class components or null for function
  // components. We just need the container.
  return {
    container,
    unmount: () => act(() => { ReactDOM.unmountComponentAtNode(container); document.body.removeChild(container); }),
    rerender: () => {
      // Re-render with same props (useful after updating state internally)
    },
  };
}

// ── extractSymbols tests ─────────────────────────────────────────────────

describe('extractSymbols', () => {
  // ── Go patterns ──────────────────────────────────────────────────────

  describe('Go language patterns', () => {
    it('extracts func Foo( as method (capitalized)', () => {
      const content = 'func Foo(bar string) error {\n}\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'method' });
    });

    it('extracts func foo( as function (lowercase)', () => {
      const content = 'func foo(bar string) error {\n}\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts func (r *Receiver) MethodName( as method', () => {
      const content = 'func (r *Receiver) MethodName(x int) {\n}\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'MethodName', kind: 'method' });
    });

    it('extracts type Foo struct as class', () => {
      const content = 'type Foo struct {\n  Bar string\n}\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'class' });
    });

    it('extracts type Bar interface as interface and its methods', () => {
      const content = 'type Bar interface {\n  Do()\n}\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols.length).toBeGreaterThanOrEqual(1);
      expect(symbols[0]).toMatchObject({ name: 'Bar', kind: 'interface' });
      // The space-indented method pattern also catches Do() inside the interface
      expect(symbols.some(s => s.name === 'Do' && s.kind === 'method')).toBe(true);
    });

    it('extracts type Baz = as type', () => {
      const content = 'type Baz = Foo\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Baz', kind: 'type' });
    });

    it('extracts var Xxx as variable', () => {
      const content = 'var Xxx string\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Xxx', kind: 'variable' });
    });

    it('extracts const Yyy = as constant', () => {
      const content = 'const Yyy = 42\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Yyy', kind: 'constant' });
    });

    it('extracts items from grouped const() block', () => {
      const content = 'const (\n\tMaxRetries = 3\n\tTimeout    = 30 * time.Second\n)\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols.length).toBeGreaterThanOrEqual(2);
      expect(symbols.some(s => s.name === 'MaxRetries' && s.kind === 'constant')).toBe(true);
      expect(symbols.some(s => s.name === 'Timeout' && s.kind === 'constant')).toBe(true);
    });
  });

  // ── TypeScript/TSX patterns ─────────────────────────────────────────

  describe('TypeScript/TSX patterns', () => {
    it('extracts function foo( as function', () => {
      const content = 'function foo() {\n}\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts export function foo( as function', () => {
      const content = 'export function foo() {\n}\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts async function foo( as function', () => {
      const content = 'async function foo() {\n}\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts class Foo as class', () => {
      const content = 'class Foo {\n  bar() {}\n}\n';
      const symbols = extractSymbols(content, '.ts');
      // Should extract the class
      const classes = symbols.filter((s) => s.kind === 'class');
      expect(classes.length).toBeGreaterThanOrEqual(1);
      expect(classes[0]).toMatchObject({ name: 'Foo' });
    });

    it('extracts interface Foo as interface', () => {
      const content = 'interface Foo {\n}\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'interface' });
    });

    it('extracts type Foo = as type', () => {
      const content = 'type Foo = string | number;\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'type' });
    });

    it('extracts const foo = () => as function (arrow function)', () => {
      const content = 'const foo = () => {};\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts const foo = (params) => as function', () => {
      const content = 'const foo = (x: number) => x + 1;\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts const foo: number as variable', () => {
      const content = 'const foo: number = 42;\n';
      const symbols = extractSymbols(content, '.ts');
      // Should be extracted as a variable (the const foo: pattern)
      expect(symbols.length).toBeGreaterThanOrEqual(1);
      expect(symbols.some((s) => s.name === 'foo' && s.kind === 'variable')).toBe(true);
    });

    it('extracts const FOO_BAR as variable (uppercase consts match generic const pattern first in TS)', () => {
      // In TypeScript patterns, `const FOO_BAR = 1` is matched by the
      // generic `const x =` pattern before the uppercase constant pattern.
      // This is the actual production behaviour.
      const content = 'const FOO_BAR = 1;\n';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'FOO_BAR', kind: 'variable' });
    });
  });

  // ── JavaScript patterns ─────────────────────────────────────────────

  describe('JavaScript patterns', () => {
    it('extracts function foo( as function', () => {
      const content = 'function foo() {\n}\n';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts class Foo as class', () => {
      const content = 'class Foo {\n  constructor() {}\n}\n';
      const symbols = extractSymbols(content, '.js');
      expect(symbols.length).toBeGreaterThanOrEqual(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'class' });
    });

    it('extracts const foo = () => as function', () => {
      const content = 'const foo = () => {};\n';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });
  });

  // ── Python patterns ─────────────────────────────────────────────────

  describe('Python patterns', () => {
    it('extracts class Foo: as class', () => {
      const content = 'class Foo:\n    pass\n';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'class' });
    });

    it('extracts def foo( as function', () => {
      const content = 'def foo():\n    pass\n';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts async def foo( as function', () => {
      const content = 'async def foo():\n    pass\n';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts FOO = value as constant (uppercase, module-level)', () => {
      const content = 'FOO = 42\n';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'FOO', kind: 'constant' });
    });

    it('extracts foo = value as variable (lowercase, module-level)', () => {
      const content = 'foo = 42\n';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'variable' });
    });
  });

  // ── Generic patterns ────────────────────────────────────────────────

  describe('Generic/unrecognized language', () => {
    it('extracts function foo( as function', () => {
      const content = 'function foo() {\n}\n';
      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'foo', kind: 'function' });
    });

    it('extracts class Foo as class', () => {
      const content = 'class Foo {\n}\n';
      const symbols = extractSymbols(content, '.txt');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'Foo', kind: 'class' });
    });
  });

  // ── Edge cases ──────────────────────────────────────────────────────

  describe('Edge cases', () => {
    it('returns empty array for empty content', () => {
      const symbols = extractSymbols('', '.go');
      expect(symbols).toHaveLength(0);
    });

    it('returns empty array for comment-only content', () => {
      const content = '// This is a comment\n// Another comment\n/* block */\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(0);
    });

    it('deduplicates symbols (same name+line returned once, different lines kept)', () => {
      const content = 'func Foo() {}\nfunc Foo() {}\n';
      const symbols = extractSymbols(content, '.go');
      // Both lines define Foo — they are at different lines, so both are kept.
      // Dedup only prevents multiple patterns from matching the same line.
      const fooSymbols = symbols.filter((s) => s.name === 'Foo');
      expect(fooSymbols).toHaveLength(2);
    });

    it('respects MAX_SYMBOLS limit (500)', () => {
      // Generate 600 function lines
      const lines = [];
      for (let i = 0; i < 600; i++) {
        lines.push(`func UniqueFunc${i}() {}`);
      }
      const content = lines.join('\n');
      const symbols = extractSymbols(content, '.go');
      // MAX_SYMBOLS is 500 but could be less due to dedup (all unique here)
      expect(symbols.length).toBeLessThanOrEqual(500);
    });

    it('returns correct 1-based line numbers', () => {
      const content = 'package main\n\nfunc Foo() {}\nfunc Bar() {}\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(2);
      // Foo is on line 3, Bar is on line 4
      expect(symbols.find((s) => s.name === 'Foo')!.line).toBe(3);
      expect(symbols.find((s) => s.name === 'Bar')!.line).toBe(4);
    });

    it('skips blank lines', () => {
      const content = '\n\n\n\nfunc Foo() {}\n\n\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0].name).toBe('Foo');
      expect(symbols[0].line).toBe(5);
    });

    it('strips Go single-line comments before matching', () => {
      // Before the fix, "// func HandleRequest(" was extracted as a symbol.
      const content = '// func HandleRequest(w http.ResponseWriter)\nfunc RealFunc()\n';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(1);
      expect(symbols[0]).toMatchObject({ name: 'RealFunc' });
      // Go convention: uppercase-starting functions are classified as 'method'.
      // (Go uses the same convention for all exported identifiers.)
      expect(['function', 'method']).toContain(symbols[0].kind);
    });

    it('does not match control-flow keywords as methods in TypeScript', () => {
      const content = [
        'function outer() {',
        '  if (x > 0) {',
        '    for (let i = 0; i < 10; i++) {',
        '      while (true) { break; }',
        '    }',
        '    switch (x) {',
        '      case 1: return (x);',
        '    }',
        '  }',
        '  const handler: Handler = async (e) => {',
        '    typeof (x)',
        '  };',
        '}',
      ].join('\n');
      const symbols = extractSymbols(content, '.ts');
      // Only the real function 'outer' and the handler should match, not
      // if/for/while/switch/return/typeof.
      const names = symbols.map((s) => s.name);
      expect(names).toContain('outer');
      expect(names).toContain('handler');
      expect(names).not.toContain('if');
      expect(names).not.toContain('for');
      expect(names).not.toContain('while');
      expect(names).not.toContain('switch');
      expect(names).not.toContain('return');
      expect(names).not.toContain('typeof');
      expect(names).not.toContain('x');
    });
  });
});

// ── GoToSymbolOverlay component tests ────────────────────────────────────

describe('GoToSymbolOverlay component', () => {
  it('renders nothing when visible=false', () => {
    const { container } = renderOverlay({ visible: false });
    // The component returns null when !visible, so the container should be empty
    expect(container.innerHTML).toBe('');
  });

  it('renders the overlay when visible=true', () => {
    const { container } = renderOverlay({ visible: true, content: '' });
    expect(container.querySelector('.goto-symbol-overlay')).not.toBeNull();
    expect(container.querySelector('.goto-symbol-input')).not.toBeNull();
  });

  it('shows symbols extracted from content', () => {
    const content = 'func Hello() {}\nfunc World() {}\n';
    const { container } = renderOverlay({ visible: true, content, fileExtension: '.go' });
    // When no query, all symbols are shown with count heading
    const countEl = container.querySelector('.goto-symbol-count');
    expect(countEl).not.toBeNull();
    expect(countEl!.textContent).toContain('2 symbols');
    // Items should be visible
    const items = container.querySelectorAll('.goto-symbol-item');
    expect(items.length).toBe(2);
  });

  it('shows "No symbols found" for empty content with no query', () => {
    const { container } = renderOverlay({ visible: true, content: '' });
    const emptyEl = container.querySelector('.goto-symbol-empty');
    expect(emptyEl).not.toBeNull();
    expect(emptyEl!.textContent).toBe('No symbols found');
  });

  it('shows symbol count text when no query entered', () => {
    const content = 'func Foo() {}\n';
    const { container } = renderOverlay({ visible: true, content, fileExtension: '.go' });
    const countEl = container.querySelector('.goto-symbol-count');
    expect(countEl).not.toBeNull();
    expect(countEl!.textContent).toContain('1 symbol');
  });

  it('calls onSelectSymbol with correct line when item is clicked', () => {
    const onSelectSymbol = jest.fn();
    const content = 'func FirstFunc() {}\nfunc SecondFunc() {}\n';
    const { container } = renderOverlay({
      visible: true,
      content,
      fileExtension: '.go',
      onSelectSymbol,
    });

    // Click on the first item (FirstFunc on line 1)
    const items = container.querySelectorAll('.goto-symbol-item');
    act(() => {
      items[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onSelectSymbol).toHaveBeenCalledTimes(1);
    expect(onSelectSymbol).toHaveBeenCalledWith(1);
  });

  it('calls onClose when Escape is pressed', () => {
    const onClose = jest.fn();
    const content = 'func Foo() {}\n';
    const { container } = renderOverlay({
      visible: true,
      content,
      fileExtension: '.go',
      onClose,
    });

    const input = container.querySelector('.goto-symbol-input');
    act(() => {
      const keyEvent = new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
      });
      input.dispatchEvent(keyEvent);
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('keyboard navigation: ArrowDown, ArrowUp, Enter work', () => {
    const onSelectSymbol = jest.fn();
    const content = 'func Alpha() {}\nfunc Beta() {}\nfunc Gamma() {}\n';
    const { container } = renderOverlay({
      visible: true,
      content,
      fileExtension: '.go',
      onSelectSymbol,
    });

    const input = container.querySelector('.goto-symbol-input');

    // Initial selection is index 0. ArrowDown should move to index 1.
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });
    let activeItems = container.querySelectorAll('.goto-symbol-item-active');
    expect(activeItems.length).toBe(1);
    // The second item should now be active (Beta)
    expect(activeItems[0].textContent).toContain('Beta');

    // ArrowDown again → index 2 (Gamma)
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    });
    activeItems = container.querySelectorAll('.goto-symbol-item-active');
    expect(activeItems[0].textContent).toContain('Gamma');

    // ArrowUp → index 1 (Beta)
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
    });
    activeItems = container.querySelectorAll('.goto-symbol-item-active');
    expect(activeItems[0].textContent).toContain('Beta');

    // Enter should select Beta (line 2) and call onClose
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });
    expect(onSelectSymbol).toHaveBeenCalledTimes(1);
    expect(onSelectSymbol).toHaveBeenCalledWith(2);
  });
});
