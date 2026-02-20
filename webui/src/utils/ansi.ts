/**
 * ANSI escape code utilities
 */

/**
 * Strip ANSI escape codes from text
 * Removes color codes, formatting, and other terminal control sequences
 */
export function stripAnsiCodes(text: string): string {
  // Remove ANSI escape sequences
  // Matches: \x1b[ ... (any character) ... m
  // Examples: \x1b[31m (red), \x1b[1m (bold), \x1b[0m (reset)
  // eslint-disable-next-line no-control-regex
  const ansiRegex = /\x1b\[[0-9;]*[a-zA-Z]/g;
  return text.replace(ansiRegex, '');
}

/**
 * Check if text contains ANSI codes
 */
export function hasAnsiCodes(text: string): boolean {
  // eslint-disable-next-line no-control-regex
  const ansiRegex = /\x1b\[[0-9;]*[a-zA-Z]/g;
  return ansiRegex.test(text);
}
