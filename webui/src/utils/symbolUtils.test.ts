import { describe, it, expect } from 'vitest';
import {
  extractSymbols,
  findSymbolScopeEnd,
  getEnclosingSymbols,
  getScopePath,
  buildScopePaths,
  MAX_SYMBOLS,
  KIND_ICONS,
  CONTAINER_KINDS,
  type SymbolInfo,
} from './symbolUtils';

// ── Constants ────────────────────────────────────────────────────────────

describe('MAX_SYMBOLS constant', () => {
  it('equals 500', () => {
    expect(MAX_SYMBOLS).toBe(500);
  });
});

describe('KIND_ICONS constant', () => {
  it('has all 8 symbol kinds', () => {
    expect(Object.keys(KIND_ICONS)).toHaveLength(8);
  });

  it('maps function and method to ƒ', () => {
    expect(KIND_ICONS.function).toBe('ƒ');
    expect(KIND_ICONS.method).toBe('ƒ');
  });

  it('maps class to C', () => {
    expect(KIND_ICONS.class).toBe('C');
  });

  it('maps variable to V', () => {
    expect(KIND_ICONS.variable).toBe('V');
  });

  it('maps type to T', () => {
    expect(KIND_ICONS.type).toBe('T');
  });

  it('maps constant to K', () => {
    expect(KIND_ICONS.constant).toBe('K');
  });

  it('maps interface to I', () => {
    expect(KIND_ICONS.interface).toBe('I');
  });
});

describe('CONTAINER_KINDS constant', () => {
  it('contains function, method, class, interface', () => {
    expect(CONTAINER_KINDS.has('function')).toBe(true);
    expect(CONTAINER_KINDS.has('method')).toBe(true);
    expect(CONTAINER_KINDS.has('class')).toBe(true);
    expect(CONTAINER_KINDS.has('interface')).toBe(true);
  });

  it('does not contain variable, type, constant', () => {
    expect(CONTAINER_KINDS.has('variable')).toBe(false);
    expect(CONTAINER_KINDS.has('type')).toBe(false);
    expect(CONTAINER_KINDS.has('constant')).toBe(false);
  });
});

// ── extractSymbols ───────────────────────────────────────────────────────

