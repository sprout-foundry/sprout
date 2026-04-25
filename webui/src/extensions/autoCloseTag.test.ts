/**
 * autoCloseTag.test.ts — Unit tests for the auto-close tag extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the CM imports are mocked. We test the pure logic functions.
 */

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/state', () => {
  const mockCompartment = jest.fn(() => ({
    reconfigure: jest.fn(() => ({ type: 'StateEffect' })),
  }));

  return {
    Extension: {},
    Compartment: mockCompartment,
  };
});

jest.mock('@codemirror/view', () => ({
  EditorView: {
    updateListener: {
      of: jest.fn(() => ({ type: 'UpdateListener' })),
    },
  },
}));

jest.mock('@codemirror/language', () => ({
  indentUnit: {
    combine: jest.fn(),
  },
}));

// ── Module under test (Jest hoists mocks above imports) ─────────────

import {
  buildAutoCloseTagExtensions,
  createAutoCloseTagCompartment,
  getInitialAutoCloseTagExtensions,
  reconfigureAutoCloseTag,
  isAutoCloseTagLanguage,
} from './autoCloseTag';

// ── isAutoCloseTagLanguage tests ───────────────────────────────────

describe('isAutoCloseTagLanguage', () => {
  // -------------------------------------------------------------------------
  // Null/undefined/empty inputs
  // -------------------------------------------------------------------------

  it('returns false for null', () => {
    expect(isAutoCloseTagLanguage(null)).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isAutoCloseTagLanguage(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isAutoCloseTagLanguage('')).toBe(false);
  });

  // -------------------------------------------------------------------------
  // Auto-close tag supported languages
  // -------------------------------------------------------------------------

  it('returns true for html', () => {
    expect(isAutoCloseTagLanguage('html')).toBe(true);
  });

  it('returns true for xml', () => {
    expect(isAutoCloseTagLanguage('xml')).toBe(true);
  });

  it('returns true for javascript-jsx', () => {
    expect(isAutoCloseTagLanguage('javascript-jsx')).toBe(true);
  });

  it('returns true for typescript-jsx', () => {
    expect(isAutoCloseTagLanguage('typescript-jsx')).toBe(true);
  });

  it('returns false for css', () => {
    expect(isAutoCloseTagLanguage('css')).toBe(false);
  });

  it('returns false for scss', () => {
    expect(isAutoCloseTagLanguage('scss')).toBe(false);
  });

  it('returns false for sass', () => {
    expect(isAutoCloseTagLanguage('sass')).toBe(false);
  });

  it('returns true for php', () => {
    expect(isAutoCloseTagLanguage('php')).toBe(true);
  });

  // -------------------------------------------------------------------------
  // Non-supported languages
  // -------------------------------------------------------------------------

  it('returns false for python', () => {
    expect(isAutoCloseTagLanguage('python')).toBe(false);
  });

  it('returns false for go', () => {
    expect(isAutoCloseTagLanguage('go')).toBe(false);
  });

  it('returns false for javascript', () => {
    expect(isAutoCloseTagLanguage('javascript')).toBe(false);
  });

  it('returns false for typescript', () => {
    expect(isAutoCloseTagLanguage('typescript')).toBe(false);
  });

  it('returns false for json', () => {
    expect(isAutoCloseTagLanguage('json')).toBe(false);
  });

  it('returns false for markdown', () => {
    expect(isAutoCloseTagLanguage('markdown')).toBe(false);
  });

  it('returns false for yaml', () => {
    expect(isAutoCloseTagLanguage('yaml')).toBe(false);
  });

  it('returns false for rust', () => {
    expect(isAutoCloseTagLanguage('rust')).toBe(false);
  });

  it('returns false for java', () => {
    expect(isAutoCloseTagLanguage('java')).toBe(false);
  });

  // -------------------------------------------------------------------------
  // Case sensitivity
  // -------------------------------------------------------------------------

  it('returns false for HTML (uppercase)', () => {
    expect(isAutoCloseTagLanguage('HTML')).toBe(false);
  });

  it('returns false for CSS (uppercase)', () => {
    expect(isAutoCloseTagLanguage('CSS')).toBe(false);
  });

  it('returns false for PHP (uppercase)', () => {
    expect(isAutoCloseTagLanguage('PHP')).toBe(false);
  });

  it('returns true for html (lowercase)', () => {
    expect(isAutoCloseTagLanguage('html')).toBe(true);
  });

  it('returns false for css (lowercase)', () => {
    expect(isAutoCloseTagLanguage('css')).toBe(false);
  });

  it('returns true for php (lowercase)', () => {
    expect(isAutoCloseTagLanguage('php')).toBe(true);
  });
});

