import { memo, useEffect, useRef, useState } from 'react';
import { Info } from 'lucide-react';
import type { SproutEvent } from '@sprout/events';
import { useEvents } from '../../contexts/EventsContext';
import { progressEventSummary } from '../../utils/progressSummary';
import './ProgressStrip.css';

export interface ProgressStripProps {
  /**
   * Active chat id, passed through from ChatProps.chatId by the chat
   * surface. The events bus is shared across chats, so a payload can
   * carry another chat's routing id; the strip only reacts to events for
   * the chat it is rendered in, or to chatless payloads (which predate
   * per-chat routing and have no other sensible owner).
   */
  chatId?: string;
}

/**
 * Read the routing chat_id off an event's data payload. Absent/empty means
 * the event was published without per-chat routing — such events pass the
 * chat filter in every chat, matching how the stream buffer and
 * useCommandOutput treat unscoped payloads.
 */
function eventChatId(e: SproutEvent): string | undefined {
  const data = e.data;
  if (!data || typeof data !== 'object') return undefined;
  const candidate = (data as Record<string, unknown>).chat_id;
  return typeof candidate === 'string' && candidate.length > 0 ? candidate : undefined;
}

/**
 * Compact SP-151 progress strip (item 151.7, SP-151 §151c): a memo'd
 * one-liner in the chat rendering the latest deterministic template
 * summary of the active run's progress events — the same text the CLI
 * renders (item 151.6). No model call, no invented detail.
 *
 * Subscribes to the events transport the way useGitWorkspace does: a
 * stable callback registered via onEvent/removeEvent in a single effect.
 * The latest non-empty summary wins; a `query_started` event (a new
 * run) clears the stored summary so the previous run's state is not
 * shown.
 *
 * The strip filters the shared bus by chat: an event is accepted when
 * its `chat_id` is absent/empty (singleton/legacy payload) or equals the
 * `chatId` prop, and ignored when it names a different chat. The
 * top-level `chat_id` is the right key for milestone batches too — the
 * stream coalescer copies the route keys onto the batch envelope — so
 * flat and batched payloads filter identically. Switching chats clears
 * the stored summary (effect on `chatId`), so a chat never inherits the
 * previous chat's progress line.
 *
 * Self-hides (renders null) when there is no stored summary — nothing
 * to show yet, or a fresh run.
 */
function ProgressStripInner({ chatId }: ProgressStripProps): JSX.Element | null {
  const events = useEvents();
  const [summary, setSummary] = useState('');
  // Latest chatId readable from the event callback: the subscription is
  // stable (registered once per mount), so the guard reads a ref instead
  // of closing over a stale prop value.
  const chatIdRef = useRef(chatId);
  chatIdRef.current = chatId;

  // Switching chats must not carry the previous chat's progress line into
  // the new one. Clearing on the prop change covers every path that swaps
  // the active chat (switch, restore, new chat) without waiting for the
  // new chat's next event.
  useEffect(() => {
    setSummary('');
  }, [chatId]);

  useEffect(() => {
    const handleEvent = (e: SproutEvent): void => {
      const eventChat = eventChatId(e);
      const activeChat = chatIdRef.current;
      // Multi-chat guard: the bus carries every chat's events. Drop an
      // event only when BOTH sides are known and differ — a chatless
      // payload (no routing stamp) is accepted everywhere, and an event
      // arriving before the active chat id resolves is not misfiled as
      // foreign. Mirrors the stream buffer's chat check.
      if (eventChat !== undefined && activeChat !== undefined && eventChat !== activeChat) {
        return;
      }
      if (e.type === 'query_started') {
        setSummary('');
        return;
      }
      const next = progressEventSummary(e.type, e.data);
      if (next !== '') setSummary(next);
    };
    events.onEvent(handleEvent);
    return () => {
      events.removeEvent(handleEvent);
    };
  }, [events]);

  if (summary === '') return null;

  return (
    <div className="progress-strip" data-testid="progress-strip" role="status" aria-live="polite">
      <Info size={12} aria-hidden="true" />
      <span className="progress-strip-text">{summary}</span>
    </div>
  );
}

export const ProgressStrip = memo(ProgressStripInner);

export default ProgressStrip;
