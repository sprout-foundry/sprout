// @ts-nocheck
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { WorktreeChatDialog } from './WorktreeChatDialog';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock requestAnimationFrame so close-listener effect fires synchronously.
// jest does not auto-flush rAF; without this, close listeners never attach.
let rafId = 0;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  global.requestAnimationFrame = ((cb) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = jest.fn();

  // Mock setTimeout so the branch-input auto-focus timer fires synchronously.
  // The component calls setTimeout(() => branchInputRef.current?.focus(), 50).
  global.setTimeout = ((fn: TimerHandler, _delay?: number, ...args: unknown[]) => {
    if (typeof fn === 'function') {
      fn.apply(global, args);
    }
    return ++rafId;
  }) as typeof setTimeout;
  global.clearTimeout = jest.fn();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let mountPoint: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  jest.clearAllMocks();
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (mountPoint) {
    document.body.removeChild(mountPoint);
    mountPoint = null;
  }
  document.querySelectorAll('.wt-chat-dialog-overlay').forEach((el) => el.remove());
});

type DialogProps = {
  isOpen?: boolean;
  isCreating?: boolean;
  error?: string | null;
};

/**
 * Renders WorktreeChatDialog with the given props. Returns spies.
 */
function renderDialog(props: DialogProps = {}) {
  const { isOpen = true, isCreating = false, error = null } = props;
  const onClose = jest.fn();
  const onSubmit = jest.fn();

  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root = createRoot(mountPoint!);
    root.render(
      <WorktreeChatDialog
        isOpen={isOpen}
        onClose={onClose}
        onSubmit={onSubmit}
        isCreating={isCreating}
        error={error}
      />,
    );
  });

  return { onClose, onSubmit };
}

/**
 * Convenience: re-render the dialog with updated props on the existing root.
 */
function rerenderDialog(
  props: {
    isOpen?: boolean;
    isCreating?: boolean;
    error?: string | null;
    onClose?: ReturnType<typeof jest.fn>;
    onSubmit?: ReturnType<typeof jest.fn>;
  },
) {
  const { isOpen = true, isCreating = false, error = null, onClose, onSubmit } = props;
  act(() => {
    root!.render(
      <WorktreeChatDialog
        isOpen={isOpen}
        onClose={onClose ?? jest.fn()}
        onSubmit={onSubmit ?? jest.fn()}
        isCreating={isCreating}
        error={error}
      />,
    );
  });
}

/**
 * Set the value of an input element and dispatch a React-handled change event.
 */
