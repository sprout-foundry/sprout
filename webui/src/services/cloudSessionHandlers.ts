/**
 * Cloud session endpoint handlers for CloudAdapter.
 *
 * These mirror the shape of the platform `/api/sessions*` endpoints but
 * are backed entirely by the browser-local {@link cloudSessionStore}.
 *
 * In cloud mode the platform backend returns an empty session list, so
 * the CloudAdapter intercepts the session endpoints *before* the generic
 * synthetic-response fallback and routes them here. This is what makes
 * the session picker, restore-on-mount, and `/clear` rotation work in
 * the browser without any server-side per-client session state.
 *
 * Response shapes intentionally match what `webui/src/services/api/sessionApi.ts`
 * expects (SessionsResponse / SessionRestoreResponse) so the existing
 * service-layer and UI code needs no changes.
 */

import { deleteSession, listSessions, restoreSession, deserializeMessages } from './cloudSessionStore';
import { jsonError, jsonOk } from './cloudWasmHandlers';

/**
 * Handle GET /api/sessions — list all locally-persisted sessions.
 *
 * @param _urlPath  The matched API path (unused; present for signature symmetry)
 * @param fullUrl   The full request URL (used to read the `scope` query param)
 */
export function handleCloudSessionList(_urlPath: string, fullUrl: string): Response {
  const { sessions, current_session_id } = listSessions();
  // The webui requests `?scope=current` on mount to decide whether to
  // restore. We honour the scope filter by returning the full list in
  // both cases — `current` is a backend-specific optimisation that the
  // client-side store doesn't need, and returning everything keeps the
  // restore-on-mount logic simple.
  void fullUrl;
  void current_session_id;
  return jsonOk({
    message: 'ok',
    current_session_id: current_session_id || sessions[0]?.session_id || '',
    sessions: sessions.map((s) => ({
      session_id: s.session_id,
      name: s.name,
      working_directory: s.working_directory,
      last_updated: s.last_updated,
      message_count: s.message_count,
      total_tokens: s.total_tokens,
    })),
  });
}

/**
 * Handle POST /api/sessions/restore — return a saved transcript.
 *
 * Request body: `{ "session_id": string }`
 * Response: `SessionRestoreResponse` (messages deserialised to the
 * `{ role, content }` wire shape the webui's restore path expects).
 */
export function handleCloudSessionRestore(bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  let parsed: { session_id?: string };
  try {
    parsed = JSON.parse(bodyStr);
  } catch {
    return jsonError('Invalid JSON body', 400);
  }
  const sessionId = typeof parsed?.session_id === 'string' ? parsed.session_id : '';
  if (!sessionId) return jsonError('session_id is required', 400);

  const stored = restoreSession(sessionId);
  if (!stored) return jsonError('Session not found', 404);

  // Serialise into the { role, content } shape SessionRestoreResponse uses.
  const messages = deserializeMessages(stored.messages).map((m) => ({
    role: m.type,
    content: m.content,
  }));

  return jsonOk({
    message: 'ok',
    session_id: sessionId,
    name: stored.name,
    working_directory: stored.working_directory,
    message_count: messages.length,
    total_tokens: stored.total_tokens,
    messages,
  });
}

/**
 * Handle DELETE /api/sessions/{id} — remove a single session.
 *
 * Also accepts POST /api/sessions/delete with `{ session_id }` for
 * symmetry with the chat-session delete endpoint shape.
 */
export function handleCloudSessionDelete(urlPath: string, bodyStr?: string): Response {
  let sessionId = '';

  // Only extract the id from the path for {id}-style DELETE routes.
  // POST /api/sessions/delete (body-based) must NOT match here, otherwise
  // the regex captures the literal string "delete" as the session id.
  if (urlPath !== '/api/sessions/delete' && urlPath !== '/api/sessions/restore' && urlPath !== '/api/sessions/search') {
    const pathMatch = urlPath.match(/^\/api\/sessions\/([^/?#]+)$/);
    sessionId = pathMatch?.[1] ?? '';
  }

  // Fall back to a JSON body { session_id } (POST /api/sessions/delete).
  if (!sessionId && bodyStr) {
    try {
      const parsed = JSON.parse(bodyStr) as { session_id?: string };
      sessionId = typeof parsed?.session_id === 'string' ? parsed.session_id : '';
    } catch {
      /* ignore — leave sessionId empty, return 400 below */
    }
  }

  if (!sessionId) return jsonError('session_id is required', 400);
  deleteSession(sessionId);
  return jsonOk({ message: 'ok', session_id: sessionId });
}

/**
 * Dispatch a cloud-session request. Called by the CloudAdapter for any
 * path under `/api/sessions` (except the search sub-endpoint, which
 * remains synthetic). Returns null when the request does not match a
 * known session operation so the adapter can fall through to the next
 * handler.
 */
export function handleCloudSessionsEndpoint(
  urlPath: string,
  method: string,
  fullUrl: string,
  bodyStr?: string,
): Response | null {
  const m = method.toUpperCase();

  if (urlPath === '/api/sessions' && m === 'GET') {
    return handleCloudSessionList(urlPath, fullUrl);
  }
  if (urlPath === '/api/sessions/restore' && m === 'POST') {
    return handleCloudSessionRestore(bodyStr);
  }
  // /api/sessions/search is intentionally NOT handled here — it stays a
  // synthetic empty response (browser mode has no cross-session index).
  // POST /api/sessions/delete with { session_id } body — must be checked
  // before the /api/sessions/{id} DELETE route below, otherwise "delete"
  // would be captured as the id.
  if (urlPath === '/api/sessions/delete' && m === 'POST') {
    return handleCloudSessionDelete(urlPath, bodyStr);
  }
  // DELETE /api/sessions/{id} — id is in the path (exclude the known
  // sub-paths restore/delete/search which are handled above).
  if (/^\/api\/sessions\/[^/?#]+$/.test(urlPath) && m === 'DELETE') {
    return handleCloudSessionDelete(urlPath, bodyStr);
  }
  return null;
}
