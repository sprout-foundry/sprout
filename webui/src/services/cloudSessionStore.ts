/**
 * Cloud session store — browser-local persistence for chat conversations.
 *
 * In cloud mode the conversation is ephemeral by default: the WASM shell
 * keeps the agent in memory, and the platform backend has no per-client
 * session store. Refreshing the browser therefore wipes the conversation.
 *
 * This module persists conversations to localStorage so they survive page
 * reloads. The persisted sessions surface through the same `/api/sessions`
 * and `/api/sessions/restore` endpoints the webui already calls (they are
 * intercepted by the CloudAdapter before the synthetic "not available"
 * fallback fires), which means the existing session picker, restore-on-
 * mount logic, and `/clear` rotation all work without UI changes.
 *
 * Storage layout (all keys share the `sprout-cloud-` prefix):
 *   sprout-cloud-sessions            → JSON index of metadata for all sessions
 *   sprout-cloud-session-{id}        → JSON messages for a single session
 *
 * localStorage has a ~5 MB ceiling. Conversations are text, so this is
 * plenty for typical use. If a write hits QuotaExceededError we trim the
 * oldest sessions from the index and retry — this keeps recent history
 * accessible rather than failing the whole save.
 */

import type { Message } from '../types/app';
import { debugLog } from '../utils/log';

/** Public key prefix — exported so tests / callers can clear cloud data. */
export const CLOUD_SESSION_PREFIX = 'sprout-cloud-session-';
const INDEX_KEY = 'sprout-cloud-sessions';
const MAX_TITLE_LEN = 50;
/** Hard cap on the number of sessions kept in the index. */
const MAX_SESSIONS = 100;

/**
 * The session id currently loaded into the UI, if any. Set by
 * {@link restoreSession} (when the restore-on-mount flow loads a saved
 * transcript) and reset by {@link resetActiveSessionId} (on `/clear` or
 * explicit new-session). {@link saveSession} reuses this id so that
 * follow-up saves UPDATE the restored record instead of creating a
 * duplicate.
 *
 * This is deliberately module-local rather than React state: the restore
 * path (useAppInitialization → CloudAdapter → handleCloudSessionRestore)
 * runs entirely outside React, and the save path needs the same value.
 */
let activeSessionId: string | null = null;

/** Metadata stored in the index for each session. */
export interface CloudSessionMeta {
  session_id: string;
  name: string;
  working_directory: string;
  last_updated: string; // ISO 8601
  message_count: number;
  total_tokens: number;
}

/** Full session record: metadata plus the message transcript. */
interface StoredSession extends CloudSessionMeta {
  messages: SerializedMessage[];
}

/**
 * Serialized message shape. Mirrors {@link Message} but with the Date
 * converted to an ISO string so it survives JSON round-tripping.
 */
interface SerializedMessage {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: string;
  reasoning?: string;
  toolRefs?: Message['toolRefs'];
}

/** Shape of the persisted index. */
interface SessionIndex {
  current_session_id: string;
  sessions: CloudSessionMeta[];
}

function emptyIndex(): SessionIndex {
  return { current_session_id: '', sessions: [] };
}

/** Lazily resolve localStorage; absent in SSR / restricted contexts. */
function storage(): Storage | null {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return null;
    return window.localStorage;
  } catch {
    // localStorage access can throw in private-mode or sandboxed iframes.
    return null;
  }
}

function sessionKey(sessionId: string): string {
  return `${CLOUD_SESSION_PREFIX}${sessionId}`;
}

/**
 * Derive a human-readable title from a transcript. Uses the first user
 * message (first {@link MAX_TITLE_LEN} chars, collapsed whitespace) and
 * falls back to "Cloud chat" when there is no user message yet.
 */
export function deriveSessionTitle(messages: Message[]): string {
  const firstUser = messages.find((m) => m.type === 'user' && m.content?.trim());
  const raw = firstUser?.content?.trim();
  if (!raw) return 'Cloud chat';
  const collapsed = raw.replace(/\s+/g, ' ');
  return collapsed.length > MAX_TITLE_LEN ? `${collapsed.slice(0, MAX_TITLE_LEN)}…` : collapsed;
}

function toSerialized(messages: Message[]): SerializedMessage[] {
  return messages.map((m) => {
    const out: SerializedMessage = {
      id: m.id,
      type: m.type,
      content: m.content,
      timestamp: m.timestamp instanceof Date ? m.timestamp.toISOString() : new Date(m.timestamp).toISOString(),
    };
    if (m.reasoning) out.reasoning = m.reasoning;
    if (m.toolRefs && m.toolRefs.length > 0) out.toolRefs = m.toolRefs;
    return out;
  });
}

/**
 * Convert a stored session's messages back to the runtime {@link Message}
 * shape, parsing the ISO timestamp back into a Date.
 */
