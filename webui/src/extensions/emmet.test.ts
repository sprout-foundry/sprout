/**
 * emmet.test.ts — Unit tests for the emmet extension.
 *
 * Since CodeMirror 6 modules and @emmetio/codemirror6-plugin use ESM
 * and cannot load in Jest 27.x, the CM and Emmet imports are mocked.
 * We test the exported public functions with mocked dependencies.
 */

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/state', () => ({
  Extension: {},
  Compartment: jest.fn().mockImplementation(() => ({
    reconfigure: jest.fn(() => ({ type: 'StateEffect' })),
  })),
}));

jest.mock('@codemirror/view', () => ({
  keymap: {
    of: jest.fn(() => ({ type: 'KeymapExtension' })),
  },
  EditorView: {},
}));

// ── Mock @emmetio/codemirror6-plugin (ESM-only) ────────────────────

jest.mock('@emmetio/codemirror6-plugin', () => ({
  abbreviationTracker: jest.fn(() => ({ type: 'AbbreviationTracker' })),
  wrapWithAbbreviation: jest.fn(() => ({ type: 'WrapWithAbbreviation' })),
  expandAbbreviation: jest.fn(() => ({ type: 'ExpandAbbreviation' })),
  EmmetKnownSyntax: {
    html: 'html',
    css: 'css',
    scss: 'scss',
    sass: 'sass',
    jsx: 'jsx',
    tsx: 'tsx',
  },
}));

// ── Module under test (Jest hoists mocks above imports) ─────────────

import {
  buildEmmetExtensions,
  createEmmetCompartment,
  getInitialEmmetExtensions,
  reconfigureEmmet,
  isEmmetLanguage,
} from './emmet';

// ── buildEmmetExtensions tests ─────────────────────────────────────

describe('buildEmmetExtensions', () => {
  // -------------------------------------------------------------------------
  // Empty / null inputs
  // -------------------------------------------------------------------------

  it('returns empty array for null language ID', () => {
    const result = buildEmmetExtensions(null);
    expect(result).toEqual([]);
  });

  it('returns empty array for undefined language ID', () => {
    const result = buildEmmetExtensions(undefined);
    expect(result).toEqual([]);
  });

  it('returns empty array for empty string language ID', () => {
    const result = buildEmmetExtensions('');
    expect(result).toEqual([]);
  });

  // -------------------------------------------------------------------------
  // Non-Emmet languages
  // -------------------------------------------------------------------------

  it('returns empty array for python', () => {
    const result = buildEmmetExtensions('python');
    expect(result).toEqual([]);
  });

  it('returns empty array for go', () => {
    const result = buildEmmetExtensions('go');
    expect(result).toEqual([]);
  });

  it('returns empty array for javascript (non-JSX)', () => {
    const result = buildEmmetExtensions('javascript');
    expect(result).toEqual([]);
  });

  it('returns empty array for typescript (non-JSX)', () => {
    const result = buildEmmetExtensions('typescript');
    expect(result).toEqual([]);
  });

  it('returns empty array for json', () => {
    const result = buildEmmetExtensions('json');
    expect(result).toEqual([]);
  });

  it('returns empty array for markdown', () => {
    const result = buildEmmetExtensions('markdown');
    expect(result).toEqual([]);
  });

  it('returns empty array for yaml', () => {
    const result = buildEmmetExtensions('yaml');
    expect(result).toEqual([]);
  });

  it('returns empty array for rust', () => {
    const result = buildEmmetExtensions('rust');
    expect(result).toEqual([]);
  });

  // -------------------------------------------------------------------------
  // Emmet-supported languages
  // -------------------------------------------------------------------------

  it('returns non-empty array for html', () => {
    const result = buildEmmetExtensions('html');
    expect(result).toHaveLength(3);
    expect(result[0]).toHaveProperty('type', 'AbbreviationTracker');
    expect(result[1]).toHaveProperty('type', 'WrapWithAbbreviation');
    expect(result[2]).toHaveProperty('type', 'KeymapExtension');
  });

  it('returns non-empty array for css', () => {
    const result = buildEmmetExtensions('css');
    expect(result).toHaveLength(3);
    expect(result[0]).toHaveProperty('type', 'AbbreviationTracker');
    expect(result[1]).toHaveProperty('type', 'WrapWithAbbreviation');
    expect(result[2]).toHaveProperty('type', 'KeymapExtension');
  });

  it('returns non-empty array for scss', () => {
    const result = buildEmmetExtensions('scss');
    expect(result).toHaveLength(3);
    expect(result[0]).toHaveProperty('type', 'AbbreviationTracker');
    expect(result[1]).toHaveProperty('type', 'WrapWithAbbreviation');
    expect(result[2]).toHaveProperty('type', 'KeymapExtension');
  });

  it('returns non-empty array for sass', () => {
    const result = buildEmmetExtensions('sass');
    expect(result).toHaveLength(3);
    expect(result[0]).toHaveProperty('type', 'AbbreviationTracker');
    expect(result[1]).toHaveProperty('type', 'WrapWithAbbreviation');
    expect(result[2]).toHaveProperty('type', 'KeymapExtension');
  });

  it('returns non-empty array for javascript-jsx', () => {
    const result = buildEmmetExtensions('javascript-jsx');
    expect(result).toHaveLength(3);
    expect(result[0]).toHaveProperty('type', 'AbbreviationTracker');
    expect(result[1]).toHaveProperty('type', 'WrapWithAbbreviation');
    expect(result[2]).toHaveProperty('type', 'KeymapExtension');
  });

  it('returns non-empty array for typescript-jsx', () => {
    const result = buildEmmetExtensions('typescript-jsx');
    expect(result).toHaveLength(3);
    expect(result[0]).toHaveProperty('type', 'AbbreviationTracker');
    expect(result[1]).toHaveProperty('type', 'WrapWithAbbreviation');
    expect(result[2]).toHaveProperty('type', 'KeymapExtension');
  });

  // -------------------------------------------------------------------------
  // Case sensitivity
  // -------------------------------------------------------------------------

  it('returns empty array for HTML (uppercase)', () => {
    const result = buildEmmetExtensions('HTML');
    expect(result).toEqual([]);
  });

  it('returns empty array for CSS (uppercase)', () => {
    const result = buildEmmetExtensions('CSS');
    expect(result).toEqual([]);
  });

  it('returns non-empty array for html (lowercase)', () => {
    const result = buildEmmetExtensions('html');
    expect(result).toHaveLength(3);
  });
});

