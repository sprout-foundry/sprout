/**
 * Where to ask "is this chat's agent running right now".
 *
 * With a daemon the server is authoritative: `/api/query/status?chat_id=`.
 * When the agent runs in the page (the hosted WASM build, or a host that
 * selected the in-browser agent), the server behind the status endpoints is
 * the platform, which knows nothing about the run — the page's own run
 * registry is the truth.
 */

import { getActiveHost } from '../host/accessor';
import { clientFetch } from './clientSession';
import { WebSocketService } from './websocket';

/** Whether the agent runs in this page rather than behind the server. */
export function agentRunsInPage(): boolean {
  if (WebSocketService.getInstance().getLocalAgentState() !== 'none') return true;
  const transport = getActiveHost()?.transport;
  return transport?.authMode === 'bearer' || transport?.agent?.kind === 'wasm';
}

/**
 * Whether the chat has a run in flight. Throws when the server cannot be
 * reached so callers can retry.
 */
export async function fetchChatRunning(
  chatId: string | null | undefined,
  fetchFn: typeof fetch = clientFetch,
): Promise<boolean> {
  if (agentRunsInPage()) {
    const { isChatRunning } = await import('./cloudChatSessions');
    return isChatRunning(chatId);
  }
  const url = chatId ? `/api/query/status?chat_id=${encodeURIComponent(chatId)}` : '/api/query/status';
  const response = await fetchFn(url);
  if (!response.ok) throw new Error(`query status: HTTP ${response.status}`);
  const body = (await response.json()) as { active?: unknown };
  return body.active === true;
}
