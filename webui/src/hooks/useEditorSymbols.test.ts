/**
 * useEditorSymbols.test.ts — Unit tests for the useEditorSymbols hook.
 *
 * Covers:
 * - Empty/null content returns empty enclosingSymbols
 * - Null/undefined buffer returns empty enclosingSymbols
 * - Symbol extraction from TypeScript content
 * - Symbol extraction from Go content
 * - Cursor position filtering (enclosing symbols)
 * - Breadcrumb depth limit (max 3 levels)
 * - File extension filtering (no extraction for unknown extensions)
 * - Re-memoization behavior (content changes vs cursor changes)
 * - Content-key (checksum) memoization: new string references with identical content
 *   do NOT trigger re-extraction; combined cursor-move + string-reference-change
 *   also does NOT trigger re-extraction.
 */
// @ts-nocheck
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — MUST be declared at module scope BEFORE vi.mock so the factory
// can reference them and test code can assert on them.
// ---------------------------------------------------------------------------

const mockExtractSymbols = vi.fn();
const mockFindSymbolScopeEnd = vi.fn();

vi.mock('../utils/symbolUtils', () => ({
  extractSymbols: (...args) => mockExtractSymbols(...args),
  findSymbolScopeEnd: (...args) => mockFindSymbolScopeEnd(...args),
  CONTAINER_KINDS: new Set(['function', 'class', 'method', 'struct', 'interface', 'type']),
}));

// Static import — Vitest hoists vi.mock above all imports automatically
import { useEditorSymbols } from './useEditorSymbols';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();

  // Default mock: no symbols, scope ends at line 10
  mockExtractSymbols.mockReturnValue([]);
  mockFindSymbolScopeEnd.mockReturnValue(10);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Render the hook inside a minimal wrapper component so React effects fire.
 */
function renderTestHook(
  options: {
    localContent?: string | undefined;
    buffer?: any;
  } = {},
) {
  const { localContent = '', buffer = null } = options;

  let hookReturn: any = null;

  function HookWrapper() {
    hookReturn = useEditorSymbols(localContent, buffer);
    return null;
  }

  act(() => {
    root.render(createElement(HookWrapper));
  });

  return {
    getReturn: () => hookReturn,
  };
}

/** Create a minimal EditorBuffer-like object for testing */
function createBuffer(
  options: {
    fileExt?: string;
    cursorLine?: number;
    cursorColumn?: number;
    fileName?: string;
  } = {},
) {
  const { fileExt = 'ts', cursorLine = 0, cursorColumn = 0, fileName = 'test.ts' } = options;

  return {
    file: {
      ext: `.${fileExt}`,
      name: fileName,
    },
    cursorPosition: {
      line: cursorLine,
      column: cursorColumn,
    },
  };
}

// ---------------------------------------------------------------------------
// Tests: empty/null content returns empty
// ---------------------------------------------------------------------------

