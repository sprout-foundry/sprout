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

export async function stopQuery(fetchFn: typeof fetch): Promise<void> {
  const response = await fetchFn('/api/query/stop', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to stop query');
}

export async function recordDriftResponse(fetchFn: typeof fetch, startedNewChat: boolean): Promise<void> {
  const response = await fetchFn('/api/drift-response', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ startedNewChat }),
  });
  if (!response.ok) throw new Error('Failed to record drift response');
}
