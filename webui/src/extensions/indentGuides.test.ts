/**
 * indentGuides.test.ts — Unit tests for the indentGuides extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the three CM imports are mocked.  We test the exported `measureIndent`
 * helper directly — it's a pure function with no CM dependencies.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { measureIndent, computeGuidePositions } from './indentGuides';

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

// ── computeGuidePositions tests ────────────────────────────────────

describe('computeGuidePositions', () => {
  // -------------------------------------------------------------------------
  // Empty / trivial inputs
  // -------------------------------------------------------------------------

  it('returns [] for an empty string', () => {
    expect(computeGuidePositions('', 4)).toEqual([]);
  });

  it('returns [] for a string with no leading whitespace', () => {
    expect(computeGuidePositions('hello', 4)).toEqual([]);
  });

  it('returns [] for a string starting with a non-whitespace character', () => {
    expect(computeGuidePositions('x  ', 2)).toEqual([]);
  });

  // -------------------------------------------------------------------------
  // Pure spaces
  // -------------------------------------------------------------------------

  it('returns [4] for exactly one indent unit of spaces (tabSize=4)', () => {
    expect(computeGuidePositions('    ', 4)).toEqual([4]);
  });

  it('returns [4, 8] for exactly two indent units of spaces (tabSize=4)', () => {
    expect(computeGuidePositions('        ', 4)).toEqual([4, 8]);
  });

  it('returns [] for partial indent (less than one unit) with code', () => {
    expect(computeGuidePositions('   code', 4)).toEqual([]);
  });

  it('returns [4, 8] for more than one indent unit with code (tabSize=4)', () => {
    expect(computeGuidePositions('        code', 4)).toEqual([4, 8]);
  });

  it('returns [3] for exact unit with odd tabSize=3', () => {
    expect(computeGuidePositions('   code', 3)).toEqual([3]);
  });

  it('returns [3, 6] for two units with tabSize=3', () => {
    expect(computeGuidePositions('      code', 3)).toEqual([3, 6]);
  });

  it('returns [2] for exact unit with tabSize=2', () => {
    expect(computeGuidePositions('  code', 2)).toEqual([2]);
  });

  it('returns [2, 4, 6] for three units with tabSize=2', () => {
    expect(computeGuidePositions('      code', 2)).toEqual([2, 4, 6]);
  });

  // -------------------------------------------------------------------------
  // Pure tabs
  // -------------------------------------------------------------------------

  it('returns [1] for one tab (tabSize=4)', () => {
    expect(computeGuidePositions('\tcode', 4)).toEqual([1]);
  });

  it('returns [1, 2] for two tabs (tabSize=4)', () => {
    expect(computeGuidePositions('\t\tcode', 4)).toEqual([1, 2]);
  });

  it('returns [1, 2, 3] for three tabs (tabSize=4)', () => {
    expect(computeGuidePositions('\t\t\tcode', 4)).toEqual([1, 2, 3]);
  });

  it('returns [2] for tab at non-zero column (1 space then tab, tabSize=4)', () => {
    // 1 space → col 1, tab jumps to col 4, boundary at col 4
    expect(computeGuidePositions(' \tcode', 4)).toEqual([2]);
  });

  it('returns [2, 3] for tabs at non-zero columns (2 spaces, 2 tabs, tabSize=4)', () => {
    // 2 spaces → col 2, tab→col4 (boundary, index 3), tab→col8 (boundary, index 4)
    expect(computeGuidePositions('  \t\tcode', 4)).toEqual([3, 4]);
  });

  // -------------------------------------------------------------------------
  // Mixed spaces and tabs
  // -------------------------------------------------------------------------

  it('handles spaces then tab: "  \\tcode" tabSize=4 → [3]', () => {
    // 2 spaces → col2, tab jumps to col4 (boundary), position after tab = index 3
    expect(computeGuidePositions('  \tcode', 4)).toEqual([3]);
  });

  it('handles 3 spaces then tab: "   \\tcode" tabSize=4 → [4]', () => {
    // 3 spaces → col3, tab jumps to col4 (boundary), position after tab = index 4
    expect(computeGuidePositions('   \tcode', 4)).toEqual([4]);
  });

  it('handles tab then spaces: "\\t  code" tabSize=4 → [1] (only 6 cols, one unit)', () => {
    // tab → col4 (boundary at index 1), 2 spaces → col6 (no second boundary)
    // measureIndent = 6, numGuides = floor(6/4) = 1
    expect(computeGuidePositions('\t  code', 4)).toEqual([1]);
  });

  it('handles space + tab + spaces: " \\t   code" tabSize=4 returns [2]', () => {
    // 1 space → col1, tab → col4 (boundary, index 2), 3 spaces → col7
    // No second boundary (col7 < 8), so only [2]
    expect(computeGuidePositions(' \t   code', 4)).toEqual([2]);
  });

  it('handles tab + 4 spaces: "\\t    code" tabSize=4 → [1, 5]', () => {
    // tab → col4 (boundary at index 1), 4 spaces → col8 (boundary at index 5)
    expect(computeGuidePositions('\t    code', 4)).toEqual([1, 5]);
  });

  it('handles complex mixed: 1 space + 3 tabs, tabSize=2', () => {
    // 1 space → col1, tab → col2 (boundary, index 2), tab → col4 (boundary, index 3), tab → col6 (boundary, index 4)
    expect(computeGuidePositions(' \t\t\tcode', 2)).toEqual([2, 3, 4]);
  });

  it('handles tab + spaces + tab: "\\t  \\tcode" tabSize=4', () => {
    // tab → col4 (boundary, index 1), 2 spaces → col6, tab → col8 (boundary, index 4)
    expect(computeGuidePositions('\t  \tcode', 4)).toEqual([1, 4]);
  });

  // -------------------------------------------------------------------------
  // Tab spanning multiple boundaries (tabSize=2, col=1)
  // -------------------------------------------------------------------------

  it('deduplicates when a tab spans a boundary it already crossed', () => {
    // 1 space → col1, tab jumps to col2 (only 1 col advance with tabSize=2, not a full tab stop),
    // boundary at col2 → index 2
    expect(computeGuidePositions(' \tcode', 2)).toEqual([2]);
  });

  // -------------------------------------------------------------------------
  // Edge cases
  // -------------------------------------------------------------------------

  it('returns [] for tabSize=0', () => {
    expect(computeGuidePositions('    code', 0)).toEqual([]);
    expect(computeGuidePositions('\t\tcode', 0)).toEqual([]);
    expect(computeGuidePositions(' \t  code', 0)).toEqual([]);
  });

  it('returns [] for negative tabSize', () => {
    expect(computeGuidePositions('    code', -1)).toEqual([]);
    expect(computeGuidePositions('\tcode', -4)).toEqual([]);
  });

  it('tabSize=1: every leading space is a boundary', () => {
    expect(computeGuidePositions('   x', 1)).toEqual([1, 2, 3]);
  });

  it('tabSize=1: tab counts as one column', () => {
    // With tabSize=1, tab advances 1 column (since 1 - (col%1) = 1-0 = 1)
    expect(computeGuidePositions('\tx', 1)).toEqual([1]);
    expect(computeGuidePositions('\t\tx', 1)).toEqual([1, 2]);
  });

  it('tabSize=1: mixed spaces and tabs', () => {
    expect(computeGuidePositions(' \t x', 1)).toEqual([1, 2, 3]);
  });

  it('returns [] when line is all spaces but fewer than one unit', () => {
    // Only 3 spaces, tabSize=4: indentCol=3, numGuides=floor(3/4)=0 → []
    expect(computeGuidePositions('   ', 4)).toEqual([]);
  });

  it('returns correct positions for very deep indent', () => {
    // 16 spaces, tabSize=2 → 8 guides
    const line = '                code'; // 16 spaces
    expect(computeGuidePositions(line, 2)).toEqual([2, 4, 6, 8, 10, 12, 14, 16]);
  });

  it('stops at first non-whitespace character', () => {
    // "  // comment" — only 2 leading spaces, tabSize=4 → no full unit
    expect(computeGuidePositions('  // comment', 4)).toEqual([]);
  });

  it('handles exactly tabSize spaces followed by non-whitespace', () => {
    expect(computeGuidePositions('    // comment', 4)).toEqual([4]);
  });
});
