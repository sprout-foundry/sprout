/**
 * Multiple conversations in the hosted editor, kept in the browser.
 *
 * The agent runs in the page (WASM) and the platform keeps no chat state, so
 * the `/api/chat-sessions*` endpoints the chat UI already speaks are served
 * here instead: a small chat list in localStorage, where each chat owns one
 * transcript in {@link cloudSessionStore}. Switching chats makes that
 * transcript current, so the existing save-on-turn and restore-on-reload
 * paths follow the active chat. The in-page agent keeps one conversation per
 * chat id (see cmd/wasm agent_funcs).
 *
 * Response shapes match what services/chatSessions.ts expects from the
 * daemon, so the chat UI needs no cloud-specific branches.
 */

import type { ChatSession } from './chatSessions';
import {
  activateCloudSession,
  deleteSession,
  getCurrentCloudSessionId,
  listSessions,
  newCloudSessionId,
  restoreSession,
  startNewCloudSession,
} from './cloudSessionStore';
import { jsonError, jsonOk } from './cloudWasmHandlers';

const STORAGE_KEY = 'sprout-cloud-chats';

interface StoredChat {
  id: string;
  /** Set only when the user renamed the chat; otherwise the transcript names it. */
  name?: string;
  mode: 'code' | 'design';
  session_id: string;
  created_at: string;
  last_active_at: string;
}

interface ChatIndex {
  active_id: string;
  chats: StoredChat[];
}

function now(): string {
  return new Date().toISOString();
}

