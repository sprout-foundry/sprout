/**
 * autoCloseTag.test.ts — Unit tests for the auto-close tag extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the CM imports are mocked. We test the pure logic functions.
 */

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

vi.mock('@codemirror/state', () => {
  const mockCompartment = vi.fn(() => ({
    reconfigure: vi.fn(() => ({ type: 'StateEffect' })),
  }));

  return {
    Extension: {},
    Compartment: mockCompartment,
  };
});

vi.mock('@codemirror/view', () => ({
  EditorView: {
    updateListener: {
      of: vi.fn(() => ({ type: 'UpdateListener' })),
    },
  },
}));

vi.mock('@codemirror/language', () => ({
  indentUnit: {
    combine: vi.fn(),
  },
}));

vi.mock('../utils/editorHotkeys', () => ({
  getLineIndent: vi.fn((text: string) => {
    const match = text.match(/^(\s*)/);
    return match ? match[1] : '';
  }),
}));

// ── Module under test (Jest hoists mocks above imports) ─────────────

import {
  buildAutoCloseTagExtensions,
  createAutoCloseTagCompartment,
  getInitialAutoCloseTagExtensions,
  reconfigureAutoCloseTag,
  isAutoCloseTagLanguage,
  extractTagName,
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
    const view = { dispatch: vi.fn() };
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
    const view = { dispatch: vi.fn() };
    expect(() => {
      reconfigureAutoCloseTag(compartment, view as any, 'html');
    }).not.toThrow();
  });

  it('does not throw when called with null language ID', () => {
    const compartment = createAutoCloseTagCompartment();
    const view = { dispatch: vi.fn() };
    expect(() => {
      reconfigureAutoCloseTag(compartment, view as any, null);
    }).not.toThrow();
  });
});

// ── extractTagName tests ───────────────────────────────────────────

