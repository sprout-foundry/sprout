/**
 * Structured unified-diff parser.
 *
 * Produces a hunk model with real old/new line numbers (from the @@ headers),
 * paired -/+ rows for GitHub-style rendering, word-level intraline diffs,
 * and +N/-M stats — everything the old flow threw away when it flattened a
 * diff into two full documents for @codemirror/merge to re-diff.
 */

import { computeWordDiff, type WordDiffPart } from './wordDiff';

export type DiffRowType = 'context' | 'add' | 'del' | 'hunk-header';

export interface DiffLineRow {
  type: DiffRowType;
  /** Content without the +/-/space prefix. */
  text: string;
  /** 1-based line number in the old file; null for pure adds. */
  oldNumber: number | null;
  /** 1-based line number in the new file; null for pure dels. */
  newNumber: number | null;
  /** Word-level highlight spans (paired -/+ runs only). */
  wordDiff?: WordDiffPart[];
}

export interface DiffHunk {
  /** Lines of hunk content (rows), first row is the hunk header. */
  rows: DiffLineRow[];
  /** Old-side start line and count from the @@ header. */
  oldStart: number;
  oldCount: number;
  /** New-side start line and count from the @@ header. */
  newStart: number;
  newCount: number;
  /** Trailing context after @@ on the same line (e.g. "function foo()"). */
  headerContext: string;
}

export interface ParsedDiffFile {
  /** Old path (--- line), e.g. "a/src/foo.ts" or "/dev/null". */
  oldPath: string;
  /** New path (+++ line). */
  newPath: string;
  hunks: DiffHunk[];
  additions: number;
  deletions: number;
  /** True when the diff had no recognizable hunks. */
  binary?: boolean;
}

export interface ParsedDiff {
  files: ParsedDiffFile[];
  additions: number;
  deletions: number;
}

const HUNK_HEADER_RE = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$/;

/** Strip a/ b/ prefixes from ---/+++ paths. */
function cleanPath(raw: string): string {
  const trimmed = raw
    .trim()
    .replace(/"[^"]*"$/, (m) => m)
    .trim();
  if (trimmed === '/dev/null') return '/dev/null';
  // git quotes paths with special chars: "--- \"a/sp ace.ts\""
  const unquoted = trimmed.startsWith('"') && trimmed.endsWith('"') ? trimmed.slice(1, -1) : trimmed;
  if (unquoted.startsWith('a/') || unquoted.startsWith('b/')) return unquoted.slice(2);
  return unquoted;
}

function parseHunkHeaderLine(line: string): DiffHunk | null {
  const m = HUNK_HEADER_RE.exec(line);
  if (!m) return null;
  return {
    rows: [],
    oldStart: parseInt(m[1], 10),
    oldCount: m[2] === undefined ? 1 : parseInt(m[2], 10),
    newStart: parseInt(m[3], 10),
    newCount: m[4] === undefined ? 1 : parseInt(m[4], 10),
    headerContext: (m[5] || '').trim(),
  };
}

/**
 * Attach word-level diffs to adjacent del→add runs. Only pairs a single
 * del block with a single add block (the GitHub heuristic) and skips
 * pairing when either side is very large — intraline detail on huge
 * rewrites is noise, and the cost is O(n·m).
 */
function attachWordDiffs(rows: DiffLineRow[]): void {
  for (let i = 0; i < rows.length; i++) {
    if (rows[i].type !== 'del') continue;
    let delEnd = i;
    while (delEnd + 1 < rows.length && rows[delEnd + 1].type === 'del') delEnd++;

    const j = delEnd + 1;
    if (j >= rows.length || rows[j].type !== 'add') continue;
    let addEnd = j;
    while (addEnd + 1 < rows.length && rows[addEnd + 1].type === 'add') addEnd++;

    const delLines = rows.slice(i, delEnd + 1);
    const addLines = rows.slice(j, addEnd + 1);
    const delLen = delLines.reduce((n, r) => n + r.text.length, 0);
    const addLen = addLines.reduce((n, r) => n + r.text.length, 0);

    // Pair small-to-medium runs only.
    if (delLen <= 2000 && addLen <= 2000 && delLines.length <= 32 && addLines.length <= 32) {
      const pairWordDiffs = computeWordDiff(
        delLines.map((r) => r.text).join('\n'),
        addLines.map((r) => r.text).join('\n'),
      );
      // Redistribute the joined diff back onto individual lines.
      distributeWordDiffAcrossLines(delLines, pairWordDiffs.del);
      distributeWordDiffAcrossLines(addLines, pairWordDiffs.add);
    }

    i = addEnd;
  }
}

