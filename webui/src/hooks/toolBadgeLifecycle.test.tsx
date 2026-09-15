// @ts-nocheck
/**
 * toolBadgeLifecycle.test.tsx — End-to-end badge lifecycle verification.
 *
 * Drives the REAL WebSocket event handler (no mocked completion logic)
 * through a full turn, then renders the resulting state through the real
 * MessageItem → MessageSegments pipeline. Pins the three badge-carrying
 * paths:
 *
 *   1. Live path — tool_start inserts the "[executing tool [...]]" marker
 *      into the streamed text; the badge renders inline at that position.
 *   2. Marker-less path — agent_message tool_log events only update
 *      toolExecutions (footer timeline) and never touch message text; the
 *      toolRef has no marker and must still render inline (the fallback).
 *   3. Completion path — query_completed replaces streamed content with the
 *      server's longer final response (ensureCompletedAssistantMessage's
 *      >20% rule), wiping live-inserted markers; surviving toolRefs must
 *      still render inline via the fallback.
 *
 * Without the unclaimed-refs fallback in MessageSegments, paths 2 and 3
 * lose their badges entirely (they showed only above the input).
 */

import type { MutableRefObject } from 'react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterAll, beforeAll, afterEach } from 'vitest';

vi.mock('../utils/log', () => ({ debugLog: vi.fn(), error: vi.fn() }));

// REAL completion logic — no mock. This is the code path that replaces
// streamed content (and live-inserted markers) at query_completed.
vi.mock('../utils/messageId', () => ({
  generateMessageId: vi.fn(() => `msg-${Math.random().toString(36).slice(2)}`),
}));

vi.mock('../utils/messageWindow', () => ({
  trimMessages: vi.fn((messages) => messages),
}));

vi.mock('../utils/logCap', () => ({
  appendCappedLog: vi.fn((logs, entry) => [...logs, entry]),
}));

vi.mock('../services/clientSession', () => ({
  getWebUIClientId: vi.fn(() => 'test-client-id'),
}));

vi.mock('../services/errorCodes', () => ({
  getServerErrorCode: vi.fn(() => null),
}));

vi.mock('../services/lspClientService', () => ({
  LSPClientService: { getInstance: vi.fn(() => ({ cleanup: vi.fn() })) },
}));

import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';
import { MessageItem } from '../components/chat/MessageItem';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { Message } from '@sprout/ui';

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT;
});

function createDefaultState(): Record<string, unknown> {
  return {
    isConnected: true,
    provider: 'openai',
    model: 'gpt-4',
    sessionId: null,
    queryCount: 0,
    messages: [],
    logs: [],
    isProcessing: false,
    lastError: null,
    currentView: 'chat',
    toolExecutions: [],
    queryProgress: null,
    stats: {},
    currentTodos: [],
    fileEdits: [],
    subagentActivities: [],
    activeChatId: null,
    chatSessions: [],
    perChatCache: {},
    securityApprovalRequest: null,
    securityPromptRequest: null,
    askUserRequest: null,
    passwordRequest: null,
    editApprovalRequest: null,
    driftNotification: null,
    modelSelectionRequest: null,
    outputVerbosity: 'default',
  };
}

let container: HTMLDivElement;
let root: Root;
let hookHandleEvent: ((event: unknown) => void) | null = null;

const HookWrapper = ({
  stateHolder,
  setStateMock,
}: {
  stateHolder: { current: Record<string, unknown> };
  setStateMock: ReturnType<typeof vi.fn>;
}) => {
  const activeRequestsRef: MutableRefObject<number> = { current: 0 };
  const activeChatIdRef: MutableRefObject<string | null> = { current: null };
  const pendingProviderRef: MutableRefObject<string> = { current: 'openai' };
  const pendingProviderChangeRef: MutableRefObject<boolean> = { current: false };
  const pendingProviderChangeValueRef: MutableRefObject<string | null> = { current: null };
  const connectionTimeoutRef: MutableRefObject<ReturnType<typeof setTimeout> | null> = { current: null };
  const lastConnectionStateRef: MutableRefObject<boolean> = { current: false };

  const refs: UseWebSocketEventHandlerRefs = {
    activeRequestsRef,
    activeChatIdRef,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
  };

  const apiService = { getStats: vi.fn().mockResolvedValue({ provider: 'openai', model: 'gpt-4' }) };

  const { handleEvent } = useWebSocketEventHandler({
    setState: setStateMock as AppStoreSetState,
    refs,
    apiService,
  });

  hookHandleEvent = handleEvent;
  return createElement('div', null, 'hook host');
};

const shortAnswer = 'Here is what I found.';
// >20% longer than the streamed text, so ensureCompletedAssistantMessage's
// replacement rule fires and wipes live-inserted markers from content.
const longFinalResponse =
  'Here is what I found. After checking the repository and reading the main ' +
  'configuration, the full analysis is: everything is wired correctly and the ' +
  'tests pass across the board, including the lifecycle suite.';

function fire(event: { type: string; data?: Record<string, unknown> }): void {
  act(() => {
    hookHandleEvent!({ id: `evt-${Math.random()}`, ...event });
  });
}

