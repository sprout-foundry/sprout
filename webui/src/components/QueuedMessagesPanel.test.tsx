// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import QueuedMessagesPanel from './QueuedMessagesPanel';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
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

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/** Render the panel with the given props and flush microtask queue. */
async function renderPanel(props: Partial<React.ComponentProps<typeof QueuedMessagesPanel>> = {}) {
  const onRemove = props.onRemove ?? jest.fn();
  const onEdit = props.onEdit ?? jest.fn();
  const onReorder = props.onReorder ?? jest.fn();
  const onClear = props.onClear ?? jest.fn();
  const onClose = props.onClose ?? jest.fn();

  await act(async () => {
    root.render(
      <QueuedMessagesPanel
        messages={props.messages ?? []}
        onRemove={onRemove}
        onEdit={onEdit}
        onReorder={onReorder}
        onClear={onClear}
        onClose={onClose}
      />
    );
  });
  await flushPromises();

  return { onRemove, onEdit, onReorder, onClear, onClose };
}

/** Helper to fire a click event on a button matching the given CSS selector. */
function clickButton(selector: string): void {
  const btn = container.querySelector(selector) as HTMLButtonElement;
  if (!btn) throw new Error(`Button not found: ${selector}`);
  act(() => {
    btn.click();
  });
}

/** Helper to fire a keyboard event on a textarea. */
function fireKeyDown(selector: string, key: string, shiftKey = false): void {
  const el = container.querySelector(selector) as HTMLTextAreaElement;
  if (!el) throw new Error(`Element not found: ${selector}`);
  act(() => {
    el.dispatchEvent(
      new KeyboardEvent('keydown', {
        key,
        shiftKey,
        bubbles: true,
        cancelable: true,
      })
    );
  });
}

// ===========================================================================
// 1. Rendering basics
// ===========================================================================

describe('QueuedMessagesPanel – rendering', () => {
  it('renders the list of queued messages with correct count in title', async () => {
    await renderPanel({
      messages: ['First message', 'Second message', 'Third message'],
    });

    const title = container.querySelector('.queue-panel-title');
    expect(title).not.toBeNull();
    expect(title?.textContent).toBe('Queued Messages (3)');

    // Verify each message is displayed
    const itemTexts = container.querySelectorAll('.queue-panel-item-text');
    expect(itemTexts.length).toBe(3);
    expect(itemTexts[0].textContent).toBe('First message');
    expect(itemTexts[1].textContent).toBe('Second message');
    expect(itemTexts[2].textContent).toBe('Third message');
  });

  it('shows message index numbers starting from 1', async () => {
    await renderPanel({
      messages: ['Alpha', 'Beta', 'Gamma'],
    });

    const indices = container.querySelectorAll('.queue-panel-item-index');
    expect(indices.length).toBe(3);
    expect(indices[0].textContent).toBe('1');
    expect(indices[1].textContent).toBe('2');
    expect(indices[2].textContent).toBe('3');
  });

  it('shows "No queued messages" when the list is empty', async () => {
    await renderPanel({ messages: [] });

    const title = container.querySelector('.queue-panel-title');
    expect(title?.textContent).toBe('Queued Messages');

    const emptyMsg = container.querySelector('.queue-panel-empty');
    expect(emptyMsg).not.toBeNull();
    expect(emptyMsg?.textContent).toBe('No queued messages');

    // Should have the "empty" class on the panel
    const panel = container.querySelector('.queue-panel');
    expect(panel?.classList.contains('empty')).toBe(true);

    // No Clear All or list items should be present
    expect(container.querySelector('.queue-panel-clear')).toBeNull();
    expect(container.querySelector('.queue-panel-item')).toBeNull();
  });
});

// ===========================================================================
// 2. Truncation of long messages
// ===========================================================================