export function deserializeMessages(stored: SerializedMessage[]): Message[] {
  return (stored ?? [])
    .filter((m) => m && (m.type === 'user' || m.type === 'assistant'))
    .map((m, i) => {
      const msg: Message = {
        id: m.id || `cloud-${i}`,
        type: m.type,
        content: typeof m.content === 'string' ? m.content : '',
        timestamp: m.timestamp ? new Date(m.timestamp) : new Date(),
      };
      if (m.reasoning) msg.reasoning = m.reasoning;
      if (m.toolRefs && m.toolRefs.length > 0) msg.toolRefs = m.toolRefs;
      return msg;
    });
}

function readIndex(): SessionIndex {
  const ls = storage();
  if (!ls) return emptyIndex();
  try {
    const raw = ls.getItem(INDEX_KEY);
    if (!raw) return emptyIndex();
    const parsed = JSON.parse(raw) as Partial<SessionIndex>;
    const sessions = Array.isArray(parsed.sessions) ? parsed.sessions : [];
    return {
      current_session_id: typeof parsed.current_session_id === 'string' ? parsed.current_session_id : '',
      sessions: sessions.filter(isValidMeta),
    };
  } catch (err) {
    debugLog('[cloudSessionStore] failed to read index:', err);
    return emptyIndex();
  }
}

function isValidMeta(m: unknown): m is CloudSessionMeta {
  return (
    !!m &&
    typeof m === 'object' &&
    typeof (m as CloudSessionMeta).session_id === 'string' &&
    (m as CloudSessionMeta).session_id.length > 0
  );
}

function writeIndex(index: SessionIndex): void {
  const ls = storage();
  if (!ls) return;
  try {
    ls.setItem(INDEX_KEY, JSON.stringify(index));
  } catch (err) {
    debugLog('[cloudSessionStore] failed to write index:', err);
  }
}

function readSession(sessionId: string): StoredSession | null {
  const ls = storage();
  if (!ls) return null;
  try {
    const raw = ls.getItem(sessionKey(sessionId));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<StoredSession>;
    if (!parsed || !Array.isArray(parsed.messages)) return null;
    return {
      session_id: sessionId,
      name: typeof parsed.name === 'string' ? parsed.name : 'Cloud chat',
      working_directory: typeof parsed.working_directory === 'string' ? parsed.working_directory : '',
      last_updated: typeof parsed.last_updated === 'string' ? parsed.last_updated : new Date().toISOString(),
      message_count: Array.isArray(parsed.messages) ? parsed.messages.length : 0,
      total_tokens: typeof parsed.total_tokens === 'number' ? parsed.total_tokens : 0,
      messages: parsed.messages,
    };
  } catch (err) {
    debugLog(`[cloudSessionStore] failed to read session ${sessionId}:`, err);
    return null;
  }
}

function writeSession(session: StoredSession): boolean {
  const ls = storage();
  if (!ls) return false;
  try {
    ls.setItem(sessionKey(session.session_id), JSON.stringify(session));
    return true;
  } catch (err) {
    debugLog(`[cloudSessionStore] write failed for ${session.session_id}:`, err);
    return false;
  }
}

/**
 * Persist (or update) a conversation. Generates an id when none is
 * supplied, updates the index metadata, and writes the full transcript.
 * Returns the session id used so callers can track the "current" session.
 *
 * If localStorage is full we evict the oldest sessions (by last_updated)
 * and retry once, then give up gracefully.
 */
export function saveSession(
  messages: Message[],
  options?: {
    sessionId?: string;
    name?: string;
    totalTokens?: number;
    workingDirectory?: string;
  },
): string | null {
  const ls = storage();
  if (!ls) return null;

  // An empty transcript should never create a session record — clearing a
  // conversation must rotate the previous one into history, not persist a
  // blank session on top.
  if (messages.length === 0) return null;

  // Reuse the currently-active session id (set on restore) unless the
  // caller explicitly overrides it. This keeps a restored conversation
  // as a single record through multiple turns instead of forking a new
  // id on every save.
  const sessionId =
    options?.sessionId && options.sessionId.trim()
      ? options.sessionId.trim()
      : (activeSessionId ?? generateSessionId());
  const now = new Date().toISOString();
  const serialized = toSerialized(messages);
  const record: StoredSession = {
    session_id: sessionId,
    name: options?.name && options.name.trim() ? options.name.trim() : deriveSessionTitle(messages),
    working_directory: options?.workingDirectory ?? '',
    last_updated: now,
    message_count: serialized.length,
    total_tokens: typeof options?.totalTokens === 'number' ? options.totalTokens : 0,
    messages: serialized,
  };

  if (!writeSession(record)) {
    // Quota pressure — evict the oldest sessions (excluding the current one)
    // and retry the write once.
    evictOldest(sessionId, Math.max(1, Math.ceil(MAX_SESSIONS / 4)));
    if (!writeSession(record)) {
      debugLog('[cloudSessionStore] save aborted: storage quota exceeded');
      return null;
    }
  }

  const index = readIndex();
  const meta: CloudSessionMeta = {
    session_id: sessionId,
    name: record.name,
    working_directory: record.working_directory,
    last_updated: now,
    message_count: serialized.length,
    total_tokens: record.total_tokens,
  };
  index.sessions = [meta, ...index.sessions.filter((s) => s.session_id !== sessionId)].slice(0, MAX_SESSIONS);
  index.current_session_id = sessionId;
  writeIndex(index);

  return sessionId;
}

