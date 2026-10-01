import type { Message } from '@sprout/ui';
import type { ChatSessionSwitchResponseChatSession } from '../services/chatSessions';

type TranscriptEntry = NonNullable<ChatSessionSwitchResponseChatSession['messages']>[number];

/**
 * A chat's server transcript as chat messages: user and assistant turns only.
 * An assistant turn with neither text nor reasoning is the tool-calling step
 * of a run — its tool calls aren't part of this view, so rendering it would
 * leave an empty "(no response text)" bubble. Ids follow the transcript
 * position, so they stay stable whether or not such turns are skipped.
 */
export function chatTranscriptToMessages(chatId: string, entries: TranscriptEntry[] | undefined): Message[] {
  const out: Message[] = [];
  (entries ?? []).forEach((m, i) => {
    const role = m.role;
    if (role !== 'user' && role !== 'assistant') return;
    const content = typeof m.content === 'string' ? m.content : '';
    const reasoning = m.reasoning_content ?? '';
    if (role === 'assistant' && !content.trim() && !reasoning.trim()) return;
    out.push({
      id: `chat-${chatId}-${i}`,
      type: role,
      content,
      timestamp: new Date(),
      ...(reasoning ? { reasoning } : {}),
    });
  });
  return out;
}