describe('QueuedMessagesPanel – message truncation', () => {
  it('truncates messages longer than 120 characters', async () => {
    const longMsg = 'A'.repeat(200);
    await renderPanel({ messages: [longMsg] });

    const textEl = container.querySelector('.queue-panel-item-text');
    expect(textEl).not.toBeNull();
    // Truncated text should end with ellipsis and be 121 chars (120 + \u2026)
    expect(textEl?.textContent).toBe('A'.repeat(120) + '\u2026');
    expect(textEl?.textContent!.length).toBe(121);
  });

  it('puts the full message in the title attribute for truncated messages', async () => {
    const longMsg = 'B'.repeat(200);
    await renderPanel({ messages: [longMsg] });

    const textEl = container.querySelector('.queue-panel-item-text') as HTMLElement;
    expect(textEl).not.toBeNull();
    // The title attribute should contain the full message
    expect(textEl.title).toBe(longMsg);
    expect(textEl.title.length).toBe(200);
  });

  it('does not truncate short messages', async () => {
    const shortMsg = 'Hello world';
    await renderPanel({ messages: [shortMsg] });

    const textEl = container.querySelector('.queue-panel-item-text');
    expect(textEl?.textContent).toBe('Hello world');
    expect(textEl?.textContent!.length).toBe(shortMsg.length);
  });

  it('does not truncate messages at exactly 120 characters', async () => {
    const exactMsg = 'C'.repeat(120);
    await renderPanel({ messages: [exactMsg] });

    const textEl = container.querySelector('.queue-panel-item-text');
    expect(textEl?.textContent).toBe(exactMsg);
    // No ellipsis for exactly 120 chars
    expect(textEl?.textContent!.endsWith('\u2026')).toBe(false);
  });
});

// ===========================================================================
// 3. Remove button
// ===========================================================================

describe('QueuedMessagesPanel – remove button', () => {
  it('calls onRemove with the correct index when remove is clicked', async () => {
    const { onRemove } = await renderPanel({
      messages: ['First', 'Second', 'Third'],
    });

    // Get all remove (danger) buttons
    const dangerButtons = container.querySelectorAll('.queue-panel-action.danger');
    expect(dangerButtons.length).toBe(3);

    // Click the remove button for the second item (index 1)
    act(() => {
      (dangerButtons[1] as HTMLButtonElement).click();
    });

    expect(onRemove).toHaveBeenCalledTimes(1);
    expect(onRemove).toHaveBeenCalledWith(1);
  });

  it('calls onRemove with index 0 for the first item', async () => {
    const { onRemove } = await renderPanel({
      messages: ['Only one'],
    });

    clickButton('.queue-panel-action.danger');

    expect(onRemove).toHaveBeenCalledWith(0);
  });
});

// ===========================================================================
// 4. Move up / Move down
// ===========================================================================

describe('QueuedMessagesPanel – reorder buttons', () => {
  it('calls onReorder(index, index-1) when moving up', async () => {
    const { onReorder } = await renderPanel({
      messages: ['A', 'B', 'C'],
    });

    // Move up buttons use ChevronUp icon; they are the first action buttons
    const items = container.querySelectorAll('.queue-panel-item');
    // Click the move-up button on the second item (index 1)
    const upButtons = items[1].querySelectorAll('.queue-panel-action:not(.danger)');

    // The action buttons order for non-editing: up, down, edit, remove(danger)
    // Find by title attribute
    const upBtn = items[1].querySelector('button[title="Move up"]') as HTMLButtonElement;
    act(() => {
      upBtn.click();
    });

    expect(onReorder).toHaveBeenCalledTimes(1);
    expect(onReorder).toHaveBeenCalledWith(1, 0);
  });

  it('calls onReorder(index, index+1) when moving down', async () => {
    const { onReorder } = await renderPanel({
      messages: ['A', 'B', 'C'],
    });

    const items = container.querySelectorAll('.queue-panel-item');
    const downBtn = items[1].querySelector('button[title="Move down"]') as HTMLButtonElement;
    act(() => {
      downBtn.click();
    });

    expect(onReorder).toHaveBeenCalledTimes(1);
    expect(onReorder).toHaveBeenCalledWith(1, 2);
  });

  it('disables the move-up button for the first item', async () => {
    await renderPanel({ messages: ['A', 'B'] });

    const items = container.querySelectorAll('.queue-panel-item');
    const upBtn = items[0].querySelector('button[title="Move up"]') as HTMLButtonElement;
    expect(upBtn.disabled).toBe(true);
  });

  it('disables the move-down button for the last item', async () => {
    await renderPanel({ messages: ['A', 'B'] });

    const items = container.querySelectorAll('.queue-panel-item');
    const downBtn = items[1].querySelector('button[title="Move down"]') as HTMLButtonElement;
    expect(downBtn.disabled).toBe(true);
  });

  it('both move buttons are enabled for middle items', async () => {
    await renderPanel({ messages: ['A', 'B', 'C'] });

    const items = container.querySelectorAll('.queue-panel-item');
    const upBtn = items[1].querySelector('button[title="Move up"]') as HTMLButtonElement;
    const downBtn = items[1].querySelector('button[title="Move down"]') as HTMLButtonElement;
    expect(upBtn.disabled).toBe(false);
    expect(downBtn.disabled).toBe(false);
  });

  it('both move buttons are disabled when there is only one message', async () => {
    await renderPanel({ messages: ['Solo'] });

    const items = container.querySelectorAll('.queue-panel-item');
    const upBtn = items[0].querySelector('button[title="Move up"]') as HTMLButtonElement;
    const downBtn = items[0].querySelector('button[title="Move down"]') as HTMLButtonElement;
    expect(upBtn.disabled).toBe(true);
    expect(downBtn.disabled).toBe(true);
  });
});