function setInputValue(selector: string, value: string) {
  const input = document.querySelector(selector) as HTMLInputElement;
  const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
    window.HTMLInputElement.prototype,
    'value',
  )!.set!;
  act(() => {
    nativeInputValueSetter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

/**
 * Click a checkbox to toggle its state. Since it defaults to checked (autoSwitch=true),
 * a single click will uncheck it; a second click will re-check it.
 */
function clickCheckbox(selector: string) {
  const input = document.querySelector(selector) as HTMLInputElement;
  act(() => {
    input.click();
  });
}

/**
 * Submit the dialog form by clicking the create button inside act().
 */
function clickCreateButton() {
  const btn = document.querySelector(
    '.wt-chat-dialog-btn-create',
  ) as HTMLButtonElement;
  act(() => {
    btn.click();
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WorktreeChatDialog', () => {
  test('does not render when isOpen is false', () => {
    renderDialog({ isOpen: false });
    const overlay = document.querySelector('.wt-chat-dialog-overlay');
    expect(overlay).toBeNull();
  });

  test('renders dialog when isOpen is true (overlay + card + header)', () => {
    renderDialog({ isOpen: true });

    const overlay = document.querySelector('.wt-chat-dialog-overlay');
    expect(overlay).not.toBeNull();

    const card = document.querySelector('.wt-chat-dialog-card');
    expect(card).not.toBeNull();

    const header = document.querySelector('.wt-chat-dialog-header');
    expect(header).not.toBeNull();

    const title = document.querySelector('#wt-chat-dialog-title');
    expect(title).not.toBeNull();
    expect(title!.textContent).toContain('Create Chat in Worktree');
  });

  test('calls onClose when clicking the overlay (outside the card)', () => {
    const { onClose } = renderDialog({ isOpen: true });

    const overlay = document.querySelector(
      '.wt-chat-dialog-overlay',
    ) as HTMLElement;

    act(() => {
      // Simulate a click directly on the overlay (not on any child).
      // React's handleOverlayClick checks e.target === e.currentTarget.
      overlay.dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      );
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('does NOT call onClose when clicking inside the card or onSubmit', () => {
    const { onClose, onSubmit } = renderDialog({ isOpen: true });

    const card = document.querySelector('.wt-chat-dialog-card') as HTMLElement;

    act(() => {
      card.dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      );
    });

    // Clicking inside the card should not close the dialog
    expect(onClose).not.toHaveBeenCalled();
    // It also should not submit the form (card is not a submit button)
    expect(onSubmit).not.toHaveBeenCalled();
  });

  test('calls onClose when Escape is pressed', () => {
    const { onClose } = renderDialog({ isOpen: true });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('shows error message when error prop is provided', () => {
    const errorMsg = 'Branch already exists';
    renderDialog({ isOpen: true, error: errorMsg });

    const errorEl = document.querySelector('.wt-chat-dialog-error');
    expect(errorEl).not.toBeNull();
    expect(errorEl!.textContent).toContain(errorMsg);
    expect(errorEl!.getAttribute('role')).toBe('alert');
  });

  test('all inputs are disabled when isCreating is true', () => {
    renderDialog({ isOpen: true, isCreating: true });

    const branchInput = document.querySelector('#wt-branch') as HTMLInputElement;
    const baseRefInput = document.querySelector('#wt-base-ref') as HTMLInputElement;
    const nameInput = document.querySelector('#wt-name') as HTMLInputElement;
    const checkbox = document.querySelector(
      '#wt-auto-switch',
    ) as HTMLInputElement;

    expect(branchInput.disabled).toBe(true);
    expect(baseRefInput.disabled).toBe(true);
    expect(nameInput.disabled).toBe(true);
    expect(checkbox.disabled).toBe(true);

    // Cancel button should also be disabled
    const cancelBtn = document.querySelector(
      '.wt-chat-dialog-btn-cancel',
    ) as HTMLButtonElement;
    expect(cancelBtn.disabled).toBe(true);
  });

  test('create button is disabled when branch is empty', () => {
    const { onSubmit } = renderDialog({ isOpen: true });

    const createBtn = document.querySelector(
      '.wt-chat-dialog-btn-create',
    ) as HTMLButtonElement;

    // Initially branch is empty, so create button should be disabled
    expect(createBtn.disabled).toBe(true);

    // Clicking the disabled button should not submit
    act(() => {
      createBtn.click();
    });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  test('form calls onSubmit with correct params when submit is valid', () => {
    const { onSubmit } = renderDialog({ isOpen: true });

    // Fill in all fields (checkbox defaults to checked/true)
    setInputValue('#wt-branch', 'feature/my-feature');
    setInputValue('#wt-base-ref', 'develop');
    setInputValue('#wt-name', 'My Feature Chat');
    // Don't click checkbox — it's already true by default

    clickCreateButton();

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith({
      branch: 'feature/my-feature',
      baseRef: 'develop',
      name: 'My Feature Chat',
      autoSwitch: true,
    });
  });

  test('calls onSubmit with empty strings for optional fields when not filled', () => {
    const { onSubmit } = renderDialog({ isOpen: true });

    // Only fill required field (branch)
    setInputValue('#wt-branch', 'feature/quick-fix');

    clickCreateButton();

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith({
      branch: 'feature/quick-fix',
      baseRef: '',
      name: '',
      autoSwitch: true, // default is true
    });
  });

  test('auto-switch checkbox defaults to true and submits correct value when unchecked', () => {
    const { onSubmit } = renderDialog({ isOpen: true });

    // Verify checkbox is checked by default
    const checkbox = document.querySelector(
      '#wt-auto-switch',
    ) as HTMLInputElement;
    expect(checkbox.checked).toBe(true);

    // Uncheck it (defaults to true, so a click unchecks it)
    clickCheckbox('#wt-auto-switch');
    expect(checkbox.checked).toBe(false);

    // Fill branch and submit
    setInputValue('#wt-branch', 'feature/no-switch');

    clickCreateButton();

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith({
      branch: 'feature/no-switch',
      baseRef: '',
      name: '',
      autoSwitch: false,
    });
  });

  test('shows loading state text when isCreating is true', () => {
    renderDialog({ isOpen: true, isCreating: true });

    const createBtn = document.querySelector(
      '.wt-chat-dialog-btn-create',
    ) as HTMLButtonElement;

    expect(createBtn.disabled).toBe(true);
    expect(createBtn.textContent).toContain('Creating...');

    // Verify the spinner icon is rendered
    const spinner = document.querySelector('.wt-chat-dialog-spinner');
    expect(spinner).not.toBeNull();

    // Verify it does NOT show the default "Create" text
    expect(createBtn.textContent).not.toContain('Create');
  });

  test('does not show error element when error is null', () => {
    renderDialog({ isOpen: true, error: null });

    const errorEl = document.querySelector('.wt-chat-dialog-error');
    expect(errorEl).toBeNull();
  });

  test('dialog is removed from DOM when isOpen transitions to false', () => {
    const onClose = jest.fn();
    renderDialog({ isOpen: true });

    expect(document.querySelector('.wt-chat-dialog-overlay')).not.toBeNull();

    // Re-render with isOpen=false using the same onClose
    rerenderDialog({ isOpen: false, onClose });

    expect(document.querySelector('.wt-chat-dialog-overlay')).toBeNull();
  });
});
