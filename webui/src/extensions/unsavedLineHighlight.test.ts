/**
 * unsavedLineHighlight.test.ts — Unit tests for the unsavedLineHighlight extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the CM imports are mocked. We test the exported `computeModifiedLines`
 * helper directly — it's a pure function with no CM dependencies.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { computeModifiedLines } from './unsavedLineHighlight';

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/state', () => ({
  StateEffect: { define: jest.fn() },
  StateField: { define: jest.fn() },
}));
jest.mock('@codemirror/view', () => ({
  Decoration: {
    line: jest.fn(() => ({ range: jest.fn() })),
    none: [],
    set: jest.fn(),
  },
  ViewPlugin: { fromClass: jest.fn() },
  EditorView: { baseTheme: jest.fn(() => []) },
}));

// ── computeModifiedLines tests ──────────────────────────────────────

describe('computeModifiedLines', () => {
  // -------------------------------------------------------------------------
  // Identical content
  // -------------------------------------------------------------------------

  it('returns empty set for identical single-line content', () => {
    const result = computeModifiedLines(['hello'], ['hello']);
    expect(result.size).toBe(0);
  });

  it('returns empty set for identical multi-line content', () => {
    const lines = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(lines, lines);
    expect(result.size).toBe(0);
  });

  it('returns empty set for two empty arrays', () => {
    const result = computeModifiedLines([], []);
    expect(result.size).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Single-line modification
  // -------------------------------------------------------------------------

  it('detects a single modified line', () => {
    const current = ['hello world'];
    const original = ['hello'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1]));
  });

  it('detects a modified line in the middle', () => {
    const current = ['line 1', 'CHANGED', 'line 3'];
    const original = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([2]));
  });

  it('detects a modified line at the start', () => {
    const current = ['CHANGED', 'line 2', 'line 3'];
    const original = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1]));
  });

  it('detects a modified line at the end', () => {
    const current = ['line 1', 'line 2', 'CHANGED'];
    const original = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([3]));
  });

  // -------------------------------------------------------------------------
  // Multiple modifications
  // -------------------------------------------------------------------------

  it('detects multiple modified lines', () => {
    const current = ['CHANGED1', 'line 2', 'CHANGED2'];
    const original = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1, 3]));
  });

  it('detects all lines modified', () => {
    const current = ['a', 'b', 'c'];
    const original = ['x', 'y', 'z'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1, 2, 3]));
  });

  // -------------------------------------------------------------------------
  // Line additions
  // -------------------------------------------------------------------------

  it('detects a single added line at the end', () => {
    const current = ['line 1', 'line 2', 'new line'];
    const original = ['line 1', 'line 2'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([3]));
  });

  it('detects a single added line at the start', () => {
    const current = ['new line', 'line 1', 'line 2'];
    const original = ['line 1', 'line 2'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1]));
  });

  it('detects an added line in the middle', () => {
    const current = ['line 1', 'inserted', 'line 2'];
    const original = ['line 1', 'line 2'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([2]));
  });

  it('detects multiple added lines', () => {
    const current = ['line 1', 'a', 'b', 'line 2'];
    const original = ['line 1', 'line 2'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([2, 3]));
  });

  // -------------------------------------------------------------------------
  // Line deletions (no lines in current are affected, but surrounding
  // lines may shift)
  // -------------------------------------------------------------------------

  it('handles a deleted line at the end', () => {
    const current = ['line 1'];
    const original = ['line 1', 'line 2'];
    const result = computeModifiedLines(current, original);
    expect(result.size).toBe(0);
  });

  it('handles a deleted line in the middle', () => {
    const current = ['line 1', 'line 3'];
    const original = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(current, original);
    // line 3 in current matches line 3 in original, line 2 is deleted
    expect(result).toEqual(new Set([]));
  });

  // -------------------------------------------------------------------------
  // Empty original (new file)
  // -------------------------------------------------------------------------

  it('marks all lines as modified when original is empty', () => {
    const current = ['line 1', 'line 2', 'line 3'];
    const result = computeModifiedLines(current, []);
    expect(result).toEqual(new Set([1, 2, 3]));
  });

  it('returns empty set when both current and original are empty', () => {
    const result = computeModifiedLines([], []);
    expect(result.size).toBe(0);
  });

  it('marks a single line as modified when original is empty', () => {
    const result = computeModifiedLines(['hello'], []);
    expect(result).toEqual(new Set([1]));
  });

  // -------------------------------------------------------------------------
  // Empty current (all content deleted)
  // -------------------------------------------------------------------------

  it('returns empty set when current is empty but original has content', () => {
    const result = computeModifiedLines([], ['line 1']);
    expect(result.size).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Complex multi-edit scenarios
  // -------------------------------------------------------------------------

  it('handles interleaved additions and modifications', () => {
    const current = [
      'unchanged 1',
      'modified',
      'inserted',
      'unchanged 2',
      'also modified',
    ];
    const original = [
      'unchanged 1',
      'original line',
      'unchanged 2',
      'original too',
    ];
    const result = computeModifiedLines(current, original);
    // Lines 2 and 3 are new/changed, line 5 is changed
    expect(result.has(2)).toBe(true);
    expect(result.has(3)).toBe(true);
    expect(result.has(5)).toBe(true);
    // Unchanged lines should not be in the result
    expect(result.has(1)).toBe(false);
    expect(result.has(4)).toBe(false);
  });

  it('handles duplicate lines correctly', () => {
    const current = ['dup', 'dup', 'dup'];
    const original = ['dup', 'dup'];
    const result = computeModifiedLines(current, original);
    // LCS matches lines 2 and 3 in current with lines 1 and 2 in original;
    // line 1 is the extra line not in the LCS.
    expect(result).toEqual(new Set([1]));
  });

  // -------------------------------------------------------------------------
  // Fast-path for large files (>3000 lines)
  // -------------------------------------------------------------------------

  it('uses fast path for files over 3000 lines - prefix/suffix detection', () => {
    // Create a 3001-line file
    function makeLines(n, changeAt) {
      return Array.from({ length: n }, (_, i) =>
        changeAt.includes(i) ? 'CHANGED ' + i : 'line ' + i,
      );
    }

    const original = makeLines(3001, []);
    const current = makeLines(3001, [1500, 1501, 1502]);
    const result = computeModifiedLines(current, original);

    // Should detect the changed lines somewhere in the middle
    expect(result.size).toBeGreaterThan(0);
  });

  it('fast path correctly identifies unchanged prefix and suffix', () => {
    // Create a large file where only the middle differs
    const n = 3100;
    var original = [];
    var current = [];

    for (var i = 0; i < n; i++) {
      var line = 'line ' + i;
      original.push(line);
      if (i >= 1500 && i <= 1505) {
        current.push('CHANGED ' + i);
      } else {
        current.push(line);
      }
    }

    const result = computeModifiedLines(current, original);

    // Modified lines should include 1501..1506 (1-based)
    for (let i = 1501; i <= 1506; i++) {
      expect(result.has(i)).toBe(true);
    }
    // Lines before the change should NOT be marked
    expect(result.has(1)).toBe(false);
    expect(result.has(1499)).toBe(false);
    // Lines after the change should NOT be marked
    expect(result.has(1510)).toBe(false);
  });

  it('fast path returns empty set when prefix and suffix cover entire file', () => {
    // When current and original are identical (prefix covers everything),
    // the fast path should return an empty set, not mark all lines.
    const lines = [];
    for (let i = 0; i < 3100; i++) {
      lines.push('line ' + i);
    }
    const result = computeModifiedLines([...lines], lines);
    expect(result.size).toBe(0);
  });

  it('fast path handles overlap where prefix plus suffix exceeds file length', () => {
    // When current has extra lines at the end but the prefix matches up to
    // the original length, the suffix should still be computed correctly.
    const original = [];
    for (let i = 0; i < 3100; i++) {
      original.push('line ' + i);
    }
    const current = [...original, 'extra line'];
    const result = computeModifiedLines(current, original);
    // Only the extra line should be marked
    expect(result).toEqual(new Set([3101]));
  });

  // -------------------------------------------------------------------------
  // Edge cases
  // -------------------------------------------------------------------------

  it('handles a single character change', () => {
    const current = ['a'];
    const original = ['b'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1]));
  });

  it('handles whitespace-only changes', () => {
    const current = ['  hello  '];
    const original = ['  hello'];
    const result = computeModifiedLines(current, original);
    expect(result).toEqual(new Set([1]));
  });

  it('handles swapping two lines', () => {
    const current = ['line B', 'line A'];
    const original = ['line A', 'line B'];
    const result = computeModifiedLines(current, original);
    // LCS finds 'line A' at current position 2 matches original position 1,
    // so line 1 ('line B' at a different position) is marked as modified.
    expect(result).toEqual(new Set([1]));
  });
});
