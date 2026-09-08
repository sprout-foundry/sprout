/**
 * Human-readable error message extraction.
 *
 * UI surfaces (panels, banners, toasts) should never render raw exception
 * text: stack frames, multi-line dumps, or `Error: ` prefixes read as noise
 * on a tablet. `toUserErrorMessage` normalizes an unknown thrown value into
 * one short, single-line sentence with a caller-supplied fallback.
 */

/** Strip the leading `Error: ` that V8 prepends to Error#message. */
const ERROR_PREFIX_RE = /^(?:\w*Error|Uncaught \w*Error):\s*/;

/** Stack traces start on their own line — keep at most the first. */
const FIRST_LINE_RE = /[^\n]*/;

/** Cap so a runaway message cannot blow out a banner or toast layout. */
const MAX_MESSAGE_LENGTH = 300;

/**
 * Convert a thrown value into a short, human-readable message.
 *
 * - `Error` → its `message` (first line only, `Error: ` prefix stripped)
 * - string → used as-is (first line)
 * - anything else (object, number, undefined) → `fallback`
 */
export function toUserErrorMessage(err: unknown, fallback: string): string {
  let message = '';
  if (err instanceof Error) {
    message = err.message || '';
  } else if (typeof err === 'string') {
    message = err;
  }

  if (!message.trim()) return fallback;

  const firstLine = (message.match(FIRST_LINE_RE) ?? [''])[0];
  const stripped = firstLine.replace(ERROR_PREFIX_RE, '').trim();
  if (!stripped) return fallback;

  return stripped.length > MAX_MESSAGE_LENGTH ? `${stripped.slice(0, MAX_MESSAGE_LENGTH)}…` : stripped;
}
