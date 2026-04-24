/**
 * stickyScroll.test.ts — Unit tests for sticky scroll extension helper functions.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * we mock the CM imports and test the exported `computeStickyScopes`
 * helper directly — it's a pure function with no CM dependencies.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { computeStickyScopes, stickyScrollPlugin } from './stickyScroll';

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

// Mock the GoToSymbolOverlay module
jest.mock('../components/GoToSymbolOverlay', () => ({
  extractSymbols: jest.fn(),
  getEnclosingSymbols: jest.fn(),
}));

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