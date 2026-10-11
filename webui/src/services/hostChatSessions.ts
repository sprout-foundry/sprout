/**
 * The host-backed chat session store.
 *
 * When a host advertises the `chatSessions` capability it serves the chat
 * session store itself, and Sprout's chat session calls go to it instead of
 * the daemon or browser storage. The calls use the same request shapes as the
 * daemon's `/api/chat-sessions*` endpoints (see `services/chatSessions.ts`),
 * resolved against the host transport's `apiBaseURL` — so a host implements
 * one small set of endpoints and the chat UI needs no host-specific branch.
 *
 * This module supplies the `ChatSessionsApi` the chat unit consumes. The
 * chat unit selects it when the capability is on (see
 * `views/WorkspaceChatContext.tsx`); with the capability off the daemon's
 * endpoints (or the browser-local store for the in-browser agent) are used
 * unchanged.
 *
 * The endpoint shapes a host must implement are documented in
 * `docs/integration/host-contract.md`.
 */

import { getActiveHost } from '../host/accessor';
import {
  createChatSessionsApi,
  fetchChatSessionMessages,
  listChatSessions,
  type ChatSessionsApi,
  type ChatSessionsResponse,
  type ChatSessionSwitchResponse,
} from './chatSessions';
import { clientFetch } from './clientSession';

/**
 * Resolve a host API path against the host's base URL. `''` is the
 * same-origin sentinel, so a relative path is returned unchanged and the
 * ordinary transport (the adapter or the page's own origin) handles it.
 */
export function hostApiURL(apiBaseURL: string, path: string): string {
  if (!apiBaseURL) return path;
  return `${apiBaseURL.replace(/\/+$/, '')}${path}`;
}

/**
 * The project the host's chat session store is scoped to. A host store keeps
 * one chat list per project (see `docs/integration/host-contract.md`), so every
 * host session call names the project of the mounted workspace. Recorded by
 * the chat unit (`WorkspaceChatProvider`'s `chatSessionsProject`) for the
 * module-level paths — the turn append and the list/messages refreshes — the
 * same way the active host is recorded for them; the chat unit's own session
 * API binds its project explicitly. Empty means no project: the host's
 * unscoped bucket.
 */
let activeChatSessionsProject = '';

/**
 * Record the project the host chat session calls are scoped to, returning a
 * restore function that puts back the previous one (the `registerActiveHost`
 * pattern, so an unmounting workspace hands the scope back).
 */
export function registerActiveChatSessionsProject(project: string | undefined): () => void {
  const previous = activeChatSessionsProject;
  activeChatSessionsProject = project?.trim() ?? '';
  let restored = false;
  return () => {
    if (restored) return;
    restored = true;
    activeChatSessionsProject = previous;
  };
}

/** The project the module-level host chat session calls are scoped to. */
export function activeChatSessionsProjectForHost(): string {
  return activeChatSessionsProject;
}

/**
 * Add the project to one chat session request: a `project` query parameter on
 * a GET, a `project` field on a JSON body otherwise (a body-less POST such as
 * delete-all gets a `{ project }` body). This is the single place the project
 * reaches the wire, so every call through `hostChatSessionsFetch` carries it.
 */
