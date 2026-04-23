/**
 * whitespaceRendering.test.ts — Unit tests for the whitespaceRendering extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the CM imports are mocked. We test the exported `findWhitespacePositions`
 * helper directly — it's a pure function with no CM dependencies.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { findWhitespacePositions, type WhitespaceRenderingMode } from './whitespaceRendering';

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/view', () => ({
  Decoration: {
    mark: jest.fn(() => ({ range: jest.fn() })),
    replace: jest.fn(() => ({ range: jest.fn() })),
    none: [],
    set: jest.fn(),
    widget: jest.fn(),
  },
  ViewPlugin: { fromClass: jest.fn() },
  EditorView: { baseTheme: jest.fn(() => []) },
  WidgetType: class {},
}));

jest.mock('@codemirror/state', () => ({}));

// ── findWhitespacePositions tests ───────────────────────────────

describe('findWhitespacePositions', () => {
  // -------------------------------------------------------------------------
  // Empty / trivial inputs
  // -------------------------------------------------------------------------

  it('returns empty array for empty string', () => {
    const result = findWhitespacePositions('', 'all');
    expect(result).toEqual([]);
  });

  it('returns empty array for "none" mode', () => {
    const result = findWhitespacePositions('hello world', 'none');
    expect(result).toEqual([]);
  });

  it('returns empty array for no whitespace in any mode', () => {
    expect(findWhitespacePositions('helloworld', 'all')).toEqual([]);
    expect(findWhitespacePositions('helloworld', 'boundary')).toEqual([]);
    expect(findWhitespacePositions('helloworld', 'none')).toEqual([]);
  });

  // -------------------------------------------------------------------------
  // Tab detection
  // -------------------------------------------------------------------------

  it('detects single tab in "all" mode', () => {
    const result = findWhitespacePositions('\t', 'all');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 0, to: 1, type: 'tab' });
  });

  it('detects single tab in "boundary" mode', () => {
    const result = findWhitespacePositions('\t', 'boundary');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 0, to: 1, type: 'tab' });
  });

  it('detects tab in "none" mode', () => {
    const result = findWhitespacePositions('\t', 'none');
    expect(result).toEqual([]);
  });

  it('detects multiple tabs in "all" mode', () => {
    const result = findWhitespacePositions('\t\t\t', 'all');
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('tab');
    });
  });

  it('detects multiple tabs in "boundary" mode', () => {
    const result = findWhitespacePositions('\t\t\t', 'boundary');
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('tab');
    });
  });

  // -------------------------------------------------------------------------
  // Space detection in "all" mode
  // -------------------------------------------------------------------------

  it('detects single space in "all" mode', () => {
    const result = findWhitespacePositions(' ', 'all');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 0, to: 1, type: 'space' });
  });

  it('detects multiple spaces in "all" mode', () => {
    const result = findWhitespacePositions('   ', 'all');
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('space');
    });
  });

  it('detects internal spaces in "all" mode', () => {
    const result = findWhitespacePositions('hello world', 'all');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 5, to: 6, type: 'space' });
  });

  it('detects leading spaces in "all" mode', () => {
    const result = findWhitespacePositions('   hello', 'all');
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('space');
    });
  });

  // -------------------------------------------------------------------------
  // Space detection in "boundary" mode
  // -------------------------------------------------------------------------

  it('returns empty for internal space in "boundary" mode', () => {
    const result = findWhitespacePositions('hello world', 'boundary');
    // Internal space after "hello" is not trailing, so it should not be rendered
    expect(result).toEqual([]);
  });

  it('returns empty for leading spaces in "boundary" mode', () => {
    const result = findWhitespacePositions('   hello', 'boundary');
    // Leading spaces are not trailing in this context
    expect(result).toEqual([]);
  });

  it('detects trailing spaces in "boundary" mode', () => {
    const result = findWhitespacePositions('hello   ', 'boundary');
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('trailing');
    });
  });

  it('detects trailing space after code in "boundary" mode', () => {
    const result = findWhitespacePositions('const x = 1;  ', 'boundary');
    expect(result).toHaveLength(2);
    result.forEach((r) => {
      expect(r.type).toBe('trailing');
    });
  });

  // -------------------------------------------------------------------------
  // Mixed content
  // -------------------------------------------------------------------------

  it('detects tab in mixed content in "all" mode', () => {
    const result = findWhitespacePositions('\thello', 'all');
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe('tab');
  });

  it('detects both tab and trailing spaces in "boundary" mode', () => {
    const result = findWhitespacePositions('\t  ', 'boundary');
    // Tab is rendered, but trailing spaces should also be rendered because they ARE trailing
    expect(result).toHaveLength(3);
    expect(result[0].type).toBe('tab');
    expect(result[1].type).toBe('trailing');
    expect(result[2].type).toBe('trailing');
  });

  it('detects leading spaces, tab, and trailing spaces in "all" mode', () => {
    const result = findWhitespacePositions('\t\thello world  \t\t', 'all');
    // Two leading tabs + internal space + two trailing spaces + two trailing tabs
    expect(result.length).toBeGreaterThan(0);
    // First two should be tabs (leading)
    expect(result[0].type).toBe('tab');
    expect(result[1].type).toBe('tab');
    // Last ones should be tabs (trailing)
    expect(result[result.length - 1].type).toBe('tab');
    expect(result[result.length - 2].type).toBe('tab');
  });

  // -------------------------------------------------------------------------
  // Whitespace-only lines
  // -------------------------------------------------------------------------

  it('treats line with only spaces as all trailing in "boundary" mode', () => {
    const result = findWhitespacePositions('   ', 'boundary');
    // A line of only spaces - all are trailing (no content to be trailing FROM)
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('trailing');
    });
  });

  it('treats line with only spaces as all spaces in "all" mode', () => {
    const result = findWhitespacePositions('   ', 'all');
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('space');
    });
  });

  it('treats line with only tabs as all trailing in "boundary" mode', () => {
    const result = findWhitespacePositions('\t\t\t', 'boundary');
    // Tabs are always rendered in boundary mode
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('tab');
    });
  });

  // -------------------------------------------------------------------------
  // Edge cases
  // -------------------------------------------------------------------------

  it('handles carriage return as whitespace', () => {
    const result = findWhitespacePositions('hello\r', 'all');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 5, to: 6, type: 'space' });
  });

  it('handles mixed spaces and carriage returns in boundary mode', () => {
    const result = findWhitespacePositions('hello \r ', 'boundary');
    // The \r at position 6 is whitespace too, so all three (\r, space, space) after "hello" are trailing
    // This matches VS Code behavior - any whitespace after content is "trailing"
    expect(result).toHaveLength(3);
    result.forEach((r) => {
      expect(r.type).toBe('trailing');
    });
  });

  it('handles code followed by mixed trailing whitespace in boundary mode', () => {
    const result = findWhitespacePositions('hello  \t', 'boundary');
    // Two trailing spaces + trailing tab should all render
    expect(result).toHaveLength(3);
    expect(result[0].type).toBe('trailing');
    expect(result[1].type).toBe('trailing');
    expect(result[2].type).toBe('tab');
  });

  it('handles empty string in all modes', () => {
    expect(findWhitespacePositions('', 'none')).toEqual([]);
    expect(findWhitespacePositions('', 'boundary')).toEqual([]);
    expect(findWhitespacePositions('', 'all')).toEqual([]);
  });

  it('handles single character with no whitespace', () => {
    expect(findWhitespacePositions('a', 'all')).toEqual([]);
    expect(findWhitespacePositions('a', 'boundary')).toEqual([]);
  });

  it('handles single character with trailing space', () => {
    const result = findWhitespacePositions('a ', 'all');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 1, to: 2, type: 'space' });
  });

  it('handles single character with trailing space in boundary mode', () => {
    const result = findWhitespacePositions('a ', 'boundary');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 1, to: 2, type: 'trailing' });
  });

  // -------------------------------------------------------------------------
  // Edge cases (additional)
  // -------------------------------------------------------------------------

  it('handles mixed whitespace-only line in boundary mode', () => {
    const result = findWhitespacePositions('\t  \t', 'boundary');
    // '\t  \t' is all whitespace - tabs rendered as 'tab', spaces as 'trailing'
    expect(result).toHaveLength(4);
    // First tab at position 0
    expect(result[0].type).toBe('tab');
    expect(result[0].from).toBe(0);
    expect(result[0].to).toBe(1);
    // Two spaces at positions 1-2 are trailing
    expect(result[1].type).toBe('trailing');
    expect(result[1].from).toBe(1);
    expect(result[1].to).toBe(2);
    expect(result[2].type).toBe('trailing');
    expect(result[2].from).toBe(2);
    expect(result[2].to).toBe(3);
    // Last tab at position 3
    expect(result[3].type).toBe('tab');
    expect(result[3].from).toBe(3);
    expect(result[3].to).toBe(4);
  });

  it('handles mixed whitespace-only line in all mode', () => {
    const result = findWhitespacePositions(' \t ', 'all');
    // ' \t ' - space at 0, tab at 1, space at 2
    expect(result).toHaveLength(3);
    expect(result[0].type).toBe('space');
    expect(result[0].from).toBe(0);
    expect(result[0].to).toBe(1);
    expect(result[1].type).toBe('tab');
    expect(result[1].from).toBe(1);
    expect(result[1].to).toBe(2);
    expect(result[2].type).toBe('space');
    expect(result[2].from).toBe(2);
    expect(result[2].to).toBe(3);
  });

  it('returns empty array for carriage return in none mode', () => {
    const result = findWhitespacePositions('hello\r', 'none');
    expect(result).toEqual([]);
  });

  it('handles code followed by tab in boundary mode', () => {
    const result = findWhitespacePositions('hello\t', 'boundary');
    // Tab at position 5 is trailing after "hello"
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 5, to: 6, type: 'tab' });
  });

  it('ignores leading spaces but detects trailing spaces in boundary mode', () => {
    const result = findWhitespacePositions('  hello  ', 'boundary');
    // Leading spaces (positions 0-1) are not trailing - they come BEFORE content
    // Trailing spaces (positions 7-8) are trailing - they come AFTER content
    expect(result).toHaveLength(2);
    result.forEach((r) => {
      expect(r.type).toBe('trailing');
    });
    expect(result[0].from).toBe(7);
    expect(result[0].to).toBe(8);
    expect(result[1].from).toBe(8);
    expect(result[1].to).toBe(9);
  });
});