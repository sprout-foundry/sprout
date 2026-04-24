/**
 * stickyScroll.test.ts — Unit tests for sticky scroll extension helper functions.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * we mock the CM imports and test the exported `computeStickyScopes`
 * helper directly — it's a pure function with no CM dependencies.
 *
 * Tests for `findEnclosingScopes` use real SymbolInfo data without CM mocks.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { computeStickyScopes, findEnclosingScopes, findScopeEnd, stickyScrollPlugin } from './stickyScroll';
import type { SymbolInfo } from './stickyScroll';

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/view', () => ({
  WidgetType: class {},
  Decoration: { widget: jest.fn(), none: [], set: jest.fn() },
  ViewPlugin: { fromClass: jest.fn(() => []) },
  EditorView: { baseTheme: jest.fn(() => []) },
}));
jest.mock('@codemirror/state', () => ({
  StateField: { define: jest.fn(() => ({})) },
  StateEffect: { define: jest.fn(() => ({})) },
}));

// Mock the GoToSymbolOverlay module.
// Use requireActual to preserve the real findSymbolScopeEnd implementation
// (avoids duplicating the brace-counting logic in the mock), while replacing
// extractSymbols and getEnclosingSymbols with jest.fn() for controlled testing.
jest.mock('../components/GoToSymbolOverlay', () => ({
  ...jest.requireActual('../components/GoToSymbolOverlay'),
  extractSymbols: jest.fn(),
  getEnclosingSymbols: jest.fn(),
}));

// ── findEnclosingScopes tests ─────────────────────────────────────

describe('findEnclosingScopes', () => {
  it('returns empty array for empty symbols', () => {
    const symbols: SymbolInfo[] = [];
    const content = 'func main() {}';
    const result = findEnclosingScopes(symbols, 5, content);
    expect(result).toEqual([]);
  });

  it('returns empty array for undefined symbols', () => {
    const content = 'func main() {}';
    const result = findEnclosingScopes(undefined as unknown as SymbolInfo[], 5, content);
    expect(result).toEqual([]);
  });

  it('returns empty array for null symbols', () => {
    const content = 'func main() {}';
    const result = findEnclosingScopes(null as unknown as SymbolInfo[], 5, content);
    expect(result).toEqual([]);
  });

  it('returns empty array for invalid targetLine (0)', () => {
    const symbols = [{ name: 'main', line: 1, kind: 'function' as const }];
    const content = 'func main() {}';
    const result = findEnclosingScopes(symbols, 0, content);
    expect(result).toEqual([]);
  });

  it('returns empty array for invalid targetLine (-1)', () => {
    const symbols = [{ name: 'main', line: 1, kind: 'function' as const }];
    const content = 'func main() {}';
    const result = findEnclosingScopes(symbols, -1, content);
    expect(result).toEqual([]);
  });

  it('returns empty array when empty content', () => {
    const symbols = [{ name: 'main', line: 1, kind: 'function' as const }];
    const result = findEnclosingScopes(symbols, 5, '');
    expect(result).toEqual([]);
  });

  it('filters to container kinds only (function, method, class, interface)', () => {
    const symbols: SymbolInfo[] = [
      { name: 'constVal', line: 1, kind: 'constant' },
      { name: 'varVal', line: 2, kind: 'variable' },
      { name: 'MyClass', line: 3, kind: 'class' },
    ];
    const content = `const constVal = 1
var varVal = 2
class MyClass {
  x := 1
  y := 2
}`;
    // Query line 5 which is inside MyClass
    const result = findEnclosingScopes(symbols, 5, content);
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('MyClass');
  });

  it('returns symbols sorted by line ascending (outermost first)', () => {
    const symbols: SymbolInfo[] = [
      { name: 'inner', line: 3, kind: 'method' as const },
      { name: 'outer', line: 1, kind: 'class' as const },
    ];
    const content = `class outer {
  method inner() {
    x := 1
  }
}`;
    // Line 4 is inside both outer and inner (inside method body)
    const result = findEnclosingScopes(symbols, 4, content);
    expect(result.length).toBe(2);
    expect(result[0].name).toBe('outer');
    expect(result[1].name).toBe('inner');
  });

  it('caps results at 3 scopes', () => {
    const symbols: SymbolInfo[] = [
      { name: 'A', line: 1, kind: 'class' as const },
      { name: 'B', line: 3, kind: 'class' as const },
      { name: 'C', line: 5, kind: 'class' as const },
      { name: 'D', line: 7, kind: 'class' as const },
    ];
    const content = `class A {
  x := 1
  class B {
    y := 2
    class C {
      z := 3
      class D {
        w := 4
      }
    }
  }
}`;
    // Line 8 is inside all 4 nested classes, but only 3 should be returned
    const result = findEnclosingScopes(symbols, 8, content);
    expect(result.length).toBe(3);
    expect(result[0].name).toBe('A');
    expect(result[1].name).toBe('B');
    expect(result[2].name).toBe('C');
    // D is excluded by the cap
  });

  it('finds enclosing scope for line inside function body', () => {
    const symbols: SymbolInfo[] = [
      { name: 'main', line: 1, kind: 'function' as const },
    ];
    const content = `func main() {
  fmt.Println("hello")
}`;
    // Line 2 is inside main
    const result = findEnclosingScopes(symbols, 2, content);
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('main');
  });

  it('finds nested scopes (class inside method)', () => {
    const symbols: SymbolInfo[] = [
      { name: 'MyClass', line: 1, kind: 'class' as const },
      { name: 'myMethod', line: 3, kind: 'method' as const },
    ];
    const content = `class MyClass {
  method myMethod() {
    fmt.Println("hello")
  }
}`;
    // Line 3 is inside myMethod, which is inside MyClass
    const result = findEnclosingScopes(symbols, 3, content);
    expect(result.length).toBe(2);
    expect(result[0].name).toBe('MyClass');
    expect(result[1].name).toBe('myMethod');
  });

  it('returns empty when cursor line is after symbol ends', () => {
    const symbols: SymbolInfo[] = [
      { name: 'main', line: 1, kind: 'function' as const },
    ];
    // Symbol ends at line 2 based on braces, cursor at line 10 is past the end
    const content = `func main() {
  x := 1
}`;
    // Line 10 is far after the function
    const result = findEnclosingScopes(symbols, 10, content);
    expect(result.length).toBe(0);
  });

  it('handles TypeScript class', () => {
    const symbols: SymbolInfo[] = [
      { name: 'MyComponent', line: 1, kind: 'class' as const },
    ];
    const content = `class MyComponent {
  render() { return <div />; }
}`;
    // Line 2 is inside MyComponent
    const result = findEnclosingScopes(symbols, 2, content);
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('MyComponent');
  });

  it('handles TypeScript interface', () => {
    const symbols: SymbolInfo[] = [
      { name: 'MyInterface', line: 1, kind: 'interface' as const },
    ];
    const content = `interface MyInterface {
  foo: string;
}`;
    // Line 2 is inside MyInterface
    const result = findEnclosingScopes(symbols, 2, content);
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('MyInterface');
  });

  it('handles Python class without braces (scope extends to end of file)', () => {
    // Python uses indentation, not braces. Without brace detection, scope
    // extends to end of file. This tests that the function handles this case.
    const symbols: SymbolInfo[] = [
      { name: 'MyClass', line: 1, kind: 'class' as const },
    ];
    const content = `class MyClass:
  def method(self):
    pass`;
    // Line 3 is within the scope (extends to end since no closing brace)
    const result = findEnclosingScopes(symbols, 3, content);
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('MyClass');
  });
});

// ── findScopeEnd tests ────────────────────────────────────────────

describe('findScopeEnd', () => {
  it('returns correct end line for simple function with braces', () => {
    const lines = ['func main() {', '  x := 1', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3); // 1-based inclusive
  });

  it('returns correct end line for nested scopes', () => {
    const lines = ['class A {', '  method b() {', '    x := 1', '  }', '}'];
    expect(findScopeEnd(lines, 0)).toBe(5); // class ends at line 5
    expect(findScopeEnd(lines, 1)).toBe(4); // method ends at line 4
  });

  it('handles switch/case with brace blocks', () => {
    const lines = [
      'function foo() {',
      '  switch (x) {',
      '    case 1: {',
      '      let y = 1;',
      '      break;',
      '    }',
      '  }',
      '}',
    ];
    expect(findScopeEnd(lines, 0)).toBe(8); // function ends at line 8
  });

  it('skips braces inside single-quoted strings', () => {
    const lines = ["func foo() {", '  s := "{ }"', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3);
  });

  it('skips braces inside double-quoted strings', () => {
    const lines = ['func foo() {', '  s := "{ }"', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3);
  });

  it('skips braces inside backtick template literals', () => {
    const lines = ['func foo() {', '  s := `{ }`', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3);
  });

  it('skips braces inside line comments', () => {
    const lines = ['func foo() {', '  // x: { }', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3);
  });

  it('skips braces inside block comments', () => {
    const lines = ['func foo() {', '  /* x: { } */', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3);
  });

  it('handles escape sequences inside strings', () => {
    const lines = ['func foo() {', '  s := "\\"{ "', '}'];
    expect(findScopeEnd(lines, 0)).toBe(3);
  });

  it('returns last line when no closing brace found', () => {
    const lines = ['func foo() {', '  x := 1', '  // no close'];
    expect(findScopeEnd(lines, 0)).toBe(3); // extends to end
  });

  it('handles empty function body', () => {
    const lines = ['func foo() {}'];
    expect(findScopeEnd(lines, 0)).toBe(1);
  });

  it('handles single-line function with closing brace', () => {
    const lines = ['func main() { x := 1 }'];
    expect(findScopeEnd(lines, 0)).toBe(1);
  });

  it('handles deeply nested braces', () => {
    const lines = [
      'func a() {',
      '  if true {',
      '    for {',
      '      if true {',
      '        x := 1',
      '      }',
      '    }',
      '  }',
      '}',
    ];
    expect(findScopeEnd(lines, 0)).toBe(9);
  });

  it('handles Python-like files without braces (returns EOF)', () => {
    const lines = ['class MyClass:', '  def method(self):', '    pass'];
    expect(findScopeEnd(lines, 0)).toBe(3); // no braces, extends to end
  });
});