function newChatId(): string {
  return `chat-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function newChat(sessionId: string, mode: 'code' | 'design' = 'code', name?: string): StoredChat {
  const at = now();
  return { id: newChatId(), name, mode, session_id: sessionId, created_at: at, last_active_at: at };
}

function read(): ChatIndex {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? 'null') as ChatIndex | null;
    if (parsed && Array.isArray(parsed.chats) && parsed.chats.length > 0) return parsed;
  } catch {
    // Corrupt or unavailable storage: start over with the current transcript.
  }
  // First use: the conversation the editor already had becomes the first chat.
  const first = newChat(getCurrentCloudSessionId() || startNewCloudSession());
  const index = { active_id: first.id, chats: [first] };
  write(index);
  return index;
}

function write(index: ChatIndex): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(index));
  } catch {
    // Storage full or unavailable: the chat list lasts for this page only.
  }
}

function toChatSession(chat: StoredChat, index: ChatIndex): ChatSession {
  const meta = listSessions().sessions.find((s) => s.session_id === chat.session_id);
  return {
    id: chat.id,
    name: chat.name || meta?.name || 'New conversation',
    created_at: chat.created_at,
    last_active_at: meta?.last_updated || chat.last_active_at,
    message_count: meta?.message_count ?? 0,
    current_session_id: chat.session_id,
    active_query: false,
    is_pinned: false,
    mode: chat.mode,
    is_default: chat.id === index.chats[0]?.id,
    is_active: chat.id === index.active_id,
  };
}

function transcript(chat: StoredChat) {
  return (restoreSession(chat.session_id)?.messages ?? []).map((m) => ({
    role: m.type,
    content: m.content,
    ...(m.reasoning ? { reasoning_content: m.reasoning } : {}),
    timestamp: m.timestamp,
  }));
}

function withMessages(chat: StoredChat, index: ChatIndex) {
  return { ...toChatSession(chat, index), messages: transcript(chat) };
}

function activate(index: ChatIndex, chat: StoredChat): void {
  index.active_id = chat.id;
  chat.last_active_at = now();
  activateCloudSession(chat.session_id);
  write(index);
}

function parseBody(body: string | undefined): Record<string, unknown> {
  if (!body) return {};
  try {
    const parsed = JSON.parse(body);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

/**
 * Serve a `/api/chat-sessions*` request, or return null for paths this store
 * does not own (worktree and compaction endpoints stay synthetic).
 */
export function handleCloudChatSessionsEndpoint(
  urlPath: string,
  method: string,
  fullUrl: string,
  body?: string,
): Response | null {
  const index = read();
  const find = (id: unknown) => index.chats.find((c) => c.id === id);
  const req = parseBody(body);

  if (urlPath === '/api/chat-sessions' && method === 'GET') {
    return jsonOk({
      message: 'ok',
      chat_sessions: index.chats.map((c) => toChatSession(c, index)),
      active_chat_id: index.active_id,
      total_sessions: index.chats.length,
    });
  }

  if (urlPath === '/api/chat-sessions/create' && method === 'POST') {
    // Creating does not switch — as with the daemon, the UI switches to the
    // new chat itself, which keeps its idea of the active chat and ours in
    // step (a mismatch would file one chat's messages under the other).
    const mode = req.mode === 'design' ? 'design' : 'code';
    const chat = newChat(newCloudSessionId(), mode, typeof req.name === 'string' ? req.name : undefined);
    index.chats.push(chat);
    write(index);
    return jsonOk({ message: 'Chat session created', chat_session: toChatSession(chat, index) });
  }

  if (urlPath === '/api/chat-sessions/switch' && method === 'POST') {
    const chat = find(req.id);
    if (!chat) return jsonError('chat session not found', 404);
    activate(index, chat);
    return jsonOk({ message: 'Switched', active_chat_id: chat.id, chat_session: withMessages(chat, index) });
  }

  if (urlPath === '/api/chat-sessions/messages' && method === 'GET') {
    const chat = find(new URL(fullUrl, 'http://local').searchParams.get('chat_id'));
    if (!chat) return jsonError('chat session not found', 404);
    return jsonOk({ message: 'ok', active_chat_id: index.active_id, chat_session: withMessages(chat, index) });
  }

  if (urlPath === '/api/chat-sessions/rename' && method === 'POST') {
    const chat = find(req.id);
    if (!chat || typeof req.name !== 'string' || !req.name.trim()) return jsonError('invalid rename', 400);
    chat.name = req.name.trim();
    write(index);
    return jsonOk({ message: 'Renamed', chat_session: toChatSession(chat, index) });
  }

  if (urlPath === '/api/chat-sessions/delete' && method === 'POST') {
    const chat = find(req.id);
    if (!chat) return jsonError('chat session not found', 404);
    index.chats = index.chats.filter((c) => c.id !== chat.id);
    deleteSession(chat.session_id);
    if (index.chats.length === 0) index.chats.push(newChat(startNewCloudSession()));
    if (index.active_id === chat.id) activate(index, index.chats[0]);
    else write(index);
    return jsonOk({ message: 'Deleted' });
  }

  if (urlPath === '/api/chat-sessions/delete-all' && method === 'POST') {
    const deleted = index.chats.length;
    for (const chat of index.chats) deleteSession(chat.session_id);
    const fresh = newChat(startNewCloudSession());
    const next = { active_id: fresh.id, chats: [fresh] };
    write(next);
    return jsonOk({ message: 'Deleted all', deleted_count: deleted, active_chat_id: fresh.id });
  }

  return null;
}

export function __resetCloudChatsForTests(): void {
  window.localStorage.removeItem(STORAGE_KEY);
}

/** The transcript a chat's messages are saved under, or null when unknown. */
export function transcriptIdForChat(chatId: string | null | undefined): string | null {
  if (!chatId || typeof window === 'undefined') return null;
  return read().chats.find((c) => c.id === chatId)?.session_id ?? null;
}

/** Point a chat at a new transcript (after /clear starts one). */
export function rebindChatTranscript(chatId: string | null | undefined, sessionId: string): void {
  if (!chatId || typeof window === 'undefined') return;
  const index = read();
  const chat = index.chats.find((c) => c.id === chatId);
  if (!chat) return;
  chat.session_id = sessionId;
  write(index);
}
