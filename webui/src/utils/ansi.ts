/**
 * ANSI escape code utilities
 */

/**
 * Strip ANSI escape codes from text
 * Removes color codes, formatting, and other terminal control sequences
 */
export function stripAnsiCodes(text: string): string {
  // Normalize common line endings first.
  let cleaned = text.replace(/\r\n/g, '\n').replace(/\r/g, '');

  // Remove OSC (Operating System Command) sequences:
  // ESC ] ... BEL   or   ESC ] ... ESC \
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B\][\s\S]*?(?:\x07|\x1B\\)/g, '');

  // Remove CSI (Control Sequence Introducer) sequences:
  // ESC [ <params> <intermediates> <final>
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, '');

  // Remove other 2-byte ESC sequences (e.g. ESC c, ESC 7, ESC 8, etc).
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B[@-_]/g, '');

  // Remove leftover non-printable C0 control chars except tab/newline.
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/[\x00-\x08\x0B-\x1F\x7F]/g, '');

  return cleaned;
}

/**
 * Check if text contains ANSI codes
 */
export function hasAnsiCodes(text: string): boolean {
  return stripAnsiCodes(text) !== text;
}