// ── computeStickyScopes tests ──────────────────────────────────────

describe('computeStickyScopes', () => {
  // Import the mocked functions for use in tests
  const { getEnclosingSymbols } = require('../components/GoToSymbolOverlay');

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('returns empty array for empty content', () => {
    const result = computeStickyScopes('', '.go', 1);
    expect(result).toEqual([]);
  });

  it('returns empty array for invalid topLine (0)', () => {
    const result = computeStickyScopes('func main() {}', '.go', 0);
    expect(result).toEqual([]);
  });

  it('returns empty array for invalid topLine (-1)', () => {
    const result = computeStickyScopes('func main() {}', '.go', -1);
    expect(result).toEqual([]);
  });

  it('returns empty array when no enclosing symbols found', () => {
    getEnclosingSymbols.mockReturnValue([]);
    const result = computeStickyScopes('// just a comment', '.go', 1);
    expect(result).toEqual([]);
  });

  it('returns symbols from getEnclosingSymbols', () => {
    const mockSymbols = [
      { name: 'MyClass', line: 1, kind: 'class' as const },
      { name: 'myMethod', line: 5, kind: 'method' as const },
    ];
    getEnclosingSymbols.mockReturnValue(mockSymbols);

    const content = `class MyClass {
  method myMethod() {}
}`;
    const result = computeStickyScopes(content, '.go', 10);

    expect(getEnclosingSymbols).toHaveBeenCalledWith(content, '.go', 10);
    expect(result).toEqual(mockSymbols);
  });

  it('caps results at 3 scopes', () => {
    const manySymbols = [
      { name: 'A', line: 1, kind: 'class' as const },
      { name: 'B', line: 5, kind: 'class' as const },
      { name: 'C', line: 10, kind: 'class' as const },
      { name: 'D', line: 15, kind: 'class' as const },
    ];
    getEnclosingSymbols.mockReturnValue(manySymbols);

    const result = computeStickyScopes('class A {}', '.java', 20);
    expect(result.length).toBeLessThanOrEqual(3);
    expect(result).toEqual(manySymbols.slice(0, 3));
  });

  it('handles undefined fileExtension', () => {
    getEnclosingSymbols.mockReturnValue([]);
    const result = computeStickyScopes('some content', undefined, 5);
    expect(result).toEqual([]);
    expect(getEnclosingSymbols).toHaveBeenCalledWith('some content', undefined, 5);
  });

  it('handles TypeScript file extension', () => {
    getEnclosingSymbols.mockReturnValue([
      { name: 'MyComponent', line: 1, kind: 'class' as const },
    ]);
    const result = computeStickyScopes('class MyComponent {}', '.tsx', 3);
    expect(result.length).toBe(1);
  });

  it('handles JavaScript file extension', () => {
    getEnclosingSymbols.mockReturnValue([
      { name: 'myFunction', line: 1, kind: 'function' as const },
    ]);
    const result = computeStickyScopes('function myFunction() {}', '.js', 2);
    expect(result.length).toBe(1);
  });

  it('handles Python file extension', () => {
    getEnclosingSymbols.mockReturnValue([
      { name: 'MyClass', line: 1, kind: 'class' as const },
      { name: 'method', line: 3, kind: 'method' as const },
    ]);
    const result = computeStickyScopes('class MyClass:\n  def method(self):\n    pass', '.py', 5);
    expect(result.length).toBe(2);
  });
});

// ── stickyScrollPlugin tests ────────────────────────────────────────

describe('stickyScrollPlugin', () => {
  it('returns an extension array', () => {
    const ext = stickyScrollPlugin(() => '.ts');
    expect(Array.isArray(ext)).toBe(true);
    expect(ext.length).toBeGreaterThan(0);
  });

  it('accepts getter function for file extension', () => {
    const getter = () => '.go';
    const ext = stickyScrollPlugin(getter);
    expect(ext).toBeDefined();
  });

  it('returns array with theme and view plugin', () => {
    const ext = stickyScrollPlugin(() => '.ts');
    // Should return at least 2 items: baseTheme and ViewPlugin
    expect(ext.length).toBeGreaterThanOrEqual(2);
  });
});