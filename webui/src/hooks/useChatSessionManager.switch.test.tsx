import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { AppState } from '../types/app';
import { useChatSessionManager, type QueuedMessage } from './useChatSessionManager';

const sessions = vi.hoisted(() => ({
  switchChatSession: vi.fn(),
  listChatSessions: vi.fn().mockResolvedValue({ chat_sessions: [] }),
  deleteChatSession: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../services/api', () => ({ ApiService: { getInstance: () => ({}) } }));
vi.mock('../services/chatSessions', () => ({
  ...sessions,
  createChatSession: vi.fn(),
  deleteAllChatSessions: vi.fn(),
  renameChatSession: vi.fn(),
  createChatSessionInWorktree: vi.fn(),
  fetchChatSessionMessages: vi.fn(),
}));
vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));
vi.mock('../services/notificationBus', () => ({ notificationBus: { notify: vi.fn() } }));

function setup() {
  let state = {
    activeChatId: 'chat-a',
    messages: [{ id: 'a1', type: 'user', content: 'from A', timestamp: new Date() }],
    isProcessing: false,
    perChatCache: {},
    toolExecutions: [],
    fileEdits: [],
    subagentActivities: [],
    currentTodos: [],
  } as unknown as AppState;
  const setState: AppStoreSetState = (updater) => {
    const partial = typeof updater === 'function' ? (updater as (p: AppState) => Partial<AppState>)(state) : updater;
    state = { ...state, ...partial };
  };
  const activeChatIdRef = { current: 'chat-a' as string | null };
  const { result } = renderHook(() =>
    useChatSessionManager({
      setState,
      activeRequestsRef: { current: 0 },
      activeChatIdRef,
      queuedMessagesRef: { current: [] as QueuedMessage[] },
      isProcessing: false,
    }),
  );
  return { result, activeChatIdRef, getState: () => state };
}

describe('chat switch', () => {
  it('moves the screen back to the previous chat when the switch fails', async () => {
    sessions.switchChatSession.mockRejectedValueOnce(new Error('mode_mismatch'));
    const h = setup();

    let ok = true;
    await act(async () => {
      ok = await h.result.current.handleActiveChatChange('chat-b');
    });

    expect(ok).toBe(false);
    expect(h.activeChatIdRef.current).toBe('chat-a');
    expect(h.getState().activeChatId).toBe('chat-a');
    expect(h.getState().messages.map((m) => m.content)).toEqual(['from A']);
  });

  it('lands on the chat the user picked even if the server names another active chat', async () => {
    sessions.switchChatSession.mockResolvedValueOnce({
      active_chat_id: 'chat-c',
      chat_session: { messages: [{ role: 'user', content: 'from B' }], active_query: false },
    });
    const h = setup();

    await act(async () => {
      await h.result.current.handleActiveChatChange('chat-b');
    });

    expect(h.getState().activeChatId).toBe('chat-b');
    expect(h.getState().messages.map((m) => m.content)).toEqual(['from B']);
  });

  it('deleting the chat you are in moves to another chat first, then deletes', async () => {
    const calls: string[] = [];
    sessions.listChatSessions.mockResolvedValue({
      chat_sessions: [
        { id: 'chat-a', mode: 'code' },
        { id: 'chat-d', mode: 'design' },
        { id: 'chat-b', mode: 'code' },
      ],
    });
    sessions.switchChatSession.mockImplementation(async (id: string) => {
      calls.push(`switch:${id}`);
      return { active_chat_id: id, chat_session: { messages: [], active_query: false } };
    });
    sessions.deleteChatSession.mockImplementation(async (id: string) => {
      calls.push(`delete:${id}`);
    });
    const h = setup();

    await act(async () => {
      await h.result.current.handleDeleteChat('chat-a');
    });

    expect(calls).toEqual(['switch:chat-b', 'delete:chat-a']);
    expect(h.getState().activeChatId).toBe('chat-b');
  });

  it('keeps a message sent while the switch was in flight', async () => {
    let finishSwitch!: (v: unknown) => void;
    sessions.switchChatSession.mockImplementationOnce(() => new Promise((resolve) => (finishSwitch = resolve)));
    const h = setup();

    let switching!: Promise<boolean>;
    act(() => {
      switching = h.result.current.handleActiveChatChange('chat-b');
    });
    // The user sends in chat B before the switch response lands.
    h.getState().messages.push({ id: 'sent', type: 'user', content: 'LEFT-THREE?', timestamp: new Date() } as never);
    await act(async () => {
      finishSwitch({
        active_chat_id: 'chat-b',
        chat_session: {
          active_query: false,
          messages: [
            { role: 'user', content: 'LEFT-ONE?' },
            { role: 'assistant', content: 'LEFT-ONE' },
          ],
        },
      });
      await switching;
    });

    expect(h.getState().messages.map((m) => m.content)).toEqual(['LEFT-ONE?', 'LEFT-ONE', 'LEFT-THREE?']);
    expect(h.getState().isProcessing).toBe(true);
  });
});
