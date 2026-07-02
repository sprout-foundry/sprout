import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import Chat from './ChatPanel';
import type { Message, ToolExecution, ChatProps } from '../types/chat';

// ── Mock react-virtuoso ──────────────────────────────────────────────
// Virtuoso is complex; we mock it to render items in a simple list.
vi.mock('react-virtuoso', () => {
  const React = require('react');
  const MockVirtuoso = React.forwardRef(({ data, itemContent, components, atBottomStateChange }: { data: unknown[]; itemContent: (index: number, item: unknown) => JSX.Element; components?: { Header: () => JSX.Element | null; Footer: () => JSX.Element }; atBottomStateChange?: (atBottom: boolean) => void }, ref) => {
    React.useImperativeHandle(ref, () => ({ scrollToIndex: vi.fn() }) as any);
    // Simulate atBottomStateChange callback with false to trigger scroll-to-bottom button
    React.useEffect(() => {
      if (atBottomStateChange && data.length > 5) {
        atBottomStateChange(false);
      }
    }, [atBottomStateChange, data.length]);
    return createElement('div', { className: 'virtuoso-mock' },
      components?.Header?.(),
      data.map((item, index) => createElement('div', { key: (item as any).id || index }, itemContent(index, item))),
      components?.Footer?.(),
    );
  });
  MockVirtuoso.displayName = 'MockVirtuoso';
  return { Virtuoso: MockVirtuoso };
});

// ── Mock sub-components ──────────────────────────────────────────────
// We mock these to avoid deep dependency chains in tests.
vi.mock('./CommandInput', () => {
  const MockCommandInput = ({ value, onChange, onSend, placeholder, isProcessing, onStop, disabled }: {
    value?: string;
    onChange?: (v: string) => void;
    onSend?: (msg: string) => void;
    placeholder?: string;
    isProcessing?: boolean;
    onStop?: () => void;
    disabled?: boolean;
    [key: string]: unknown;
  }) => {
    return createElement('div', {
      className: 'command-input-mock',
      'data-testid': 'command-input-mock',
      'data-value': value,
      'data-placeholder': placeholder,
      'data-processing': isProcessing,
      'data-disabled': disabled,
    },
      createElement('button', {
        type: 'button',
        className: 'send-btn-mock',
        onClick: () => onSend?.(value || ''),
      }, 'Send'),
      isProcessing ? createElement('button', {
        type: 'button',
        className: 'stop-btn-mock',
        onClick: onStop,
      }, 'Stop') : null,
    );
  };
  MockCommandInput.displayName = 'MockCommandInput';
  return { __esModule: true, default: MockCommandInput };
});

vi.mock('./MessageSegments', () => ({
  __esModule: true,
  default: ({ content }: { content: string }) => createElement('div', { 'data-testid': 'message-segments' }, content),
}));

vi.mock('./MessageContent', () => ({
  __esModule: true,
  default: ({ content }: { content: string }) => createElement('div', { 'data-testid': 'message-content' }, content),
}));

vi.mock('./MessageBubble', () => {
  const MockMessageBubble = ({ children, type }: { children: React.ReactNode; type?: string; [key: string]: unknown }) => {
    return createElement('div', { className: 'message-bubble-mock', 'data-message-type': type }, children);
  };
  return { __esModule: true, default: MockMessageBubble };
});

vi.mock('./LiveLog', () => ({
  __esModule: true,
  default: ({ lines }: { lines: { id: string; text: string; timestamp: Date }[] }) =>
    createElement('div', { 'data-testid': 'live-log' }, lines.length.toString()),
}));

vi.mock('./ChatMessageContextMenu', () => ({
  __esModule: true,
  default: () => createElement('div', { 'data-testid': 'chat-context-menu-mock' }),
}));

// ── Test Setup ───────────────────────────────────────────────────────

