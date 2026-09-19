import type { QueryCompletedData, QueryProgressData, QueryStartedData, StreamChunkData } from '@sprout/events';
import type { Message } from '@sprout/ui';
import type { AppStoreSetState } from '../../contexts/AppStore';
import { notifyIfHidden } from '../../services/desktopNotify';
import { toQueryProgress } from '../../types/app';
import { ensureCompletedAssistantMessage } from '../../utils/chatCompletion';
import { debugLog } from '../../utils/log';
import { appendCappedLog } from '../../utils/logCap';
import { generateMessageId } from '../../utils/messageId';
import { trimMessages } from '../../utils/messageWindow';
import { createLogEntry, type EventHandlerContext, lastPrimaryAssistantIndex } from '../webSocketEventHelpers';

// Handle query_started event
export const handleQueryStarted = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'query';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as QueryStartedData;
  const startedQuery = String(data.query || '');
  // Wakeup (auto-resume) turns carry a user-facing display text — e.g.
  // "Looking into 'make build'…" — and must never surface the internal
  // [wakeup] batch that goes to the model.
  const isWakeupTurn = String(data.source || '') === 'auto-resume';
  const startedDisplay = isWakeupTurn
    ? String(data.display || 'Checking on a background task…')
    : String(data.display || startedQuery);
  const isClearCommand = startedQuery.trim().toLowerCase() === '/clear';

  // Subagent ProcessQuery calls publish their own query_started through the
  // same bus, decorated with subagent_depth > 0 by SP-051's
  // decorateEventPayload. If we treat those as user prompts we (a) render the
  // subagent's task as a user bubble in the primary chat, and (b) show the
  // same text three places (this user bubble + the run_subagent tool args +
  // the SubagentActivityFeed spawn card) — that is the "messages sent to
  // subagents look like user prompts and get duplicated" bug. Skip the
  // message append for subagent-originated query_started; the rest of the
  // state reset (isProcessing, toolExecutions, etc.) is owned by the
  // primary's query_started so subagents must not touch it either.
  const startedRaw = event.data as Record<string, unknown> | undefined;
  const startedDepth = Number(startedRaw?.subagent_depth ?? 0);
  if (Number.isFinite(startedDepth) && startedDepth > 0) {
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
    debugLog('[>>] Subagent query started (suppressed from chat):', startedQuery);
    return;
  }

  setState((prev) => {
    // Avoid duplicating the user message: handleSendMessage may have already
    // added it optimistically (e.g. for concurrent queries). Only add if the
    // last message is not already a user message with the same content.
    const lastMsg = prev.messages[prev.messages.length - 1];
    const alreadyPresent =
      lastMsg != null &&
      lastMsg.type === 'user' &&
      (lastMsg.content === startedDisplay || lastMsg.content === startedQuery);

    return {
      isProcessing: true,
      lastError: null,
      queryCount: prev.queryCount + 1,
      messages: isClearCommand
        ? prev.messages
        : alreadyPresent
          ? prev.messages
          : [
              ...prev.messages,
              { id: generateMessageId(), type: 'user', content: startedDisplay, timestamp: new Date() },
            ],
      // Preserve historical tool executions across turns. Wiping the array
      // here (the original behavior) broke two visible features: (a)
      // MessageSegments badges on past turns lost their status lookup and
      // regressed to the running-pill render — the "tool badges flash on
      // and off" symptom — and (b) clicking a past-turn tool badge fired
      // contextPanelRef.highlightTool(toolId), which then couldn't find the
      // tool in state and silently no-op'd. handleToolStart tags every new
      // tool with the current queryCount so per-turn views (timeline bar,
      // current-turn activity group) still filter cleanly; old entries
      // stay reachable for lookup and the sidebar's "Earlier" group.
      toolExecutions: prev.toolExecutions,
      fileEdits: [],
      subagentActivities: [],
      queryProgress: null,
      currentTodos: [],
      logs: appendCappedLog(prev.logs, logEntry),
    };
  });
  debugLog('[>>] Query started:', startedQuery);
};

// Handle query_progress event
export const handleQueryProgress = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const data = (event.data ?? {}) as QueryProgressData;
  setState((_prev) => ({ queryProgress: toQueryProgress(data as Record<string, unknown>) }));
  debugLog('[>>] Query progress:', data);
};

/** Flush cadence for buffered stream chunks (≈20 updates/sec ceiling). */
const STREAM_FLUSH_INTERVAL_MS = 48;

