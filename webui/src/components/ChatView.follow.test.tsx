// @ts-nocheck
/**
 * SP-140-10b — live visibility while the agent is working.
 *
 * Pins the Chat's streaming-visibility contract (the gripe "content was
 * never visible even though it was doing things"):
 *
 * - while the user is at the bottom, `followOutput` resolves to 'smooth',
 *   so appended (streaming) messages keep the latest message in view;
 * - while scrolled up, `followOutput` resolves to false (no yank) and the
 *   jump-to-latest button appears; clicking it lands at the last message.
 *
 * The Virtuoso instance is stubbed (house pattern, ChatView.export.test):
 * the stub renders every item, exposes the imperative handle the jump
 * button drives, and lets the test simulate virtuoso's at-bottom state —
 * the part of the contract that lives in Chat, not in react-virtuoso.
 */

import { fireEvent, screen, waitFor } from '@testing-library/react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { forwardRef } from 'react';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing ChatView or any of its deps
// ---------------------------------------------------------------------------

/* --- lucide-react --- */
vi.mock('lucide-react', async (importOriginal) => {
  const actual = await importOriginal();
  const Stub = (props: any) => createElement('svg', { 'data-testid': 'icon', ...props });
  return {
    ...actual,
    ChevronDown: Stub,
    Download: Stub,
  };
});

/* --- @sprout/ui --- */
vi.mock('@sprout/ui', () => ({
  ChatMessageContextMenu: () => null,
  createHttpCommandCompletionApi: () => ({}),
}));

/* --- react-virtuoso: a stub that renders every item and exposes the
     imperative handle (scrollToIndex). The test drives virtuoso's
     at-bottom state through the captured atBottomStateChange. --- */
const virtuoso = {
  followOutput: undefined as ((atBottom: boolean) => 'smooth' | 'auto' | false) | undefined,
  atBottomStateChange: undefined as ((atBottom: boolean) => void) | undefined,
  scrollToIndex: vi.fn(),
  data: [] as any[],
};

vi.mock('react-virtuoso', () => ({
  Virtuoso: forwardRef(({ data, itemContent, ...rest }: any, ref: any) => {
    virtuoso.followOutput = rest.followOutput;
    virtuoso.atBottomStateChange = rest.atBottomStateChange;
    virtuoso.data = data ?? [];
    if (ref) ref.current = { scrollToIndex: virtuoso.scrollToIndex };
    return createElement(
      'div',
      { 'data-testid': 'virtuoso-stub', style: rest.style },
      (data ?? []).map((msg: any, i: number) => createElement('div', { key: i }, itemContent?.(i, msg))),
    );
  }),
}));

/* --- config/mode --- */
vi.mock('../config/mode', () => ({
  supportsSSH: false,
  supportsExport: true,
  isCloud: false,
}));

/* --- services/apiAdapter --- */
vi.mock('../services/apiAdapter', () => ({
  requiresBackendHealthCheck: () => false,
}));

/* --- services/api/chatApi --- */
vi.mock('../services/api/chatApi', () => ({
  rewindQuery: vi.fn(),
}));

/* --- services/clientSession --- */
vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));

/* --- ThemedDialog --- */
vi.mock('./ThemedDialog', () => ({
  showThemedAlert: vi.fn(() => Promise.resolve()),
  showThemedConfirm: vi.fn(() => Promise.resolve(false)),
}));

/* --- chat sub-components (MessageItem renders the content so the tests
     can assert the last message is in the transcript) --- */
vi.mock('./chat', () => ({
  ChatFooter: () => null,
  ChatHeader: () => null,
  EmptyChatPanel: () => createElement('div', { 'data-testid': 'empty-chat' }),
  MessageItem: ({ message }: any) =>
    createElement('div', { 'data-testid': 'message', 'data-content': message?.content }, message?.content),
}));

/* --- CommandInput --- */
vi.mock('./CommandInput', () => ({
  default: () => createElement('div', { 'data-testid': 'command-input-stub' }),
}));

/* --- InlineTodoSummary --- */
vi.mock('./InlineTodoSummary', () => ({
  default: () => null,
}));

