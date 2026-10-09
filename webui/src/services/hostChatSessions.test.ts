/**
 * The host-backed chat session store.
 *
 * When the active host advertises the `chatSessions` capability, the chat
 * session calls go to the host (same request shapes as the daemon's
 * `/api/chat-sessions*` endpoints, at the host's API base) and finished turns
 * are appended to it. These tests drive a fake host store through the
 * `ChatSessionsApi` the chat unit consumes and prove a reload restores the
 * transcript from the host.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { headlessHost } from '../host/HostProvider';
import { setActiveHost } from '../host/accessor';
import {
  appendHostTurn,
  createHostChatSessionsApi,
  fetchChatSessionMessagesForActiveStore,
  hostApiURL,
  listChatSessionsForActiveStore,
} from './hostChatSessions';
import { __setRepoScopeForTests } from './repoScope';

const HOST_BASE = 'https://host.test/backend';

/** A minimal fake host chat session store, served under `HOST_BASE`. */
function fakeHostStore() {
  interface Chat {
    id: string;
    name?: string;
    session_id: string;
    created_at: string;
    last_active_at: string;
    mode: 'code' | 'design';
    turns: Array<{ role: 'user' | 'assistant'; content: string; timestamp: string }>;
  }
  const chats: Chat[] = [];
  let activeId = '';
  let seq = 0;

  const now = () => new Date().toISOString();
  const newChat = (mode: 'code' | 'design' = 'code', name?: string): Chat => {
    seq += 1;
    const chat: Chat = {
      id: `host-chat-${seq}`,
      name,
      session_id: `host-session-${seq}`,
      created_at: now(),
      last_active_at: now(),
      mode,
      turns: [],
    };
    chats.push(chat);
    return chat;
  };
  newChat();
  activeId = chats[0].id;

  const json = (data: unknown, status = 200) =>
    new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });

  const toSession = (c: Chat) => ({
    id: c.id,
    name: c.name || c.turns.find((t) => t.role === 'user')?.content || 'New conversation',
    created_at: c.created_at,
    last_active_at: c.last_active_at,
    message_count: c.turns.length,
    current_session_id: c.session_id,
    active_query: false,
    is_pinned: false,
    mode: c.mode,
    is_default: c.id === chats[0].id,
    is_active: c.id === activeId,
  });

  const fetchFn: typeof fetch = async (input, init) => {
    const url = new URL(typeof input === 'string' ? input : input.toString());
    // The host serves the endpoints under its API base; strip it to get the
    // daemon-shaped path the store matches on.
    const path = url.pathname.replace(new URL(HOST_BASE).pathname, '');
    const method = (init?.method ?? 'GET').toUpperCase();
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};
    const find = (id: unknown) => chats.find((c) => c.id === id);

    if (path === '/api/chat-sessions' && method === 'GET') {
      return json({
        message: 'ok',
        chat_sessions: chats.map(toSession),
        active_chat_id: activeId,
        total_sessions: chats.length,
      });
    }
    if (path === '/api/chat-sessions/create' && method === 'POST') {
      const chat = newChat(body.mode === 'design' ? 'design' : 'code', body.name as string | undefined);
      return json({ message: 'Chat session created', chat_session: toSession(chat) });
    }
    if (path === '/api/chat-sessions/switch' && method === 'POST') {
      const chat = find(body.id);
      if (!chat) return json({ message: 'not found' }, 404);
      activeId = chat.id;
      chat.last_active_at = now();
      return json({
        message: 'Switched',
        active_chat_id: chat.id,
        chat_session: { ...toSession(chat), messages: chat.turns },
      });
    }
    if (path === '/api/chat-sessions/messages' && method === 'GET') {
      const chat = find(url.searchParams.get('chat_id'));
      if (!chat) return json({ message: 'not found' }, 404);
      return json({
        message: 'ok',
        active_chat_id: activeId,
        chat_session: { ...toSession(chat), messages: chat.turns },
      });
    }
    if (path === '/api/chat-sessions/rename' && method === 'POST') {
      const chat = find(body.id);
      if (!chat) return json({ message: 'not found' }, 404);
      chat.name = String(body.name);
      return json({ message: 'Renamed', chat_session: toSession(chat) });
    }
    if (path === '/api/chat-sessions/delete' && method === 'POST') {
      const idx = chats.findIndex((c) => c.id === body.id);
      if (idx < 0) return json({ message: 'not found' }, 404);
      const [removed] = chats.splice(idx, 1);
      if (activeId === removed.id) activeId = chats[0]?.id ?? '';
      if (chats.length === 0) {
        const fresh = newChat();
        activeId = fresh.id;
      }
      return json({ message: 'Deleted' });
    }
    if (path === '/api/chat-sessions/delete-all' && method === 'POST') {
      const deleted = chats.length;
      chats.length = 0;
      const fresh = newChat();
      activeId = fresh.id;
      return json({ message: 'Deleted all', deleted_count: deleted, active_chat_id: fresh.id });
    }
    if (path === '/api/chat-sessions/turn' && method === 'POST') {
      const chat = find(body.chat_id);
      if (!chat) return json({ message: 'not found' }, 404);
      const last = chat.turns[chat.turns.length - 1];
      if (typeof body.query === 'string' && !(last?.role === 'user' && last.content === body.query)) {
        chat.turns.push({ role: 'user', content: body.query, timestamp: now() });
      }
      if (typeof body.response === 'string') {
        const tail = chat.turns[chat.turns.length - 1];
        if (!(tail?.role === 'assistant' && tail.content === body.response)) {
          chat.turns.push({ role: 'assistant', content: body.response, timestamp: now() });
        }
      }
      chat.last_active_at = now();
      return json({ message: 'ok' });
    }
    return json({ message: 'not found' }, 404);
  };

  return { fetchFn, chats };
}