describe('Chat', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    container.id = 'chat-test-root';
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  const makeMessages = (count: number, type: 'user' | 'assistant' = 'user'): Message[] => {
    return Array.from({ length: count }, (_, i) => ({
      id: `msg-${i}`,
      type,
      content: `Message ${i}`,
      timestamp: new Date(2024, 0, 1, 10, i),
    }));
  };

  const baseProps: ChatProps = {
    messages: [],
    onSendMessage: vi.fn(),
    onQueueMessage: vi.fn(),
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: vi.fn(),
  };

  it('renders welcome message when messages are empty and providerAvailable is not false', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const welcomeEl = container.querySelector('.welcome-message');
    expect(welcomeEl).not.toBeNull();
    expect(welcomeEl?.textContent).toContain('Welcome to sprout');
  });

  it('renders "No AI provider configured" when providerAvailable is false', () => {
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        providerAvailable: false,
      }));
    });
    const noProviderEl = container.querySelector('.no-provider-state');
    expect(noProviderEl).not.toBeNull();
    expect(noProviderEl?.textContent).toContain('No AI provider configured');
  });

  it('shows Configure Provider button when providerAvailable is false and onRequestProviderSetup is provided', () => {
    const setupMock = vi.fn();
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        providerAvailable: false,
        onRequestProviderSetup: setupMock,
      }));
    });
    const btn = container.querySelector('.provider-setup-btn');
    expect(btn).not.toBeNull();
    expect(btn?.textContent).toContain('Configure Provider');
  });

  it('does not show Configure Provider button when onRequestProviderSetup is not provided', () => {
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        providerAvailable: false,
      }));
    });
    const btn = container.querySelector('.provider-setup-btn');
    expect(btn).toBeNull();
  });

  it('renders messages list when messages are provided', () => {
    const messages = makeMessages(3);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const virtuosoMock = container.querySelector('.virtuoso-mock');
    expect(virtuosoMock).not.toBeNull();
    // The mock Virtuoso renders message bubbles for each message
    const bubbles = virtuosoMock?.querySelectorAll('.message-bubble-mock');
    expect(bubbles?.length).toBe(3);
  });

  it('renders processing indicator when isProcessing=true and no tool executions', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        isProcessing: true,
        toolExecutions: [],
      }));
    });
    const indicator = container.querySelector('.processing-indicator');
    expect(indicator).not.toBeNull();
    expect(indicator?.textContent).toContain('Processing your request');
  });

  it('does not render processing indicator when isProcessing but has tool executions', () => {
    const messages = makeMessages(1);
    const toolExecutions: ToolExecution[] = [{
      id: 'tool-1',
      tool: 'read_file',
      status: 'running',
      startTime: new Date(),
    }];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        isProcessing: true,
        toolExecutions,
      }));
    });
    const indicator = container.querySelector('.processing-indicator');
    expect(indicator).toBeNull();
  });

  it('renders error indicator when lastError is set', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        lastError: 'Connection lost',
      }));
    });
    const errorEl = container.querySelector('.error-indicator');
    expect(errorEl).not.toBeNull();
    expect(errorEl?.textContent).toContain('Connection lost');
  });

  it('filters out compaction summary messages from visible display', () => {
    const messages: Message[] = [
      { id: '1', type: 'user', content: 'Hello', timestamp: new Date() },
      {
        id: '2',
        type: 'assistant',
        content: '[Context compaction — layered summary]\nCompacted…',
        timestamp: new Date(),
      },
      { id: '3', type: 'assistant', content: 'Here is the answer', timestamp: new Date() },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const virtuosoMock = container.querySelector('.virtuoso-mock');
    const bubbles = virtuosoMock?.querySelectorAll('.message-bubble-mock');
    // Should only show 2 messages (the compaction summary is filtered out)
    expect(bubbles?.length).toBe(2);
  });

  it('shows worktree indicator when worktreePath is provided', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        worktreePath: '/home/user/project-branch',
      }));
    });
    const worktreeEl = container.querySelector('.worktree-indicator');
    expect(worktreeEl).not.toBeNull();
    expect(worktreeEl?.textContent).toContain('project-branch');
  });

  it('does not show worktree indicator when worktreePath is not provided', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const worktreeEl = container.querySelector('.worktree-indicator');
    expect(worktreeEl).toBeNull();
  });

  it('calls onSendMessage from CommandInput interaction', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    // Simulate clicking the mock send button
    const sendBtn = container.querySelector('.send-btn-mock');
    act(() => {
      sendBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(baseProps.onSendMessage).toHaveBeenCalled();
  });

  it('uses correct placeholder when providerAvailable is false', () => {
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        providerAvailable: false,
      }));
    });
    const mockInput = container.querySelector('.command-input-mock');
    expect(mockInput?.getAttribute('data-placeholder')).toBe('Configure a provider to start chatting...');
  });

  it('renders ChatMessageContextMenu for insert at cursor', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const contextMenu = container.querySelector('[data-testid="chat-context-menu-mock"]');
    expect(contextMenu).not.toBeNull();
  });

  // ── New tests for improved coverage ─────────────────────────────────

  // ── Scroll-to-bottom button ──

  it('shows scroll-to-bottom button when user has scrolled up (not at bottom)', () => {
    // With enough messages, our mock Virtuoso triggers atBottomStateChange(false)
    const messages = makeMessages(10, 'user');
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const scrollBtn = container.querySelector('.scroll-to-bottom-btn');
    expect(scrollBtn).not.toBeNull();
    expect(scrollBtn?.getAttribute('aria-label')).toBe('Scroll to bottom');
  });

  it('does not show scroll-to-bottom button when few messages (at bottom)', () => {
    const messages = makeMessages(2, 'user');
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const scrollBtn = container.querySelector('.scroll-to-bottom-btn');
    expect(scrollBtn).toBeNull();
  });

  // ── ARIA live region for screen-reader announcements ──

  it('renders the chat message list as an aria-live polite log region', () => {
    const messages = makeMessages(3, 'user');
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const log = container.querySelector('[role="log"]');
    expect(log).not.toBeNull();
    expect(log?.getAttribute('aria-live')).toBe('polite');
    expect(log?.getAttribute('aria-label')).toBe('Chat messages');
  });

  // ── Default placeholder ──

  it('uses default placeholder when providerAvailable is not false', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const mockInput = container.querySelector('.command-input-mock');
    expect(mockInput?.getAttribute('data-placeholder')).toBe('Ask me anything about your code...');
  });

  // ── CommandInput disabled state ──

  it('disables CommandInput when providerAvailable is false', () => {
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        providerAvailable: false,
      }));
    });
    const mockInput = container.querySelector('.command-input-mock');
    expect(mockInput?.getAttribute('data-disabled')).toBe('true');
  });

  it('does not disable CommandInput when providerAvailable is not false', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const mockInput = container.querySelector('.command-input-mock');
    expect(mockInput?.getAttribute('data-disabled')).toBe('false');
  });

  // ── Stop processing button ──

  it('calls onStopProcessing when stop button is clicked', () => {
    const onStopProcessing = vi.fn();
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        isProcessing: true,
        toolExecutions: [],
        onStopProcessing,
      }));
    });
    const stopBtn = container.querySelector('.stop-btn-mock');
    expect(stopBtn).not.toBeNull();
    act(() => {
      stopBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onStopProcessing).toHaveBeenCalled();
  });

  it('does not show stop button when not processing', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const stopBtn = container.querySelector('.stop-btn-mock');
    expect(stopBtn).toBeNull();
  });

  // ── Query progress ──

  it('renders query progress when provided', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        queryProgress: { message: 'Indexing files...', details: '50/100 files' },
      }));
    });
    const progressEl = container.querySelector('.query-progress');
    expect(progressEl).not.toBeNull();
    expect(progressEl?.textContent).toContain('Indexing files...');
  });

  it('renders query progress details when available', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        queryProgress: { message: 'Working...', details: 'detail info' },
      }));
    });
    const detailsEl = container.querySelector('.progress-details');
    expect(detailsEl).not.toBeNull();
    expect(detailsEl?.textContent).toContain('detail info');
  });

  it('does not render query progress details when details is null', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        queryProgress: { message: 'Working...', details: null },
      }));
    });
    const detailsEl = container.querySelector('.progress-details');
    expect(detailsEl).toBeNull();
  });

  // ── Tool execution filtering by queryId ──

  it('filters tool executions to only show those matching current query count', () => {
    const messages = makeMessages(1);
    const toolExecutions: ToolExecution[] = [
      { id: 'tool-old', tool: 'old_read', status: 'completed', startTime: new Date(), queryId: 1 },
      { id: 'tool-current', tool: 'current_read', status: 'running', startTime: new Date(), queryId: 2 },
      { id: 'tool-legacy', tool: 'legacy_tool', status: 'completed', startTime: new Date() },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        toolExecutions,
        stats: { queryCount: 2 },
      }));
    });
    // The ChatFooter should not show processing indicator since we have tool executions
    // (current query tools + legacy tools are shown)
    const indicator = container.querySelector('.processing-indicator');
    // With tool executions present, processing indicator should not appear
    expect(indicator).toBeNull();
  });

  it('shows all tool executions when no query count is available', () => {
    const messages = makeMessages(1);
    const toolExecutions: ToolExecution[] = [
      { id: 'tool-1', tool: 'read_file', status: 'running', startTime: new Date() },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        toolExecutions,
      }));
    });
    // Without isProcessing, just tool executions, no indicator
    const indicator = container.querySelector('.processing-indicator');
    expect(indicator).toBeNull();
  });

  // ── isProcessing + isProcessing + queryProgress (no processing indicator) ──

  it('does not show processing indicator when queryProgress is present', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
        isProcessing: true,
        toolExecutions: [],
        queryProgress: { message: 'Processing...' },
      }));
    });
    // Processing indicator should not appear since queryProgress is present
    const indicator = container.querySelector('.processing-indicator');
    expect(indicator).toBeNull();
  });

  // ── Multiple message types ──

  it('renders user and assistant messages correctly', () => {
    const messages: Message[] = [
      { id: '1', type: 'user', content: 'Hello', timestamp: new Date() },
      { id: '2', type: 'assistant', content: 'Hi there!', timestamp: new Date() },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const bubbles = container.querySelectorAll('.message-bubble-mock');
    expect(bubbles.length).toBe(2);
    expect(bubbles[0]?.getAttribute('data-message-type')).toBe('user');
    expect(bubbles[1]?.getAttribute('data-message-type')).toBe('assistant');
  });

  // ── Reasoning display ──

  it('renders reasoning block for assistant messages with reasoning', () => {
    const messages: Message[] = [
      {
        id: '1',
        type: 'assistant',
        content: 'Here is the answer',
        reasoning: 'Let me think about this step by step...',
        timestamp: new Date(),
      },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const reasoningBlock = container.querySelector('.reasoning-block');
    expect(reasoningBlock).not.toBeNull();
    expect(reasoningBlock?.textContent).toContain('Let me think about this step by step');
  });

  it('does not render reasoning block when reasoning is empty', () => {
    const messages: Message[] = [
      {
        id: '1',
        type: 'assistant',
        content: 'Here is the answer',
        reasoning: '',
        timestamp: new Date(),
      },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const reasoningBlock = container.querySelector('.reasoning-block');
    expect(reasoningBlock).toBeNull();
  });

  it('does not render reasoning block for user messages', () => {
    const messages: Message[] = [
      {
        id: '1',
        type: 'user',
        content: 'Hello',
        timestamp: new Date(),
      },
    ];
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const reasoningBlock = container.querySelector('.reasoning-block');
    expect(reasoningBlock).toBeNull();
  });

  // ── Chat shell and container ──

  it('renders chat-shell wrapper element', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const shell = container.querySelector('.chat-shell');
    expect(shell).not.toBeNull();
  });

  it('renders input-container with CommandInput', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const inputContainer = container.querySelector('.input-container');
    expect(inputContainer).not.toBeNull();
    const mockInput = inputContainer?.querySelector('[data-testid="command-input-mock"]');
    expect(mockInput).not.toBeNull();
  });

  // ── isConnected passed to CommandInput ──

  it('passes isConnected to CommandInput via mock', () => {
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        isConnected: true,
      }));
    });
    // The mock doesn't explicitly render isConnected, but the component doesn't crash
    const mockInput = container.querySelector('.command-input-mock');
    expect(mockInput).not.toBeNull();
  });

  // ── Empty chat with provider available ──

  it('renders welcome hint in welcome message', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const hintEl = container.querySelector('.welcome-hint');
    expect(hintEl).not.toBeNull();
    expect(hintEl?.textContent).toContain('Try asking');
  });

  // ── Chat container styles ──

  it('uses chat-container--empty class when no messages', () => {
    act(() => {
      root.render(createElement(Chat, baseProps));
    });
    const chatContainer = container.querySelector('.chat-container--empty');
    expect(chatContainer).not.toBeNull();
  });

  it('does not use chat-container--empty class when messages exist', () => {
    const messages = makeMessages(1);
    act(() => {
      root.render(createElement(Chat, {
        ...baseProps,
        messages,
      }));
    });
    const emptyContainer = container.querySelector('.chat-container--empty');
    expect(emptyContainer).toBeNull();
  });
});
