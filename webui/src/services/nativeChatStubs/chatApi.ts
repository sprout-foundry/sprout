/**
 * Track R (--native-chat, R-4) stub for services/api/chatApi.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/api/chatApi for this module when the dist build is invoked with
 * VITE_SPROUT_NATIVE_CHAT=1 (scripts/build-webui-dist.mjs --native-chat).
 * The fetch/SSE agent-turn chat transport is provided natively by the shell,
 * so the real module (POST /api/query, /api/query/steer, /api/query/stop,
 * /api/query/rewind, /api/command/execute, /api/upload/image) is
 * hard-excluded from the bundle.
 *
 * This file is a no-op stand-in: it mirrors the REAL public signatures of
 * `chatApi` (copied from the real module so `tsc --noEmit` passes in the
 * --native-chat build too) but every function is a safe no-op that NEVER
 * issues a fetch, NEVER logs, and never touches the network. It resolves
 * inert values matching the real return types. The type-only import
 * (`UploadImageResponse` from `../api/types`) is erased at compile time, so
 * it pulls no real code into the bundle.*
 * See docs/adr-0008-webui-native-seams.md (chat seam) and
 * docs/WEBUI_DECOUPLING_AUDIT.md §1.3. In a --native-chat dist the shell owns
 * the chat loop, so the webui never issues agent-turn HTTP itself.
 */

import type { UploadImageResponse } from '../api/types';

/**
 * Track R: chat is provided natively by the shell, so the webui never POSTs
 * /api/query. Resolves void — a safe no-op matching the real signature.
 */
export async function sendQuery(_fetchFn: typeof fetch, _query: string, _chatId?: string): Promise<void> {
  // no-op: the native shell owns the agent-turn chat loop; no network here.
}

/**
 * Track R: chat is provided natively by the shell, so the webui never uploads
 * an image to /api/upload/image. Resolves an inert UploadImageResponse-shaped
 * value (no bytes, no network).
 */
export async function uploadImage(_fetchFn: typeof fetch, _file: File | Blob): Promise<UploadImageResponse> {
  return { path: '', filename: '' };
}

/**
 * Track R: chat is provided natively by the shell, so the webui never POSTs
 * /api/query/steer. Resolves void — a safe no-op matching the real signature.
 */
export async function steerQuery(_fetchFn: typeof fetch, _query: string, _chatId?: string): Promise<void> {
  // no-op: the native shell owns steering; no network here.
}

export interface RetractSteerResponse {
  success: boolean;
  message: string;
}

/**
 * Track R: chat is provided natively by the shell, so the webui never POSTs
 * /api/query/steer/retract. Resolves an inert success response (no network).
 */
export async function retractSteer(_fetchFn: typeof fetch, _chatId?: string): Promise<RetractSteerResponse> {
  return { success: true, message: 'Chat provided by the native shell' };
}

/**
 * SP-114 Phase 2: execute a slash command on the dedicated /api/command/execute
 * surface. Returns the captured stdout from the command. Throws on error.
 *
 * Unlike steerQuery this endpoint does not require an active query — it's the
 * canonical surface for invoking safe read-only / config commands from the
 * WebUI command bar at any time.
 */
export interface ExecuteCommandResponse {
  command: string;
  output: string;
  error: string;
  accepted: boolean;
}

/**
 * Track R: chat is provided natively by the shell, so the webui never POSTs
 * /api/command/execute. Resolves an inert ExecuteCommandResponse-shaped value
 * (empty output, not accepted — no network, no command runs).
 */
export async function executeCommand(
  _fetchFn: typeof fetch,
  command: string,
  _chatId?: string,
): Promise<ExecuteCommandResponse> {
  return { command, output: '', error: 'Chat provided by the native shell', accepted: false };
}

/**
 * Track R: chat is provided natively by the shell, so the webui never POSTs
 * /api/query/stop. Resolves void — a safe no-op matching the real signature.
 */
export async function stopQuery(_fetchFn: typeof fetch): Promise<void> {
  // no-op: the native shell owns the query lifecycle; no network here.
}

export interface RewindResponse {
  turns_discarded: number;
  messages_removed: number;
  files_reverted: string[];
  files_skipped: string[];
  checkpoints_dropped: number;
}

/**
 * Track R: chat is provided natively by the shell, so the webui never POSTs
 * /api/query/rewind. Resolves an inert RewindResponse-shaped value (zeroed
 * counts, no network, no checkpoints dropped).
 */
export async function rewindQuery(
  _fetchFn: typeof fetch,
  _toTurn: number,
  _revertFiles: boolean = true,
  _chatId?: string,
): Promise<RewindResponse> {
  return {
    turns_discarded: 0,
    messages_removed: 0,
    files_reverted: [],
    files_skipped: [],
    checkpoints_dropped: 0,
  };
}
