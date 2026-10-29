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
import { hostRunsInBrowserAgent } from '../host/agentBackend';
import { clientFetch } from './clientSession';
import { WebSocketService } from './websocket';

/**
 * Whether the agent runs in this page rather than behind the server.
 *
 * A daemon backend is authoritative and wins over everything else, including
 * a connection state the in-browser wiring left behind during an in-place
 * swap: the daemon runs the agent, so its status endpoint is the truth.
 */
export function agentRunsInPage(): boolean {
  const host = getActiveHost();
  if (host?.transport.agent?.kind === 'daemon') return false;
  if (WebSocketService.getInstance().getLocalAgentState() !== 'none') return true;
  return hostRunsInBrowserAgent(host);
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
