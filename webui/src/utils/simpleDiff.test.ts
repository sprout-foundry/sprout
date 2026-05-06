import { describe, it, expect } from 'vitest';
import { generateUnifiedDiff } from './simpleDiff';

describe('generateUnifiedDiff', () => {
  it('returns null when strings are identical', () => {
    expect(generateUnifiedDiff('hello', 'hello')).toBeNull();
  });

  it('returns null for identical empty strings', () => {
    expect(generateUnifiedDiff('', '')).toBeNull();
  });

  it('uses default labels "editor" and "disk"', () => {
    const diff = generateUnifiedDiff('a', 'b');
    expect(diff).toContain('--- editor');
    expect(diff).toContain('+++ disk');
  });

  it('uses custom labels when provided', () => {
    const diff = generateUnifiedDiff('a', 'b', 'original', 'modified');
    expect(diff).toContain('--- original');
    expect(diff).toContain('+++ modified');
  });

  it('diffs single line change', () => {
    const diff = generateUnifiedDiff('old\n', 'new\n');
    expect(diff).toContain('--- editor');
    expect(diff).toContain('+++ disk');
    expect(diff).toContain('-old');
    expect(diff).toContain('+new');
  });

  it('diffs single line without trailing newline', () => {
    const diff = generateUnifiedDiff('old', 'new');
    expect(diff).toContain('-old');
    expect(diff).toContain('+new');
  });

  it('shows multi-line additions', () => {
    const oldText = 'line1\n';
    const newText = 'line1\nline2\nline3\n';
    const diff = generateUnifiedDiff(oldText, newText);
    expect(diff).toContain(' line1');
    expect(diff).toContain('+line2');
    expect(diff).toContain('+line3');
  });

  it('shows multi-line removals', () => {
    const oldText = 'line1\nline2\nline3\n';
    const newText = 'line1\n';
    const diff = generateUnifiedDiff(oldText, newText);
    expect(diff).toContain(' line1');
    expect(diff).toContain('-line2');
    expect(diff).toContain('-line3');
  });

  it('shows both additions and removals', () => {
    const oldText = 'a\nb\nc\n';
    const newText = 'a\nd\nc\n';
    const diff = generateUnifiedDiff(oldText, newText);
    expect(diff).toContain(' a');
    expect(diff).toContain('-b');
    expect(diff).toContain('+d');
    expect(diff).toContain(' c');
  });

  it('handles empty old string (all additions)', () => {
    const diff = generateUnifiedDiff('', 'new\n');
    expect(diff).not.toBeNull();
    expect(diff).toContain('+new');
  });

  it('handles empty new string (all removals)', () => {
    const diff = generateUnifiedDiff('old\n', '');
    expect(diff).not.toBeNull();
    expect(diff).toContain('-old');
  });

  it('handles single line with newline change', () => {
    const diff = generateUnifiedDiff('hello', 'hello\n');
    expect(diff).not.toBeNull();
    expect(diff).toContain('--- editor');
    expect(diff).toContain('+++ disk');
  });

  it('returns null for identical strings with newlines', () => {
    expect(generateUnifiedDiff('a\nb\n', 'a\nb\n')).toBeNull();
  });

  it('respects MAX_DIFF_LINES guard for large files', () => {
    const large = Array.from({ length: 2001 }, (_, i) => `line${i}`).join('\n');
    const result = generateUnifiedDiff('small', large);
    expect(result).not.toBeNull();
    expect(result).toContain('file too large for inline diff');
  });

  it('returns the too-large message with line counts', () => {
    const large = Array.from({ length: 2001 }, (_, i) => `line${i}`).join('\n');
    const result = generateUnifiedDiff('small', large, 'a', 'b');
    expect(result).toContain('--- a');
    expect(result).toContain('+++ b');
    expect(result).toContain('... (file too large for inline diff');
  });

  it('guards when old lines exceed MAX_DIFF_LINES', () => {
    const large = Array.from({ length: 2001 }, (_, i) => `line${i}`).join('\n');
    const result = generateUnifiedDiff(large, 'small');
    expect(result).toContain('file too large for inline diff');
  });

  it('handles completely different content', () => {
    const diff = generateUnifiedDiff('foo\nbar\n', 'baz\nqux\n');
    expect(diff).not.toBeNull();
    expect(diff).toContain('-foo');
    expect(diff).toContain('-bar');
    expect(diff).toContain('+baz');
    expect(diff).toContain('+qux');
  });

  it('preserves common lines as context', () => {
    const oldText = 'keep\nchange\nkeep\n';
    const newText = 'keep\nmodified\nkeep\n';
    const diff = generateUnifiedDiff(oldText, newText);
    expect(diff).toContain(' keep');
    expect(diff).toContain('-change');
    expect(diff).toContain('+modified');
  });

  it('diff starts with header format', () => {
    const diff = generateUnifiedDiff('a', 'b');
    const lines = diff!.split('\n');
    expect(lines[0]).toBe('--- editor');
    expect(lines[1]).toBe('+++ disk');
  });
});