const originalFetch = globalThis.fetch;
let host: ReturnType<typeof fakeHostStore>;

beforeEach(() => {
  host = fakeHostStore();
  vi.stubGlobal('fetch', host.fetchFn);
  window.localStorage.clear();
  __setRepoScopeForTests(null);
});

afterEach(() => {
  vi.unstubAllGlobals();
  globalThis.fetch = originalFetch;
  __setRepoScopeForTests(null);
});

describe('hostApiURL', () => {
  it('resolves a relative API path against the host base', () => {
    expect(hostApiURL('https://host.test/api', '/api/chat-sessions')).toBe('https://host.test/api/api/chat-sessions');
  });

  it('leaves the path unchanged for the same-origin sentinel', () => {
    expect(hostApiURL('', '/api/chat-sessions')).toBe('/api/chat-sessions');
  });

  it('does not double the separator when the base has a trailing slash', () => {
    expect(hostApiURL('https://host.test/api/', '/api/chat-sessions')).toBe('https://host.test/api/api/chat-sessions');
  });
});

describe('createHostChatSessionsApi', () => {
  it('round-trips list / create / switch / rename / delete through the host store', async () => {
    const api = createHostChatSessionsApi(HOST_BASE);

    const initial = await api.listChatSessions();
    expect(initial.chat_sessions).toHaveLength(1);
    expect(initial.active_chat_id).toBe(initial.chat_sessions[0].id);

    const created = await api.createChatSession('Checkout work');
    expect(created.chat_session.name).toBe('Checkout work');
    expect((await api.listChatSessions()).chat_sessions).toHaveLength(2);

    const switched = await api.switchChatSession(created.chat_session.id);
    expect(switched.active_chat_id).toBe(created.chat_session.id);

    const renamed = await api.renameChatSession(created.chat_session.id, 'Renamed chat');
    expect(renamed.chat_session.name).toBe('Renamed chat');

    await api.deleteChatSession(created.chat_session.id);
    const after = await api.listChatSessions();
    expect(after.chat_sessions).toHaveLength(1);
    expect(after.chat_sessions[0].id).not.toBe(created.chat_session.id);
  });

  it('reaches the host at its API base (not the page origin)', async () => {
    const seen: string[] = [];
    const spy: typeof fetch = (input, init) => {
      seen.push(typeof input === 'string' ? input : input.toString());
      return host.fetchFn(input, init);
    };
    vi.stubGlobal('fetch', spy);

    const api = createHostChatSessionsApi(HOST_BASE);
    await api.listChatSessions();
    expect(seen[0]).toBe(`${HOST_BASE}/api/chat-sessions`);
  });

  it('delete-all clears the store and keeps one fresh chat', async () => {
    const api = createHostChatSessionsApi(HOST_BASE);
    await api.createChatSession('Second');
    const result = await api.deleteAllChatSessions();
    expect(result.deleted_count).toBe(2);
    const after = await api.listChatSessions();
    expect(after.chat_sessions).toHaveLength(1);
    expect(after.active_chat_id).toBe(result.active_chat_id);
  });
});

