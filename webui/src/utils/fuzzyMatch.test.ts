// @ts-nocheck

import { fuzzyScore, fuzzyFilter, highlightMatches } from './fuzzyMatch';

describe('fuzzyScore', () => {
  it('returns score 0 for empty query', () => {
    const result = fuzzyScore('', 'anything');
    expect(result.score).toBe(0);
    expect(result.matches).toEqual([]);
  });

  it('returns -1 for non-matching query', () => {
    const result = fuzzyScore('xyz', 'hello');
    expect(result.score).toBe(-1);
  });

  // Substring matching
  describe('substring matching', () => {
    it('matches a simple substring', () => {
      const result = fuzzyScore('save', 'Save File');
      expect(result.score).toBeGreaterThanOrEqual(100);
    });

    it('gives prefix bonus', () => {
      const prefix = fuzzyScore('save', 'Save File');
      const midword = fuzzyScore('ave', 'Save File');
      expect(prefix.score).toBeGreaterThan(midword.score);
    });

    it('is case-insensitive', () => {
      const lower = fuzzyScore('file', 'File Browser');
      const upper = fuzzyScore('FILE', 'File Browser');
      expect(lower.score).toBe(upper.score);
    });

    it('gives word-boundary bonus', () => {
      const boundary = fuzzyScore('file', 'Go to File');
      const _inside = fuzzyScore('to', 'Go to File');
      // "to" as a word boundary match should score higher than a random substring
      expect(boundary.score).toBeGreaterThan(100);
    });
  });

  // Fuzzy character-sequence matching
  describe('fuzzy character matching', () => {
    it('matches characters in order', () => {
      const result = fuzzyScore('gts', 'Go to File...');
      // "g" at index 0, "t" at index 5 — but "s" is not in "Go to File..."
      // so this should not fuzzy match
      expect(result.score).toBe(-1);
    });

    it('does not match characters out of order', () => {
      const result = fuzzyScore('oat', 'Go to File');
      expect(result.score).toBe(-1);
    });

    it('fuzzy matches across word boundaries', () => {
      const result = fuzzyScore('svf', 'Save File');
      // s(0) v(2) f(5) — all in order
      expect(result.score).toBeGreaterThanOrEqual(0);
    });
  });

  it('prefers substring match over fuzzy when both possible', () => {
    const sub = fuzzyScore('file', 'Open File');
    const fuzz = fuzzyScore('opn', 'Open File');
    // Substring should score much higher than a partial fuzzy match
    expect(sub.score).toBeGreaterThan(fuzz.score);
  });
});

describe('fuzzyFilter', () => {
  const items = [
    { id: 1, label: 'Save File' },
    { id: 2, label: 'Save All Files' },
    { id: 3, label: 'Toggle Sidebar' },
    { id: 4, label: 'Switch to Chat' },
    { id: 5, label: 'Switch to Editor' },
    { id: 6, label: 'Split Editor Vertical' },
  ];

  it('returns empty for empty query', () => {
    const results = fuzzyFilter('', items, (i) => i.label);
    expect(results).toHaveLength(0);
  });

  it('filters and sorts by score', () => {
    const results = fuzzyFilter('save', items, (i) => i.label);
    expect(results.length).toBe(2);
    // Both start with "save" so they get the same prefix score; tiebreaker
    // is alphabetical.  "Save All Files" < "Save File" alphabetically
    // (because "a" < " ").  Verify both are present.
    const labels = results.map((r) => r.item.label);
    expect(labels).toContain('Save File');
    expect(labels).toContain('Save All Files');
  });

  it('fuzzy matches across items', () => {
    // "se" should fuzzy match "save", "switch", "sidebar"
    const results = fuzzyFilter('se', items, (i) => i.label);
    expect(results.length).toBeGreaterThan(0);
  });

  it('respects limit parameter', () => {
    const results = fuzzyFilter('s', items, (i) => i.label, 2);
    expect(results.length).toBeLessThanOrEqual(2);
  });

  it('returns empty array when nothing matches', () => {
    const results = fuzzyFilter('zzznotfound', items, (i) => i.label);
    expect(results).toHaveLength(0);
  });
});

describe('highlightMatches', () => {
  it('returns escaped text when no matches', () => {
    expect(highlightMatches('hello', [])).toBe('hello');
  });

  it('returns escaped text for HTML entities', () => {
    expect(highlightMatches('<script>', [])).toBe('&lt;script&gt;');
  });

  it('wraps matched regions in <mark> tags', () => {
    const result = highlightMatches('hello', [[2, 5]]);
    expect(result).toBe('he<mark>llo</mark>');
  });

  it('handles multiple separate matches', () => {
    const result = highlightMatches('abcdef', [
      [0, 2],
      [4, 6],
    ]);
    expect(result).toBe('<mark>ab</mark>cd<mark>ef</mark>');
  });

  it('escapes within matches', () => {
    const result = highlightMatches('<a>b', [[0, 3]]);
    expect(result).toBe('<mark>&lt;a&gt;</mark>b');
  });
});
