// @ts-nocheck
/**
 * ChatView rendered with no chat props — what an embedding gets when its
 * views arrangement names `chat` but supplies no chat data (ViewsLayout passes
 * no per-view props). It must render an empty, disabled chat, not crash on the
 * missing message list. Harness mirrors ChatView.export.test.tsx.
 */

import { fireEvent, screen, waitFor } from '@testing-library/react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

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

/* --- react-virtuoso --- */
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({ data, itemContent, children, components, ...rest }: any) =>
    createElement(
      'div',
      { 'data-testid': 'virtuoso-stub', ...rest },
      data?.map((msg: any, i: number) => createElement('div', { key: i }, itemContent?.(i, msg))),
      components?.Header?.(),
      components?.Footer?.(),
    ),
}));

/* --- config/mode --- */
// host.8: ChatView reads capabilities from the host, not config/mode.
vi.mock('../config/mode', () => ({}));

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

/* --- chat sub-components --- */
vi.mock('./chat', () => ({
  ChatFooter: () => null,
  ChatHeader: () => null,
  EmptyChatPanel: () => createElement('div', { 'data-testid': 'empty-chat' }),
  MessageItem: () => null,
}));

/* --- CommandInput --- */
const commandInputProps: { disabled?: boolean; placeholder?: string } = {};
vi.mock('./CommandInput', () => ({
  default: (p: { disabled?: boolean; placeholder?: string }) => {
    commandInputProps.disabled = p.disabled;
    commandInputProps.placeholder = p.placeholder;
    return createElement('div', { 'data-testid': 'command-input-stub' });
  },
}));

/* --- InlineTodoSummary --- */
vi.mock('./InlineTodoSummary', () => ({
  default: () => null,
}));

/* --- ExportDialog — stub that records its props --- */
const exportDialogProps: { isOpen: boolean; sessionId: string } = { isOpen: false, sessionId: '' };

vi.mock('./ExportDialog', () => ({
  default: ({ isOpen, sessionId }: { isOpen: boolean; sessionId: string }) => {
    exportDialogProps.isOpen = isOpen;
    exportDialogProps.sessionId = sessionId;
    if (!isOpen) return null;
    return createElement('div', { 'data-testid': 'export-dialog-stub', 'data-session-id': sessionId });
  },
}));

/* --- CSS --- */
vi.mock('./Chat.css', () => ({}));

/* --- utils/log --- */
vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  // ChatHistorySwitcher (rendered in the chat-header row on every state,
  // including empty chats) calls useLog for error toasts.
  useLog: () => ({ info: vi.fn(), error: vi.fn(), success: vi.fn(), warn: vi.fn() }),
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks
// ---------------------------------------------------------------------------

import { EventsContextProvider } from '../contexts/EventsContext';
import ChatView from './ChatView';
import { HostProvider, headlessHost } from '../host/HostProvider';

// ---------------------------------------------------------------------------
// Test setup
// ---------------------------------------------------------------------------

/** Mock events transport: ChatView renders ProgressStrip, which
 *  subscribes via useEvents — provide a no-op provider like the app's
 *  EventsContextProvider does. */
const provider = {
  connect: vi.fn(),
  disconnect: vi.fn(),
  onEvent: vi.fn(),
  removeEvent: vi.fn(),
  sendEvent: vi.fn(),
  isConnected: vi.fn(() => true),
  onReconnect: vi.fn(),
  freeze: vi.fn(),
  resume: vi.fn(),
  resetAndReconnect: vi.fn(),
  getQueuedMessageCount: vi.fn(() => 0),
  flushQueuedMessages: vi.fn(() => 0),
};

// host.8: ChatView reads capabilities (ssh/export) from the host contract.
const testHost = { ...headlessHost(), capabilities: { ...headlessHost().capabilities, ssh: false, export: true } };

function wrap(node: React.ReactNode) {
  return createElement(HostProvider, { host: testHost }, createElement(EventsContextProvider, { provider }, node));
}

// ---------------------------------------------------------------------------
// Test setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  container.style.width = '400px';
  container.style.height = '600px';
  document.body.appendChild(container);
  root = createRoot(container);
  exportDialogProps.isOpen = false;
  exportDialogProps.sessionId = '';

  // Re-establish ResizeObserver mock (vi.restoreAllMocks in afterEach clears it)
  // Constructible: ChatView.tsx does `new ResizeObserver(updateHeight)`, and
  // Vitest 4's spy invokes the mock implementation with `new` — arrows are not
  // constructible.
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
  vi.clearAllMocks();
});

describe('ChatView without chat props', () => {
  it('renders an empty, disabled chat instead of crashing', async () => {
    await act(async () => {
      root.render(wrap(createElement(ChatView, {})));
    });
    expect(screen.getByTestId('command-input-stub')).toBeTruthy();
    expect(commandInputProps.disabled).toBe(true);
    expect(commandInputProps.placeholder).toBe('Chat is not available here.');
  });

  it('keeps the input enabled when the host supplies a send handler', async () => {
    await act(async () => {
      root.render(wrap(createElement(ChatView, { messages: [], onSendMessage: vi.fn(), onInputChange: vi.fn(), inputValue: '' })));
    });
    expect(commandInputProps.disabled).toBe(false);
  });
});
