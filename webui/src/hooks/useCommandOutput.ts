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
import { useCallback, useEffect, useRef, useState } from 'react';
import { WebSocketService } from '../services/websocket';

export interface CommandOutputState {
  output: string;
  isRunning: boolean;
  droppedBytes: number;
  /** Characters trimmed from the head because the output exceeded the cap. */
  truncatedChars: number;
  command: string | null;
  error: Error | null;
}

/**
 * Chunks are batched into one state update per window (leading edge: the
 * first chunk of a burst applies immediately). Every update copies the
 * retained string and re-renders the chat, so per-chunk updates made long
 * command output quadratic.
 */
export const COMMAND_OUTPUT_FLUSH_MS = 100;

/**
 * Retained-output cap. A command can stream megabytes; the panel only needs a
 * readable tail, and the cost of each flush scales with what is retained.
 */
export const MAX_COMMAND_OUTPUT_CHARS = 200_000;

const INITIAL_STATE: CommandOutputState = {
  output: '',
  isRunning: false,
  droppedBytes: 0,
  truncatedChars: 0,
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

  // Chunks wait here between flushes. An array of chunks joined once per
  // flush avoids re-copying the whole output for every chunk.
  const pendingChunksRef = useRef<string[]>([]);
  const pendingCommandRef = useRef<string | null>(null);
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastFlushRef = useRef(0);

  /** Apply the buffered chunks. `isRunning` undefined leaves it unchanged. */
  const flush = useCallback((isRunning?: boolean) => {
    if (flushTimerRef.current !== null) {
      clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    const chunks = pendingChunksRef.current;
    const commandName = pendingCommandRef.current;
    pendingChunksRef.current = [];
    pendingCommandRef.current = null;
    lastFlushRef.current = Date.now();
    if (chunks.length === 0 && commandName === null && isRunning === undefined) return;
    const appended = chunks.join('');

    setState((prev) => {
      // Is this chunk the start of a NEW command (compared with the command
      // we're currently tracking)? `null` command names mean "missing", which
      // we treat as "same as whatever we had".
      const switchingCommand = commandName !== null && prev.command !== null && commandName !== prev.command;
      const isFirstChunk = prev.command === null && commandName !== null;
      const resets = switchingCommand || isFirstChunk;

      let output = resets ? appended : prev.output + appended;
      // The trim counter belongs to the retained output: a new command starts
      // clean, otherwise one trimmed command would keep warning about the
      // text of every command after it.
      let truncatedChars = resets ? 0 : prev.truncatedChars;
      if (output.length > MAX_COMMAND_OUTPUT_CHARS) {
        truncatedChars += output.length - MAX_COMMAND_OUTPUT_CHARS;
        output = output.slice(-MAX_COMMAND_OUTPUT_CHARS);
      }

      return {
        ...prev,
        command: commandName ?? prev.command,
        output,
        truncatedChars,
        ...(isRunning === undefined ? {} : { isRunning }),
      };
    });
  }, []);

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
        // Land the buffered text first so the warning appears after the
        // output it describes.
        flush();
        const dropped = typeof data.dropped_bytes === 'number' ? data.dropped_bytes : 0;
        setState((prev) => ({ ...prev, droppedBytes: prev.droppedBytes + dropped }));
        return;
      }

      // event.type === 'command_output'
      const isFinal = data.is_final === true;
      const chunk = typeof data.chunk === 'string' ? data.chunk : '';
      const commandName = typeof data.command === 'string' ? data.command : null;
      // A command switch inside a batch would be collapsed by the join below
      // (the whole batch would be attributed to the new command). Flush the
      // previous command's tail first so each batch carries one command.
      if (pendingCommandRef.current !== null && commandName !== null && commandName !== pendingCommandRef.current) {
        flush(true);
      }
      const startsBatch = pendingChunksRef.current.length === 0;
      pendingChunksRef.current.push(chunk);
      if (commandName !== null) pendingCommandRef.current = commandName;

      // The terminator is never delayed: the panel's final state must land
      // with the last chunk.
      if (isFinal) {
        flush(false);
        return;
      }

      const now = Date.now();
      if (startsBatch && now - lastFlushRef.current >= COMMAND_OUTPUT_FLUSH_MS) {
        flush(true);
        return;
      }
      if (flushTimerRef.current === null) {
        const delay = Math.max(0, COMMAND_OUTPUT_FLUSH_MS - (now - lastFlushRef.current));
        flushTimerRef.current = setTimeout(() => {
          flushTimerRef.current = null;
          flush(true);
        }, delay);
      }
    };

    ws.onEvent(handle);
    return () => {
      ws.removeEvent(handle);
      if (flushTimerRef.current !== null) {
        clearTimeout(flushTimerRef.current);
        flushTimerRef.current = null;
      }
      // A chat switch or unmount drops the tail; the command's HTTP response
      // carries the complete transcript.
      pendingChunksRef.current = [];
      pendingCommandRef.current = null;
    };
  }, [chatId, flush]);

  // Reset state when the chat changes so a /info from chat-A doesn't
  // bleed into chat-B's first render.
  useEffect(() => {
    setState(INITIAL_STATE);
  }, [chatId]);

  return state;
}

export default useCommandOutput;
