/**
 * useCommandOutput — SP-114 Phase 2d client-side wire-up.
 *
 * Subscribes to the `command_output` and `command_output_dropped` WebSocket
 * events emitted by the server-side `commandOutputStreamer` (see
 * pkg/webui/api_command.go) and exposes the streaming state to React
 * components.
 *
 * Wire shape (server → client, see pkg/webui/api_query.go::publishClientEventWithChat):
 *   - The server stamps `chat_id`, `client_id`, and `user_id` onto the event
 *     data before publishing. So the WsEvent arriving here carries:
 *       { type: 'command_output', data: { command, chunk, is_final, seq, chat_id, client_id, ... } }
 *   - command_output_dropped carries:
 *       { type: 'command_output_dropped', data: { command, dropped_bytes, chat_id, client_id, ... } }
 *
 * Filtering: if the event data carries a `chat_id` field, the hook only
 * updates state when it matches the `chatId` argument. This matches how
 * `useWebSocketEventHandler` filters per-chat events (see
 * `perChatEvents` check there). When `chatId` is undefined we still
 * subscribe but accept every chat's events — the caller is responsible
 * for what to do with them.
 *
 * v1 simplification: only the most recent command per chat is tracked.
 * If the user runs /info then /help, the /info output is dropped as soon
 * as /help starts streaming. This is the panel-as-modal-command-output
 * shape; a v2 with a history list per chat is a future task.
 *
 * Reconnect / early-event behaviour: WebSocketService.getInstance() is a
 * singleton; if the user submits a command DURING a brief reconnect the
 * server still emits the events through the bus. On reconnect the
 * service re-invokes callbacks; chunks can land in this hook BEFORE
 * the user even has a chance to submit (i.e. stale events from a prior
 * command that was in flight when the socket died). We accept this:
 * the hook subscribes unconditionally whenever mounted, and any
 * matching chat_id updates state. The component owner gates the panel
 * on a panelVisible flag set in handleSendCommand, so stale events
 * simply leave the panel hidden. This is documented here as a
 * conscious trade-off — gating on a per-command UUID would require
 * the server to stamp an "active command id" into the response and
 * the hook to track it, which is scope creep for this phase.
 */

import type { SproutEvent } from '@sprout/events';
import { useEffect, useState } from 'react';
import { WebSocketService } from '../services/websocket';

export interface CommandOutputState {
  output: string;
  isRunning: boolean;
  droppedBytes: number;
  command: string | null;
  error: Error | null;
}

const INITIAL_STATE: CommandOutputState = {
  output: '',
  isRunning: false,
  droppedBytes: 0,
  command: null,
  error: null,
};

/**
 * Pull the chat_id (if any) out of an event's data field. Server-side
 * `publishClientEventWithChat` may or may not stamp a chat_id (it omits
 * it only if TrimSpace is empty). We treat undefined and non-string as
 * "no chat scope" → events pass through without further filtering.
 */
function eventChatId(event: SproutEvent): string | undefined {
  const data = (event as SproutEvent & { data?: unknown }).data;
  if (!data || typeof data !== 'object') return undefined;
  const candidate = (data as Record<string, unknown>).chat_id;
  return typeof candidate === 'string' && candidate.length > 0 ? candidate : undefined;
}

export function useCommandOutput(chatId: string | undefined): CommandOutputState {
  const [state, setState] = useState<CommandOutputState>(INITIAL_STATE);

  useEffect(() => {
    const ws = WebSocketService.getInstance();

    const handle = (event: SproutEvent) => {
      if (event.type !== 'command_output' && event.type !== 'command_output_dropped') {
        return;
      }
      const eventChat = eventChatId(event);
      // Filter by chat_id when both sides have one. If the event has no
      // chat_id (server-side chatId was empty) we still accept it — the
      // server only emits chat-scoped events when called via the
      // chat-resolved /api/command/execute path, so this is rare and
      // not a user-visible leak.
      if (chatId !== undefined && eventChat !== undefined && eventChat !== chatId) {
        return;
      }

      const data = (event.data ?? {}) as Record<string, unknown>;

      if (event.type === 'command_output_dropped') {
        const dropped = typeof data.dropped_bytes === 'number' ? data.dropped_bytes : 0;
        setState((prev) => ({ ...prev, droppedBytes: prev.droppedBytes + dropped }));
        return;
      }

      // event.type === 'command_output'
      const isFinal = data.is_final === true;
      const chunk = typeof data.chunk === 'string' ? data.chunk : '';
      const commandName = typeof data.command === 'string' ? data.command : null;

      setState((prev) => {
        // Is this chunk the start of a NEW command (compared with the
        // command we're currently tracking)? `null` command names mean
        // "missing", which we treat as "same as whatever we had".
        const switchingCommand = commandName !== null && prev.command !== null && commandName !== prev.command;
        const isFirstChunk = prev.command === null && commandName !== null;
        return {
          command: switchingCommand || isFirstChunk ? commandName : (prev.command ?? commandName),
          output: switchingCommand || isFirstChunk ? chunk : prev.output + chunk,
          isRunning: isFinal ? false : true,
          droppedBytes: prev.droppedBytes,
          error: prev.error,
        };
      });
    };

    ws.onEvent(handle);
    return () => {
      ws.removeEvent(handle);
    };
  }, [chatId]);

  // Reset state when the chat changes so a /info from chat-A doesn't
  // bleed into chat-B's first render.
  useEffect(() => {
    setState(INITIAL_STATE);
  }, [chatId]);

  return state;
}

export default useCommandOutput;