/**
 * Split a word-diff of a multi-line string back into per-line spans.
 * `parts` covers `lines.join('\n')`; walk it, emitting a span per line
 * fragment, restarting the span list at each newline boundary.
 */
function distributeWordDiffAcrossLines(lines: DiffLineRow[], parts: WordDiffPart[] | undefined): void {
  if (!parts || parts.length === 0) return;
  let lineIdx = 0;
  let spans: WordDiffPart[] = [];
  const flush = () => {
    if (lineIdx < lines.length && spans.length > 0) {
      lines[lineIdx].wordDiff = spans;
    }
    spans = [];
  };
  for (const part of parts) {
    // A part may itself contain newlines when tokens span lines.
    const pieces = part.text.split('\n');
    pieces.forEach((piece, k) => {
      if (k > 0) {
        flush();
        lineIdx++;
      }
      if (piece.length > 0) {
        spans.push({ text: piece, changed: part.changed });
      }
    });
  }
  flush();
}

/**
 * Parse a unified diff (single or multi-file) into a structured model.
 * Falls back to a single synthetic file when no @@ headers are found
 * (binary diffs, malformed input) so the UI can still show something.
 */
export function parseUnifiedDiff(diffText: string): ParsedDiff {
  const result: ParsedDiff = { files: [], additions: 0, deletions: 0 };
  if (!diffText || !diffText.trim()) return result;

  const lines = diffText.split('\n');
  let currentFile: ParsedDiffFile | null = null;
  let currentHunk: DiffHunk | null = null;
  let oldLine = 0;
  let newLine = 0;

  const ensureFile = (): ParsedDiffFile => {
    if (!currentFile) {
      currentFile = { oldPath: '', newPath: '', hunks: [], additions: 0, deletions: 0 };
      result.files.push(currentFile);
    }
    return currentFile;
  };

  for (const raw of lines) {
    if (raw.startsWith('diff --git ')) {
      // Start of a new file section in a multi-file diff.
      currentFile = null;
      currentHunk = null;
      // Pre-extract path from "diff --git a/x b/x" for files without
      // ---/+++ headers (e.g. binary or mode-change entries).
      const pair = raw.slice('diff --git '.length).trim();
      const paths = pair.split(' ');
      const gitPath = cleanPath(paths[paths.length - 1] || '');
      currentFile = { oldPath: gitPath, newPath: gitPath, hunks: [], additions: 0, deletions: 0 };
      result.files.push(currentFile);
      continue;
    }

    if (raw.startsWith('--- ')) {
      const file = ensureFile();
      file.oldPath = cleanPath(raw.slice(4));
      currentHunk = null;
      continue;
    }
    if (raw.startsWith('+++ ')) {
      const file = ensureFile();
      file.newPath = cleanPath(raw.slice(4));
      currentHunk = null;
      continue;
    }
    if (raw.startsWith('Binary files ') || raw.startsWith('GIT binary patch')) {
      const file = ensureFile();
      file.binary = true;
      currentHunk = null;
      continue;
    }

    const hunkMatch = parseHunkHeaderLine(raw);
    if (hunkMatch) {
      const file = ensureFile();
      currentHunk = hunkMatch;
      file.hunks.push(currentHunk);
      oldLine = currentHunk.oldStart;
      newLine = currentHunk.newStart;
      continue;
    }

    if (!currentHunk) continue;

    if (raw.startsWith('\\')) {
      // "\ No newline at end of file" — attach to previous row as plain text.
      const prev = currentHunk.rows[currentHunk.rows.length - 1];
      if (prev) prev.text += raw;
      continue;
    }

    const prefix = raw.charAt(0);
    const text = raw.slice(1);
    if (prefix === '+') {
      currentHunk.rows.push({ type: 'add', text, oldNumber: null, newNumber: newLine++ });
      ensureFile().additions++;
    } else if (prefix === '-') {
      currentHunk.rows.push({ type: 'del', text, oldNumber: oldLine++, newNumber: null });
      ensureFile().deletions++;
    } else if (prefix === ' ' || raw === '') {
      // Context line (or an empty trailing line in a context run).
      currentHunk.rows.push({ type: 'context', text, oldNumber: oldLine++, newNumber: newLine++ });
    }
    // Other prefixes (rare extensions) are ignored.
  }

  for (const file of result.files) {
    for (const hunk of file.hunks) {
      attachWordDiffs(hunk.rows);
    }
    result.additions += file.additions;
    result.deletions += file.deletions;
  }

  return result;
}

/** Display path for a file: prefer the new path, fall back to old. */
export function displayPath(file: ParsedDiffFile): string {
  const p = file.newPath && file.newPath !== '/dev/null' ? file.newPath : file.oldPath;
  return p || '(unknown)';
}
