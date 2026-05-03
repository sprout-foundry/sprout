import type { Message } from '@sprout/ui';

/**
 * Maximum number of messages to keep in React state.
 * Older messages are trimmed to bound memory usage during long sessions.
 * The limit is only enforced when a query completes (not during streaming).
 */
export const DEFAULT_MAX_MESSAGES = 200;

/**
 * Trim a messages array to the last `maxSize` entries.
 * Returns the same array reference if already within bounds (avoids allocations).
 * Drops older messages from the head of the array. Earlier conversation turns
 * are lost after trimming (acceptable since the server maintains its own context).
 */
export function trimMessages(messages: Message[], maxSize: number = DEFAULT_MAX_MESSAGES): Message[] {
  if (messages.length <= maxSize) {
    return messages;
  }
  return messages.slice(-maxSize);
}
