import { describe, it, expect } from 'vitest';
import { parseGitDiff, type DiffLineChange } from './gitDiffParser';

describe('parseGitDiff', () => {
  describe('empty input', () => {
    it('returns empty array for empty string', () => {
      expect(parseGitDiff('')).toEqual([]);
    });

    it('returns empty array for whitespace-only input', () => {
      expect(parseGitDiff('\n\n  \n')).toEqual([]);
    });

    it('returns empty array for headers-only input', () => {
      const diff = `diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt`;
      expect(parseGitDiff(diff)).toEqual([]);
    });
  });

  describe('single hunk - additions', () => {
    it('parses a single added line', () => {
      const diff = `@@ -5,3 +5,4 @@
 context
+added line
 context`;
      const result = parseGitDiff(diff);
      // newStart=5 (1-based) → newLineNum=4 (0-based); context → 5; + at newLineNum=5
      expect(result).toEqual([{ type: 'added', newLine: 5 }]);
    });

    it('parses multiple consecutive additions', () => {
      const diff = `@@ -10,2 +10,4 @@
 context
+line one
+line two
 context`;
      const result = parseGitDiff(diff);
      // newStart=10 → newLineNum=9; context → 10; + at 10, + at 11
      expect(result).toEqual([
        { type: 'added', newLine: 10 },
        { type: 'added', newLine: 11 },
      ]);
    });

    it('parses additions at the start of the file', () => {
      const diff = `@@ -0,0 +1,2 @@
+first line
+second line`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'added', newLine: 0 },
        { type: 'added', newLine: 1 },
      ]);
    });
  });

  describe('single hunk - removals', () => {
    it('does not emit standalone removals (no following +)', () => {
      // Implementation: removals only produce output when followed by a + line
      // (for modification detection). Without a following +, the removal is silent.
      const diff = `@@ -5,4 +5,3 @@
 context
-removed line
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([]);
    });

    it('does not emit multiple standalone removals', () => {
      const diff = `@@ -5,5 +5,3 @@
 context
-removed one
-removed two
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([]);
    });
  });

  describe('single hunk - modifications', () => {
    it('detects modification when - is followed by +', () => {
      const diff = `@@ -5,3 +5,3 @@
 context
-old line
+new line
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'modified', newLine: 5 }]);
    });

    it('detects multiple modifications', () => {
      const diff = `@@ -5,5 +5,5 @@
 context
-old one
+new one
-old two
+new two
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'modified', newLine: 5 },
        { type: 'modified', newLine: 6 },
      ]);
    });

    it('handles mixed modifications and additions', () => {
      const diff = `@@ -5,3 +5,4 @@
 context
-old line
+new line
+extra addition
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'modified', newLine: 5 },
        { type: 'added', newLine: 6 },
      ]);
    });

    it('handles mixed modifications and removals', () => {
      const diff = `@@ -5,4 +5,2 @@
 context
-old line
+new line
-also removed
 context`;
      const result = parseGitDiff(diff);
      // The -also removed is followed by context (no +), so it's silent
      expect(result).toEqual([{ type: 'modified', newLine: 5 }]);
    });

    it('does NOT reset pendingRemoved when context line appears between - and +', () => {
      // Implementation detail: context lines do not reset pendingRemoved,
      // so a - followed by context then + is still treated as modified
      const diff = `@@ -5,4 +5,3 @@
 context
-removed
 context
+added`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'modified', newLine: 6 }]);
    });
  });

  describe('multiple hunks', () => {
    it('parses changes from multiple hunks', () => {
      const diff = `@@ -1,2 +1,3 @@
+added at start
 context

@@ -10,3 +11,3 @@
-old
+new`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'added', newLine: 0 },
        { type: 'modified', newLine: 10 },
      ]);
    });

    it('handles hunks with different line number offsets', () => {
      const diff = `@@ -1,2 +1,3 @@
 context
+first hunk add

@@ -20,2 +21,2 @@
-second hunk remove
 context`;
      const result = parseGitDiff(diff);
      // Removal not followed by + is silent
      expect(result).toEqual([{ type: 'added', newLine: 1 }]);
    });

    it('handles three or more hunks', () => {
      const diff = `@@ -1,2 +1,3 @@
+one

@@ -10,2 +11,2 @@
-two
+two-new

@@ -20,2 +20,3 @@
+three`;
      const result = parseGitDiff(diff);
      // newStart=20 → newLineNum=19; + at newLineNum=19
      expect(result).toEqual([
        { type: 'added', newLine: 0 },
        { type: 'modified', newLine: 10 },
        { type: 'added', newLine: 19 },
      ]);
    });
  });

  describe('full diff with headers', () => {
    it('parses a realistic multi-file diff', () => {
      const diff = `diff --git a/src/main.go b/src/main.go
index abc123..def456 100644
--- a/src/main.go
+++ b/src/main.go
@@ -1,5 +1,7 @@
 package main

+import "fmt"
 func main() {
-  fmt.Println("old")
+  fmt.Println("new")
 }
+// end comment
`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'added', newLine: 2 },
        { type: 'modified', newLine: 4 },
        { type: 'added', newLine: 6 },
      ]);
    });
  });

  describe('no-newline markers', () => {
    it('ignores \\ No newline at end of file markers', () => {
      const diff = `@@ -1,2 +1,2 @@
-old
+new
\\ No newline at end of file`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'modified', newLine: 0 }]);
    });

    it('handles no-newline after removal (removal is silent without following +)', () => {
      const diff = `@@ -1,2 +1,1 @@
 context
-removed
\\ No newline at end of file`;
      const result = parseGitDiff(diff);
      // Removal not followed by + is silent
      expect(result).toEqual([]);
    });

    it('handles no-newline after addition', () => {
      const diff = `@@ -1,1 +1,2 @@
 context
+added no newline
\\ No newline at end of file`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'added', newLine: 1 }]);
    });

    it('handles multiple no-newline markers across hunks', () => {
      const diff = `@@ -1,2 +1,2 @@
-a
+b
\\ No newline at end of file

@@ -10,2 +10,2 @@
-x
+y
\\ No newline at end of file`;
      const result = parseGitDiff(diff);
      // newStart=10 → newLineNum=9; - then + = modified at 9
      expect(result).toEqual([
        { type: 'modified', newLine: 0 },
        { type: 'modified', newLine: 9 },
      ]);
    });
  });

  describe('edge cases', () => {
    it('handles hunk header without length counts', () => {
      const diff = `@@ -1 +1 @@
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([]);
    });

    it('handles hunk header with only start line', () => {
      const diff = `@@ -1 +2 @@
+added`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'added', newLine: 1 }]);
    });

    it('handles empty lines within a hunk as context', () => {
      const diff = `@@ -1,5 +1,5 @@
 line1

 line3
+added
 line5`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'added', newLine: 3 }]);
    });

    it('handles lines that start with spaces as context', () => {
      const diff = `@@ -1,3 +1,3 @@
 context
-  indented
+  new indented
 context`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'modified', newLine: 1 }]);
    });

    it('handles diff --git header lines before hunk', () => {
      const diff = `diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1 +2 @@
+new line`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([{ type: 'added', newLine: 1 }]);
    });

    it('handles garbage lines between hunk and next hunk', () => {
      const diff = `@@ -1,2 +1,2 @@
+added
random garbage
@@ -10,2 +10,2 @@
+another`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'added', newLine: 0 },
        { type: 'added', newLine: 9 },
      ]);
    });

    it('tracks newLine correctly through mixed operations', () => {
      const diff = `@@ -1,5 +1,7 @@
 context
+add1
 context
-old
+new
 context
+add2`;
      const result = parseGitDiff(diff);
      expect(result).toEqual([
        { type: 'added', newLine: 1 },
        { type: 'modified', newLine: 3 },
        { type: 'added', newLine: 5 },
      ]);
    });
  });
});