describe('empty/null content', () => {
  it('returns empty enclosingSymbols when localContent is undefined', () => {
    const { getReturn } = renderTestHook({
      localContent: undefined,
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 2 }),
    });

    expect(getReturn().enclosingSymbols).toEqual([]);
  });

  it('returns empty enclosingSymbols when localContent is empty string', () => {
    const { getReturn } = renderTestHook({
      localContent: '',
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 0 }),
    });

    expect(getReturn().enclosingSymbols).toEqual([]);
  });

  it('returns empty enclosingSymbols when buffer is null', () => {
    const { getReturn } = renderTestHook({
      localContent: 'const x = 1;',
      buffer: null,
    });

    expect(getReturn().enclosingSymbols).toEqual([]);
  });

  it('returns empty enclosingSymbols when buffer is undefined', () => {
    const { getReturn } = renderTestHook({
      localContent: 'const x = 1;',
      buffer: undefined,
    });

    expect(getReturn().enclosingSymbols).toEqual([]);
  });

  it('returns empty enclosingSymbols when cursorPosition is missing from buffer', () => {
    const buf = createBuffer({ fileExt: 'ts' });
    delete buf.cursorPosition;

    const { getReturn } = renderTestHook({
      localContent: 'const x = 1;',
      buffer: buf,
    });

    expect(getReturn().enclosingSymbols).toEqual([]);
  });

  it('returns empty enclosingSymbols when buffer.file.ext is missing', () => {
    const buf = createBuffer({ fileExt: 'ts' });
    buf.file.ext = undefined;

    const { getReturn } = renderTestHook({
      localContent: 'const x = 1;',
      buffer: buf,
    });

    expect(getReturn().enclosingSymbols).toEqual([]);
    expect(mockExtractSymbols).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: symbol extraction by file extension
// ---------------------------------------------------------------------------

describe('symbol extraction by file extension', () => {
  it('calls extractSymbols with correct content and extension for TypeScript', () => {
    const content = 'class MyClass { myMethod() { function innerFunc() {} } }';

    renderTestHook({
      localContent: content,
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 5 }),
    });

    // Hook passes buffer.file.ext which includes the dot prefix
    expect(mockExtractSymbols).toHaveBeenCalledWith(content, '.ts');
  });

  it('calls extractSymbols with correct content and extension for Go', () => {
    const content = 'type MyStruct struct { field string }';

    renderTestHook({
      localContent: content,
      buffer: createBuffer({ fileExt: 'go', cursorLine: 2 }),
    });

    expect(mockExtractSymbols).toHaveBeenCalledWith(content, '.go');
  });

  it('calls extractSymbols with correct content and extension for Python', () => {
    const content = 'class MyClass:\n    def my_method(self):\n        pass';

    renderTestHook({
      localContent: content,
      buffer: createBuffer({ fileExt: 'python', cursorLine: 2 }),
    });

    expect(mockExtractSymbols).toHaveBeenCalledWith(content, '.python');
  });

  it('returns empty result for unknown file extensions that yield no symbols', () => {
    mockExtractSymbols.mockReturnValue([]);

    const content = 'some random content';
    const { getReturn } = renderTestHook({
      localContent: content,
      buffer: createBuffer({ fileExt: 'xyz', cursorLine: 0 }),
    });

    expect(mockExtractSymbols).toHaveBeenCalledWith(content, '.xyz');
    expect(getReturn().enclosingSymbols).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Tests: enclosing symbol computation
// ---------------------------------------------------------------------------

describe('enclosing symbol computation', () => {
  it('returns all enclosing symbols when cursor is within all nested scopes', () => {
    mockExtractSymbols.mockReturnValue([
      { kind: 'class', name: 'MyClass', line: 1 },
      { kind: 'method', name: 'myMethod', line: 3 },
      { kind: 'function', name: 'innerFunc', line: 5 },
    ]);

    // Each scope ends far enough to include cursor at line 5 (0-based) = line 6 (1-based)
    mockFindSymbolScopeEnd.mockImplementation((lines, startLine0based) => {
      return startLine0based + 20; // well past cursor
    });

    const { getReturn } = renderTestHook({
      localContent: 'class MyClass { myMethod() { function innerFunc() {} } }',
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 5 }),
    });

    expect(getReturn().enclosingSymbols).toHaveLength(3);
    expect(getReturn().enclosingSymbols[0].name).toBe('MyClass');
    expect(getReturn().enclosingSymbols[1].name).toBe('myMethod');
    expect(getReturn().enclosingSymbols[2].name).toBe('innerFunc');
  });

  it('excludes symbols whose scope ends before the cursor', () => {
    mockExtractSymbols.mockReturnValue([
      { kind: 'class', name: 'MyClass', line: 1 },
      { kind: 'method', name: 'myMethod', line: 3 },
      { kind: 'function', name: 'innerFunc', line: 5 },
    ]);

    // class: startLine0=0, end=10; method: startLine0=2, end=8; function: startLine0=4, end=7
    mockFindSymbolScopeEnd.mockImplementation((lines, startLine0based) => {
      if (startLine0based === 0) return 10;
      if (startLine0based === 2) return 8;
      if (startLine0based === 4) return 7;
      return startLine0based + 1;
    });

    // Cursor at line 7 (0-based) = line 8 (1-based)
    // class: line 1 <= 8 ✓, end 10 >= 8 ✓ → enclosing
    // method: line 3 <= 8 ✓, end 8 >= 8 ✓ → enclosing
    // function: line 5 <= 8 ✓, end 7 < 8 ✗ → NOT enclosing
    const { getReturn } = renderTestHook({
      localContent: 'class MyClass { myMethod() { function innerFunc() {} } }',
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 7 }),
    });

    expect(getReturn().enclosingSymbols).toHaveLength(2);
    expect(getReturn().enclosingSymbols[0].name).toBe('MyClass');
    expect(getReturn().enclosingSymbols[1].name).toBe('myMethod');
  });

  it('excludes symbols whose line is after the cursor', () => {
    mockExtractSymbols.mockReturnValue([
      { kind: 'class', name: 'MyClass', line: 1 },
      { kind: 'method', name: 'myMethod', line: 3 },
      { kind: 'function', name: 'innerFunc', line: 5 },
    ]);

    mockFindSymbolScopeEnd.mockReturnValue(100);

    // Cursor at line 1 (0-based) = line 2 (1-based)
    // class: line 1 <= 2 ✓, end 100 >= 2 ✓ → enclosing
    // method: line 3 > 2 ✗ → NOT enclosing (symbol starts after cursor)
    // function: line 5 > 2 ✗ → NOT enclosing
    const { getReturn } = renderTestHook({
      localContent: 'class MyClass { myMethod() { function innerFunc() {} } }',
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 1 }),
    });

    expect(getReturn().enclosingSymbols).toHaveLength(1);
    expect(getReturn().enclosingSymbols[0].name).toBe('MyClass');
  });

  it('caps results at 3 levels deep even when more containers enclose cursor', () => {
    mockExtractSymbols.mockReturnValue([
      { kind: 'class', name: 'A', line: 1 },
      { kind: 'method', name: 'B', line: 2 },
      { kind: 'function', name: 'C', line: 3 },
      { kind: 'function', name: 'D', line: 4 },
      { kind: 'function', name: 'E', line: 5 },
    ]);

    mockFindSymbolScopeEnd.mockReturnValue(100);

    const { getReturn } = renderTestHook({
      localContent: 'nested content',
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 5 }),
    });

    expect(getReturn().enclosingSymbols).toHaveLength(3);
    expect(getReturn().enclosingSymbols[0].name).toBe('A');
    expect(getReturn().enclosingSymbols[1].name).toBe('B');
    expect(getReturn().enclosingSymbols[2].name).toBe('C');
  });
});

