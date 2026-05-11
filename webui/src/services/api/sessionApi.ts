/**
 * Session domain API — adapter-aware session operations.
 */

import type { SessionsResponse, SessionRestoreResponse } from './types';

export async function getSessions(fetchFn: typeof fetch, scope?: string): Promise<SessionsResponse> {
  const url = scope ? `/api/sessions?scope=${encodeURIComponent(scope)}` : '/api/sessions';
  const response = await fetchFn(url);
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function restoreSession(fetchFn: typeof fetch, sessionId: string): Promise<SessionRestoreResponse> {
  const response = await fetchFn('/api/sessions/restore', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}
