// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import QueuedMessagesPanel from './QueuedMessagesPanel';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: QueuedMessagesPanel
// ---------------------------------------------------------------------------

describe('QueuedMessagesPanel', () => {
  const onRemove = jest.fn();
  const onEdit = jest.fn();
  const onReorder = jest.fn();
  const onClear = jest.fn();
  const onClose = jest.fn();

  beforeEach(() => {
    onRemove.mockClear();
    onEdit.mockClear();
    onReorder.mockClear();
    onClear.mockClear();
    onClose.mockClear();
  });

  const defaultProps = {
    onRemove,
    onEdit,
    onReorder,
    onClear,
    onClose,
  };

  it('shows empty state when messages array is empty', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: [],
        })
      );
    });
    expect(container.querySelector('.queue-panel.empty')).not.toBeNull();
    expect(container.querySelector('.queue-panel-empty')?.textContent).toBe('No queued messages');
  });

  it('renders header with title and message count', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['msg1', 'msg2', 'msg3'],
        })
      );
    });
    const title = container.querySelector('.queue-panel-title');
    expect(title).not.toBeNull();
    expect(title?.textContent).toContain('3');
  });

  it('renders close button in header', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['msg1'],
        })
      );
    });
    expect(container.querySelector('.queue-panel-close')).not.toBeNull();
  });

  it('calls onClose when close button is clicked', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['msg1'],
        })
      );
    });
    act(() => {
      container.querySelector('.queue-panel-close')?.click();
    });
    expect(onClose).toHaveBeenCalled();
  });

  it('renders Clear All button in header when messages are present', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['msg1', 'msg2'],
        })
      );
    });
    const clearBtn = container.querySelector('.queue-panel-clear');
    expect(clearBtn).not.toBeNull();
    expect(clearBtn?.textContent).toBe('Clear All');
  });

  it('calls onClear when Clear All is clicked', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['msg1'],
        })
      );
    });
    act(() => {
      container.querySelector('.queue-panel-clear')?.click();
    });
    expect(onClear).toHaveBeenCalled();
  });

  it('renders list of messages with index and content', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['first', 'second'],
        })
      );
    });
    const items = container.querySelectorAll('.queue-panel-item');
    expect(items).toHaveLength(2);
    const indices = container.querySelectorAll('.queue-panel-item-index');
    expect(indices[0].textContent).toBe('1');
    expect(indices[1].textContent).toBe('2');
  });

  it('calls onRemove when delete button is clicked', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['delete me'],
        })
      );
    });
    const dangerBtn = container.querySelector('.queue-panel-action.danger');
    act(() => {
      dangerBtn?.click();
    });
    expect(onRemove).toHaveBeenCalledWith(0);
  });

  it('calls onReorder when move up is clicked (not first item)', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['first', 'second'],
        })
      );
    });
    // Second item's move-up button
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[1].querySelectorAll('.queue-panel-action');
    // First action in non-editing mode is Move Up
    act(() => {
      actions[0]?.click();
    });
    expect(onReorder).toHaveBeenCalledWith(1, 0);
  });

  it('calls onReorder when move down is clicked (not last item)', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['first', 'second'],
        })
      );
    });
    // First item's move-down button (second action)
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[1]?.click();
    });
    expect(onReorder).toHaveBeenCalledWith(0, 1);
  });

  it('disables move up for first item', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['first', 'second'],
        })
      );
    });
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    expect(actions[0]?.getAttribute('disabled')).not.toBeNull();
  });

  it('disables move down for last item', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['first', 'second'],
        })
      );
    });
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[1].querySelectorAll('.queue-panel-action');
    // Move down is second action
    expect(actions[1]?.getAttribute('disabled')).not.toBeNull();
  });

  it('truncates very long messages for display', () => {
    const longMsg = 'x'.repeat(200);
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: [longMsg],
        })
      );
    });
    const textSpan = container.querySelector('.queue-panel-item-text');
    // Should be truncated to 120 chars + ellipsis
    expect(textSpan?.textContent?.length).toBeLessThan(200);
    expect(textSpan?.textContent).toContain('\u2026');
  });

  it('shows full message in title attribute', () => {
    const longMsg = 'x'.repeat(200);
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: [longMsg],
        })
      );
    });
    const textSpan = container.querySelector('.queue-panel-item-text');
    expect(textSpan?.getAttribute('title')).toBe(longMsg);
  });

  it('calls handleStartEdit when edit button is clicked', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['edit me'],
        })
      );
    });
    // Edit button is the 3rd action (after move up/down)
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    // Move up (disabled), move down (disabled), edit, delete
    const editBtn = actions[2];
    act(() => {
      editBtn?.click();
    });
    // After clicking edit, the item should have editing class
    expect(items[0].className).toContain('editing');
    // Should have textarea
    expect(items[0].querySelector('textarea')).not.toBeNull();
  });

  it('shows edit textarea and save/cancel buttons when editing', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original text'],
        })
      );
    });
    // Click edit button
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click(); // edit button
    });

    // Should show save and cancel buttons
    expect(container.querySelector('.queue-panel-action.save')).not.toBeNull();
    expect(container.querySelector('.queue-panel-action.cancel')).not.toBeNull();
  });

  it('calls onEdit with trimmed value when save is clicked', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original'],
        })
      );
    });
    // Click edit
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click();
    });

    // Change textarea value
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      Object.defineProperty(textarea, 'value', { value: 'new value  ' });
      textarea.dispatchEvent(new Event('change', { bubbles: true }));
    });

    // Click save
    act(() => {
      container.querySelector('.queue-panel-action.save')?.click();
    });

    expect(onEdit).toHaveBeenCalledWith(0, 'new value');
  });

  it('rejects empty save and shakes the item', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original'],
        })
      );
    });
    // Click edit
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click();
    });

    // Clear textarea
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      Object.defineProperty(textarea, 'value', { value: '   ' });
      textarea.dispatchEvent(new Event('change', { bubbles: true }));
    });

    // Click save
    act(() => {
      container.querySelector('.queue-panel-action.save')?.click();
    });

    // onEdit should NOT be called
    expect(onEdit).not.toHaveBeenCalled();
  });

  it('calls handleCancelEdit when cancel button is clicked', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original'],
        })
      );
    });
    // Click edit
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click();
    });

    // Click cancel
    act(() => {
      container.querySelector('.queue-panel-action.cancel')?.click();
    });

    // Should go back to non-editing state (no textarea)
    expect(container.querySelector('textarea')).toBeNull();
  });

  it('handles Enter key to save edit', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original'],
        })
      );
    });
    // Click edit
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click();
    });

    // Type in textarea and press Enter
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      Object.defineProperty(textarea, 'value', { value: 'saved via enter' });
      textarea.dispatchEvent(new Event('change', { bubbles: true }));
      textarea.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      );
    });

    expect(onEdit).toHaveBeenCalledWith(0, 'saved via enter');
  });

  it('handles Escape key to cancel edit', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original'],
        })
      );
    });
    // Click edit
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click();
    });

    // Press Escape
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      textarea.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
      );
    });

    expect(onEdit).not.toHaveBeenCalled();
    // Should go back to non-editing state
    expect(container.querySelector('textarea')).toBeNull();
  });

  it('Shift+Enter does not save (allows multi-line input)', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['original'],
        })
      );
    });
    // Click edit
    const items = container.querySelectorAll('.queue-panel-item');
    const actions = items[0].querySelectorAll('.queue-panel-action');
    act(() => {
      actions[2]?.click();
    });

    // Press Shift+Enter
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    act(() => {
      textarea.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true })
      );
    });

    expect(onEdit).not.toHaveBeenCalled();
  });

  it('renders header without Clear All button when empty', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: [],
        })
      );
    });
    expect(container.querySelector('.queue-panel-clear')).toBeNull();
  });

  it('renders message list container when messages are present', () => {
    act(() => {
      root.render(
        createElement(QueuedMessagesPanel, {
          ...defaultProps,
          messages: ['msg'],
        })
      );
    });
    expect(container.querySelector('.queue-panel-list')).not.toBeNull();
  });
});