/**
 * Pending stream_chunk text, held OUTSIDE React state. Every chunk calling
 * setState directly re-rendered the whole app per token; buffered text is
 * flushed on a timer (or synchronously before any other event routes) so
 * React sees one update per interval instead of one per token.
 */
export interface PendingStreamChunks {
  text: string;
  reasoning: string;
  /** chat_id the chunks belong to — the buffer is discarded if the user
   * switches chats before the flush fires. Tagged from the FIRST chunk in
   * the batch: a chat switch mid-batch drops at most one interval (~48ms)
   * of the new chat's text, recovered by the authoritative transcript
   * fetch on switch-back. */
  chatId: string | undefined;
}

/** Public surface of the stream buffer used by the dispatch loop. */
export interface StreamFlusher {
  buffer: (chunkContent: string, chunkType: string, chatId: string | undefined) => void;
  flush: () => void;
  discard: () => void;
}

/**
 * Build the stream-chunk buffer. Flush appends the buffered text into
 * state using the same append-or-create logic as the old direct path.
 * The dispatch loop calls flush() synchronously before routing any
 * non-chunk event — query_completed must see the full streamed text or
 * its completion heuristics mis-fire.
 */
export const makeStreamFlusher = (
  setState: AppStoreSetState,
  bufferRef: React.MutableRefObject<PendingStreamChunks | null>,
  timerRef: React.MutableRefObject<ReturnType<typeof setTimeout> | null>,
  activeChatIdRef: React.MutableRefObject<string | null>,
): StreamFlusher => {
  const flush = (): void => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    const pending = bufferRef.current;
    bufferRef.current = null;
    if (!pending) return;
    // Chat switched while chunks were buffered — drop them (the new chat's
    // authoritative transcript fetch will render on switch).
    if (pending.chatId !== undefined && activeChatIdRef.current && pending.chatId !== activeChatIdRef.current) {
      return;
    }
    setState((prev) => {
      const newMessages = [...prev.messages];
      const lastMessage = newMessages[newMessages.length - 1];
      // An inline subagent-run message (isSubagentRun) is never a valid
      // append target for primary-agent chunks: its content is rendered
      // inside the subagent's collapsible block. Create a fresh primary
      // assistant message instead.
      const canAppendToLast = lastMessage != null && lastMessage.type === 'assistant' && !lastMessage.isSubagentRun;
      if (canAppendToLast) {
        if (pending.reasoning) {
          newMessages[newMessages.length - 1] = {
            ...lastMessage,
            reasoning: (lastMessage.reasoning || '') + pending.reasoning,
          };
        }
        if (pending.text) {
          newMessages[newMessages.length - 1] = {
            ...newMessages[newMessages.length - 1],
            content: newMessages[newMessages.length - 1].content + pending.text,
          };
        }
      } else {
        const newMsg: Message = {
          id: generateMessageId(),
          type: 'assistant',
          content: pending.text,
          timestamp: new Date(),
        };
        if (pending.reasoning) newMsg.reasoning = pending.reasoning;
        newMessages.push(newMsg);
      }
      return { messages: newMessages };
    });
  };

  const buffer = (chunkContent: string, chunkType: string, chatId: string | undefined): void => {
    if (!bufferRef.current) {
      bufferRef.current = { text: '', reasoning: '', chatId };
      timerRef.current = setTimeout(flush, STREAM_FLUSH_INTERVAL_MS);
    }
    if (chunkType === 'reasoning') {
      bufferRef.current.reasoning += chunkContent;
    } else {
      bufferRef.current.text += chunkContent;
    }
  };

  const discard = (): void => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    bufferRef.current = null;
  };

  return { buffer, flush, discard };
};

// Handle stream_chunk event — buffers the text; the flusher owns state.
export const handleStreamChunk = (ctx: EventHandlerContext, streamFlusher: StreamFlusher): void => {
  const { event } = ctx;

  // Subagent stream_chunk events are decorated with subagent_depth > 0.
  // Without this guard, the subagent's LLM output gets appended to the
  // primary agent's last assistant message — the subagent's prose shows
  // up mixed into the main chat response. Subagent output is surfaced
  // through the SubagentActivityFeed and the run_subagent tool result.
  const streamRaw = event.data as Record<string, unknown> | undefined;
  const streamDepth = Number(streamRaw?.subagent_depth ?? 0);
  if (Number.isFinite(streamDepth) && streamDepth > 0) {
    return;
  }

  const data = (event.data ?? {}) as StreamChunkData;
  const chunkContent = String(data.chunk || '');
  const chunkType = String(data.content_type || 'assistant_text');
  if (!chunkContent) return;

  const eventChatId = typeof data.chat_id === 'string' && data.chat_id ? data.chat_id : undefined;
  streamFlusher.buffer(chunkContent, chunkType, eventChatId);
};

