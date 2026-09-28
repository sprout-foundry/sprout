import { beforeEach, describe, expect, it } from 'vitest';
import {
  __resetCloudChatsForTests,
  handleCloudChatSessionsEndpoint,
  historyForChat,
  recordTurn,
  setChatRunning,
} from './cloudChatSessions';
import { getCurrentCloudSessionId, saveSession } from './cloudSessionStore';

async function call(path: string, method = 'GET', body?: unknown) {
  const res = handleCloudChatSessionsEndpoint(
    path.split('?')[0],
    method,
    `http://x${path}`,
    body === undefined ? undefined : JSON.stringify(body),
  );
  expect(res).not.toBeNull();
  return { status: res!.status, json: await res!.json() };
}

const msg = (type: 'user' | 'assistant', content: string, i: number) => ({
  id: `m${i}`,
  type,
  content,
  timestamp: new Date(),
});

beforeEach(() => {
  window.localStorage.clear();
  __resetCloudChatsForTests();
});

describe('cloud chat sessions', () => {
  it('starts with the existing conversation as the only chat', async () => {
    const { json } = await call('/api/chat-sessions');
    expect(json.chat_sessions).toHaveLength(1);
    expect(json.active_chat_id).toBe(json.chat_sessions[0].id);
  });

  it('keeps a separate transcript per chat and restores it on switch', async () => {
    const first = (await call('/api/chat-sessions')).json.active_chat_id;
    saveSession([msg('user', 'first chat question', 1), msg('assistant', 'first answer', 2)] as never);

    const created = (await call('/api/chat-sessions/create', 'POST', {})).json.chat_session;
    // Creating does not switch; the UI switches next.
    expect((await call('/api/chat-sessions')).json.active_chat_id).toBe(first);
    await call('/api/chat-sessions/switch', 'POST', { id: created.id });
    expect(getCurrentCloudSessionId()).toBe(created.current_session_id);
    saveSession([msg('user', 'second chat question', 3)] as never);

    const back = (await call('/api/chat-sessions/switch', 'POST', { id: first })).json;
    expect(back.active_chat_id).toBe(first);
    expect(back.chat_session.messages.map((m: { content: string }) => m.content)).toEqual([
      'first chat question',
      'first answer',
    ]);

    const list = (await call('/api/chat-sessions')).json.chat_sessions;
    expect(list).toHaveLength(2);
    expect(list.find((c: { id: string }) => c.id === first).name).toBe('first chat question');
  });

  it('renames and deletes, keeping at least one chat', async () => {
    const id = (await call('/api/chat-sessions')).json.active_chat_id;
    const renamed = (await call('/api/chat-sessions/rename', 'POST', { id, name: 'Checkout work' })).json;
    expect(renamed.chat_session.name).toBe('Checkout work');
    await call('/api/chat-sessions/delete', 'POST', { id });
    const after = (await call('/api/chat-sessions')).json;
    expect(after.chat_sessions).toHaveLength(1);
    expect(after.chat_sessions[0].id).not.toBe(id);
  });

  it('leaves worktree endpoints to the synthetic stubs', () => {
    expect(handleCloudChatSessionsEndpoint('/api/chat-sessions/compact', 'POST', 'http://x')).toBeNull();
  });

  it('gives a chat its saved turns as agent history, without the pending query', async () => {
    const id = (await call('/api/chat-sessions')).json.active_chat_id;
    saveSession([msg('user', 'hello', 1), msg('assistant', 'hi there', 2), msg('user', 'next question', 3)] as never);
    expect(historyForChat(id, 'next question')).toEqual([
      { role: 'user', content: 'hello' },
      { role: 'assistant', content: 'hi there' },
    ]);
    expect(historyForChat('missing-chat')).toEqual([]);
  });

  it('saves an off-screen chat’s answer without taking over the on-screen chat', async () => {
    const first = (await call('/api/chat-sessions')).json.active_chat_id;
    saveSession([msg('user', 'long question', 1)] as never);
    const second = (await call('/api/chat-sessions/create', 'POST', {})).json.chat_session;
    await call('/api/chat-sessions/switch', 'POST', { id: second.id });
    const onScreen = getCurrentCloudSessionId();

    setChatRunning(first, true);
    expect((await call('/api/chat-sessions')).json.chat_sessions[0].active_query).toBe(true);
    recordTurn(first, 'long question', 'long answer');
    setChatRunning(first, false);

    expect(getCurrentCloudSessionId()).toBe(onScreen);
    const back = (await call('/api/chat-sessions/switch', 'POST', { id: first })).json.chat_session;
    expect(back.active_query).toBe(false);
    expect(back.messages.map((m: { content: string }) => m.content)).toEqual(['long question', 'long answer']);
  });

  it('records the question at the start and the answer once', async () => {
    const id = (await call('/api/chat-sessions')).json.active_chat_id;
    recordTurn(id, 'q1');
    expect(historyForChat(id)).toEqual([{ role: 'user', content: 'q1' }]);
    recordTurn(id, 'q1', 'a1');
    recordTurn(id, 'q1', 'a1');
    expect(historyForChat(id)).toEqual([
      { role: 'user', content: 'q1' },
      { role: 'assistant', content: 'a1' },
    ]);
  });
});