// ---------------------------------------------------------------------------
// Tests: symbol extraction only on content/extension changes (memoization)
// ---------------------------------------------------------------------------

describe('memoization behavior', () => {
  it('does NOT re-call extractSymbols when only cursor position changes', () => {
    const content = 'class MyClass { method() {} }';
    let cursorLine = 0;

    function HookWrapper() {
      useEditorSymbols(content, createBuffer({ fileExt: 'ts', cursorLine }));
      return null;
    }

    act(() => {
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);

    // Change cursor position via closure variable and re-render same component
    act(() => {
      cursorLine = 5;
      root.render(createElement(HookWrapper));
    });

    // extractSymbols should NOT be called again — only cursor changed, not content/ext
    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);
  });

  it('re-calls extractSymbols when content changes', () => {
    let content = 'class MyClass { method() {} }';

    function HookWrapper() {
      useEditorSymbols(content, createBuffer({ fileExt: 'ts', cursorLine: 0 }));
      return null;
    }

    act(() => {
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);

    // Change content and re-render same component
    act(() => {
      content = 'class OtherClass { otherMethod() {} }';
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(2);
    // Hook passes buffer.file.ext which includes the dot prefix
    expect(mockExtractSymbols).toHaveBeenLastCalledWith('class OtherClass { otherMethod() {} }', '.ts');
  });

  it('re-calls extractSymbols when file extension changes', () => {
    const content = 'some content';
    let fileExt = 'ts';

    function HookWrapper() {
      useEditorSymbols(content, createBuffer({ fileExt, cursorLine: 0 }));
      return null;
    }

    act(() => {
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);

    // Change extension and re-render same component
    act(() => {
      fileExt = 'go';
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(2);
    // Hook passes buffer.file.ext which includes the dot prefix
    expect(mockExtractSymbols).toHaveBeenLastCalledWith(content, '.go');
  });

  it('does NOT re-call extractSymbols when a new string reference has identical content', () => {
    // This tests the contentChecksum (djb2 hash) key behavior:
    // When the parent re-creates a string with identical content (different reference),
    // the computed hash is the same, so useMemo skips re-extraction.
    // This prevents unnecessary work when e.g. a React state updater returns
    // a new string that happens to be identical to the previous one.
    const originalContent = 'class MyClass { method() {} }';
    let currentContent = originalContent;

    function HookWrapper() {
      useEditorSymbols(currentContent, createBuffer({ fileExt: 'ts', cursorLine: 0 }));
      return null;
    }

    // Initial render
    act(() => {
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);

    // Re-render with a NEW string object that has IDENTICAL content.
    // Simulates: setLocalContent(prev => prev) or setLocalContent(prev => prev + '')
    act(() => {
      currentContent = originalContent + ''; // new reference, same content
      root.render(createElement(HookWrapper));
    });

    // extractSymbols should NOT be called again — checksum is identical
    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);

    // Re-render yet again with yet another new string reference (spread trick)
    act(() => {
      currentContent = [...originalContent].join(''); // another new reference, same content
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);
  });

  it('does NOT re-call extractSymbols when cursor moves but string reference also changes', () => {
    // Combined scenario: parent passes both a new string reference AND new cursor.
    // If content is identical, extractSymbols should still not be re-called,
    // even though the enclosingSymbols useMemo will re-run for the cursor change.
    const content = 'class MyClass { method() {} }';
    let currentContent = content;
    let cursorLine = 0;

    function HookWrapper() {
      useEditorSymbols(currentContent, createBuffer({ fileExt: 'ts', cursorLine }));
      return null;
    }

    act(() => {
      root.render(createElement(HookWrapper));
    });

    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);

    // Re-render: new string reference (identical content) + new cursor line
    act(() => {
      currentContent = content + ''; // new reference, same content
      cursorLine = 5;
      root.render(createElement(HookWrapper));
    });

    // extractSymbols should NOT be called again — content checksum is the same
    // (the enclosingSymbols stage will re-run due to cursor change, but that's cheap)
    expect(mockExtractSymbols).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: symbol filtering by CONTAINER_KINDS
// ---------------------------------------------------------------------------

describe('container kind filtering', () => {
  it('excludes non-container kinds from enclosingSymbols', () => {
    mockExtractSymbols.mockReturnValue([
      { kind: 'class', name: 'MyClass', line: 1 },
      { kind: 'variable', name: 'x', line: 2 },
      { kind: 'function', name: 'myFunc', line: 3 },
    ]);

    mockFindSymbolScopeEnd.mockReturnValue(100);

    const { getReturn } = renderTestHook({
      localContent: 'content',
      buffer: createBuffer({ fileExt: 'ts', cursorLine: 5 }),
    });

    expect(getReturn().enclosingSymbols).toHaveLength(2);
    expect(getReturn().enclosingSymbols[0].name).toBe('MyClass');
    expect(getReturn().enclosingSymbols[1].name).toBe('myFunc');
    expect(getReturn().enclosingSymbols.some((s) => s.kind === 'variable')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Tests: export verification
// ---------------------------------------------------------------------------

describe('exports', () => {
  it('useEditorSymbols is exported as a function', () => {
    expect(typeof useEditorSymbols).toBe('function');
  });
});