// Handle query_completed event
export const handleQueryCompleted = (ctx: EventHandlerContext): void => {
  const { event, setState, activeRequestsRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'query';
  logEntry.level = 'success';
  const data = (event.data ?? {}) as QueryCompletedData;
  const completedQuery = String(data.query || '')
    .trim()
    .toLowerCase();
  const completedResponse = data.response;
  const wasClearCommand = completedQuery === '/clear';
  const tokensUsed = typeof data.tokens_used === 'number' ? data.tokens_used : undefined;
  const cost = typeof data.cost === 'number' ? data.cost : undefined;

  // Mirror the handleQueryStarted guard: subagent ProcessQuery calls fire
  // their own query_completed through the same bus, decorated with
  // subagent_depth > 0. If we ran the full primary-completion path we would
  // (a) decrement activeRequestsRef before the primary actually finishes,
  // prematurely flipping isProcessing to false, and (b) inject the
  // subagent's response into the main chat as a second assistant bubble
  // next to the run_subagent tool result — the response side of the same
  // duplication the user reported. The subagent's output is already
  // surfaced through SubagentActivityFeed and the run_subagent tool
  // execution card.
  const completedRaw = event.data as Record<string, unknown> | undefined;
  const completedSubDepth = Number(completedRaw?.subagent_depth ?? 0);
  if (Number.isFinite(completedSubDepth) && completedSubDepth > 0) {
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
    debugLog('[OK] Subagent query completed (suppressed from chat):', completedQuery);
    return;
  }

  if (activeRequestsRef.current > 0) activeRequestsRef.current -= 1;

  setState((prev) => {
    let nextMessages = wasClearCommand
      ? []
      : ensureCompletedAssistantMessage(prev.messages, completedResponse, (responseText) => ({
          id: generateMessageId(),
          type: 'assistant',
          content: responseText,
          timestamp: new Date(),
        }));

    if (!wasClearCommand && nextMessages.length > 0) {
      const lastMsg = nextMessages[nextMessages.length - 1] as Message;
      if (
        lastMsg.type === 'assistant' &&
        !lastMsg.isSubagentRun &&
        lastMsg.reasoning?.trim() &&
        lastMsg.content?.trim() &&
        lastMsg.content === lastMsg.reasoning
      ) {
        nextMessages = [...nextMessages.slice(0, -1), { ...lastMsg, reasoning: undefined }];
      }
    }

    // SP-053-perTurnCost: annotate the turn's primary assistant message with
    // per-turn cost. Never an inline subagent-run message — the cost belongs
    // to the primary turn, not the delegated run.
    if (!wasClearCommand && (tokensUsed != null || cost != null)) {
      const idx = lastPrimaryAssistantIndex(nextMessages);
      if (idx >= 0) {
        const annotated: Message = { ...nextMessages[idx] };
        if (tokensUsed != null) annotated.tokensUsed = tokensUsed;
        if (cost != null) annotated.cost = cost;
        nextMessages = [...nextMessages.slice(0, idx), annotated, ...nextMessages.slice(idx + 1)];
      }
    }

    if (!wasClearCommand) nextMessages = trimMessages(nextMessages);

    return {
      messages: nextMessages,
      currentTodos: wasClearCommand ? [] : prev.currentTodos,
      isProcessing: activeRequestsRef.current > 0,
      lastError: null,
      queryProgress: null,
      toolExecutions: wasClearCommand
        ? []
        : prev.toolExecutions.map((tool) => {
            if (tool.status === 'started' || tool.status === 'running') {
              return { ...tool, status: 'completed', endTime: tool.endTime || new Date() };
            }
            return tool;
          }),
      logs: appendCappedLog(prev.logs, logEntry),
    };
  });
  debugLog('[OK] Query completed');
  // SP-070-4: desktop notification when tab is backgrounded
  notifyIfHidden('Sprout', 'Task complete');
};