describe('extractTagName', () => {
  // -------------------------------------------------------------------------
  // Simple tags
  // -------------------------------------------------------------------------

  describe('simple tags', () => {
    it('extracts "div" from <div>', () => {
      expect(extractTagName('<div>', 5)).toBe('div');
    });

    it('extracts "span" from <span>', () => {
      expect(extractTagName('<span>', 6)).toBe('span');
    });

    it('extracts "h1" from <h1>', () => {
      expect(extractTagName('<h1>', 4)).toBe('h1');
    });

    it('returns null when there is no opening <', () => {
      expect(extractTagName('p>', 2)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Tags with attributes (should extract just the tag name)
  // -------------------------------------------------------------------------

  describe('tags with attributes', () => {
    it('extracts "div" from <div class="foo">', () => {
      // <div class="foo">  → 17 chars
      expect(extractTagName('<div class="foo">', 17)).toBe('div');
    });

    it('extracts "span" from <span id="x" class="y">', () => {
      // <span id="x" class="y">  → 23 chars
      expect(extractTagName('<span id="x" class="y">', 23)).toBe('span');
    });

    it('extracts "button" from <button disabled>', () => {
      // <button disabled>  → 17 chars
      expect(extractTagName('<button disabled>', 17)).toBe('button');
    });
  });

  // -------------------------------------------------------------------------
  // Web components and namespaced tags
  // -------------------------------------------------------------------------

  describe('web components and namespaced tags', () => {
    it('extracts "my-component" from <my-component>', () => {
      expect(extractTagName('<my-component>', 14)).toBe('my-component');
    });

    it('extracts "ns:tag" from <ns:tag>', () => {
      expect(extractTagName('<ns:tag>', 8)).toBe('ns:tag');
    });

    it('extracts "custom-element" from <custom-element id="a">', () => {
      // <custom-element id="a">  → 23 chars
      expect(extractTagName('<custom-element id="a">', 23)).toBe('custom-element');
    });
  });

  // -------------------------------------------------------------------------
  // Void elements (case-insensitive, should all return null)
  // -------------------------------------------------------------------------

  describe('void elements', () => {
    it('returns null for <br>', () => {
      expect(extractTagName('<br>', 4)).toBeNull();
    });

    it('returns null for uppercase <BR>', () => {
      expect(extractTagName('<BR>', 4)).toBeNull();
    });

    it('returns null for mixed case <Img>', () => {
      expect(extractTagName('<Img>', 5)).toBeNull();
    });

    it('returns null for <input type="text">', () => {
      // <input type="text">  → 19 chars
      expect(extractTagName('<input type="text">', 19)).toBeNull();
    });

    it('returns null for <hr>', () => {
      expect(extractTagName('<hr>', 4)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Closing tags (should return null)
  // -------------------------------------------------------------------------

  describe('closing tags', () => {
    it('returns null for </div>', () => {
      expect(extractTagName('</div>', 6)).toBeNull();
    });

    it('returns null for </span>', () => {
      expect(extractTagName('</span>', 7)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Self-closing tags (should return null)
  // -------------------------------------------------------------------------

  describe('self-closing tags', () => {
    it('returns null for <br/>', () => {
      expect(extractTagName('<br/>', 5)).toBeNull();
    });

    it('returns null for <br /> (with space)', () => {
      expect(extractTagName('<br />', 6)).toBeNull();
    });

    it('returns null for <div/>', () => {
      expect(extractTagName('<div/>', 6)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Comments, doctype, processing instructions
  // -------------------------------------------------------------------------

  describe('comments, doctype, and processing instructions', () => {
    it('returns null for short comment <!-- >', () => {
      // <!-- >  → 6 chars: '<','!','-','-',' ','>'
      expect(extractTagName('<!-- >', 6)).toBeNull();
    });

    it('returns null for full comment <!-- comment -->', () => {
      expect(extractTagName('<!-- comment -->', 16)).toBeNull();
    });

    it('returns null for <!DOCTYPE html>', () => {
      expect(extractTagName('<!DOCTYPE html>', 15)).toBeNull();
    });

    it('returns null for lowercase <!doctype>', () => {
      expect(extractTagName('<!doctype>', 10)).toBeNull();
    });

    it('returns null for processing instruction <?xml version="1.0"?>', () => {
      expect(extractTagName('<?xml version="1.0"?>', 21)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Quote-escaped > in attributes (should still extract correctly)
  // -------------------------------------------------------------------------

  describe('quote tracking for > inside attributes', () => {
    it('extracts "div" when > appears inside double-quoted attr <div title="a>b">', () => {
      // <div title="a>b">  → 17 chars
      expect(extractTagName('<div title="a>b">', 17)).toBe('div');
    });

    it('extracts "div" when > appears inside single-quoted attr', () => {
      // <div title='a>b'>  → 17 chars
      expect(extractTagName("<div title='a>b'>", 17)).toBe('div');
    });

    it('extracts "div" when single quote appears in double-quoted attr <div title="a\'b">', () => {
      // <div title="a'b">  → 17 chars
      expect(extractTagName('<div title="a\'b">', 17)).toBe('div');
    });

    it('extracts "span" when double quote appears in single-quoted attr', () => {
      // <span data-val='a"b'>  → 21 chars
      expect(extractTagName("<span data-val='a\"b'>", 21)).toBe('span');
    });
  });

  // -------------------------------------------------------------------------
  // Invalid tag names
  // -------------------------------------------------------------------------

  describe('invalid tag names', () => {
    it('returns null for empty tag <>', () => {
      expect(extractTagName('<>', 2)).toBeNull();
    });

    it('returns null for tag starting with digit <1div>', () => {
      expect(extractTagName('<1div>', 6)).toBeNull();
    });

    it('returns null for tag starting with hyphen <-bad>', () => {
      expect(extractTagName('<-bad>', 6)).toBeNull();
    });

    it('returns null for tag starting with colon <:tag>', () => {
      expect(extractTagName('<:tag>', 6)).toBeNull();
    });

    it('returns null for tag starting with digit <3d-model>', () => {
      expect(extractTagName('<3d-model>', 10)).toBeNull();
    });

    it('returns null for minimal invalid <->', () => {
      expect(extractTagName('<->', 3)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Double > bug (regression test for spurious > in tag name)
  // -------------------------------------------------------------------------

  describe('double > regression', () => {
    it('returns null for <div>> (extra closing bracket)', () => {
      // <div>>  → 6 chars; scans back to < at 0, tagText becomes "div>"
      expect(extractTagName('<div>>', 6)).toBeNull();
    });

    it('returns null for <<div>> (double braces)', () => {
      // <<div>>  → 7 chars; scans back to second < at 1, tagText becomes "div>"
      expect(extractTagName('<<div>>', 7)).toBeNull();
    });
  });

  // -------------------------------------------------------------------------
  // Edge cases / boundary conditions
  // -------------------------------------------------------------------------

  describe('edge cases and boundary conditions', () => {
    it('returns null when cursorPos is 0', () => {
      expect(extractTagName('', 0)).toBeNull();
    });

    it('returns null when cursorPos is 1', () => {
      expect(extractTagName('<', 1)).toBeNull();
    });

    it('extracts "_custom" for tag starting with underscore', () => {
      // <_custom>  → 9 chars
      expect(extractTagName('<_custom>', 9)).toBe('_custom');
    });
  });

  // -------------------------------------------------------------------------
  // Nested tags (should return the most recently opened tag)
  // -------------------------------------------------------------------------

  describe('nested tags', () => {
    it('returns "span" when cursor is after <div><span>', () => {
      // <div><span>  → 11 chars; scans back to most recent < before >
      expect(extractTagName('<div><span>', 11)).toBe('span');
    });
  });

  // -------------------------------------------------------------------------
  // Brace depth tracking (JSX expressions)
  // -------------------------------------------------------------------------

  describe('brace depth tracking (JSX expressions)', () => {
    it('returns null for <div data-x={5 > (comparison in JSX expression)', () => {
      // <div data-x={5 >  → 16 chars, cursorPos=16
      expect(extractTagName('<div data-x={5 >', 16)).toBeNull();
    });

    it('returns "div" for <div data-x={5} > (closed brace before tag close)', () => {
      // <div data-x={5} >  → 17 chars, cursorPos=17
      expect(extractTagName('<div data-x={5} >', 17)).toBe('div');
    });

    it('returns null for <div style={{ width: width > (nested unclosed braces)', () => {
      // <div style={{ width: width >  → 28 chars, cursorPos=28
      expect(extractTagName('<div style={{ width: width >', 28)).toBeNull();
    });

    it('returns "div" for <div style={{ width: 100 }} > (closed nested braces)', () => {
      // <div style={{ width: 100 }} >  → 29 chars, cursorPos=29
      expect(extractTagName('<div style={{ width: 100 }} >', 29)).toBe('div');
    });

    it('returns null for <div onClick={a > (partial ternary in JSX expression)', () => {
      // <div onClick={a >  → 17 chars, cursorPos=17
      expect(extractTagName('<div onClick={a >', 17)).toBeNull();
    });

    it('returns "div" for <div onClick={a > b ? fn : fn2} > (closed ternary in braces)', () => {
      // <div onClick={a > b ? fn : fn2} >  → 33 chars, cursorPos=33
      expect(extractTagName('<div onClick={a > b ? fn : fn2} >', 33)).toBe('div');
    });

    it('returns null for deeply nested unclosed braces <div x={{a: {b: 1 >', () => {
      // <div x={{a: {b: 1 >  → 19 chars, cursorPos=19
      expect(extractTagName('<div x={{a: {b: 1 >', 19)).toBeNull();
    });

    it('extracts "div" when braces appear inside quoted attribute <div x=\'{foo}\'>', () => {
      // <div x='{foo}'>  → 15 chars, cursorPos=15
      // Braces inside quotes should not affect brace depth tracking
      expect(extractTagName("<div x='{foo}'>", 15)).toBe('div');
    });
  });
});