describe('extractSymbols', () => {
  // ── Go ─────────────────────────────────────────────────────────────────

  describe('Go (.go)', () => {
    it('extracts exported methods (func (receiver) Name)', () => {
      const content = 'func (h *Handler) ServeHTTP(w http.ResponseWriter) {}';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'ServeHTTP', line: 1, kind: 'method' }]);
    });

    it('extracts unexported functions', () => {
      const content = 'func main() {}';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'main', line: 1, kind: 'function' }]);
    });

    it('extracts type struct', () => {
      const content = 'type User struct {\n\tName string\n}';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'User', line: 1, kind: 'class' }]);
    });

    it('extracts type interface', () => {
      const content = 'type Handler interface {\n\tHandle()\n}';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toContainEqual({ name: 'Handler', line: 1, kind: 'interface' });
    });

    it('extracts type alias', () => {
      const content = 'type MyString = string';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'MyString', line: 1, kind: 'type' }]);
    });

    it('extracts underlying type', () => {
      const content = 'type MyString string';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'MyString', line: 1, kind: 'type' }]);
    });

    it('extracts exported variables', () => {
      const content = 'var Global int = 42';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'Global', line: 1, kind: 'variable' }]);
    });

    it('extracts exported constants', () => {
      const content = 'const MaxSize = 100';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([{ name: 'MaxSize', line: 1, kind: 'constant' }]);
    });

    it('extracts multiple symbols from a file', () => {
      const content = [
        'package main',
        '',
        'type Config struct {}',
        '',
        'func New() *Config {}',
        '',
        'func main() {}',
      ].join('\n');

      const symbols = extractSymbols(content, '.go');
      expect(symbols).toHaveLength(3);
      expect(symbols[0]).toEqual({ name: 'Config', line: 3, kind: 'class' });
      expect(symbols[1]).toEqual({ name: 'New', line: 5, kind: 'method' });
      expect(symbols[2]).toEqual({ name: 'main', line: 7, kind: 'function' });
    });

    it('extracts interface method signatures', () => {
      const content = ['type Reader interface {', '\tRead(p []byte) (n int, err error)', '}'].join('\n');

      const symbols = extractSymbols(content, '.go');
      expect(symbols).toContainEqual({ name: 'Read', line: 2, kind: 'method' });
    });

    it('extracts const block items', () => {
      const content = ['const (', '\tMaxItems = 100', '\tMinItems = 1', ')'].join('\n');

      const symbols = extractSymbols(content, '.go');
      expect(symbols).toContainEqual({ name: 'MaxItems', line: 2, kind: 'constant' });
      expect(symbols).toContainEqual({ name: 'MinItems', line: 3, kind: 'constant' });
    });

    it('skips // comments in Go code', () => {
      const content = '// func Fake() {}';
      const symbols = extractSymbols(content, '.go');
      expect(symbols).toEqual([]);
    });
  });

  // ── Python ─────────────────────────────────────────────────────────────

  describe('Python (.py)', () => {
    it('extracts class definitions', () => {
      const content = 'class MyClass:\n    pass';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'MyClass', line: 1, kind: 'class' }]);
    });

    it('extracts class with inheritance', () => {
      const content = 'class Foo(BaseClass):';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'Foo', line: 1, kind: 'class' }]);
    });

    it('extracts def functions', () => {
      const content = 'def my_func(x, y):';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'my_func', line: 1, kind: 'function' }]);
    });

    it('extracts async def functions', () => {
      const content = 'async def fetch_data(url):';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'fetch_data', line: 1, kind: 'function' }]);
    });

    it('extracts module-level constants (UPPER_CASE)', () => {
      const content = 'MAX_RETRIES = 3';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'MAX_RETRIES', line: 1, kind: 'constant' }]);
    });

    it('extracts module-level variables', () => {
      const content = 'count = 0';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'count', line: 1, kind: 'variable' }]);
    });

    it('extracts multiple Python symbols', () => {
      const content = [
        'MAX_SIZE = 1024',
        'count = 0',
        '',
        'class Parser:',
        '    def parse(self, data):',
        '        pass',
      ].join('\n');

      const symbols = extractSymbols(content, '.py');
      expect(symbols).toHaveLength(4);
      expect(symbols[0].name).toBe('MAX_SIZE');
      expect(symbols[1].name).toBe('count');
      expect(symbols[2].name).toBe('Parser');
      expect(symbols[3].name).toBe('parse');
    });

    it('skips # comments in Python code', () => {
      const content = '# def not_a_func():';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([]);
    });

    it('does not strip # inside strings in Python', () => {
      const content = 'url = "http://example.com#anchor"';
      const symbols = extractSymbols(content, '.py');
      // url is a valid variable match, and # inside string shouldn't strip the line
      expect(symbols).toEqual([{ name: 'url', line: 1, kind: 'variable' }]);
    });

    it('handles method-like constant pattern correctly', () => {
      const content = 'FOO_BAR = 42';
      const symbols = extractSymbols(content, '.py');
      expect(symbols).toEqual([{ name: 'FOO_BAR', line: 1, kind: 'constant' }]);
    });
  });

  // ── TypeScript ─────────────────────────────────────────────────────────

  describe('TypeScript (.ts)', () => {
    it('extracts function declarations', () => {
      const content = 'function myFunc(): void {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('extracts export function declarations', () => {
      const content = 'export function myFunc(): void {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('extracts async function declarations', () => {
      const content = 'async function fetchData(): Promise<void> {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'fetchData', line: 1, kind: 'function' }]);
    });

    it('extracts export default function', () => {
      const content = 'export default function App() {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'App', line: 1, kind: 'function' }]);
    });

    it('extracts class declarations', () => {
      const content = 'class MyClass {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'MyClass', line: 1, kind: 'class' }]);
    });

    it('extracts export class declarations', () => {
      const content = 'export class MyClass {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'MyClass', line: 1, kind: 'class' }]);
    });

    it('extracts abstract class', () => {
      const content = 'export abstract class Base {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'Base', line: 1, kind: 'class' }]);
    });

    it('extracts interface declarations', () => {
      const content = 'interface MyInterface {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'MyInterface', line: 1, kind: 'interface' }]);
    });

    it('extracts export interface declarations', () => {
      const content = 'export interface MyInterface {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'MyInterface', line: 1, kind: 'interface' }]);
    });

    it('extracts type aliases', () => {
      const content = 'type MyType = string | number';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'MyType', line: 1, kind: 'type' }]);
    });

    it('extracts export type aliases', () => {
      const content = 'export type MyType = string';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'MyType', line: 1, kind: 'type' }]);
    });

    it('extracts const arrow functions', () => {
      const content = 'const myFunc = (x: number) => x * 2';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('extracts const IIFE (const = (', () => {
      const content = 'const result = (function() { return 1; })()';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'result', line: 1, kind: 'function' }]);
    });

    it('extracts const with type annotation', () => {
      const content = 'const myVar: string = "hello"';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'myVar', line: 1, kind: 'variable' }]);
    });

    it('extracts let declarations', () => {
      const content = 'let count = 0';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'count', line: 1, kind: 'variable' }]);
    });

    it('extracts var declarations', () => {
      const content = 'var name = "test"';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([{ name: 'name', line: 1, kind: 'variable' }]);
    });

    it('extracts uppercase constants (matched as variable due to pattern order)', () => {
      const content = 'const MAX_SIZE = 100';
      const symbols = extractSymbols(content, '.ts');
      // The generic const pattern matches before the uppercase const pattern
      // since patterns are tested in order and variable is first
      expect(symbols).toEqual([{ name: 'MAX_SIZE', line: 1, kind: 'variable' }]);
    });

    it('extracts class methods', () => {
      const content = [
        'class Foo {',
        '  constructor() {}',
        '  bar(x: number) { return x; }',
        '  private internal() {}',
        '}',
      ].join('\n');

      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toContainEqual({ name: 'Foo', line: 1, kind: 'class' });
      expect(symbols).toContainEqual({ name: 'constructor', line: 2, kind: 'method' });
      expect(symbols).toContainEqual({ name: 'bar', line: 3, kind: 'method' });
      expect(symbols).toContainEqual({ name: 'internal', line: 4, kind: 'method' });
    });

    it('skips // comments in TypeScript code', () => {
      const content = '// function Fake() {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toEqual([]);
    });

    it('handles .tsx files the same as .ts', () => {
      const content = 'function MyComponent() {}';
      const symbols = extractSymbols(content, '.tsx');
      expect(symbols).toEqual([{ name: 'MyComponent', line: 1, kind: 'function' }]);
    });
  });

  // ── JavaScript ─────────────────────────────────────────────────────────

  describe('JavaScript (.js)', () => {
    it('extracts function declarations', () => {
      const content = 'function myFunc() {}';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('extracts class declarations', () => {
      const content = 'class MyClass {}';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'MyClass', line: 1, kind: 'class' }]);
    });

    it('extracts const arrow functions', () => {
      const content = 'const myFunc = (x) => x * 2';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('extracts const IIFE', () => {
      const content = 'const result = (function() { return 1; })()';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'result', line: 1, kind: 'function' }]);
    });

    it('extracts const variables', () => {
      const content = 'const name = "test"';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'name', line: 1, kind: 'variable' }]);
    });

    it('extracts let variables', () => {
      const content = 'let count = 0';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'count', line: 1, kind: 'variable' }]);
    });

    it('extracts var variables', () => {
      const content = 'var name = "test"';
      const symbols = extractSymbols(content, '.js');
      expect(symbols).toEqual([{ name: 'name', line: 1, kind: 'variable' }]);
    });

    it('extracts uppercase constants (matched as variable due to pattern order)', () => {
      const content = 'const MAX_SIZE = 100';
      const symbols = extractSymbols(content, '.js');
      // The generic const pattern matches before the uppercase const pattern
      // since patterns are tested in order and variable is first
      expect(symbols).toEqual([{ name: 'MAX_SIZE', line: 1, kind: 'variable' }]);
    });

    it('handles .jsx and .mjs the same as .js', () => {
      const symbols = extractSymbols('function Foo() {}', '.jsx');
      expect(symbols).toEqual([{ name: 'Foo', line: 1, kind: 'function' }]);

      const symbols2 = extractSymbols('function Bar() {}', '.mjs');
      expect(symbols2).toEqual([{ name: 'Bar', line: 1, kind: 'function' }]);
    });
  });

  // ── Generic fallback ───────────────────────────────────────────────────

  describe('Generic fallback', () => {
    it('uses generic patterns for unknown extensions', () => {
      const content = 'function myFunc() {}';
      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('uses generic patterns for undefined languageId', () => {
      const content = 'function myFunc() {}';
      const symbols = extractSymbols(content);
      expect(symbols).toEqual([{ name: 'myFunc', line: 1, kind: 'function' }]);
    });

    it('uses generic patterns for empty string languageId', () => {
      const content = 'class MyClass {}';
      const symbols = extractSymbols(content, '');
      expect(symbols).toEqual([{ name: 'MyClass', line: 1, kind: 'class' }]);
    });

    it('generic fallback extracts class, function, interface, def, func', () => {
      const content = [
        'function jsFunc() {}',
        'class MyClass {}',
        'interface MyInterface {}',
        'def pyFunc():',
        'func goFunc() {}',
      ].join('\n');

      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toHaveLength(5);
      expect(symbols.map((s) => s.name)).toEqual(['jsFunc', 'MyClass', 'MyInterface', 'pyFunc', 'goFunc']);
    });

    it('generic fallback extracts type struct', () => {
      const content = 'type Foo struct {}';
      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toEqual([{ name: 'Foo', line: 1, kind: 'class' }]);
    });

    it('generic fallback extracts type interface', () => {
      const content = 'type Foo interface {}';
      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toEqual([{ name: 'Foo', line: 1, kind: 'interface' }]);
    });
  });

  // ── Edge cases ─────────────────────────────────────────────────────────

  describe('Edge cases', () => {
    it('returns empty array for empty content', () => {
      expect(extractSymbols('')).toEqual([]);
    });

    it('returns empty array for whitespace-only content', () => {
      expect(extractSymbols('   \n  \t  ')).toEqual([]);
    });

    it('respects MAX_SYMBOLS limit', () => {
      // Create content with more than 500 function definitions
      const lines = Array.from({ length: 600 }, (_, i) => `function func${i}() {}`);
      const content = lines.join('\n');
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(MAX_SYMBOLS);
    });

    it('deduplicates symbols with same name and line', () => {
      // A line that could match multiple patterns
      const content = 'export function Foo() {}';
      const symbols = extractSymbols(content, '.ts');
      expect(symbols).toHaveLength(1);
      expect(symbols[0].name).toBe('Foo');
    });

    it('deduplicates symbols with same name on same line', () => {
      const content = 'const FOO = 100';
      const symbols = extractSymbols(content, '.ts');
      // Should not produce duplicate entries for FOO
      expect(symbols).toHaveLength(1);
    });

    it('allows same name on different lines', () => {
      const content = ['function render() {}', 'class render {}'].join('\n');
      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toHaveLength(2);
      expect(symbols[0].line).toBe(1);
      expect(symbols[1].line).toBe(2);
    });

    it('handles content with no symbols', () => {
      const content = 'just some random text\nno functions here\nnothing to extract';
      const symbols = extractSymbols(content, '.unknown');
      expect(symbols).toEqual([]);
    });
  });
});

