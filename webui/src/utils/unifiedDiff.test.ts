import { describe, expect, it } from 'vitest';
import { displayPath, parseUnifiedDiff } from './unifiedDiff';

describe('parseUnifiedDiff', () => {
  it('parses a simple single-hunk diff with real line numbers', () => {
    const diff = [
      '--- a/src/foo.ts',
      '+++ b/src/foo.ts',
      '@@ -10,4 +10,5 @@ function foo()',
      ' context line',
      '-removed line',
      '+added line',
      '+second added line',
      ' more context',
    ].join('\n');

    const parsed = parseUnifiedDiff(diff);
    expect(parsed.files).toHaveLength(1);
    const file = parsed.files[0];
    expect(file.oldPath).toBe('src/foo.ts');
    expect(file.newPath).toBe('src/foo.ts');
    expect(file.hunks).toHaveLength(1);

    const hunk = file.hunks[0];
    expect(hunk.oldStart).toBe(10);
    expect(hunk.newStart).toBe(10);
    expect(hunk.headerContext).toBe('function foo()');

    const types = hunk.rows.map((r) => r.type);
    expect(types).toEqual(['context', 'del', 'add', 'add', 'context']);

    // Real line numbers: old side 10..13, new side 10..14
    expect(hunk.rows[0]).toMatchObject({ oldNumber: 10, newNumber: 10 });
    expect(hunk.rows[1]).toMatchObject({ oldNumber: 11, newNumber: null });
    expect(hunk.rows[2]).toMatchObject({ oldNumber: null, newNumber: 11 });
    expect(hunk.rows[3]).toMatchObject({ oldNumber: null, newNumber: 12 });
    expect(hunk.rows[4]).toMatchObject({ oldNumber: 12, newNumber: 13 });

    expect(file.additions).toBe(2);
    expect(file.deletions).toBe(1);
    expect(parsed.additions).toBe(2);
    expect(parsed.deletions).toBe(1);
  });

  it('continues numbering across multiple hunks', () => {
    const diff = [
      '--- a/f',
      '+++ b/f',
      '@@ -1,2 +1,2 @@',
      ' a',
      '-b',
      '+B',
      '@@ -100,2 +100,2 @@',
      ' c',
      '-d',
      '+D',
    ].join('\n');

    const parsed = parseUnifiedDiff(diff);
    const hunks = parsed.files[0].hunks;
    expect(hunks).toHaveLength(2);
    // Second hunk jumps to the real file line 100.
    expect(hunks[1].rows[0].oldNumber).toBe(100);
    expect(hunks[1].rows[0].newNumber).toBe(100);
  });

  it('handles single-count hunk headers (@@ -5 +5 @@)', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -5 +5 @@', '-old', '+new'].join('\n');
    const parsed = parseUnifiedDiff(diff);
    const hunk = parsed.files[0].hunks[0];
    expect(hunk.oldStart).toBe(5);
    expect(hunk.oldCount).toBe(1);
    expect(hunk.newStart).toBe(5);
    expect(hunk.newCount).toBe(1);
  });

  it('parses multi-file diffs with per-file stats', () => {
    const diff = [
      'diff --git a/one.ts b/one.ts',
      '--- a/one.ts',
      '+++ b/one.ts',
      '@@ -1,2 +1,2 @@',
      ' ctx',
      '-del1',
      '+add1',
      'diff --git a/two.ts b/two.ts',
      '--- a/two.ts',
      '+++ b/two.ts',
      '@@ -1,1 +1,2 @@',
      ' ctx',
      '+add2',
      '+add3',
    ].join('\n');

    const parsed = parseUnifiedDiff(diff);
    expect(parsed.files).toHaveLength(2);
    expect(parsed.files[0].oldPath).toBe('one.ts');
    expect(parsed.files[0].additions).toBe(1);
    expect(parsed.files[0].deletions).toBe(1);
    expect(parsed.files[1].newPath).toBe('two.ts');
    expect(parsed.files[1].additions).toBe(2);
    expect(parsed.files[1].deletions).toBe(0);
    expect(parsed.additions).toBe(3);
    expect(parsed.deletions).toBe(1);
  });

  it('marks binary files', () => {
    const diff = ['diff --git a/img.png b/img.png', 'Binary files a/img.png and b/img.png differ'].join('\n');
    const parsed = parseUnifiedDiff(diff);
    expect(parsed.files).toHaveLength(1);
    expect(parsed.files[0].binary).toBe(true);
    expect(parsed.files[0].oldPath).toBe('img.png');
  });

  it('handles new-file diffs (/dev/null)', () => {
    const diff = ['--- /dev/null', '+++ b/new.ts', '@@ -0,0 +1,2 @@', '+line1', '+line2'].join('\n');
    const parsed = parseUnifiedDiff(diff);
    const file = parsed.files[0];
    expect(file.oldPath).toBe('/dev/null');
    expect(file.newPath).toBe('new.ts');
    expect(file.additions).toBe(2);
    expect(displayPath(file)).toBe('new.ts');
  });

  it('attaches word-level diffs to adjacent del/add pairs', () => {
    const diff = [
      '--- a/f',
      '+++ b/f',
      '@@ -1,3 +1,3 @@',
      ' ctx',
      '-const foo = bar;',
      '+const foo = baz;',
      ' ctx',
    ].join('\n');
    const parsed = parseUnifiedDiff(diff);
    const rows = parsed.files[0].hunks[0].rows;
    const delRow = rows.find((r) => r.type === 'del')!;
    const addRow = rows.find((r) => r.type === 'add')!;
    expect(delRow.wordDiff).toBeDefined();
    expect(addRow.wordDiff).toBeDefined();
    // The changed token should appear marked on both sides.
    const delChanged = delRow
      .wordDiff!.filter((p) => p.changed)
      .map((p) => p.text)
      .join('');
    const addChanged = addRow
      .wordDiff!.filter((p) => p.changed)
      .map((p) => p.text)
      .join('');
    expect(delChanged).toContain('bar');
    expect(addChanged).toContain('baz');
    // Shared tokens are unmarked.
    const delUnchanged = delRow
      .wordDiff!.filter((p) => !p.changed)
      .map((p) => p.text)
      .join('');
    expect(delUnchanged).toContain('const foo = ');
  });

  it('skips word-diff pairing for large blocks', () => {
    // 40 del + 40 add lines exceeds the 32-line pairing guard.
    const dels = Array.from({ length: 40 }, (_, i) => `-old line ${i}`).join('\n');
    const adds = Array.from({ length: 40 }, (_, i) => `+new line ${i}`).join('\n');
    const diff = ['--- a/f', '+++ b/f', '@@ -1,80 +1,80 @@', dels, adds].join('\n');
    const parsed = parseUnifiedDiff(diff);
    const rows = parsed.files[0].hunks[0].rows;
    expect(rows).toHaveLength(80);
    expect(rows.find((r) => r.type === 'del')!.wordDiff).toBeUndefined();
    expect(rows.find((r) => r.type === 'add')!.wordDiff).toBeUndefined();
  });

  it('skips "\\\\ No newline" marker rows but preserves them as text', () => {
    const diff = ['--- a/f', '+++ b/f', '@@ -1,1 +1,1 @@', '-old', '\\ No newline at end of file', '+new'].join('\n');
    const parsed = parseUnifiedDiff(diff);
    const rows = parsed.files[0].hunks[0].rows;
    expect(rows).toHaveLength(2);
    expect(rows[0].text).toContain('No newline at end of file');
  });

  it('returns empty for empty or malformed input', () => {
    expect(parseUnifiedDiff('')).toEqual({ files: [], additions: 0, deletions: 0 });
    expect(parseUnifiedDiff('  \n  ')).toEqual({ files: [], additions: 0, deletions: 0 });
    expect(parseUnifiedDiff('not a diff at all').files).toHaveLength(0);
  });

  it('handles quoted paths with spaces', () => {
    const diff = ['--- "a/sp ace.ts"', '+++ "b/sp ace.ts"', '@@ -1,1 +1,1 @@', '-a', '+b'].join('\n');
    const parsed = parseUnifiedDiff(diff);
    expect(parsed.files[0].newPath).toBe('sp ace.ts');
  });
});
