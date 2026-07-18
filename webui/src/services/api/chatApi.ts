/**
 * Chat/Agent domain API — adapter-aware chat operations.
 */

import type { UploadImageResponse } from './types';

export async function sendQuery(fetchFn: typeof fetch, query: string, chatId?: string): Promise<void> {
  const reqBody: Record<string, string> = { query };
  if (chatId) reqBody.chat_id = chatId;
  const response = await fetchFn('/api/query', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(reqBody),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Query failed' }));
    throw new Error(data.message || data.error || 'Failed to send query');
  }
}

export async function uploadImage(fetchFn: typeof fetch, file: File | Blob): Promise<UploadImageResponse> {
  const formData = new FormData();
  formData.append('image', file);
  const response = await fetchFn('/api/upload/image', { method: 'POST', body: formData });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Upload failed' }));
    throw new Error(data.message || data.error || 'Failed to upload image');
  }
  return response.json();
}

export async function steerQuery(fetchFn: typeof fetch, query: string, chatId?: string): Promise<void> {
  const reqBody: Record<string, string> = { query };
  if (chatId) reqBody.chat_id = chatId;
  const response = await fetchFn('/api/query/steer', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(reqBody),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Steer failed' }));
    throw new Error(data.message || data.error || 'Failed to steer query');
  }
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

export async function executeCommand(
  fetchFn: typeof fetch,
  command: string,
  chatId?: string,
): Promise<ExecuteCommandResponse> {
  const reqBody: Record<string, string> = { command };
  if (chatId) reqBody.chat_id = chatId;
  const response = await fetchFn('/api/command/execute', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(reqBody),
  });
  const data = (await response.json().catch(() => ({}))) as Partial<ExecuteCommandResponse> & {
    error?: string;
    message?: string;
  };
  if (!response.ok) {
    throw new Error(data.error || data.message || `Command failed (HTTP ${response.status})`);
  }
  return {
    command: data.command ?? '',
    output: data.output ?? '',
    error: data.error ?? '',
    accepted: data.accepted ?? false,
  };
}

export async function stopQuery(fetchFn: typeof fetch): Promise<void> {
  const response = await fetchFn('/api/query/stop', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to stop query');
}

export interface RewindResponse {
  turns_discarded: number;
  messages_removed: number;
  files_reverted: string[];
  files_skipped: string[];
  checkpoints_dropped: number;
}

export async function rewindQuery(
  fetchFn: typeof fetch,
  toTurn: number,
  revertFiles: boolean = true,
  chatId?: string,
): Promise<RewindResponse> {
  const reqBody: Record<string, unknown> = { to_turn: toTurn, revert_files: revertFiles };
  if (chatId) reqBody.chat_id = chatId;
  const response = await fetchFn('/api/query/rewind', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(reqBody),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Rewind failed' }));
    throw new Error(data.message || data.error || 'Failed to rewind query');
  }
  return response.json();
}
