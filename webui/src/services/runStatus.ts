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
 * Whether a chat has a run in flight, for the places that adopt a chat when it
 * comes on screen (the boot load, a session switch, a gap reload).
 *
 * A store answers `active_query` alongside the transcript, but that flag only
 * describes the runs the store knows about. A host store pinned to a platform
 * knows none of a daemon backend's runs, so adopting a chat from it would show
 * a turn that is already running on the daemon as idle — and a turn started
 * outside the page (the host's own first turn, another tab) would never appear
 * as running here.
 *
 * {@link fetchChatRunning} asks whichever backend actually runs the agent: the
 * daemon's `/api/query/status`, or the in-page registry when the agent is in
 * the page. That answer wins. The store's flag is the fallback when the status
 * call fails, so a backend without the route (an older daemon) still adopts a
 * sane state instead of failing the switch.
 *
 * When the agent runs neither in the page nor behind a daemon the host named,
 * the store answering `active_query` is the daemon's own store — it runs the
 * agent, so its flag is already the answer and no extra call is made.
 */
export async function resolveChatRunning(
  chatId: string | null | undefined,
  storeSaysRunning: boolean | undefined,
  fetchFn: typeof fetch = clientFetch,
): Promise<boolean> {
  const storeIsAgent = getActiveHost()?.transport.agent?.kind !== 'daemon' && !agentRunsInPage();
  if (storeIsAgent) return storeSaysRunning === true;
  try {
    return await fetchChatRunning(chatId, fetchFn);
  } catch {
    return storeSaysRunning === true;
  }
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