// ── createEmmetCompartment tests ─────────────────────────────────────

describe('createEmmetCompartment', () => {
  it('returns a Compartment instance', () => {
    const compartment = createEmmetCompartment();
    expect(compartment).toBeDefined();
    expect(compartment).toHaveProperty('reconfigure');
    expect(typeof compartment.reconfigure).toBe('function');
  });

  it('returns a new Compartment instance on each call', () => {
    const compartment1 = createEmmetCompartment();
    const compartment2 = createEmmetCompartment();
    expect(compartment1).not.toBe(compartment2);
  });
});

// ── getInitialEmmetExtensions tests ───────────────────────────────────

describe('getInitialEmmetExtensions', () => {
  it('delegates to buildEmmetExtensions for null', () => {
    const result = getInitialEmmetExtensions(null);
    expect(result).toEqual([]);
  });

  it('delegates to buildEmmetExtensions for undefined', () => {
    const result = getInitialEmmetExtensions(undefined);
    expect(result).toEqual([]);
  });

  it('delegates to buildEmmetExtensions for non-emmet language', () => {
    const result = getInitialEmmetExtensions('python');
    expect(result).toEqual([]);
  });

  it('delegates to buildEmmetExtensions for html', () => {
    const result = getInitialEmmetExtensions('html');
    expect(result).toHaveLength(3);
  });

  it('delegates to buildEmmetExtensions for javascript-jsx', () => {
    const result = getInitialEmmetExtensions('javascript-jsx');
    expect(result).toHaveLength(3);
  });

  it('returns same result as buildEmmetExtensions for css', () => {
    const directResult = buildEmmetExtensions('css');
    const result = getInitialEmmetExtensions('css');
    expect(result).toEqual(directResult);
  });
});

// ── reconfigureEmmet tests ─────────────────────────────────────────

