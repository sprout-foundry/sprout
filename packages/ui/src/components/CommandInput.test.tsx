import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import CommandInput from './CommandInput';
import type { CommandInputProps } from './CommandInput';

// ── Mock dependencies ────────────────────────────────────────────────

vi.mock('../utils/log', () => ({
  useLog: () => ({
    debug: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  }),
  debugLog: vi.fn(),
}));

vi.mock('./command_input_history', () => ({
  createEmptyState: () => ({ commands: [], index: -1, tempInput: '' }),
  dedupeCommands: (arr: string[]) => [...new Set(arr)],
  loadCommandHistory: vi.fn().mockResolvedValue({ commands: [], index: -1, tempInput: '' }),
  persistCommandHistory: vi.fn(),
}));

vi.mock('./QueuedMessagesPanel', () => {
  const MockPanel = ({ messages, onClose }: { messages: string[]; onClose: () => void }) => {
    return createElement('div', {
      'data-testid': 'queued-messages-panel',
      'data-message-count': messages.length,
    },
      createElement('button', { onClick: onClose, type: 'button' }, 'Close Queue'),
      messages.map((msg, i) => createElement('div', { key: i, 'data-testid': 'queued-msg', 'data-msg': msg }, msg)),
    );
  };
  return { __esModule: true, default: MockPanel };
});

// ── Mock localStorage ────────────────────────────────────────────────
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();
Object.defineProperty(global, 'localStorage', { value: localStorageMock });

// ── Mock clipboard API ───────────────────────────────────────────────
Object.defineProperty(global.navigator, 'clipboard', {
  value: { writeText: vi.fn().mockResolvedValue(undefined) },
  writable: true,
});

// ── Test Setup ───────────────────────────────────────────────────────

describe('CommandInput', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    container.id = 'command-input-test-root';
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.clearAllMocks();
  });

  const baseProps: CommandInputProps = {
    value: '',
    onChange: vi.fn(),
    onSend: vi.fn(),
    placeholder: 'Ask me anything about your code...',
  };

  it('renders textarea with correct placeholder', () => {
    act(() => {
      root.render(createElement(CommandInput, baseProps));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    expect(textarea?.placeholder).toBe('Ask me anything about your code...');
  });

  it('renders with initial value', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'pre-filled text',
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    expect(textarea?.value).toBe('pre-filled text');
  });

  it('calls onSend when Enter is pressed (multiline mode)', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test message',
        onSend,
        multiline: true,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    expect(onSend).toHaveBeenCalledWith('test message');
  });

  it('inserts newline when Shift+Enter is pressed', () => {
    const onChange = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'hello',
        onChange,
        multiline: true,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      textarea!.selectionStart = 5;
      textarea!.selectionEnd = 5;
      const event = new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true });
      textarea!.dispatchEvent(event);
    });
    expect(onChange).toHaveBeenCalledWith('hello\n');
  });

  it('sends message when form is submitted', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'form submit test',
        onSend,
      }));
    });
    const form = container.querySelector('form');
    act(() => {
      form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(onSend).toHaveBeenCalledWith('form submit test');
  });

  it('does not send empty messages', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: '   ',
        onSend,
      }));
    });
    const form = container.querySelector('form');
    act(() => {
      form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(onSend).not.toHaveBeenCalled();
  });

  it('clears input after sending', () => {
    const onChange = vi.fn();
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'send this',
        onChange,
        onSend,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('shows stop button when isProcessing is true', () => {
    const onStop = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        isProcessing: true,
        onStop,
      }));
    });
    const stopBtn = container.querySelector('.stop-button');
    expect(stopBtn).not.toBeNull();
  });

  it('does not show stop button when isProcessing is false', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        isProcessing: false,
      }));
    });
    const stopBtn = container.querySelector('.stop-button');
    expect(stopBtn).toBeNull();
  });

  it('shows queue button when isProcessing and onQueue provided', () => {
    const onQueue = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'queued msg',
        isProcessing: true,
        onQueue,
      }));
    });
    const queueBtn = container.querySelector('.queue-add-button');
    expect(queueBtn).not.toBeNull();
  });

  it('does not show queue button when isProcessing is false', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        onQueue: vi.fn(),
        isProcessing: false,
      }));
    });
    const queueBtn = container.querySelector('.queue-add-button');
    expect(queueBtn).toBeNull();
  });

  it('disables input when disabled=true', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        disabled: true,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    expect(textarea?.disabled).toBe(true);
  });

  it('handles Escape key to clear input', () => {
    const onChange = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'will be cleared',
        onChange,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('shows keyboard shortcuts hints popover when info button clicked', () => {
    act(() => {
      root.render(createElement(CommandInput, baseProps));
    });
    let hintsPopover = container.querySelector('.hints-popover');
    expect(hintsPopover).toBeNull();

    const infoBtn = container.querySelector('.hints-button')!;
    act(() => {
      infoBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    hintsPopover = container.querySelector('.hints-popover');
    expect(hintsPopover).not.toBeNull();
    expect(hintsPopover?.textContent).toContain('Keyboard Shortcuts');
    expect(hintsPopover?.textContent).toContain('Enter');
    expect(hintsPopover?.textContent).toContain('Shift+Enter');
  });

  it('respects disabled and isConnected props on send button', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        disabled: true,
        isConnected: true,
      }));
    });
    const sendBtn = container.querySelector('.send-button') as HTMLButtonElement;
    expect(sendBtn?.disabled).toBe(true);

    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        disabled: false,
        isConnected: false,
      }));
    });
    const sendBtn2 = container.querySelector('.send-button') as HTMLButtonElement;
    expect(sendBtn2?.disabled).toBe(true);

    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        disabled: false,
        isConnected: true,
      }));
    });
    const sendBtn3 = container.querySelector('.send-button') as HTMLButtonElement;
    expect(sendBtn3?.disabled).toBe(false);
  });
});
