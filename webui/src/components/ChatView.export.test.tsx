// @ts-nocheck
/**
 * Focused Vitest tests for the Export button in ChatView (Chat component).
 *
 * Uses aggressive mocking to isolate the toolbar section containing the
 * Export button and the ExportDialog it controls. Mirrors the pattern in
 * Sidebar.sessionSearch.test.tsx (createRoot + act + heavy vi.mock).
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { fireEvent, screen, waitFor } from '@testing-library/react';

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
vi.mock('../config/mode', () => ({
  supportsSSH: false,
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

/* --- chat sub-components --- */
vi.mock('./chat', () => ({
  ChatFooter: () => null,
  ChatHeader: () => null,
  EmptyChatPanel: () => createElement('div', { 'data-testid': 'empty-chat' }),
  MessageItem: () => null,
}));

/* --- CommandInput --- */
vi.mock('./CommandInput', () => ({
  default: () => createElement('div', { 'data-testid': 'command-input-stub' }),
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
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks
// ---------------------------------------------------------------------------

import ChatView from './ChatView';

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
  global.ResizeObserver = vi.fn().mockImplementation(() => ({
    observe: vi.fn(),
    unobserve: vi.fn(),
    disconnect: vi.fn(),
  }));
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

/** Minimal ChatView props — enough to render without side-effects */
const minimalProps = {
  messages: [],
  onSendMessage: vi.fn(),
  onInputChange: vi.fn(),
  inputValue: '',
  isProcessing: false,
  chatId: undefined,
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ChatView Export button', () => {
  /* ---- 1. Export button does NOT render without sessionId ---- */
  it('does not render export button when chatId is undefined', () => {
    act(() => {
      root.render(createElement(ChatView, minimalProps));
    });

    expect(screen.queryByTestId('chat-export-button')).toBeNull();
  });

  /* ---- 2. Export button renders when sessionId is set ---- */
  it('renders export button when chatId is set', () => {
    act(() => {
      root.render(createElement(ChatView, { ...minimalProps, chatId: 'abc-123' }));
    });

    expect(screen.getByTestId('chat-export-button')).toBeInTheDocument();
  });

  /* ---- 3. ExportDialog is closed initially ---- */
  it('ExportDialog is closed initially when chatId is set', () => {
    act(() => {
      root.render(createElement(ChatView, { ...minimalProps, chatId: 'abc-123' }));
    });

    expect(exportDialogProps.isOpen).toBe(false);
    expect(exportDialogProps.sessionId).toBe('abc-123');
  });

  /* ---- 4. Clicking Export button opens the ExportDialog ---- */
  it('clicking the export button opens the ExportDialog', () => {
    act(() => {
      root.render(createElement(ChatView, { ...minimalProps, chatId: 'abc-123' }));
    });

    const exportBtn = screen.getByTestId('chat-export-button');
    act(() => {
      fireEvent.click(exportBtn);
    });

    expect(exportDialogProps.isOpen).toBe(true);
    expect(exportDialogProps.sessionId).toBe('abc-123');
  });

  /* ---- 5. ExportDialog receives the correct sessionId ---- */
  it('ExportDialog receives the chatId as sessionId', () => {
    const sessionId = 'session-xyz-789';

    act(() => {
      root.render(createElement(ChatView, { ...minimalProps, chatId: sessionId }));
    });

    // Dialog is closed initially but sessionId is already passed
    expect(exportDialogProps.sessionId).toBe(sessionId);

    // Open it
    act(() => {
      fireEvent.click(screen.getByTestId('chat-export-button'));
    });

    expect(exportDialogProps.isOpen).toBe(true);
    expect(exportDialogProps.sessionId).toBe(sessionId);
  });

  /* ---- 6. Export button is not present when chatId is empty string ---- */
  it('does not render export button when chatId is empty string', () => {
    act(() => {
      root.render(createElement(ChatView, { ...minimalProps, chatId: '' }));
    });

    expect(screen.queryByTestId('chat-export-button')).toBeNull();
  });
});
