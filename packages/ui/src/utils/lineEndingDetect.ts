/**
 * lineEndingDetect.ts — Detects line ending style from file content.
 *
 * Scans the content to determine whether the file uses LF (Unix/Linux/macOS)
 * or CRLF (Windows) line endings.
 */

/** Line ending type */
export type LineEnding = 'LF' | 'CRLF' | 'Mixed';

/**
 * Result of line ending detection.
 */
export interface LineEndingDetectionResult {
  /** The detected line ending style */
  lineEnding: LineEnding;
}

/**
 * Detect line ending style from file content.
 *
 * Algorithm:
 * 1. Take the first 5000 characters for performance (line endings are typically consistent)
 * 2. Scan for \r\n (CRLF) and bare \n (LF after stripping \r\n).
 * 3. If only CRLF is found → CRLF
 * 4. If only bare LF is found → LF
 * 5. If both CRLF and bare LF are found → Mixed
 *
 * @param content - The file content to analyze
 * @returns The detected line ending style
 */
export function detectLineEnding(content: string): LineEndingDetectionResult {
  // Handle empty content
  if (!content || content.length === 0) {
    return { lineEnding: 'LF' };
  }

  // Scan only the first 5000 chars for performance
  const sample = content.length > 5000 ? content.substring(0, 5000) : content;

  const hasCRLF = sample.indexOf('\r\n') !== -1;
  // Check for bare LF by stripping all \r\n sequences first
  const hasBareLF = /\n/.test(sample.replace(/\r\n/g, ''));

  let lineEnding: LineEnding;
  if (hasCRLF && hasBareLF) {
    lineEnding = 'Mixed';
  } else if (hasCRLF) {
    lineEnding = 'CRLF';
  } else {
    lineEnding = 'LF';
  }

  return { lineEnding };
}
