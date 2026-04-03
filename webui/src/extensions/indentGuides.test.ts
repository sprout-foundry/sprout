/**
 * indentGuides.test.ts — Unit tests for the indentGuides extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the three CM imports are mocked.  We test the exported `measureIndent`
 * helper directly — it's a pure function with no CM dependencies.
 */

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────
jest.mock('@codemirror/view', () => ({
  WidgetType: class {},
  Decoration: { widget: jest.fn(), none: [], set: jest.fn() },
  ViewPlugin: { fromClass: jest.fn() },
  EditorView: { baseTheme: jest.fn(() => []) },
}));
jest.mock('@codemirror/language', () => ({
  getIndentUnit: jest.fn(),
}));
jest.mock('@codemirror/state', () => ({}));

// Module under test — now safe to import because CM deps are mocked.
import { measureIndent } from './indentGuides';

// ── measureIndent tests ────────────────────────────────────────────

describe('measureIndent', () => {
  // -------------------------------------------------------------------------
  // Empty / trivial inputs
  // -------------------------------------------------------------------------

  it('returns 0 for an empty string', () => {
    expect(measureIndent('', 4)).toBe(0);
  });

  it('returns 0 for a string with no leading whitespace', () => {
    expect(measureIndent('hello', 4)).toBe(0);
  });

  it('returns 0 for a string starting with a non-whitespace character', () => {
    expect(measureIndent('x  ', 2)).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Pure spaces
  // -------------------------------------------------------------------------

  it('counts pure spaces with tabSize 2', () => {
    expect(measureIndent('    ', 2)).toBe(4); // 4 spaces → 4 visual cols
  });

  it('counts pure spaces with tabSize 4', () => {
    expect(measureIndent('        ', 4)).toBe(8); // 8 spaces → 8 visual cols
  });

  it('counts a single space', () => {
    expect(measureIndent(' ', 4)).toBe(1);
  });

  it('stops counting at the first non-space character', () => {
    expect(measureIndent('  hello', 4)).toBe(2);
  });

  it('handles partial indent (fewer spaces than one tab stop)', () => {
    expect(measureIndent(' ', 4)).toBe(1);
    expect(measureIndent('   ', 4)).toBe(3);
  });

  // -------------------------------------------------------------------------
  // Pure tabs
  // -------------------------------------------------------------------------

  it('counts a single tab as tabSize columns', () => {
    expect(measureIndent('\t', 2)).toBe(2);
    expect(measureIndent('\t', 4)).toBe(4);
  });

  it('counts a single tab at non-zero column (via preceding spaces)', () => {
    // 1 space + tab with tabSize=4: space at col 1, then tab jumps to col 4
    expect(measureIndent(' \t', 4)).toBe(4);
  });

  it('counts multiple tabs with tabSize 2', () => {
    expect(measureIndent('\t\t', 2)).toBe(4); // 2 × 2 = 4
  });

  it('counts multiple tabs with tabSize 4', () => {
    expect(measureIndent('\t\t', 4)).toBe(8); // 2 × 4 = 8
  });

  it('counts three tabs with tabSize 4', () => {
    expect(measureIndent('\t\t\t', 4)).toBe(12);
  });

  it('stops counting at the first non-tab after initial tabs', () => {
    expect(measureIndent('\t\tx', 4)).toBe(8);
  });

  // -------------------------------------------------------------------------
  // Mixed tabs and spaces
  // -------------------------------------------------------------------------

  it('handles tab + spaces: tabSize 4, tab at col 0 then 2 spaces', () => {
    // Tab jumps 0 → 4, then 2 spaces: 4+2 = 6
    expect(measureIndent('\t  ', 4)).toBe(6);
  });

  it('handles tab + spaces: tabSize 2, tab at col 0 then 2 spaces', () => {
    // Tab jumps 0 → 2, then 2 spaces: 2+2 = 4
    expect(measureIndent('\t  ', 2)).toBe(4);
  });

  it('handles spaces then tab: tabSize 4, 2 spaces then tab', () => {
    // 2 spaces → col 2, tab from col 2 jumps to col 4
    expect(measureIndent('  \t', 4)).toBe(4);
  });

  it('handles spaces then tab: tabSize 4, 3 spaces then tab', () => {
    // 3 spaces → col 3, tab from col 3 jumps to col 4 (just 1 column advance)
    expect(measureIndent('   \t', 4)).toBe(4);
  });

  it('handles complex mixed: 1 space + tab + 3 spaces, tabSize 4', () => {
    // 1 space → col 1, tab from col 1 → col 4, then 3 spaces → col 7
    expect(measureIndent(' \t   ', 4)).toBe(7);
  });

  it('handles multiple tabs with spaces in between: tabSize 4', () => {
    // Tab 0→4, 2 spaces → col 6, tab 6→8
    expect(measureIndent('\t  \t', 4)).toBe(8);
  });

  it('stops at first non-whitespace in mixed indent', () => {
    // Tab 0→4, 2 spaces → col 6, then 'x' stops
    expect(measureIndent('\t  x', 4)).toBe(6);
  });

  // -------------------------------------------------------------------------
  // Tab stop alignment edge cases
  // -------------------------------------------------------------------------

  it('handles tab at exact tab stop boundary (no advance)', () => {
    // Tab from col 0 to col 2 (tabSize=2), then another tab from col 2 to col 4
    expect(measureIndent('\t\t', 2)).toBe(4);
  });

  it('handles tab one column before next stop', () => {
    // Tab from col 3 → col 4 (tabSize=4)
    expect(measureIndent('   \t', 4)).toBe(4);
  });

  it('properly aligns tab with tabSize 8', () => {
    // Tab from col 0 → 8
    expect(measureIndent('\t', 8)).toBe(8);
    // 3 spaces → col 3, tab → col 8
    expect(measureIndent('   \t', 8)).toBe(8);
    // 7 spaces → col 7, tab → col 8
    expect(measureIndent('       \t', 8)).toBe(8);
    // Tab 0→8, then tab 8→16
    expect(measureIndent('\t\t', 8)).toBe(16);
  });

  // -------------------------------------------------------------------------
  // Other whitespace
  // -------------------------------------------------------------------------

  it('treats only spaces and tabs as indent — newline stops scanning', () => {
    expect(measureIndent('  \n  ', 4)).toBe(2);
  });

  it('treats only spaces and tabs as indent — other chars stop scanning', () => {
    expect(measureIndent('  // comment', 4)).toBe(2);
    expect(measureIndent('\t\tdef foo():', 4)).toBe(8);
  });

  // -------------------------------------------------------------------------
  // Boundary/odd tabSize values
  // -------------------------------------------------------------------------

  it('handles tabSize of 1 — every space is an indent unit', () => {
    expect(measureIndent('   ', 1)).toBe(3);
    expect(measureIndent('\t', 1)).toBe(1);
  });

  it('handles large tabSize', () => {
    expect(measureIndent('        ', 8)).toBe(8);
    expect(measureIndent('\t', 8)).toBe(8);
    // 3 spaces don't reach the first 8-col boundary
    expect(measureIndent('   hello', 8)).toBe(3);
  });

  it('returns 0 for tabSize of 0', () => {
    expect(measureIndent('    ', 0)).toBe(0);
    expect(measureIndent('\t\t', 0)).toBe(0);
    expect(measureIndent('\t   ', 0)).toBe(0);
  });

  it('returns 0 for negative tabSize', () => {
    expect(measureIndent('    ', -1)).toBe(0);
    expect(measureIndent('\t', -4)).toBe(0);
  });
});
