import type { WsEvent } from '@sprout/events';
import type { Message } from '@sprout/ui';

/** Background chats keep at most this many queued events; older ones are dropped. */
export const PENDING_EVENTS_CAP = 200;

const REPLAY_CHAT_EVENTS = 'sprout:replay-chat-events';

interface ReplayDetail {
  chatId: string;
  events: WsEvent[];
}

/**
 * Asks the WebSocket event handler to apply events a chat received while it
 * wasn't on screen, in order, as if they had just arrived.
 */
export function requestChatReplay(chatId: string, events: WsEvent[]): void {
  if (events.length === 0) return;
  window.dispatchEvent(new CustomEvent<ReplayDetail>(REPLAY_CHAT_EVENTS, { detail: { chatId, events } }));
}

export function onChatReplay(listener: (chatId: string, events: WsEvent[]) => void): () => void {
  const handler = (e: Event) => {
    const detail = (e as CustomEvent<ReplayDetail>).detail;
    if (detail?.chatId && Array.isArray(detail.events)) listener(detail.chatId, detail.events);
  };
  window.addEventListener(REPLAY_CHAT_EVENTS, handler);
  return () => window.removeEventListener(REPLAY_CHAT_EVENTS, handler);
}

function isPrimary(event: WsEvent): boolean {
  const depth = Number((event.data as Record<string, unknown> | undefined)?.subagent_depth ?? 0);
  return !(Number.isFinite(depth) && depth > 0);
}

/** Whether a chat still has a run in flight after these events. */
export function processingAfter(wasProcessing: boolean, events: WsEvent[]): boolean {
  let processing = wasProcessing;
  for (const event of events) {
    if (!isPrimary(event)) continue;
    if (event.type === 'query_started') processing = true;
    else if (event.type === 'query_completed' || event.type === 'error' || event.type === 'session_terminated') {
      processing = false;
    }
  }
  return processing;
}

/** Whether these events include the end of a primary run. */
export function endsRun(events: WsEvent[]): boolean {
  return events.some(
    (e) => isPrimary(e) && (e.type === 'query_completed' || e.type === 'error' || e.type === 'session_terminated'),
  );
}

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

/**
 * What a background chat looks like with its queued events applied: the
 * question, streamed answer text and tool badges of runs it made while off
 * screen. A display-only preview for inactive panes — switching to the chat
 * replays the events through the full handlers instead.
 */
export function previewWithPending(messages: Message[], pending: WsEvent[] | undefined): Message[] {
  if (!pending?.length) return messages;
  const out = [...messages];
  let seq = 0;
  const answer = (): Message => {
    const last = out[out.length - 1];
    if (last && last.type === 'assistant' && !last.isSubagentRun) return last;
    const created: Message = { id: `preview-${seq++}`, type: 'assistant', content: '', timestamp: new Date() };
    out.push(created);
    return created;
  };
  const replaceLast = (next: Message) => {
    out[out.length - 1] = next;
  };
  for (const event of pending) {
    if (!isPrimary(event)) continue;
    const data = asRecord(event.data);
    switch (event.type) {
      case 'query_started': {
        const text = String(data.display_query || data.query || '');
        const last = out[out.length - 1];
        if (text && !(last && last.type === 'user' && last.content === text)) {
          out.push({ id: `preview-${seq++}`, type: 'user', content: text, timestamp: new Date() });
        }
        break;
      }
      case 'stream_chunk': {
        if (String(data.content_type || 'assistant_text') !== 'assistant_text') break;
        const target = answer();
        replaceLast({ ...target, content: target.content + String(data.chunk || '') });
        break;
      }
      case 'tool_start': {
        const target = answer();
        const toolId = String(data.tool_call_id || `preview-tool-${seq++}`);
        const toolName = String(data.tool_name || 'tool');
        const refs = [...(target.toolRefs ?? [])];
        if (!refs.some((r) => r.toolId === toolId)) {
          refs.push({ toolId, toolName, label: String(data.display_name || toolName) });
        }
        replaceLast({ ...target, toolRefs: refs });
        break;
      }
      case 'query_completed': {
        const response = typeof data.response === 'string' ? data.response : '';
        const last = out[out.length - 1];
        if (response && !(last && last.type === 'assistant' && last.content.trim())) {
          const target = answer();
          replaceLast({ ...target, content: response });
        }
        break;
      }
    }
  }
  return out;
}