describe('finished turns are appended to the host store', () => {
  it('appends the question and the answer for a chat', async () => {
    const api = createHostChatSessionsApi(HOST_BASE);
    const chatId = (await api.listChatSessions()).active_chat_id;

    await appendHostTurn(HOST_BASE, chatId, 'what is 2+2?');
    await appendHostTurn(HOST_BASE, chatId, 'what is 2+2?', '4');

    const switched = await api.switchChatSession(chatId);
    expect(switched.chat_session.messages?.map((m) => [m.role, m.content])).toEqual([
      ['user', 'what is 2+2?'],
      ['assistant', '4'],
    ]);
  });

  it('a reload restores the transcript from the host store', async () => {
    const chatId = (await createHostChatSessionsApi(HOST_BASE).listChatSessions()).active_chat_id;
    await appendHostTurn(HOST_BASE, chatId, 'first question');
    await appendHostTurn(HOST_BASE, chatId, 'first question', 'first answer');

    // A reload is a fresh API instance reading the same host store — this is
    // the read path the chat unit's boot `loadChatSessions` uses.
    const afterReload = createHostChatSessionsApi(HOST_BASE);
    const listed = await afterReload.listChatSessions();
    expect(listed.active_chat_id).toBe(chatId);
    const switched = await afterReload.switchChatSession(listed.active_chat_id);
    expect(switched.chat_session.messages?.map((m) => m.content)).toEqual(['first question', 'first answer']);
  });

  it('ignores an empty query and a missing chat id', async () => {
    await appendHostTurn(HOST_BASE, 'host-chat-1', '   ');
    await appendHostTurn(HOST_BASE, null, 'q');
    const switched = await createHostChatSessionsApi(HOST_BASE).switchChatSession('host-chat-1');
    expect(switched.chat_session.messages ?? []).toEqual([]);
  });
});

describe('the active-store read helpers route to the host store', () => {
  const hostWithStore = (): void => {
    const base = headlessHost();
    setActiveHost({
      ...base,
      transport: { apiBaseURL: HOST_BASE, wsURL: '', authMode: 'bearer' },
      capabilities: { ...base.capabilities, chat: true, chatSessions: true },
    });
  };

  afterEach(() => setActiveHost(headlessHost()));

  it('reads messages and lists sessions from the host store when the capability is on', async () => {
    hostWithStore();
    const api = createHostChatSessionsApi(HOST_BASE);
    const chatId = (await api.listChatSessions()).active_chat_id;
    await appendHostTurn(HOST_BASE, chatId, 'q', 'a');

    const listed = await listChatSessionsForActiveStore();
    expect(listed.active_chat_id).toBe(chatId);
    const messages = await fetchChatSessionMessagesForActiveStore(chatId);
    expect(messages.chat_session.messages?.map((m) => m.content)).toEqual(['q', 'a']);
  });

  it('falls back to the caller fetch when the capability is off', async () => {
    setActiveHost(headlessHost());
    const calls: string[] = [];
    const fetchFn: typeof fetch = async (input) => {
      calls.push(typeof input === 'string' ? input : input.toString());
      return new Response(JSON.stringify({ message: 'ok', chat_sessions: [], active_chat_id: '' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    };
    await listChatSessionsForActiveStore(fetchFn);
    expect(calls[0]).toBe('/api/chat-sessions');
  });
});