// ── buildAutoCloseTagExtensions tests ────────────────────────────────

describe('buildAutoCloseTagExtensions', () => {
  it('returns empty array for null language ID', () => {
    const result = buildAutoCloseTagExtensions(null);
    expect(result).toEqual([]);
  });

  it('returns empty array for undefined language ID', () => {
    const result = buildAutoCloseTagExtensions(undefined);
    expect(result).toEqual([]);
  });

  it('returns empty array for empty string language ID', () => {
    const result = buildAutoCloseTagExtensions('');
    expect(result).toEqual([]);
  });

  it('returns empty array for python', () => {
    const result = buildAutoCloseTagExtensions('python');
    expect(result).toEqual([]);
  });

  it('returns empty array for go', () => {
    const result = buildAutoCloseTagExtensions('go');
    expect(result).toEqual([]);
  });

  it('returns empty array for javascript (non-JSX)', () => {
    const result = buildAutoCloseTagExtensions('javascript');
    expect(result).toEqual([]);
  });

  it('returns empty array for HTML (uppercase)', () => {
    const result = buildAutoCloseTagExtensions('HTML');
    expect(result).toEqual([]);
  });

  it('returns non-empty array for html', () => {
    const result = buildAutoCloseTagExtensions('html');
    expect(result).toBeInstanceOf(Array);
    expect(result.length).toBeGreaterThan(0);
  });

  it('returns non-empty array for xml', () => {
    const result = buildAutoCloseTagExtensions('xml');
    expect(result).toBeInstanceOf(Array);
    expect(result.length).toBeGreaterThan(0);
  });

  it('returns non-empty array for javascript-jsx', () => {
    const result = buildAutoCloseTagExtensions('javascript-jsx');
    expect(result).toBeInstanceOf(Array);
    expect(result.length).toBeGreaterThan(0);
  });

  it('returns non-empty array for php', () => {
    const result = buildAutoCloseTagExtensions('php');
    expect(result).toBeInstanceOf(Array);
    expect(result.length).toBeGreaterThan(0);
  });
});

// ── createAutoCloseTagCompartment tests ─────────────────────────────

describe('createAutoCloseTagCompartment', () => {
  it('returns a truthy value', () => {
    const compartment = createAutoCloseTagCompartment();
    expect(compartment).toBeTruthy();
  });

  it('returns a new value on each call', () => {
    const compartment1 = createAutoCloseTagCompartment();
    const compartment2 = createAutoCloseTagCompartment();
    expect(compartment1).not.toBe(compartment2);
  });

  it('returns a value that can be used in reconfigureAutoCloseTag', () => {
    const compartment = createAutoCloseTagCompartment();
    const view = { dispatch: jest.fn() };
    // Should not throw
    expect(() => {
      reconfigureAutoCloseTag(compartment, view as any, 'html');
    }).not.toThrow();
  });
});

// ── getInitialAutoCloseTagExtensions tests ───────────────────────────

describe('getInitialAutoCloseTagExtensions', () => {
  it('delegates to buildAutoCloseTagExtensions for null', () => {
    const result = getInitialAutoCloseTagExtensions(null);
    expect(result).toEqual([]);
  });

  it('delegates to buildAutoCloseTagExtensions for undefined', () => {
    const result = getInitialAutoCloseTagExtensions(undefined);
    expect(result).toEqual([]);
  });

  it('delegates to buildAutoCloseTagExtensions for non-supported language', () => {
    const result = getInitialAutoCloseTagExtensions('python');
    expect(result).toEqual([]);
  });

  it('returns non-empty array for html', () => {
    const result = getInitialAutoCloseTagExtensions('html');
    expect(result).toBeInstanceOf(Array);
    expect(result.length).toBeGreaterThan(0);
  });
});

// ── reconfigureAutoCloseTag tests ───────────────────────────────────

describe('reconfigureAutoCloseTag', () => {
  it('exists as a function', () => {
    expect(typeof reconfigureAutoCloseTag).toBe('function');
  });

  it('does not throw when called with valid parameters', () => {
    const compartment = createAutoCloseTagCompartment();
    const view = { dispatch: jest.fn() };
    expect(() => {
      reconfigureAutoCloseTag(compartment, view as any, 'html');
    }).not.toThrow();
  });

  it('does not throw when called with null language ID', () => {
    const compartment = createAutoCloseTagCompartment();
    const view = { dispatch: jest.fn() };
    expect(() => {
      reconfigureAutoCloseTag(compartment, view as any, null);
    }).not.toThrow();
  });
});