export function withChatSessionsProject(
  path: string,
  init: RequestInit | undefined,
  project: string,
): [string, RequestInit | undefined] {
  if (!project) return [path, init];
  const method = (init?.method ?? 'GET').toUpperCase();
  if (method === 'GET' || method === 'HEAD') {
    const sep = path.includes('?') ? '&' : '?';
    return [`${path}${sep}project=${encodeURIComponent(project)}`, init];
  }
  if (init?.body == null) {
    const headers = new Headers(init?.headers);
    headers.set('Content-Type', 'application/json');
    return [path, { ...init, headers, body: JSON.stringify({ project }) }];
  }
  if (typeof init.body !== 'string') return [path, init];
  let parsed: unknown;
  try {
    parsed = JSON.parse(init.body);
  } catch {
    return [path, init];
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return [path, init];
  return [path, { ...init, body: JSON.stringify({ ...(parsed as Record<string, unknown>), project }) }];
}

/**
 * A fetch that resolves relative API paths against the host's base URL and
 * scopes them to a project. The underlying fetch is the app's adapter-aware
 * `clientFetch`, so the client id header and any installed adapter are
 * honoured; only the URL (and the project) change, pointing at the host's
 * store. `project` omitted, the call uses the recorded active project at call
 * time.
 */
export function hostChatSessionsFetch(apiBaseURL: string, project?: string): typeof fetch {
  return (input, init) => {
    if (typeof input !== 'string' || !input.startsWith('/')) return clientFetch(input, init);
    const [path, scoped] = withChatSessionsProject(input, init, project ?? activeChatSessionsProject);
    return clientFetch(hostApiURL(apiBaseURL, path), scoped);
  };
}

/**
 * A `ChatSessionsApi` bound to the host's base URL and a project, so the
 * list/create/switch and the rest of the session calls reach that project's
 * chats in the host's store with the daemon's request shapes.
 */
export function createHostChatSessionsApi(apiBaseURL: string, project?: string): ChatSessionsApi {
  return createChatSessionsApi(hostChatSessionsFetch(apiBaseURL, project ?? ''));
}

/**
 * Append a finished turn to the host store when the active host advertises the
 * `chatSessions` capability. This is the single seam both agent backends share:
 * the chat unit's turn handlers call it on the run boundaries (`query_started`
 * for the question, `query_completed` for the question and answer), so a host
 * store receives turns whether the agent ran in a daemon or in the browser.
 * A no-op without the capability (the daemon or browser-local store owns the
 * turn). Best-effort: a host store hiccup must not fail a turn that already
 * rendered.
 */
export function appendTurnToHostStore(chatId: string | null | undefined, query: string, response?: string): void {
  const host = getActiveHost();
  if (!host || !host.capabilities.chatSessions) return;
  void appendHostTurn(host.transport.apiBaseURL, chatId, query, response);
}

/**
 * List the chats from whichever store the active host selected: the host's
 * store when it advertises `chatSessions`, else the daemon's endpoint through
 * the caller's fetch. The session-list refresh paths use this so a host store
 * that serves the session calls also serves the list refresh.
 */
export function listChatSessionsForActiveStore(fetchFn?: typeof fetch): Promise<ChatSessionsResponse> {
  const host = getActiveHost();
  if (host?.capabilities.chatSessions) {
    return listChatSessions(hostChatSessionsFetch(host.transport.apiBaseURL));
  }
  return fetchFn ? listChatSessions(fetchFn) : listChatSessions();
}

/**
 * Read a chat's transcript from whichever store the active host selected: the
 * host's store when it advertises `chatSessions`, else the daemon's endpoint
 * through the caller's fetch. Background panes and the replay path poll this,
 * so routing it here keeps the read on the same store the list/switch calls
 * use — a host store that served the list but not the messages would leave
 * background panes empty.
 */
export function fetchChatSessionMessagesForActiveStore(
  chatId: string,
  fetchFn?: typeof fetch,
): Promise<ChatSessionSwitchResponse> {
  const host = getActiveHost();
  if (host?.capabilities.chatSessions) {
    return fetchChatSessionMessages(chatId, hostChatSessionsFetch(host.transport.apiBaseURL));
  }
  return fetchFn ? fetchChatSessionMessages(chatId, fetchFn) : fetchChatSessionMessages(chatId);
}

export async function appendHostTurn(
  apiBaseURL: string,
  chatId: string | null | undefined,
  query: string,
  response?: string,
  project?: string,
): Promise<void> {
  if (!chatId || !query.trim()) return;
  try {
    await hostChatSessionsFetch(apiBaseURL, project)('/api/chat-sessions/turn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(response === undefined ? { chat_id: chatId, query } : { chat_id: chatId, query, response }),
    });
  } catch {
    // best-effort: the turn is already on screen; the host store self-heals.
  }
}
