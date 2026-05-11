/**
 * Parses unified diff format into original and modified document strings.
 * Used by @codemirror/merge to provide two full documents for comparison.
 */

import { debugLog } from './log';

/** Output interface containing original and modified document strings. */
export interface DiffDocuments {
  original: string;
  modified: string;
}

/**
 * Parse a unified diff string into original and modified document content.
 *
 * Handles:
 * - Standard unified diff format (--- a/file, +++ b/file, @@ hunk headers)
 * - Multiple hunks
 * - "No newline at end of file" markers
 * - New files (--- /dev/null)
 * - Deleted files (+++ /dev/null)
 *
 * @param diffText - Unified diff string from git diff or similar
 * @returns Object with original and modified document strings
 */
export function parseUnifiedDiffToDocuments(diffText: string): DiffDocuments {
  if (!diffText || !diffText.trim()) {
    return { original: '', modified: '' };
  }

  try {
    const lines = diffText.split('\n');
    const originalLines: string[] = [];
    const modifiedLines: string[] = [];

    let inHunk = false;
    const hunkHeaderRegex = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/;

    for (const line of lines) {
      // Skip header lines
      if (
        line.startsWith('diff --git') ||
        line.startsWith('index ') ||
        line.startsWith('--- ') ||
        line.startsWith('+++ ')
      ) {
        continue;
      }

      // Check for hunk header
      if (hunkHeaderRegex.test(line)) {
        inHunk = true;
        continue;
      }

      // Handle "no newline at end of file" marker
      if (line === '\\ No newline at end of file') {
        continue;
      }

      // Process hunk content lines
      if (inHunk && line.length > 0) {
        const firstChar = line.charAt(0);

        if (firstChar === ' ') {
          // Context line - appears in both original and modified
          const content = line.slice(1);
          originalLines.push(content);
          modifiedLines.push(content);
        } else if (firstChar === '-') {
          // Removed line - only in original
          originalLines.push(line.slice(1));
        } else if (firstChar === '+') {
          // Added line - only in modified
          modifiedLines.push(line.slice(1));
        } else if (!firstChar) {
          // Empty context line (edge case)
          originalLines.push('');
          modifiedLines.push('');
        }
        // else: other prefix (like '\'), skip
      }
    }

    // Warn if diff had content but produced no documents (likely malformed)
    if (originalLines.length === 0 && modifiedLines.length === 0 && diffText.trim().length > 20) {
      debugLog('[diffParser] No hunks found in diff text — the diff may be malformed');
    }

    return {
      original: originalLines.join('\n'),
      modified: modifiedLines.join('\n'),
    };
  } catch (err) {
    debugLog('[diffParser] parseUnifiedDiffToDocuments failed:', err);
    return { original: '', modified: '' };
  }
}

/**
 * Simple helper to create DiffDocuments from two full content strings.
 * Useful when you already have original and modified documents.
 */
export function documentsFromStrings(originalContent: string, modifiedContent: string): DiffDocuments {
  return {
    original: originalContent,
    modified: modifiedContent,
  };
}
