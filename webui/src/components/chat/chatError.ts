/** Prefix the chat handlers put on a failed turn's transcript entry. */
export const CHAT_ERROR_PREFIX = '[FAIL] Error: ';

/**
 * The error text of a failed-turn message, or null for any other content.
 * These entries must not go through the segment parser: "[FAIL] …" is also
 * its tool-result syntax, which it renders as nothing.
 */
export function chatErrorText(content: string | undefined): string | null {
  if (!content || !content.startsWith(CHAT_ERROR_PREFIX)) return null;
  return content.slice(CHAT_ERROR_PREFIX.length);
}
