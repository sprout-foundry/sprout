import { describe, it, expect } from 'vitest';
import { parseUnifiedDiffToDocuments, documentsFromStrings } from './diffParser';

describe('parseUnifiedDiffToDocuments', () => {
  // ── Empty / whitespace input ──────────────────────────────────────────

  it('returns empty documents for empty string', () => {
    const result = parseUnifiedDiffToDocuments('');
    expect(result).toEqual({ original: '', modified: '' });
  });

  it('returns empty documents for whitespace-only input', () => {
    const result = parseUnifiedDiffToDocuments('   \n  \t  \n  ');
    expect(result).toEqual({ original: '', modified: '' });
  });

  it('returns empty documents for undefined input', () => {
    const result = parseUnifiedDiffToDocuments('' as any);
    expect(result).toEqual({ original: '', modified: '' });
  });

  // ── Standard diff ─────────────────────────────────────────────────────

  it('parses a simple unified diff with one change', () => {
    const diff = [
      'diff --git a/file.txt b/file.txt',
      'index abc123..def456 100644',
      '--- a/file.txt',
      '+++ b/file.txt',
      '@@ -1,3 +1,3 @@',
      ' line1',
      '-old line2',
      '+new line2',
      ' line3',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('line1\nold line2\nline3');
    expect(result.modified).toBe('line1\nnew line2\nline3');
  });

  it('handles context lines (space prefix)', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,3 +1,3 @@', ' context'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('context');
    expect(result.modified).toBe('context');
  });

  it('handles removed lines (- prefix)', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,2 +1,1 @@', ' kept', '-removed'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('kept\nremoved');
    expect(result.modified).toBe('kept');
  });

  it('handles added lines (+ prefix)', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,1 +1,2 @@', ' kept', '+added'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('kept');
    expect(result.modified).toBe('kept\nadded');
  });

  // ── Multiple hunks ────────────────────────────────────────────────────

  it('parses multiple hunks', () => {
    const diff = [
      '--- a/file.go',
      '+++ b/file.go',
      '@@ -1,3 +1,4 @@',
      ' package main',
      ' ',
      '+import "fmt"',
      ' func main() {}',
      '@@ -10,3 +11,3 @@',
      ' // end of file',
      '-old comment',
      '+new comment',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toContain('package main');
    expect(result.original).toContain('old comment');
    expect(result.modified).toContain('import "fmt"');
    expect(result.modified).toContain('new comment');
    expect(result.original).not.toContain('new comment');
    expect(result.modified).not.toContain('old comment');
  });

  // ── New file (--- /dev/null) ──────────────────────────────────────────

  it('parses new file diff (--- /dev/null)', () => {
    const diff = [
      'diff --git a/new.go b/new.go',
      'new file mode 100644',
      '--- /dev/null',
      '+++ b/new.go',
      '@@ -0,0 +1,2 @@',
      '+package main',
      '+func New() {}',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('');
    expect(result.modified).toBe('package main\nfunc New() {}');
  });

  // ── Deleted file (+++ /dev/null) ──────────────────────────────────────

  it('parses deleted file diff (+++ /dev/null)', () => {
    const diff = [
      'diff --git a/old.go b/old.go',
      'deleted file mode 100644',
      '--- a/old.go',
      '+++ /dev/null',
      '@@ -1,2 +0,0 @@',
      '-package old',
      '-func Old() {}',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('package old\nfunc Old() {}');
    expect(result.modified).toBe('');
  });

  // ── No newline at end of file ─────────────────────────────────────────

  it('handles "No newline at end of file" marker', () => {
    const diff = [
      '--- a/f',
      '+++ b/f',
      '@@ -1,2 +1,2 @@',
      ' line1',
      '-old',
      '\\ No newline at end of file',
      '+new',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('line1\nold');
    expect(result.modified).toBe('line1\nnew');
  });

  it('handles "No newline" marker on both sides', () => {
    const diff = [
      '--- a/f',
      '+++ b/f',
      '@@ -1,2 +1,2 @@',
      ' line1',
      '-old',
      '\\ No newline at end of file',
      '+new',
      '\\ No newline at end of file',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('line1\nold');
    expect(result.modified).toBe('line1\nnew');
  });

  // ── Malformed / edge cases ────────────────────────────────────────────

  it('returns empty documents for malformed diff with no hunks', () => {
    const result = parseUnifiedDiffToDocuments('not a diff\njust some text\nmore random lines');
    expect(result).toEqual({ original: '', modified: '' });
  });

  it('handles hunk header without count for empty range', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -0,0 +1 @@', '+single line'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.modified).toBe('single line');
  });

  it('handles diff with only header lines and no hunk content', () => {
    const diff = ['diff --git a/x b/x', '--- a/x', '+++ b/x'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('');
    expect(result.modified).toBe('');
  });

  it('handles hunk with empty context line (single space)', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,2 +1,2 @@', ' ', ' content'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    // The " " line becomes empty string after slice(1)
    expect(result.original).toBe('\ncontent');
    expect(result.modified).toBe('\ncontent');
  });

  it('handles lines with special characters in content', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,2 +1,2 @@', '-old: a & b <c>', '+new: x | y {z}'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toBe('old: a & b <c>');
    expect(result.modified).toBe('new: x | y {z}');
  });

  it('handles tab-indented content', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,2 +1,2 @@', '\tpackage main', '\tfunc main() {}'].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    // Tab is not ' ', so these lines are not recognized as context.
    // They should be skipped (not ' ', not '+', not '-')
    expect(result.original).toBe('');
    expect(result.modified).toBe('');
  });

  it('handles real-world git diff', () => {
    const diff = [
      'diff --git a/pkg/handler.go b/pkg/handler.go',
      'index 123..456 100644',
      '--- a/pkg/handler.go',
      '+++ b/pkg/handler.go',
      '@@ -5,8 +5,10 @@',
      ' package handler',
      ' ',
      ' import (',
      '-\t"fmt"',
      '+\t"context"',
      '+\t"fmt"',
      ' )',
      ' ',
      '-func Handle() {',
      '+func Handle(ctx context.Context) {',
    ].join('\n');

    const result = parseUnifiedDiffToDocuments(diff);
    expect(result.original).toContain('package handler');
    expect(result.original).not.toContain('context');
    expect(result.modified).toContain('context');
    expect(result.modified).toContain('func Handle(ctx context.Context) {');
  });
});

describe('documentsFromStrings', () => {
  it('returns documents with both strings', () => {
    const result = documentsFromStrings('original content', 'modified content');
    expect(result.original).toBe('original content');
    expect(result.modified).toBe('modified content');
  });

  it('handles empty strings', () => {
    const result = documentsFromStrings('', '');
    expect(result.original).toBe('');
    expect(result.modified).toBe('');
  });

  it('handles multiline strings', () => {
    const result = documentsFromStrings('line1\nline2', 'line1\nline3');
    expect(result.original).toBe('line1\nline2');
    expect(result.modified).toBe('line1\nline3');
  });

  it('handles strings with special characters', () => {
    const result = documentsFromStrings('a\tb\nc', 'a\tb\nd');
    expect(result.original).toBe('a\tb\nc');
    expect(result.modified).toBe('a\tb\nd');
  });
});