/** Remove the {@code count} oldest sessions (by last_updated) from storage. */
function evictOldest(keepSessionId: string, count: number): void {
  const ls = storage();
  if (!ls) return;
  const index = readIndex();
  const sorted = [...index.sessions].sort((a, b) => a.last_updated.localeCompare(b.last_updated));
  const toEvict = sorted.filter((s) => s.session_id !== keepSessionId).slice(0, count);
  for (const m of toEvict) {
    try {
      ls.removeItem(sessionKey(m.session_id));
    } catch {
      /* best effort */
    }
  }
  const evictIds = new Set(toEvict.map((s) => s.session_id));
  index.sessions = index.sessions.filter((s) => !evictIds.has(s.session_id));
  writeIndex(index);
}

/** Return the index of saved sessions for the session picker / list view. */
export function listSessions(): { sessions: CloudSessionMeta[]; current_session_id: string } {
  const index = readIndex();
  return {
    sessions: [...index.sessions].sort((a, b) => b.last_updated.localeCompare(a.last_updated)),
    current_session_id: index.current_session_id,
  };
}

/** Returns true if a session with the given id exists in the store. */
export function hasSession(sessionId: string): boolean {
  if (!sessionId) return false;
  return readSession(sessionId) !== null;
}

/**
 * Restore a saved session's messages. Returns null when the session is
 * unknown (so callers can fall back to an empty conversation instead of
 * surfacing an error toast). Marks the session as active so subsequent
 * saves update this record.
 */
export function restoreSession(sessionId: string): StoredSession | null {
  if (!sessionId) return null;
  const stored = readSession(sessionId);
  if (stored) setActiveSessionId(sessionId);
  return stored;
}

/** Delete a single session from storage and the index. */
export function deleteSession(sessionId: string): boolean {
  const ls = storage();
  if (!ls || !sessionId) return false;
  const existed = readSession(sessionId) !== null;
  try {
    ls.removeItem(sessionKey(sessionId));
  } catch (err) {
    debugLog(`[cloudSessionStore] delete failed for ${sessionId}:`, err);
    return false;
  }
  const index = readIndex();
  const before = index.sessions.length;
  index.sessions = index.sessions.filter((s) => s.session_id !== sessionId);
  if (index.current_session_id === sessionId) index.current_session_id = '';
  writeIndex(index);
  return existed || index.sessions.length < before;
}

/** Remove ALL cloud sessions (index + per-session keys). Test/Reset helper. */
export function clearAllSessions(): void {
  const ls = storage();
  resetActiveSessionId();
  if (!ls) return;
  const index = readIndex();
  for (const m of index.sessions) {
    try {
      ls.removeItem(sessionKey(m.session_id));
    } catch {
      /* best effort */
    }
  }
  try {
    ls.removeItem(INDEX_KEY);
  } catch {
    /* best effort */
  }
}

function generateSessionId(): string {
  // Prefer crypto.randomUUID, fall back to a timestamp+random suffix.
  const g = globalThis as { crypto?: { randomUUID?: () => string } };
  if (g.crypto?.randomUUID) {
    try {
      return g.crypto.randomUUID();
    } catch {
      /* fall through */
    }
  }
  return `cloud-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

/** The session id currently loaded into the UI, or null. */
export function getActiveSessionId(): string | null {
  return activeSessionId;
}

/**
 * Mark a session as the active one. Called when a transcript is restored
 * (so subsequent saves update the same record) or when a new conversation
 * should be tracked under an explicit id.
 */
export function setActiveSessionId(id: string | null): void {
  activeSessionId = id && id.trim() ? id.trim() : null;
}

/**
 * Clear the active session id. Called on `/clear` / new conversation so
 * the next save creates a fresh record while the previous one stays in
 * history.
 */
export function resetActiveSessionId(): void {
  activeSessionId = null;
}

/**
 * Start a fresh conversation after `/clear` (or an explicit "new chat").
 *
 * Generates a new session id, marks it as the active id in memory, AND
 * persists it as `current_session_id` in the index — without creating a
 * session record (there are no messages to store yet). This is the key
 * difference from {@link resetActiveSessionId}: clearing only the
 * in-memory id left the previous conversation's id as the persisted
 * "current" marker, so a page refresh resurrected the just-cleared
 * transcript (the session list fell back to it as current).
 *
 * With a fresh id persisted as current, the session-list handler returns
 * it as `current_session_id`, and restore-on-mount sees a current session
 * with no messages and starts empty instead of restoring history.
 *
 * Returns the new session id so callers can track it if needed.
 */
export function startNewCloudSession(): string {
  const id = generateSessionId();
  activeSessionId = id;
  const index = readIndex();
  index.current_session_id = id;
  writeIndex(index);
  return id;
}