// ===========================================================================
// 5. Clear All button
// ===========================================================================

describe('QueuedMessagesPanel – clear all', () => {
  it('calls onClear when Clear All is clicked', async () => {
    const { onClear } = await renderPanel({
      messages: ['A', 'B', 'C'],
    });

    clickButton('.queue-panel-clear');

    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it('does not show Clear All when messages are empty', async () => {
    await renderPanel({ messages: [] });

    expect(container.querySelector('.queue-panel-clear')).toBeNull();
  });
});

// ===========================================================================
// 6. Close button
// ===========================================================================

describe('QueuedMessagesPanel – close button', () => {
  it('calls onClose when close button is clicked', async () => {
    const { onClose } = await renderPanel({ messages: ['A'] });

    clickButton('.queue-panel-close');

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('shows close button even when messages are empty', async () => {
    await renderPanel({ messages: [] });

    expect(container.querySelector('.queue-panel-close')).not.toBeNull();
  });
});

// ===========================================================================
// 7. Editing – start, save, cancel
// ===========================================================================

describe('QueuedMessagesPanel – editing', () => {
  it('enters edit mode when the edit button is clicked', async () => {
    await renderPanel({ messages: ['Hello world'] });

    // Initially, no textarea should be present
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();

    // Click the edit button
    clickButton('button[title="Edit"]');

    // A textarea should now be visible
    const textarea = container.querySelector('.queue-panel-edit-textarea') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    expect(textarea.value).toBe('Hello world');
  });

  it('saves edit on Enter key press', async () => {
    const { onEdit } = await renderPanel({ messages: ['Original text'] });

    // Start editing
    clickButton('button[title="Edit"]');

    // Verify textarea content
    const textarea = container.querySelector('.queue-panel-edit-textarea') as HTMLTextAreaElement;
    expect(textarea.value).toBe('Original text');

    // Clear and type new text
    act(() => {
      // Simulate the user typing by setting the value and dispatching an input event
      // React uses synthetic events so we set value and dispatch 'input'
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
      )?.set!;
      nativeInputValueSetter.call(textarea, 'Updated text');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Verify the state was updated
    expect(textarea.value).toBe('Updated text');

    // Press Enter to save
    fireKeyDown('.queue-panel-edit-textarea', 'Enter');

    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onEdit).toHaveBeenCalledWith(0, 'Updated text');

    // Textarea should no longer be present
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();
  });

  it('does not save on Shift+Enter (allows multiline)', async () => {
    const { onEdit } = await renderPanel({ messages: ['Line one'] });

    clickButton('button[title="Edit"]');
    fireKeyDown('.queue-panel-edit-textarea', 'Enter', true);

    expect(onEdit).not.toHaveBeenCalled();

    // Textarea should still be in edit mode
    expect(container.querySelector('.queue-panel-edit-textarea')).not.toBeNull();
  });

  it('cancels edit on Escape key press', async () => {
    const { onEdit } = await renderPanel({ messages: ['Original text'] });

    clickButton('button[title="Edit"]');

    // Type some text
    const textarea = container.querySelector('.queue-panel-edit-textarea') as HTMLTextAreaElement;
    act(() => {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
      )?.set!;
      nativeInputValueSetter.call(textarea, 'Changed but cancelled');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Press Escape to cancel
    fireKeyDown('.queue-panel-edit-textarea', 'Escape');

    expect(onEdit).not.toHaveBeenCalled();

    // Textarea should be gone; original text should remain as display
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();
    const displayText = container.querySelector('.queue-panel-item-text');
    expect(displayText?.textContent).toBe('Original text');
  });

  it('saves edit via the save button (pencil icon)', async () => {
    const { onEdit } = await renderPanel({ messages: ['Original text'] });

    clickButton('button[title="Edit"]');

    const textarea = container.querySelector('.queue-panel-edit-textarea') as HTMLTextAreaElement;
    act(() => {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
      )?.set!;
      nativeInputValueSetter.call(textarea, 'Saved via button');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Click the save button (appears when editing)
    clickButton('.queue-panel-action.save');

    expect(onEdit).toHaveBeenCalledWith(0, 'Saved via button');
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();
  });

  it('cancels edit via the cancel button (X icon)', async () => {
    const { onEdit } = await renderPanel({ messages: ['Original text'] });

    clickButton('button[title="Edit"]');

    // Click the cancel button (has .queue-panel-action.cancel)
    clickButton('.queue-panel-action.cancel');

    expect(onEdit).not.toHaveBeenCalled();
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();
  });

      it('cancels edit when saving empty text instead of removing message', async () => {const { onEdit } = await renderPanel({ messages: ['Will be emptied'] });

    clickButton('button[title="Edit"]');

    const textarea = container.querySelector('.queue-panel-edit-textarea') as HTMLTextAreaElement;
    act(() => {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
      )?.set!;
      nativeInputValueSetter.call(textarea, '');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    fireKeyDown('.queue-panel-edit-textarea', 'Enter');

    // Editing to empty text cancels the edit (preserves original message) rather than calling onEdit
    expect(onEdit).not.toHaveBeenCalled();
    // Edit mode should be exited (no textarea visible)
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();
    // Original message should still be visible
    expect(container.querySelector('.queue-panel-item-text')?.textContent).toBe('Will be emptied');
  });

  it('cancels edit when saving whitespace-only text', async () => {
    const { onEdit } = await renderPanel({ messages: ['Keep me'] });

    clickButton('button[title="Edit"]');

    const textarea = container.querySelector('.queue-panel-edit-textarea') as HTMLTextAreaElement;
    act(() => {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype, 'value'
      )?.set!;
      nativeInputValueSetter.call(textarea, '   ');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    fireKeyDown('.queue-panel-edit-textarea', 'Enter');

    // Whitespace-only text should cancel the edit, not call onEdit
    expect(onEdit).not.toHaveBeenCalled();
    expect(container.querySelector('.queue-panel-edit-textarea')).toBeNull();
    expect(container.querySelector('.queue-panel-item-text')?.textContent).toBe('Keep me');
  });

  it('shows save and cancel buttons instead of action buttons while editing', async () => {
    await renderPanel({ messages: ['Editable'] });

    // Before editing: should have move up, move down, edit, remove buttons
    const item = container.querySelector('.queue-panel-item');
    expect(item?.querySelector('button[title="Move up"]')).not.toBeNull();
    expect(item?.querySelector('button[title="Move down"]')).not.toBeNull();
    expect(item?.querySelector('button[title="Edit"]')).not.toBeNull();
    expect(item?.querySelector('button[title="Remove"]')).not.toBeNull();

    // Enter edit mode
    clickButton('button[title="Edit"]');

    // After editing: should have save and cancel buttons, no move/edit/remove
    expect(item?.querySelector('button[title="Move up"]')).toBeNull();
    expect(item?.querySelector('button[title="Move down"]')).toBeNull();
    expect(item?.querySelector('button[title="Edit"]')).toBeNull();
    expect(item?.querySelector('button[title="Remove"]')).toBeNull();
    expect(item?.querySelector('button[title="Save (Enter)"]')).not.toBeNull();
    expect(item?.querySelector('button[title="Cancel (Esc)"]')).not.toBeNull();
  });
});

// ===========================================================================
// 8. Only one item can be edited at a time
// ===========================================================================

describe('QueuedMessagesPanel – single item editing', () => {
  it('only one item can be edited at a time – editing a different item replaces the edit', async () => {
    await renderPanel({ messages: ['First', 'Second', 'Third'] });

    const items = container.querySelectorAll('.queue-panel-item');

    // Start editing the first item
    act(() => {
      (items[0].querySelector('button[title="Edit"]') as HTMLButtonElement).click();
    });

    // Textarea should be on the first item
    expect(items[0].querySelector('.queue-panel-edit-textarea')).not.toBeNull();
    expect(items[1].querySelector('.queue-panel-edit-textarea')).toBeNull();

    // Start editing the third item (index 2)
    act(() => {
      (items[2].querySelector('button[title="Edit"]') as HTMLButtonElement).click();
    });

    // Now the third item should have the textarea, the first should be back to display
    expect(items[0].querySelector('.queue-panel-edit-textarea')).toBeNull();
    expect(items[0].querySelector('.queue-panel-item-text')).not.toBeNull();
    expect(items[2].querySelector('.queue-panel-edit-textarea')).not.toBeNull();
  });
});