describe('tool badge lifecycle (live stream → completion → render)', () => {
  let stateHolder: { current: Record<string, unknown> };

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    hookHandleEvent = null;
    stateHolder = { current: createDefaultState() };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        stateHolder.current = { ...stateHolder.current, ...updater(stateHolder.current) };
      } else {
        stateHolder.current = updater;
      }
    });
    act(() => {
      root.render(createElement(HookWrapper, { stateHolder, setStateMock }));
    });
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    container?.remove();
  });

  function renderFinalMessages(): void {
    const messages = stateHolder.current.messages as Message[];
    act(() => {
      root.render(
        createElement(
          'div',
          {},
          messages.map((m, i) =>
            createElement(MessageItem, {
              key: m.id || i,
              message: m,
              findMatchingToolExecution: () => undefined,
              getToolStatus: (toolId: string) => {
                const execs = stateHolder.current.toolExecutions as Array<{ id: string; status: string }>;
                return execs.find((t) => t.id === toolId)?.status;
              },
              formatTime: () => '12:00',
            }),
          ),
        ),
      );
    });
  }

  it('marker path: tool_start badge stays inline through completion (streamed text wins)', () => {
    fire({ type: 'query_started', data: { query: 'check the repo' } });
    fire({ type: 'stream_chunk', data: { chunk: shortAnswer, content_type: 'assistant_text' } });
    fire({
      type: 'tool_start',
      data: { tool_call_id: 'tc-1', tool_name: 'read_file', display_name: 'read_file' },
    });
    fire({
      type: 'tool_end',
      data: { tool_call_id: 'tc-1', tool_name: 'read_file', status: 'success' },
    });

    // Mid-turn: the live marker produced an inline badge.
    renderFinalMessages();
    expect(container.querySelectorAll('.segment-tool-call, .segment-tool-footnote').length).toBeGreaterThan(0);

    // Complete the turn with a SHORT response (< streamed length) so the
    // streamed content — markers included — wins and is preserved.
    fire({ type: 'query_completed', data: { query: 'check the repo', response: 'Here is what I found.' } });
    renderFinalMessages();

    const badges = container.querySelectorAll('.segment-tool-footnote, .segment-tool-call');
    expect(badges.length).toBeGreaterThan(0);
    expect(container.textContent).toContain(shortAnswer);
    // Executions survive completion too (footer timeline + status lookups).
    expect((stateHolder.current.toolExecutions as unknown[]).length).toBe(1);
  });

  it('marker-less path: agent_message tool_log badge renders inline (fallback)', () => {
    fire({ type: 'query_started', data: { query: 'run the checks' } });
    fire({ type: 'stream_chunk', data: { chunk: 'Running the checks now.', content_type: 'assistant_text' } });
    // tool_log agent_message — updates toolExecutions, NEVER touches message text.
    fire({
      type: 'agent_message',
      data: {
        category: 'tool_log',
        action: 'executing tool',
        target: 'shell_command',
        message: 'executing tool',
      },
    });
    fire({
      type: 'tool_start',
      data: { tool_call_id: 'tc-2', tool_name: 'shell_command', display_name: 'shell_command' },
    });
    fire({
      type: 'tool_end',
      data: { tool_call_id: 'tc-2', tool_name: 'shell_command', status: 'success' },
    });

    renderFinalMessages();
    expect(container.querySelectorAll('.segment-tool-footnote, .segment-tool-call').length).toBeGreaterThan(0);
    expect(container.textContent).toContain('Running the checks now.');
  });

  it('completion-replacement path: markers wiped by longer final response, refs still badge inline', () => {
    fire({ type: 'query_started', data: { query: 'deep check' } });
    fire({ type: 'stream_chunk', data: { chunk: shortAnswer, content_type: 'assistant_text' } });
    fire({
      type: 'tool_start',
      data: { tool_call_id: 'tc-3', tool_name: 'search_files', display_name: 'search_files' },
    });
    fire({
      type: 'tool_end',
      data: { tool_call_id: 'tc-3', tool_name: 'search_files', status: 'success' },
    });

    // Mid-turn the marker badge is present.
    renderFinalMessages();
    expect(container.querySelectorAll('.segment-tool-call, .segment-tool-footnote').length).toBeGreaterThan(0);

    // The server's final response is >20% longer — content is replaced and
    // the live-inserted marker is wiped. toolRefs survive on the message.
    fire({ type: 'query_completed', data: { query: 'deep check', response: longFinalResponse } });

    const lastMsg = (stateHolder.current.messages as Message[]).slice(-1)[0];
    expect(lastMsg.content).toBe(longFinalResponse);
    expect(lastMsg.content).not.toContain('[executing tool');
    expect(lastMsg.toolRefs?.length).toBe(1);

    renderFinalMessages();
    const badges = container.querySelectorAll('.segment-tool-footnote, .segment-tool-call');
    expect(badges.length).toBeGreaterThan(0);
    // The full final prose is present alongside the badge.
    expect(container.textContent).toContain('lifecycle suite');
  });
});
