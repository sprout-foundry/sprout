/**
 * signatureHelp.test.ts — Unit tests for the signatureHelp extension.
 *
 * Tests the exported pure helper functions:
 * - splitSignatureAtParams
 * - splitByCommas
 * - isInsideFunctionCall
 * - renderSignature
 * - signatureHelpExtension (no-op for unsupported languages, language check)
 * - SIGNATURE_HELP_LANGUAGES
 *
 * CodeMirror modules are mocked since we test the pure computation logic
 * independently of the CodeMirror runtime.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest';

// ── Mock CodeMirror modules ────────────────────────────────────────

vi.mock('@codemirror/view', () => ({
  Decoration: {
    widget: vi.fn((opts) => opts),
    none: [],
    set: vi.fn((ranges) => ranges),
  },
  ViewPlugin: { fromClass: vi.fn(() => []) },
  EditorView: {
    baseTheme: vi.fn(() => 'mockTheme'),
  },
  keymap: { of: vi.fn((bindings) => bindings) },
}));

vi.mock('@codemirror/state', () => ({
  Annotation: { define: vi.fn(() => ({})) },
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: vi.fn(),
  },
}));

vi.mock('./lspExtensions', () => ({
  isLSPClientConnected: vi.fn(() => false),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// ── Module under test ──────────────────────────────────────────────
import {
  splitSignatureAtParams,
  splitByCommas,
  isInsideFunctionCall,
  renderSignature,
  signatureHelpExtension,
  SIGNATURE_HELP_LANGUAGES,
} from './signatureHelp';

// ── splitByCommas tests ────────────────────────────────────────────

describe('splitByCommas', () => {
  it('splits simple parameter list', () => {
    expect(splitByCommas('a: int, b: string')).toEqual(['a: int', 'b: string']);
  });

  it('returns single param if no comma', () => {
    expect(splitByCommas('a: int')).toEqual(['a: int']);
  });

  it('handles empty string', () => {
    expect(splitByCommas('')).toEqual([]);
  });

  it('handles nested parens', () => {
    expect(splitByCommas('fn(func(int, string)), b: int')).toEqual(['fn(func(int, string))', 'b: int']);
  });

  it('handles nested brackets', () => {
    expect(splitByCommas('arr[1, 2], b: int')).toEqual(['arr[1, 2]', 'b: int']);
  });

  it('handles nested braces', () => {
    expect(splitByCommas('map{1: "a"}, b: int')).toEqual(['map{1: "a"}', 'b: int']);
  });

  it('handles nested angle brackets (generics)', () => {
    expect(splitByCommas('List<int>, b: string')).toEqual(['List<int>', 'b: string']);
  });

  it('strips leading whitespace from each param', () => {
    expect(splitByCommas('a: int,  b: string,   c: bool')).toEqual(['a: int', 'b: string', 'c: bool']);
  });

  it('handles three params', () => {
    expect(splitByCommas('a, b, c')).toEqual(['a', 'b', 'c']);
  });

  it('handles trailing comma', () => {
    const result = splitByCommas('a, b,');
    expect(result).toEqual(['a', 'b']);
  });
});

// ── splitSignatureAtParams tests ───────────────────────────────────

describe('splitSignatureAtParams', () => {
  it('highlights the first parameter when activeParam is 0', () => {
    const parts = splitSignatureAtParams('foo(a: int, b: string) void', [], 0);
    expect(parts).toEqual([
      { text: 'foo(', highlight: false },
      { text: 'a: int', highlight: true },
      { text: ', ', highlight: false },
      { text: 'b: string', highlight: false },
      { text: ') void', highlight: false },
    ]);
  });

  it('highlights the second parameter when activeParam is 1', () => {
    const parts = splitSignatureAtParams('foo(a: int, b: string) void', [], 1);
    expect(parts).toEqual([
      { text: 'foo(', highlight: false },
      { text: 'a: int', highlight: false },
      { text: ', ', highlight: false },
      { text: 'b: string', highlight: true },
      { text: ') void', highlight: false },
    ]);
  });

  it('handles single parameter', () => {
    const parts = splitSignatureAtParams('bar(x: number) number', [], 0);
    expect(parts).toEqual([
      { text: 'bar(', highlight: false },
      { text: 'x: number', highlight: true },
      { text: ') number', highlight: false },
    ]);
  });

  it('handles no parameters (empty parens)', () => {
    const parts = splitSignatureAtParams('baz() void', [], 0);
    expect(parts).toEqual([
      { text: 'baz(', highlight: false },
      { text: ') void', highlight: false },
    ]);
  });

  it('returns full label if no parentheses', () => {
    const parts = splitSignatureAtParams('noParens', [], 0);
    expect(parts).toEqual([{ text: 'noParens', highlight: false }]);
  });

  it('clamps activeParam to max parameter index', () => {
    const parts = splitSignatureAtParams('foo(a, b)', [], 5);
    expect(parts).toEqual([
      { text: 'foo(', highlight: false },
      { text: 'a', highlight: false },
      { text: ', ', highlight: false },
      { text: 'b', highlight: true }, // clamped to last param
      { text: ')', highlight: false },
    ]);
  });

  it('handles nested parentheses in params', () => {
    const parts = splitSignatureAtParams('wrap(func(int), b)', [], 1);
    expect(parts).toEqual([
      { text: 'wrap(', highlight: false },
      { text: 'func(int)', highlight: false },
      { text: ', ', highlight: false },
      { text: 'b', highlight: true },
      { text: ')', highlight: false },
    ]);
  });
});

// ── isInsideFunctionCall tests ─────────────────────────────────────

describe('isInsideFunctionCall', () => {
  // We need a mock view with state.doc.toString() and cursor position
  function createMockView(text: string, cursorPos: number) {
    return {
      state: {
        doc: {
          toString: () => text,
          sliceString: (from: number, to: number) => text.substring(from, to),
        },
        selection: {
          main: { head: cursorPos },
        },
      },
    } as any;
  }

  it('returns true when cursor is after opening paren', () => {
    // "foo(arg|)" — cursor at position 7
    const view = createMockView('foo(arg)', 7);
    expect(isInsideFunctionCall(view, 7)).toBe(true);
  });

  it('returns true when cursor is between params after comma', () => {
    // "foo(a, |b)" — cursor at position 7
    const view = createMockView('foo(a, b)', 7);
    expect(isInsideFunctionCall(view, 7)).toBe(true);
  });

  it('returns true immediatly after opening paren', () => {
    // "foo(|a, b)" — cursor at position 4
    const view = createMockView('foo(a, b)', 4);
    expect(isInsideFunctionCall(view, 4)).toBe(true);
  });

  it('returns false when cursor is before opening paren', () => {
    // "fo|o(a, b)" — cursor at position 2
    const view = createMockView('foo(a, b)', 2);
    expect(isInsideFunctionCall(view, 2)).toBe(false);
  });

  it('returns false when cursor is after closing paren', () => {
    // "foo(a, b)|" — cursor at position 10
    const view = createMockView('foo(a, b)', 10);
    expect(isInsideFunctionCall(view, 10)).toBe(false);
  });

  it('handles nested function calls — cursor in outer call', () => {
    // "outer(inner(|), b)" — cursor at position 12
    const view = createMockView('outer(inner(), b)', 12);
    expect(isInsideFunctionCall(view, 12)).toBe(true);
  });

  it('returns false when no parens at all', () => {
    const view = createMockView('let x = 5', 5);
    expect(isInsideFunctionCall(view, 5)).toBe(false);
  });

  it('stops at semicolon boundary', () => {
    // "foo(); bar|" — cursor after semicolon, second statement with no call
    const view = createMockView('foo(); bar', 9);
    expect(isInsideFunctionCall(view, 9)).toBe(false);
  });
});

// ── renderSignature tests ──────────────────────────────────────────

describe('renderSignature', () => {
  let el: HTMLDivElement;

  beforeEach(() => {
    el = document.createElement('div');
    el.style.display = 'none';
  });

  it('renders a single signature with highlighted active param', () => {
    renderSignature(el, {
      signatures: [
        {
          label: 'foo(a: int, b: string) void',
          parameters: [{ label: 'a: int' }, { label: 'b: string' }],
        },
      ],
      activeSignature: 0,
      activeParameter: 0,
    });

    expect(el.style.display).toBe('block');
    expect(el.querySelector('.cm-signature-help-signature')).toBeTruthy();
    const activeParam = el.querySelector('.cm-signature-help-active-param');
    expect(activeParam).toBeTruthy();
    expect(activeParam?.textContent).toBe('a: int');
  });

  it('renders documentation when present', () => {
    renderSignature(el, {
      signatures: [
        {
          label: 'foo(a: int) void',
          parameters: [{ label: 'a: int' }],
          documentation: 'Does a thing',
        },
      ],
      activeSignature: 0,
      activeParameter: 0,
    });

    const doc = el.querySelector('.cm-signature-help-doc');
    expect(doc).toBeTruthy();
    expect(doc?.textContent).toBe('Does a thing');
  });

  it('renders overload count for multiple signatures', () => {
    renderSignature(el, {
      signatures: [
        { label: 'foo(a: int) void', parameters: [{ label: 'a: int' }] },
        { label: 'foo(a: int, b: string) void', parameters: [{ label: 'a: int' }, { label: 'b: string' }] },
      ],
      activeSignature: 0,
      activeParameter: 0,
    });

    const overload = el.querySelector('.cm-signature-help-overload');
    expect(overload).toBeTruthy();
    expect(overload?.textContent).toBe('1/2');
  });

  it('hides tooltip when no signatures', () => {
    renderSignature(el, {
      signatures: [],
      activeSignature: 0,
      activeParameter: 0,
    });

    expect(el.style.display).toBe('none');
  });

  it('highlights second param when activeParameter is 1', () => {
    renderSignature(el, {
      signatures: [
        {
          label: 'foo(a: int, b: string) void',
          parameters: [{ label: 'a: int' }, { label: 'b: string' }],
        },
      ],
      activeSignature: 0,
      activeParameter: 1,
    });

    const activeParams = el.querySelectorAll('.cm-signature-help-active-param');
    expect(activeParams.length).toBe(1);
    expect(activeParams[0].textContent).toBe('b: string');
  });

  it('shows 2/3 overload count for second of three signatures', () => {
    renderSignature(el, {
      signatures: [
        { label: 'fn() void', parameters: [] },
        { label: 'fn(a: int) void', parameters: [{ label: 'a: int' }] },
        { label: 'fn(a: int, b: int) void', parameters: [{ label: 'a: int' }, { label: 'b: int' }] },
      ],
      activeSignature: 1,
      activeParameter: 0,
    });

    const overload = el.querySelector('.cm-signature-help-overload');
    expect(overload?.textContent).toBe('2/3');
  });
});

// ── signatureHelpExtension tests ───────────────────────────────────

describe('signatureHelpExtension', () => {
  it('returns empty array for null languageId', () => {
    const result = signatureHelpExtension(
      () => 'test.ts',
      () => 'code',
      null,
    );
    expect(result).toEqual([]);
  });

  it('returns empty array for unsupported language', () => {
    const result = signatureHelpExtension(
      () => 'test.py',
      () => 'code',
      'python',
    );
    expect(result).toEqual([]);
  });

  it('returns empty array for undefined languageId', () => {
    const result = signatureHelpExtension(
      () => 'test.txt',
      () => 'code',
      undefined,
    );
    expect(result).toEqual([]);
  });

  it('returns non-empty array for supported language (TypeScript)', () => {
    const result = signatureHelpExtension(
      () => 'test.ts',
      () => 'code',
      'typescript',
    );
    expect(Array.isArray(result)).toBe(true);
    expect(result.length).toBeGreaterThan(0);
  });

  it('returns non-empty array for Go', () => {
    const result = signatureHelpExtension(
      () => 'test.go',
      () => 'code',
      'go',
    );
    expect(Array.isArray(result)).toBe(true);
    expect(result.length).toBeGreaterThan(0);
  });

  it('returns non-empty array for JavaScript', () => {
    const result = signatureHelpExtension(
      () => 'test.js',
      () => 'code',
      'javascript',
    );
    expect(Array.isArray(result)).toBe(true);
    expect(result.length).toBeGreaterThan(0);
  });

  it('includes theme in result for supported language', () => {
    const result = signatureHelpExtension(
      () => 'test.ts',
      () => 'code',
      'typescript',
    );
    // Should include the mocked theme string
    expect(result.some((item) => item === 'mockTheme')).toBe(true);
  });
});

// ── SIGNATURE_HELP_LANGUAGES tests ─────────────────────────────────

describe('SIGNATURE_HELP_LANGUAGES', () => {
  it('includes TypeScript', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('typescript')).toBe(true);
  });

  it('includes TypeScript JSX', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('typescript-jsx')).toBe(true);
  });

  it('includes JavaScript', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('javascript')).toBe(true);
  });

  it('includes JavaScript JSX', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('javascript-jsx')).toBe(true);
  });

  it('includes Go', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('go')).toBe(true);
  });

  it('does not include Python', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('python')).toBe(false);
  });

  it('does not include Rust', () => {
    expect(SIGNATURE_HELP_LANGUAGES.has('rust')).toBe(false);
  });

  it('has exactly 5 languages', () => {
    expect(SIGNATURE_HELP_LANGUAGES.size).toBe(5);
  });
});