/* --- ExportDialog --- */
vi.mock('./ExportDialog', () => ({
  default: () => null,
}));

/* --- CSS --- */
vi.mock('./Chat.css', () => ({}));

/* --- utils/log --- */
vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  useLog: () => ({ info: vi.fn(), error: vi.fn(), success: vi.fn(), warn: vi.fn() }),
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks
// ---------------------------------------------------------------------------

import ChatView from './ChatView';
import type { Message } from './chat/types';

// ---------------------------------------------------------------------------
// Test setup
// ---------------------------------------------------------------------------

const message = (id: string, content: string, type: 'user' | 'assistant' = 'user'): Message => ({
  id,
  type,
  content,
  timestamp: new Date(),
});

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  virtuoso.scrollToIndex.mockClear();
  virtuoso.followOutput = undefined;
  virtuoso.atBottomStateChange = undefined;
  virtuoso.data = [];

  // Constructible: ChatView does `new ResizeObserver(updateHeight)`.
  global.ResizeObserver = vi.fn(function (this: any) {
    this.observe = vi.fn();
    this.unobserve = vi.fn();
    this.disconnect = vi.fn();
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  vi.restoreAllMocks();
});

function renderChat(messages: Message[], overrides: Record<string, unknown> = {}) {
  act(() => {
    root.render(
      createElement(ChatView, {
        messages,
        onSendMessage: vi.fn(),
        onInputChange: vi.fn(),
        inputValue: '',
        isProcessing: false,
        chatId: undefined,
        ...overrides,
      }),
    );
  });
  // Simulate virtuoso's initial position: the mount jumps to the last item
  // (initialTopMostItemIndex), so the chat starts at the bottom.
  act(() => {
    virtuoso.atBottomStateChange?.(true);
  });
}

function rerenderMessages(messages: Message[]) {
  act(() => {
    root.render(
      createElement(ChatView, {
        messages,
        onSendMessage: vi.fn(),
        onInputChange: vi.fn(),
        inputValue: '',
        isProcessing: false,
        chatId: undefined,
      }),
    );
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ChatView streaming visibility (SP-140-10b)', () => {
  it('at the bottom, followOutput is smooth so appended messages stay in view', () => {
    renderChat([message('m1', 'first'), message('m2', 'second')]);
    expect(virtuoso.followOutput?.(true)).toBe('smooth');
    expect(virtuoso.followOutput?.(false)).toBe(false);

    // Append a message while at the bottom (a streaming turn): the last
    // message renders and the follow policy is unchanged.
    rerenderMessages([message('m1', 'first'), message('m2', 'second'), message('m3', 'third')]);
    expect(screen.getByText('third')).toBeInTheDocument();
    expect(virtuoso.followOutput?.(true)).toBe('smooth');
  });

  it('scrolled up: the jump-to-latest button appears and lands at the last message', () => {
    renderChat([message('m1', 'first'), message('m2', 'second')]);
    expect(screen.queryByTestId('chat-scroll-bottom')).toBeNull();

    // The user scrolls up mid-run.
    act(() => {
      virtuoso.atBottomStateChange?.(false);
    });
    const jump = screen.getByTestId('chat-scroll-bottom');

    // Clicking it drives the virtuoso handle to the LAST item (and the
    // chat returns to the bottom, so the button goes away).
    act(() => {
      fireEvent.click(jump);
    });
    expect(virtuoso.scrollToIndex).toHaveBeenCalledWith({ index: 'LAST', behavior: 'smooth', align: 'end' });
    act(() => {
      virtuoso.atBottomStateChange?.(true);
    });
    expect(screen.queryByTestId('chat-scroll-bottom')).toBeNull();
  });

  it('keeps the jump button hidden while following from the bottom', () => {
    renderChat([message('m1', 'first'), message('m2', 'second')]);
    rerenderMessages([message('m1', 'first'), message('m2', 'second'), message('m3', 'third')]);
    act(() => {
      virtuoso.atBottomStateChange?.(true);
    });
    expect(screen.queryByTestId('chat-scroll-bottom')).toBeNull();
    expect(screen.getByText('third')).toBeInTheDocument();
  });
});
