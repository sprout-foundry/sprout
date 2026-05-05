import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import Chat from './ChatPanel';
import type { Message, ToolExecution, ChatProps } from '../types/chat';

// ── Mock react-virtuoso ──────────────────────────────────────────────
// Virtuoso is complex; we mock it to render items in a simple list.
jest.mock('react-virtuoso', () => {
  const React = require('react');
  const MockVirtuoso = React.forwardRef(({ data, itemContent, components }: { data: unknown[]; itemContent: (index: number, item: unknown) => JSX.Element; components?: { Header: () => JSX.Element | null; Footer: () => JSX.Element } }, ref) => {
    React.useImperativeHandle(ref, () => null as any);
    return createElement('div', { className: 'virtuoso-mock' },
      components?.Header?.(),
      data.map((item, index) => itemContent(index, item)),
      components?.Footer?.(),
    );
  });
  MockVirtuoso.displayName = 'MockVirtuoso';
  return { Virtuoso: MockVirtuoso };
});

// ── Mock sub-components ──────────────────────────────────────────────
// We mock these to avoid deep dependency chains in tests.
jest.mock('./CommandInput', () => {
  const MockCommandInput = ({ value, onChange, onSend, placeholder, isProcessing, onStop }: {
    value?: string;
    onChange?: (v: string) => void;
    onSend?: (msg: string) => void;
    placeholder?: string;
    isProcessing?: boolean;
    onStop?: () => void;
    [key: string]: unknown;
  }) => {
    return createElement('div', {
      className: 'command-input-mock',
      'data-testid': 'command-input-mock',
      'data-value': value,
      'data-placeholder': placeholder,
      'data-processing': isProcessing,
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

jest.mock('./MessageSegments', () => ({
  __esModule: true,
  default: ({ content }: { content: string }) => createElement('div', { 'data-testid': 'message-segments' }, content),
}));

jest.mock('./MessageContent', () => ({
  __esModule: true,
  default: ({ content }: { content: string }) => createElement('div', { 'data-testid': 'message-content' }, content),
}));

jest.mock('./MessageBubble', () => {
  const MockMessageBubble = ({ children, type }: { children: React.ReactNode; type?: string; [key: string]: unknown }) => {
    return createElement('div', { className: 'message-bubble-mock', 'data-message-type': type }, children);
  };
  return { __esModule: true, default: MockMessageBubble };
});

jest.mock('./LiveLog', () => ({
  __esModule: true,
  default: ({ lines }: { lines: { id: string; text: string; timestamp: Date }[] }) =>
    createElement('div', { 'data-testid': 'live-log' }, lines.length.toString()),
}));

jest.mock('./ChatMessageContextMenu', () => ({
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
    onSendMessage: jest.fn(),
    onQueueMessage: jest.fn(),
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: jest.fn(),
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
    const setupMock = jest.fn();
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
});