// ── findSymbolScopeEnd ───────────────────────────────────────────────────

describe('findSymbolScopeEnd', () => {
  // ── Brace-based (default) ──────────────────────────────────────────────

  it('finds scope end for simple function with braces', () => {
    const lines = ['function foo() {', '  return 1;', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  it('finds scope end for class with nested braces', () => {
    const lines = ['class Foo {', '  bar() {', '    return 1;', '  }', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(5);
  });

  it('finds scope end for single-line function', () => {
    const lines = ['function foo() { return 1; }'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(1);
  });

  it('returns length of lines when no closing brace', () => {
    const lines = ['function foo() {', '  return 1;'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(2);
  });

  it('ignores braces inside double-quoted strings', () => {
    const lines = ['function foo() {', '  const s = "hello { world";', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  it('ignores braces inside single-quoted strings', () => {
    const lines = ['function foo() {', "  const s = 'hello { world';", '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  it('ignores braces inside backtick template strings', () => {
    const lines = ['function foo() {', '  const s = `hello { world`;'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(2);
  });

  it('handles multi-line backtick strings', () => {
    const lines = ['function foo() {', '  const s = `', '    hello { world', '  `;', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(5);
  });

  it('skips // line comments', () => {
    const lines = ['function foo() {', '  // } not a real close', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  it('skips /* block comments */', () => {
    const lines = ['function foo() {', '  /* { } */', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  it('handles multi-line block comments', () => {
    const lines = ['function foo() {', '  /*', '   { }', '  */', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(5);
  });

  it('handles escaped quotes in strings', () => {
    const lines = ['function foo() {', '  const s = "hello \\" {";', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  it('handles escaped backslash before quote', () => {
    const lines = ['function foo() {', '  const s = "a \\\\" {";', '}'];
    expect(findSymbolScopeEnd(lines, 0)).toBe(3);
  });

  // ── Python (indentation-based) ─────────────────────────────────────────

  describe('Python indentation', () => {
    it('finds scope end for Python class', () => {
      const lines = ['class Foo:', '    def bar(self):', '        pass', '', 'def other():'];
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(4);
    });

    it('finds scope end for Python function', () => {
      const lines = ['def foo():', '    x = 1', '    return x', '', 'def bar():'];
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(4);
    });

    it('handles Python nested scope', () => {
      const lines = ['class Foo:', '    def bar(self):', '        x = 1', '    def baz(self):', '        pass'];
      // Class scope ends at end of file (nothing at same level)
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(5);
      // Method bar scope ends at baz (same indent)
      expect(findSymbolScopeEnd(lines, 1, '.py')).toBe(3);
    });

    it('handles Python empty body (returns next line)', () => {
      const lines = ['def foo():', 'def bar():'];
      // First non-blank line after decl is at same indent → empty body
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(1);
    });

    it('handles Python blank lines and comments in body', () => {
      const lines = ['def foo():', '', '    # comment', '    x = 1', '', 'def bar():'];
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(5);
    });

    it('returns length for Python function at EOF', () => {
      const lines = ['def foo():', '    x = 1'];
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(2);
    });

    it('handles Python declaration on blank line', () => {
      const lines = ['', 'def foo():'];
      // startLineIndex = 0 is blank → returns lines.length
      expect(findSymbolScopeEnd(lines, 0, '.py')).toBe(2);
    });
  });

  // ── Mixed languageId ───────────────────────────────────────────────────

  it('uses brace-based for non-Python languageIds', () => {
    const lines = ['func foo() {', '  return 1;', '}'];
    expect(findSymbolScopeEnd(lines, 0, '.go')).toBe(3);
    expect(findSymbolScopeEnd(lines, 0, '.js')).toBe(3);
    expect(findSymbolScopeEnd(lines, 0, '.ts')).toBe(3);
  });
});

// ── getEnclosingSymbols ──────────────────────────────────────────────────

describe('getEnclosingSymbols', () => {
  it('returns empty array for empty content', () => {
    expect(getEnclosingSymbols('', '.ts', 1)).toEqual([]);
  });

  it('returns empty array for cursorLine < 1', () => {
    const content = 'function foo() { return 1; }';
    expect(getEnclosingSymbols(content, '.ts', 0)).toEqual([]);
    expect(getEnclosingSymbols(content, '.ts', -1)).toEqual([]);
  });

  it('returns enclosing containers for TypeScript', () => {
    const content = ['class Foo {', '  bar() {', '    return 1;', '  }', '}'].join('\n');

    const result = getEnclosingSymbols(content, '.ts', 3);
    expect(result).toHaveLength(2);
    expect(result[0].name).toBe('Foo');
    expect(result[1].name).toBe('bar');
  });

  it('caps at 3 enclosing symbols', () => {
    const content = [
      'class A {',
      '  b() {',
      '    const c = () => {',
      '      const d = () => {}',
      '    }',
      '  }',
      '}',
    ].join('\n');

    const result = getEnclosingSymbols(content, '.ts', 4);
    expect(result.length).toBeLessThanOrEqual(3);
  });

  it('only returns container kinds', () => {
    const content = ['const x = 1', 'function foo() {', '  return x;', '}'].join('\n');

    const result = getEnclosingSymbols(content, '.ts', 3);
    // 'x' is variable (not a container), only 'foo' should be returned
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('foo');
  });

  it('returns empty for cursor outside any container', () => {
    const content = ['function foo() {', '  return 1;', '}', '', '// outside'].join('\n');

    const result = getEnclosingSymbols(content, '.ts', 5);
    expect(result).toEqual([]);
  });

  it('returns enclosing containers for Go', () => {
    const content = [
      'type Handler struct {}',
      '',
      'func (h *Handler) ServeHTTP(w http.ResponseWriter) {',
      '  // body',
      '  h.process(w)',
      '}',
    ].join('\n');

    const result = getEnclosingSymbols(content, '.go', 5);
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe('ServeHTTP');
  });

  it('returns enclosing containers for Python', () => {
    const content = [
      'class Parser:',
      '    def parse(self, data):',
      '        return data.strip()',
      '',
      'def other():',
    ].join('\n');

    const result = getEnclosingSymbols(content, '.py', 3);
    expect(result).toHaveLength(2);
    expect(result[0].name).toBe('Parser');
    expect(result[1].name).toBe('parse');
  });

  it('sorts results by line (outermost first)', () => {
    const content = ['class Outer {', '  class Inner {', '    method() {', '      return 1;', '    }', '  }', '}'].join(
      '\n',
    );

    const result = getEnclosingSymbols(content, '.ts', 4);
    expect(result[0].name).toBe('Outer');
    expect(result[1].name).toBe('Inner');
    expect(result[2].name).toBe('method');
  });
});

// ── getScopePath ─────────────────────────────────────────────────────────

describe('getScopePath', () => {
  it('returns empty string when no enclosing containers', () => {
    const content = 'function foo() { return 1; }';
    expect(getScopePath(content, '.ts', 1, 'foo')).toBe('');
  });

  it('returns scope path with › separator', () => {
    const content = ['class Foo {', '  bar() {', '    return 1;', '  }', '}'].join('\n');

    // For 'bar' on line 2, enclosing is just 'Foo'
    const path = getScopePath(content, '.ts', 2, 'bar');
    expect(path).toBe('Foo');
  });

  it('filters out the symbol itself from enclosing', () => {
    const content = 'function foo() { return 1; }';
    const path = getScopePath(content, '.ts', 1, 'foo');
    // getEnclosingSymbols returns ['foo'], but getScopePath filters it out
    expect(path).toBe('');
  });

  it('returns nested scope path', () => {
    const content = ['class Outer {', '  class Inner {', '    method() {}', '  }', '}'].join('\n');

    const path = getScopePath(content, '.ts', 3, 'method');
    expect(path).toBe('Outer › Inner');
  });
});

// ── buildScopePaths ──────────────────────────────────────────────────────

describe('buildScopePaths', () => {
  it('returns empty map for empty content', () => {
    const map = buildScopePaths('', '.ts', []);
    expect(map.size).toBe(0);
  });

  it('returns empty map for empty symbols array', () => {
    const map = buildScopePaths('function foo() {}', '.ts', []);
    expect(map.size).toBe(0);
  });

  it('builds scope paths for multiple symbols', () => {
    const content = ['class Foo {', '  bar() { return 1; }', '  baz() { return 2; }', '}'].join('\n');

    const symbols: SymbolInfo[] = [
      { name: 'Foo', line: 1, kind: 'class' },
      { name: 'bar', line: 2, kind: 'method' },
      { name: 'baz', line: 3, kind: 'method' },
    ];

    const map = buildScopePaths(content, '.ts', symbols);
    expect(map.get(1)).toBeUndefined(); // Foo has no enclosing
    expect(map.get(2)).toBe('Foo');
    expect(map.get(3)).toBe('Foo');
  });

  it('builds nested scope paths', () => {
    const content = ['class Outer {', '  class Inner {', '    method() {}', '  }', '}'].join('\n');

    const symbols: SymbolInfo[] = [
      { name: 'Outer', line: 1, kind: 'class' },
      { name: 'Inner', line: 2, kind: 'class' },
      { name: 'method', line: 3, kind: 'method' },
    ];

    const map = buildScopePaths(content, '.ts', symbols);
    expect(map.get(1)).toBeUndefined();
    expect(map.get(2)).toBe('Outer');
    expect(map.get(3)).toBe('Outer › Inner');
  });

  it('handles non-container symbols correctly', () => {
    const content = ['class Foo {', '  bar() { return x; }', '}', 'const x = 1'].join('\n');

    const symbols: SymbolInfo[] = [
      { name: 'Foo', line: 1, kind: 'class' },
      { name: 'bar', line: 2, kind: 'method' },
      { name: 'x', line: 4, kind: 'variable' },
    ];

    const map = buildScopePaths(content, '.ts', symbols);
    expect(map.get(2)).toBe('Foo');
    // 'x' is not enclosed by any container
    expect(map.get(4)).toBeUndefined();
  });

  it('caps at 3 nesting levels', () => {
    const content = [
      'class A {',
      '  class B {',
      '    class C {',
      '      class D {',
      '        deep() {}',
      '      }',
      '    }',
      '  }',
      '}',
    ].join('\n');

    const symbols: SymbolInfo[] = [
      { name: 'A', line: 1, kind: 'class' },
      { name: 'B', line: 2, kind: 'class' },
      { name: 'C', line: 3, kind: 'class' },
      { name: 'D', line: 4, kind: 'class' },
      { name: 'deep', line: 5, kind: 'method' },
    ];

    const map = buildScopePaths(content, '.ts', symbols);
    // Path for 'deep' should be capped at 3 levels
    const path = map.get(5);
    // Should be C › D › ... or B › C › D (3 containers)
    // The cap means only 3 enclosing containers are returned
    expect(path!.split(' › ').length).toBeLessThanOrEqual(3);
  });

  it('works with Python indentation-based scoping', () => {
    const content = [
      'class Parser:',
      '    def parse(self, data):',
      '        return data.strip()',
      '',
      'def helper():',
      '    return True',
    ].join('\n');

    const symbols: SymbolInfo[] = [
      { name: 'Parser', line: 1, kind: 'class' },
      { name: 'parse', line: 2, kind: 'function' },
      { name: 'helper', line: 5, kind: 'function' },
    ];

    const map = buildScopePaths(content, '.py', symbols);
    expect(map.get(2)).toBe('Parser');
    expect(map.get(5)).toBeUndefined(); // helper not in any container
  });
});
