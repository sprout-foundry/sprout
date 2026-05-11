/**
 * trailingWhitespace.test.ts — Unit tests for the trailingWhitespace extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the CM imports are mocked. We test the exported `findTrailingWhitespaceStart`
 * helper directly — it's a pure function with no CM dependencies.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { findTrailingWhitespaceStart } from './trailingWhitespace';

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

vi.mock('@codemirror/view', () => ({
  Decoration: {
    mark: vi.fn(() => ({ range: vi.fn() })),
    none: [],
    set: vi.fn(),
  },
  ViewPlugin: { fromClass: vi.fn() },
  EditorView: { baseTheme: vi.fn(() => []) },
}));
vi.mock('@codemirror/state', () => ({}));

// ── findTrailingWhitespaceStart tests ───────────────────────────────

describe('findTrailingWhitespaceStart', () => {
  // -------------------------------------------------------------------------
  // Empty / trivial inputs
  // -------------------------------------------------------------------------

  it('returns -1 for an empty string', () => {
    expect(findTrailingWhitespaceStart('')).toBe(-1);
  });

  it('returns -1 for a line with no trailing whitespace', () => {
    expect(findTrailingWhitespaceStart('hello')).toBe(-1);
  });

  it('returns -1 for a line ending with a non-whitespace character', () => {
    expect(findTrailingWhitespaceStart('hello world')).toBe(-1);
  });

  // -------------------------------------------------------------------------
  // Trailing spaces
  // -------------------------------------------------------------------------

  it('detects a single trailing space', () => {
    expect(findTrailingWhitespaceStart('hello ')).toBe(5);
  });

  it('detects multiple trailing spaces', () => {
    expect(findTrailingWhitespaceStart('hello   ')).toBe(5);
  });

  it('detects trailing spaces after code', () => {
    expect(findTrailingWhitespaceStart('const x = 1;  ')).toBe(12);
  });

  // -------------------------------------------------------------------------
  // Trailing tabs
  // -------------------------------------------------------------------------

  it('detects a single trailing tab', () => {
    expect(findTrailingWhitespaceStart('hello\t')).toBe(5);
  });

  it('detects multiple trailing tabs', () => {
    expect(findTrailingWhitespaceStart('hello\t\t\t')).toBe(5);
  });

  // -------------------------------------------------------------------------
  // Mixed trailing whitespace
  // -------------------------------------------------------------------------

  it('detects mixed trailing spaces and tabs', () => {
    expect(findTrailingWhitespaceStart('hello \t ')).toBe(5);
  });

  it('detects trailing tab followed by spaces', () => {
    expect(findTrailingWhitespaceStart('hello\t  ')).toBe(5);
  });

  // -------------------------------------------------------------------------
  // Whitespace-only lines
  // -------------------------------------------------------------------------

  it('returns 0 for a line that is all spaces', () => {
    expect(findTrailingWhitespaceStart('   ')).toBe(0);
  });

  it('returns 0 for a line that is all tabs', () => {
    expect(findTrailingWhitespaceStart('\t\t\t')).toBe(0);
  });

  it('returns 0 for a line that is mixed spaces and tabs', () => {
    expect(findTrailingWhitespaceStart(' \t ')).toBe(0);
  });

  it('returns 0 for a single space line', () => {
    expect(findTrailingWhitespaceStart(' ')).toBe(0);
  });

  it('returns 0 for a single tab line', () => {
    expect(findTrailingWhitespaceStart('\t')).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Lines with leading whitespace but no trailing whitespace
  // -------------------------------------------------------------------------

  it('returns -1 for indented code with no trailing whitespace', () => {
    expect(findTrailingWhitespaceStart('    const x = 1;')).toBe(-1);
  });

  it('detects trailing whitespace after indented code', () => {
    expect(findTrailingWhitespaceStart('    const x = 1;  ')).toBe(16);
  });

  // -------------------------------------------------------------------------
  // Edge cases
  // -------------------------------------------------------------------------

  it('handles a line that is just a newline (empty string after trim)', () => {
    // This represents an empty line in a document
    expect(findTrailingWhitespaceStart('')).toBe(-1);
  });

  it('handles a single character line with no trailing whitespace', () => {
    expect(findTrailingWhitespaceStart('a')).toBe(-1);
  });

  it('handles a single character line with trailing whitespace', () => {
    expect(findTrailingWhitespaceStart('a ')).toBe(1);
  });

  it('handles a line where all characters are whitespace including tabs', () => {
    expect(findTrailingWhitespaceStart(' \t \t ')).toBe(0);
  });

  it('distinguishes internal spaces from trailing spaces', () => {
    // "hello world" has an internal space but no trailing whitespace
    expect(findTrailingWhitespaceStart('hello world')).toBe(-1);
  });

  it('finds trailing whitespace after internal spaces', () => {
    expect(findTrailingWhitespaceStart('hello world  ')).toBe(11);
  });

  // -------------------------------------------------------------------------
  // Carriage return edge cases (Windows line endings)
  // -------------------------------------------------------------------------

  it('treats carriage return as trailing whitespace', () => {
    // A line with \r before newline (CRLF without the LF, as CM6 strips LF)
    expect(findTrailingWhitespaceStart('hello\r')).toBe(5);
  });

  it('treats mixed \\r and spaces as trailing whitespace', () => {
    expect(findTrailingWhitespaceStart('hello \r ')).toBe(5);
  });

  it('does not treat \\r in the middle of content as trailing whitespace', () => {
    // If somehow \r appears mid-line, it's not trailing
    // Actually, \r IS whitespace, so it would be highlighted
    // This test verifies the behavior: \r is always treated as whitespace
    expect(findTrailingWhitespaceStart('he\rllo')).toBe(-1);
  });
});
