/**
 * Terminal domain API — adapter-aware terminal operations.
 */

import type { ShellInfo, TerminalHistoryResponse, AddTerminalHistoryResponse } from './types';

export async function getTerminalSessionCount(fetchFn: typeof fetch): Promise<number> {
  const response = await fetchFn('/api/terminal/sessions');
  if (!response.ok) throw new Error('Failed to fetch terminal sessions');
  const data = await response.json();
  return data.active_count ?? data.count ?? 0;
}

export async function getAvailableShells(fetchFn: typeof fetch): Promise<{ shells: ShellInfo[] }> {
  const response = await fetchFn('/api/terminal/shells');
  if (!response.ok) throw new Error('Failed to fetch available shells');
  return response.json();
}

export async function getTerminalHistory(fetchFn: typeof fetch, sessionId?: string): Promise<TerminalHistoryResponse> {
  const url = sessionId ? `/api/terminal/history?session_id=${encodeURIComponent(sessionId)}` : '/api/terminal/history';
  const response = await fetchFn(url);
  if (!response.ok) throw new Error('Failed to fetch terminal history');
  return response.json();
}

export async function addTerminalHistory(fetchFn: typeof fetch, command: string): Promise<AddTerminalHistoryResponse> {
  const response = await fetchFn('/api/terminal/history', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ command }),
  });
  if (!response.ok) throw new Error('Failed to add terminal history');
  return response.json();
}
