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
