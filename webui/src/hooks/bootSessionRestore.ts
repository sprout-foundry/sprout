import type { ChatSession } from '../services/chatSessions';

/**
 * Whether boot may fall back to restoring the most recent non-empty session
 * into the active chat. That fallback predates chats: with more than one
 * chat, "the most recent session" is usually another chat's conversation,
 * and restoring it overwrote the active chat's transcript on reload. It
 * stays only for a lone, empty chat.
 */
export function canAutoRestoreLatestSession(chats: Pick<ChatSession, 'message_count'>[]): boolean {
  return chats.length <= 1 && Number(chats[0]?.message_count ?? 0) === 0;
}

const LAST_CLEAR_KEY = 'sprout:last-clear-at';

/**
 * Remembers that the user cleared a conversation. A clear means "start fresh
 * from here": a later boot that finds the lone chat empty must not restore a
 * conversation from before it.
 */
export function recordConversationCleared(now: number = Date.now()): void {
  try {
    window.localStorage.setItem(LAST_CLEAR_KEY, String(now));
  } catch {
    // Storage unavailable: the fallback may restore a cleared conversation.
  }
}

/** Whether a saved session was last updated before the user's last clear. */
export function clearedByUser(lastUpdated: string | undefined): boolean {
  let clearedAt = 0;
  try {
    clearedAt = Number(window.localStorage.getItem(LAST_CLEAR_KEY) ?? 0);
  } catch {
    return false;
  }
  const updatedAt = Date.parse(lastUpdated ?? '');
  return clearedAt > 0 && Number.isFinite(updatedAt) && updatedAt <= clearedAt;
}
