import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import { waitFor } from '@testing-library/react';
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

  it('sends onSendCommand when onSend is not provided', () => {
    const onSendCommand = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'alt send',
        onSend: undefined,
        onSendCommand,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    expect(onSendCommand).toHaveBeenCalledWith('alt send');
  });

  it('handles Tab key for tab completion', () => {
    const onChange = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'hello',
        onChange,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      textarea!.selectionStart = 5;
      textarea!.selectionEnd = 5;
      const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
      textarea!.dispatchEvent(event);
    });
    expect(onChange).toHaveBeenCalled();
    const callArg = onChange.mock.calls[onChange.mock.calls.length - 1][0];
    expect(callArg).toContain('\t');
  });

  it('shows new session button', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
      }));
    });
    const newSessionBtn = container.querySelector('.new-session-button');
    expect(newSessionBtn).not.toBeNull();
  });

  it('handles new session button click (not processing)', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'some text',
        onSend,
        isProcessing: false,
      }));
    });
    const newSessionBtn = container.querySelector('.new-session-button')!;
    act(() => {
      newSessionBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onSend).toHaveBeenCalledWith('/clear');
  });

  it('handles new session button click with confirm when processing', () => {
    const onSend = vi.fn();
    const originalConfirm = window.confirm;
    window.confirm = vi.fn(() => true);
    try {
      act(() => {
        root.render(createElement(CommandInput, {
          ...baseProps,
          value: 'some text',
          onSend,
          isProcessing: true,
        }));
      });
      const newSessionBtn = container.querySelector('.new-session-button')!;
      act(() => {
        newSessionBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      expect(window.confirm).toHaveBeenCalledWith('A request is currently processing. Stop it and start a new session?');
      expect(onSend).toHaveBeenCalledWith('/clear');
    } finally {
      window.confirm = originalConfirm;
    }
  });

  it('aborts new session when confirm returns false', () => {
    const onSend = vi.fn();
    const originalConfirm = window.confirm;
    window.confirm = vi.fn(() => false);
    try {
      act(() => {
        root.render(createElement(CommandInput, {
          ...baseProps,
          value: 'some text',
          onSend,
          isProcessing: true,
        }));
      });
      const newSessionBtn = container.querySelector('.new-session-button')!;
      act(() => {
        newSessionBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      expect(onSend).not.toHaveBeenCalled();
    } finally {
      window.confirm = originalConfirm;
    }
  });

  it('queues message when queue button is clicked', () => {
    const onQueue = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'queued message',
        isProcessing: true,
        onQueue,
      }));
    });
    const queueBtn = container.querySelector('.queue-add-button')!;
    act(() => {
      queueBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onQueue).toHaveBeenCalledWith('queued message');
  });

  it('does not queue empty messages', () => {
    const onQueue = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: '   ',
        isProcessing: true,
        onQueue,
      }));
    });
    const queueBtn = container.querySelector('.queue-add-button')!;
    act(() => {
      queueBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onQueue).not.toHaveBeenCalled();
  });

  it('pressing Escape clears input when in history mode', () => {
    const onChange = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: '',
        onChange,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    // Press ArrowUp to attempt history navigation (empty history → no-op)
    act(() => {
      textarea!.selectionStart = 0;
      textarea!.selectionEnd = 0;
      const event = new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    // Press Escape — verifies no crash when history guard exits early
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
      textarea!.dispatchEvent(event);
    });
    // Component still renders without error (history guard exercised)
    expect(container.querySelector('textarea')).not.toBeNull();
  });

  it('does not change value on ArrowDown when history is empty', () => {
    const onChange = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: '',
        onChange,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    // Navigate up (empty history → no-op)
    act(() => {
      textarea!.selectionStart = 0;
      textarea!.selectionEnd = 0;
      const event = new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    // Navigate down
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    // Should not have changed value since history is empty
    expect(onChange).not.toHaveBeenCalled();
  });

  it('does not navigate history when cursor is not at start', () => {
    const onChange = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'hello world',
        onChange,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    // Cursor in middle, not at start
    act(() => {
      textarea!.selectionStart = 6;
      textarea!.selectionEnd = 6;
      const event = new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true });
      textarea!.dispatchEvent(event);
    });
    // Should not navigate history
    expect(onChange).not.toHaveBeenCalled();
  });

  it('does not send when disabled', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        onSend,
        disabled: true,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
      textarea!.dispatchEvent(event);
    });
    expect(onSend).not.toHaveBeenCalled();
  });

  it('handles composition start/end for IME', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'composition test',
        onSend,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;

    // Composition start and end are handled via isComposingRef
    // In the actual component, these are React event handlers that work with IME input
    // We verify the component renders correctly with composition handlers set
    expect(textarea).not.toBeNull();
    // Verify the component still works for Enter after composition events
    // by checking that the composition handler methods exist
    // (The actual composition behavior depends on browser IME which is hard to test in jsdom)
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
      textarea!.dispatchEvent(event);
    });
    expect(onSend).toHaveBeenCalledWith('composition test');
  });

  it('handles submit form with disabled=false and valid content', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'form submit',
        onSend,
        disabled: false,
      }));
    });
    const form = container.querySelector('form');
    act(() => {
      form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(onSend).toHaveBeenCalledWith('form submit');
  });

  it('does not submit when canSend is false (empty value)', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: '',
        onSend,
      }));
    });
    const form = container.querySelector('form');
    act(() => {
      form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(onSend).not.toHaveBeenCalled();
  });

  it('handles multiline=false (single line mode)', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'single line',
        onSend,
        multiline: false,
      }));
    });
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
      textarea!.dispatchEvent(event);
    });
    expect(onSend).toHaveBeenCalledWith('single line');
  });

  it('renders upload button', () => {
    act(() => {
      root.render(createElement(CommandInput, baseProps));
    });
    const uploadBtn = container.querySelector('.upload-button');
    expect(uploadBtn).not.toBeNull();
  });

  it('renders queue button wrapper when queuedCount > 0', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        queuedCount: 3,
      }));
    });
    const queueWrapper = container.querySelector('.queue-button-wrapper');
    expect(queueWrapper).not.toBeNull();
    const queueCount = container.querySelector('.queue-count');
    expect(queueCount?.textContent).toBe('3');
  });

  it('opens queue panel when queue button clicked', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        queuedCount: 2,
        queuedMessages: ['msg1', 'msg2'],
      }));
    });
    expect(container.querySelector('.queue-popover-overlay')).toBeNull();

    const queueBtn = container.querySelector('.queue-button')!;
    act(() => {
      queueBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('.queue-popover-overlay')).not.toBeNull();
  });

  it('toggles hints popover on button click', () => {
    act(() => {
      root.render(createElement(CommandInput, baseProps));
    });
    expect(container.querySelector('.hints-popover')).toBeNull();

    const hintsBtn = container.querySelector('.hints-button')!;
    act(() => {
      hintsBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('.hints-popover')).not.toBeNull();

    act(() => {
      hintsBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('.hints-popover')).toBeNull();
  });

  it('shows length indicator when draft exceeds 100 chars', () => {
    const longValue = 'a'.repeat(150);
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: longValue,
      }));
    });
    const lengthIndicator = container.querySelector('.length-indicator');
    expect(lengthIndicator).not.toBeNull();
    expect(lengthIndicator?.textContent).toBe('150');
  });

  it('hides length indicator when draft is short', () => {
    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'short',
      }));
    });
    expect(container.querySelector('.length-indicator')).toBeNull();
  });

  // ── Image upload state tests ───────────────────────────────────────

  it('disables send button while an image is uploading', () => {
    // Use a never-resolving promise to simulate an in-flight upload.
    const onUploadImage = vi.fn(() => new Promise(() => {}));

    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        onUploadImage,
      }));
    });

    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    const file = new File(['test'], 'test.png', { type: 'image/png' });
    const dataTransfer = new DataTransfer();
    dataTransfer.items.push(new DataTransferItem(file, 'image/png'));
    dataTransfer.files.push(file);
    dataTransfer.types.push('Files');

    // Dispatch synchronously inside act() so React commits the attachedImages
    // update before the assertion runs. We do NOT await the auto-upload
    // effect — a never-resolving upload would block act() forever.
    act(() => {
      const pasteEvent = new Event('paste', { bubbles: true }) as unknown as {
        clipboardData: DataTransfer;
      };
      Object.defineProperty(pasteEvent, 'clipboardData', {
        value: dataTransfer,
        writable: false,
      });
      textarea!.dispatchEvent(pasteEvent as unknown as ClipboardEvent);
    });

    const sendBtn = container.querySelector('.send-button') as HTMLButtonElement;
    expect(sendBtn?.disabled).toBe(true);
  });

  it('shows uploading status and tooltip while image is uploading', () => {
    const onUploadImage = vi.fn(() => new Promise(() => {}));

    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        onUploadImage,
      }));
    });

    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    const file = new File(['test'], 'test.png', { type: 'image/png' });
    const dataTransfer = new DataTransfer();
    dataTransfer.items.push(new DataTransferItem(file, 'image/png'));
    dataTransfer.files.push(file);
    dataTransfer.types.push('Files');

    act(() => {
      const pasteEvent = new Event('paste', { bubbles: true }) as unknown as {
        clipboardData: DataTransfer;
      };
      Object.defineProperty(pasteEvent, 'clipboardData', {
        value: dataTransfer,
        writable: false,
      });
      textarea!.dispatchEvent(pasteEvent as unknown as ClipboardEvent);
    });

    const sendBtn = container.querySelector('.send-button') as HTMLButtonElement;
    expect(sendBtn?.getAttribute('data-tooltip')).toBe('Uploading image…');
    expect(container.querySelector('.uploading-status')).not.toBeNull();
  });

  it('re-enables send button after image upload completes', async () => {
    const onUploadImage = vi.fn().mockResolvedValue({ path: '/tmp/test.png' });

    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        onUploadImage,
      }));
    });

    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    const file = new File(['test'], 'test.png', { type: 'image/png' });
    const dataTransfer = new DataTransfer();
    dataTransfer.items.push(new DataTransferItem(file, 'image/png'));
    dataTransfer.files.push(file);
    dataTransfer.types.push('Files');

    // Dispatch synchronously, then poll for the auto-upload to land.
    // The useEffect fires uploadImageAsync without awaiting, so the
    // resolved promise resolves outside React's act; waitFor handles
    // the resulting re-renders.
    act(() => {
      const pasteEvent = new Event('paste', { bubbles: true }) as unknown as {
        clipboardData: DataTransfer;
      };
      Object.defineProperty(pasteEvent, 'clipboardData', {
        value: dataTransfer,
        writable: false,
      });
      textarea!.dispatchEvent(pasteEvent as unknown as ClipboardEvent);
    });

    const sendBtn = container.querySelector('.send-button') as HTMLButtonElement;
    await waitFor(() => {
      expect(onUploadImage).toHaveBeenCalled();
      expect(sendBtn.disabled).toBe(false);
    });
  });

  it('enables send button when only failed images are attached', async () => {
    const onUploadImage = vi.fn().mockRejectedValue(new Error('Upload failed'));

    act(() => {
      root.render(createElement(CommandInput, {
        ...baseProps,
        value: 'test',
        onUploadImage,
      }));
    });

    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    const file = new File(['test'], 'test.png', { type: 'image/png' });
    const dataTransfer = new DataTransfer();
    dataTransfer.items.push(new DataTransferItem(file, 'image/png'));
    dataTransfer.files.push(file);
    dataTransfer.types.push('Files');

    act(() => {
      const pasteEvent = new Event('paste', { bubbles: true }) as unknown as {
        clipboardData: DataTransfer;
      };
      Object.defineProperty(pasteEvent, 'clipboardData', {
        value: dataTransfer,
        writable: false,
      });
      textarea!.dispatchEvent(pasteEvent as unknown as ClipboardEvent);
    });

    const sendBtn = container.querySelector('.send-button') as HTMLButtonElement;
    await waitFor(() => {
      expect(sendBtn.disabled).toBe(false);
      expect(sendBtn.getAttribute('aria-label')).toContain('failed to upload');
    });
  });
});