describe('reconfigureEmmet', () => {
  it('dispatches a reconfigure effect on the view', () => {
    const compartment = createEmmetCompartment();
    const view = {
      dispatch: jest.fn(),
    };

    reconfigureEmmet(compartment, view as any, 'html');

    expect(view.dispatch).toHaveBeenCalledTimes(1);
    expect(view.dispatch).toHaveBeenCalledWith({
      effects: expect.objectContaining({
        type: 'StateEffect',
      }),
    });
  });

  it('dispatches with empty extensions for null language', () => {
    const compartment = createEmmetCompartment();
    const view = {
      dispatch: jest.fn(),
    };

    reconfigureEmmet(compartment, view as any, null);

    expect(view.dispatch).toHaveBeenCalledTimes(1);
    const dispatchCall = (view.dispatch as jest.Mock).mock.calls[0][0];
    expect(dispatchCall).toHaveProperty('effects');
  });

  it('dispatches with empty extensions for non-emmet language', () => {
    const compartment = createEmmetCompartment();
    const view = {
      dispatch: jest.fn(),
    };

    reconfigureEmmet(compartment, view as any, 'python');

    expect(view.dispatch).toHaveBeenCalledTimes(1);
  });

  it('dispatches with non-empty extensions for html', () => {
    const compartment = createEmmetCompartment();
    const view = {
      dispatch: jest.fn(),
    };

    reconfigureEmmet(compartment, view as any, 'html');

    expect(view.dispatch).toHaveBeenCalledTimes(1);
    const dispatchCall = (view.dispatch as jest.Mock).mock.calls[0][0];
    expect(dispatchCall).toHaveProperty('effects');
  });

  it('handles language change from html to python', () => {
    const compartment = createEmmetCompartment();
    const view = {
      dispatch: jest.fn(),
    };

    // First configure with html
    reconfigureEmmet(compartment, view as any, 'html');
    expect(view.dispatch).toHaveBeenCalledTimes(1);

    // Then reconfigure with python
    reconfigureEmmet(compartment, view as any, 'python');
    expect(view.dispatch).toHaveBeenCalledTimes(2);
  });

  it('handles language change from javascript to javascript-jsx', () => {
    const compartment = createEmmetCompartment();
    const view = {
      dispatch: jest.fn(),
    };

    // First configure with javascript (no emmet)
    reconfigureEmmet(compartment, view as any, 'javascript');
    expect(view.dispatch).toHaveBeenCalledTimes(1);

    // Then reconfigure with javascript-jsx (has emmet)
    reconfigureEmmet(compartment, view as any, 'javascript-jsx');
    expect(view.dispatch).toHaveBeenCalledTimes(2);
  });
});

// ── isEmmetLanguage tests ───────────────────────────────────────────

describe('isEmmetLanguage', () => {
  // -------------------------------------------------------------------------
  // Null/undefined/empty inputs
  // -------------------------------------------------------------------------

  it('returns false for null', () => {
    expect(isEmmetLanguage(null)).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isEmmetLanguage(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isEmmetLanguage('')).toBe(false);
  });

  // -------------------------------------------------------------------------
  // Emmet-supported languages
  // -------------------------------------------------------------------------

  it('returns true for html', () => {
    expect(isEmmetLanguage('html')).toBe(true);
  });

  it('returns true for css', () => {
    expect(isEmmetLanguage('css')).toBe(true);
  });

  it('returns true for scss', () => {
    expect(isEmmetLanguage('scss')).toBe(true);
  });

  it('returns true for sass', () => {
    expect(isEmmetLanguage('sass')).toBe(true);
  });

  it('returns true for javascript-jsx', () => {
    expect(isEmmetLanguage('javascript-jsx')).toBe(true);
  });

  it('returns true for typescript-jsx', () => {
    expect(isEmmetLanguage('typescript-jsx')).toBe(true);
  });

  // -------------------------------------------------------------------------
  // Non-Emmet languages
  // -------------------------------------------------------------------------

  it('returns false for python', () => {
    expect(isEmmetLanguage('python')).toBe(false);
  });

  it('returns false for go', () => {
    expect(isEmmetLanguage('go')).toBe(false);
  });

  it('returns false for javascript', () => {
    expect(isEmmetLanguage('javascript')).toBe(false);
  });

  it('returns false for typescript', () => {
    expect(isEmmetLanguage('typescript')).toBe(false);
  });

  it('returns false for json', () => {
    expect(isEmmetLanguage('json')).toBe(false);
  });

  it('returns false for markdown', () => {
    expect(isEmmetLanguage('markdown')).toBe(false);
  });

  it('returns false for yaml', () => {
    expect(isEmmetLanguage('yaml')).toBe(false);
  });

  it('returns false for rust', () => {
    expect(isEmmetLanguage('rust')).toBe(false);
  });

  it('returns false for c', () => {
    expect(isEmmetLanguage('c')).toBe(false);
  });

  it('returns false for cpp', () => {
    expect(isEmmetLanguage('cpp')).toBe(false);
  });

  it('returns false for java', () => {
    expect(isEmmetLanguage('java')).toBe(false);
  });

  it('returns false for php', () => {
    expect(isEmmetLanguage('php')).toBe(false);
  });

  it('returns false for ruby', () => {
    expect(isEmmetLanguage('ruby')).toBe(false);
  });

  // -------------------------------------------------------------------------
  // Case sensitivity
  // -------------------------------------------------------------------------

  it('returns false for HTML (uppercase)', () => {
    expect(isEmmetLanguage('HTML')).toBe(false);
  });

  it('returns false for CSS (uppercase)', () => {
    expect(isEmmetLanguage('CSS')).toBe(false);
  });

  it('returns true for html (lowercase)', () => {
    expect(isEmmetLanguage('html')).toBe(true);
  });

  it('returns true for css (lowercase)', () => {
    expect(isEmmetLanguage('css')).toBe(true);
  });
});
