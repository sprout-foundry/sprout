/**
 * indentDetect.ts — Detects indentation style and width from file content.
 *
 * Scans the first ~100 lines to determine whether the file uses tabs or spaces,
 * and if spaces, what indent width (2, 4, or 8).
 */

/** Default indent width when detection fails or is inconclusive */
export const DEFAULT_INDENT_WIDTH = 4;

/**
 * Result of indentation detection.
 */
export interface IndentDetectionResult {
  /** Whether to use tabs for indentation */
  useTabs: boolean;
  /** The indent width in spaces (2, 4, or 8). Only meaningful when useTabs is false. */
  indentWidth: number;
  /** How many lines were actually analyzed (may be less than requested if file is short) */
  linesAnalyzed: number;
  /** How many lines had leading indentation (tabs or spaces). Used to assess detection confidence. */
  indentedLineCount: number;
}

/**
 * Compute the Greatest Common Divisor of two numbers using Euclidean algorithm.
 */
function gcd(a: number, b: number): number {
  a = Math.abs(a);
  b = Math.abs(b);
  while (b !== 0) {
    const temp = b;
    b = a % b;
    a = temp;
  }
  return a;
}

/**
 * Compute the GCD of an array of numbers.
 */
function gcdArray(numbers: number[]): number {
  if (numbers.length === 0) return 0;
  return numbers.reduce((acc, n) => gcd(acc, n));
}

/**
 * Snap a value to the nearest standard indent width (2, 4, or 8).
 * If the value is 1 or not evenly divisible by any standard width, snap to 2.
 */
function snapToStandardWidth(value: number): number {
  // Check if it's evenly divisible by standard widths
  const standardWidths = [2, 4, 8];

  for (const width of standardWidths) {
    if (value > 0 && value % width === 0) {
      // Find the largest standard width that divides evenly
      let best = width;
      for (const sw of standardWidths) {
        if (sw > best && value % sw === 0) {
          best = sw;
        }
      }
      return best;
    }
  }

  // If not evenly divisible, snap to closest
  if (value === 0) return DEFAULT_INDENT_WIDTH;

  // Find closest standard width
  const closest = standardWidths.reduce((prev, curr) =>
    Math.abs(curr - value) < Math.abs(prev - value) ? curr : prev,
  );

  return closest;
}

/**
 * Count leading whitespace on a line, stopping at first non-whitespace.
 * Tabs count as 1, spaces count individually.
 * Mixed tab+space: counts as tab (tab wins).
 *
 * @returns { tabIndent: boolean, spaceCount: number } - Whether the line starts with a tab, and how many spaces if not
 */
function analyzeLineIndent(line: string): { hasIndent: boolean; useTab: boolean; spaceCount: number } {
  if (line.length === 0) {
    return { hasIndent: false, useTab: false, spaceCount: 0 };
  }

  const firstChar = line[0];

  if (firstChar === '\t') {
    return { hasIndent: true, useTab: true, spaceCount: 0 };
  }

  if (firstChar === ' ') {
    // Count leading spaces, stop at first non-space character
    let spaceCount = 0;
    for (let i = 0; i < line.length; i++) {
      if (line[i] === ' ') {
        spaceCount++;
      } else {
        break;
      }
    }
    return { hasIndent: spaceCount > 0, useTab: false, spaceCount };
  }

  // Line starts with non-whitespace
  return { hasIndent: false, useTab: false, spaceCount: 0 };
}

/**
 * Detect indentation style from file content.
 *
 * Algorithm:
 * 1. Take the first `maxLines` lines (default 100)
 * 2. For each line that starts with whitespace, classify it:
 *    - If it starts with a tab character → count as "tab" indent
 *    - If it starts with spaces → count as "space" indent with the measured width
 * 3. For space-indented lines, determine the indent width by finding the
 *    greatest common divisor (GCD) of all space indent amounts, then round
 *    to the nearest standard width (2, 4, or 8).
 * 4. The majority style (tabs vs spaces) wins. For ties, spaces win.
 * 5. If no indented lines are found, return the default (spaces, 4).
 * 6. If fewer than `minLines` lines have indentation, return the default (spaces, 4)
 *    with linesAnalyzed reflecting what was scanned (caller can decide to ignore).
 */
export function detectIndentation(content: string, maxLines: number = 100): IndentDetectionResult {
  // Handle empty content
  if (!content || content.length === 0) {
    return {
      useTabs: false,
      indentWidth: DEFAULT_INDENT_WIDTH,
      linesAnalyzed: 0,
      indentedLineCount: 0,
    };
  }

  // Split by newlines and take first maxLines
  const lines = content.split('\n').slice(0, maxLines);
  const linesAnalyzed = lines.length;

  // Track indentation counts
  let tabIndentCount = 0;
  let spaceIndentCount = 0;
  const spaceIndentAmounts: number[] = [];

  for (const line of lines) {
    // Skip empty lines (only whitespace)
    const trimmed = line.trim();
    if (trimmed.length === 0) {
      continue;
    }

    const analysis = analyzeLineIndent(line);

    if (!analysis.hasIndent) {
      continue;
    }

    if (analysis.useTab) {
      tabIndentCount++;
    } else {
      spaceIndentCount++;
      if (analysis.spaceCount > 0) {
        spaceIndentAmounts.push(analysis.spaceCount);
      }
    }
  }

  // If fewer than 2 lines have indentation, return default
  const totalIndentedLines = tabIndentCount + spaceIndentCount;
  if (totalIndentedLines < 2) {
    return {
      useTabs: false,
      indentWidth: DEFAULT_INDENT_WIDTH,
      linesAnalyzed,
      indentedLineCount: totalIndentedLines,
    };
  }

  // Determine if tabs or spaces win (ties go to spaces)
  const useTabs = tabIndentCount > spaceIndentCount;

  // Determine space indent width
  let indentWidth = DEFAULT_INDENT_WIDTH;
  if (spaceIndentAmounts.length > 0) {
    const computedGcd = gcdArray(spaceIndentAmounts);
    indentWidth = snapToStandardWidth(computedGcd);
  }

  return {
    useTabs,
    indentWidth,
    linesAnalyzed,
    indentedLineCount: totalIndentedLines,
  };
}
