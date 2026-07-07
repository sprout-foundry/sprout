/**
 * Session domain API — adapter-aware session operations.
 */

import type { SessionRestoreResponse, SessionSearchResponse, SessionsResponse } from './types';

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

export async function searchSessions(
  fetchFn: typeof fetch,
  query: string,
  options?: {
    cwd?: string;
    since?: string;
    until?: string;
    limit?: number;
  },
): Promise<SessionSearchResponse> {
  const params = new URLSearchParams({ q: query });
  if (options?.cwd) params.set('cwd', options.cwd);
  if (options?.since) params.set('since', options.since);
  if (options?.until) params.set('until', options.until);
  if (options?.limit) params.set('limit', String(options.limit));
  const url = `/api/sessions/search?${params.toString()}`;
  const response = await fetchFn(url);
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}
