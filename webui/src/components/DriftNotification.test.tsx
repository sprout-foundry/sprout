/**
 * DriftNotification.test.tsx — Unit tests for the DriftNotification component.
 *
 * Covers:
 * - Rendering with similarity and threshold percentages
 * - Continue here button calls onDismiss
 * - Start new chat button calls onStartNewChat
 * - Dismiss X button calls onDismiss
 * - Auto-dismiss after 30 seconds
 * - Escape key dismisses notification
 * - Similarity formatting displays correct percentage values
 * - Cleanup clears timer on unmount
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import DriftNotification from './DriftNotification';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

function renderDrift(overrides: {
  similarity?: number;
  threshold?: number;
  onDismiss?: ReturnType<typeof vi.fn>;
  onStartNewChat?: ReturnType<typeof vi.fn>;
} = {}) {
  const props = {
    similarity: 0.45,
    threshold: 0.6,
    onDismiss: vi.fn(),
    onStartNewChat: vi.fn(),
    ...overrides,
  };
  act(() => {
    root.render(createElement(DriftNotification, props));
  });
  return props;
}

// ---------------------------------------------------------------------------
// Tests: Rendering
// ---------------------------------------------------------------------------

describe('Rendering', () => {
  it('renders with correct role and aria attributes', () => {
    renderDrift();
    const drift = container.querySelector('.drift-notification');
    expect(drift).not.toBeNull();
    expect(drift?.getAttribute('role')).toBe('alert');
    expect(drift?.getAttribute('aria-live')).toBe('polite');
  });

  it('renders warning icon', () => {
    renderDrift();
    const icon = container.querySelector('.drift-notification-icon');
    expect(icon?.textContent).toBe('⚠️');
  });

  it('renders "Conversation drift detected" title', () => {
    renderDrift();
    const title = container.querySelector('.drift-notification-text strong');
    expect(title?.textContent).toBe('Conversation drift detected');
  });

  it('renders both action buttons', () => {
    renderDrift();
    const buttons = container.querySelectorAll('.drift-notification-btn');
    expect(buttons).toHaveLength(2);
  });

  it('renders dismiss X button', () => {
    renderDrift();
    const dismissBtn = container.querySelector('.drift-notification-dismiss');
    expect(dismissBtn).not.toBeNull();
  });

  it('renders dismiss button with correct aria-label and title', () => {
    renderDrift();
    const dismissBtn = container.querySelector('.drift-notification-dismiss');
    expect(dismissBtn?.getAttribute('aria-label')).toBe('Dismiss notification');
    expect(dismissBtn?.getAttribute('title')).toBe('Dismiss');
  });
});

// ---------------------------------------------------------------------------
// Tests: Similarity formatting
// ---------------------------------------------------------------------------

describe('Similarity formatting', () => {
  it('displays similarity and threshold as percentages', () => {
    renderDrift({ similarity: 0.45, threshold: 0.6 });
    const detail = container.querySelector('.drift-notification-detail');
    expect(detail?.textContent).toContain('45%');
    expect(detail?.textContent).toContain('60%');
  });

  it('rounds similarity percentage correctly', () => {
    renderDrift({ similarity: 0.456, threshold: 0.7 });
    const detail = container.querySelector('.drift-notification-detail');
    expect(detail?.textContent).toContain('46%');
    expect(detail?.textContent).toContain('70%');
  });

  it('rounds threshold percentage correctly', () => {
    renderDrift({ similarity: 0.5, threshold: 0.654 });
    const detail = container.querySelector('.drift-notification-detail');
    expect(detail?.textContent).toContain('50%');
    expect(detail?.textContent).toContain('65%');
  });

  it('displays 0% when similarity is 0', () => {
    renderDrift({ similarity: 0, threshold: 0.5 });
    const detail = container.querySelector('.drift-notification-detail');
    expect(detail?.textContent).toContain('0%');
  });

  it('displays 100% when similarity is 1', () => {
    renderDrift({ similarity: 1, threshold: 0.5 });
    const detail = container.querySelector('.drift-notification-detail');
    expect(detail?.textContent).toContain('100%');
  });

  it('formats decimal values correctly', () => {
    renderDrift({ similarity: 0.789, threshold: 0.987 });
    const detail = container.querySelector('.drift-notification-detail');
    expect(detail?.textContent).toContain('79%');
    expect(detail?.textContent).toContain('99%');
  });
});

// ---------------------------------------------------------------------------
// Tests: Continue here button
// ---------------------------------------------------------------------------

describe('Continue here button', () => {
  it('calls onDismiss when "Continue here" is clicked', () => {
    const props = renderDrift();
    const continueBtn = container.querySelector('.drift-notification-btn-primary');
    act(() => {
      continueBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(props.onDismiss).toHaveBeenCalledTimes(1);
  });

  it('has autoFocus on continue button', () => {
    renderDrift();
    const continueBtn = container.querySelector('.drift-notification-btn-primary') as HTMLButtonElement | null;
    expect(continueBtn).not.toBeNull();
    // autoFocus focuses the button on mount
    expect(document.activeElement).toBe(continueBtn);
  });

  it('has type="button" to prevent form submission', () => {
    renderDrift();
    const continueBtn = container.querySelector('.drift-notification-btn-primary');
    expect(continueBtn?.getAttribute('type')).toBe('button');
  });
});

// ---------------------------------------------------------------------------
// Tests: Start new chat button
// ---------------------------------------------------------------------------

describe('Start new chat button', () => {
  it('calls onStartNewChat when "Start new chat" is clicked', () => {
    const props = renderDrift();
    const newChatBtn = container.querySelector('.drift-notification-btn-secondary');
    act(() => {
      newChatBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(props.onStartNewChat).toHaveBeenCalledTimes(1);
  });

  it('does NOT call onDismiss when "Start new chat" is clicked', () => {
    const props = renderDrift();
    const newChatBtn = container.querySelector('.drift-notification-btn-secondary');
    act(() => {
      newChatBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(props.onDismiss).not.toHaveBeenCalled();
  });

  it('has type="button" to prevent form submission', () => {
    renderDrift();
    const newChatBtn = container.querySelector('.drift-notification-btn-secondary');
    expect(newChatBtn?.getAttribute('type')).toBe('button');
  });
});

// ---------------------------------------------------------------------------
// Tests: Dismiss X button
// ---------------------------------------------------------------------------

describe('Dismiss X button', () => {
  it('calls onDismiss when X dismiss button is clicked', () => {
    const props = renderDrift();
    const dismissBtn = container.querySelector('.drift-notification-dismiss');
    act(() => {
      dismissBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(props.onDismiss).toHaveBeenCalledTimes(1);
  });

  it('does NOT call onStartNewChat when dismiss button is clicked', () => {
    const props = renderDrift();
    const dismissBtn = container.querySelector('.drift-notification-dismiss');
    act(() => {
      dismissBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(props.onStartNewChat).not.toHaveBeenCalled();
  });

  it('has type="button" to prevent form submission', () => {
    renderDrift();
    const dismissBtn = container.querySelector('.drift-notification-dismiss');
    expect(dismissBtn?.getAttribute('type')).toBe('button');
  });
});

// ---------------------------------------------------------------------------
// Tests: Escape key
// ---------------------------------------------------------------------------

describe('Escape key', () => {
  it('calls onDismiss when Escape key is pressed', () => {
    const props = renderDrift();
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(props.onDismiss).toHaveBeenCalledTimes(1);
  });

  it('does not call onDismiss for other keys', () => {
    const props = renderDrift();
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    });
    expect(props.onDismiss).not.toHaveBeenCalled();
  });

  it('cleans up Escape key listener when component unmounts', () => {
    const props = renderDrift();
    act(() => {
      root.unmount();
    });
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(props.onDismiss).toHaveBeenCalledTimes(0);
  });

  it('clears auto-dismiss timer when Escape is pressed', () => {
    vi.useFakeTimers();
    const props = renderDrift();
    act(() => {
      vi.advanceTimersByTime(10000);
    });
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(props.onDismiss).toHaveBeenCalledTimes(1);
    // Timer should be cleared, so advancing further should not trigger again
    act(() => {
      vi.advanceTimersByTime(25000);
    });
    expect(props.onDismiss).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});

// ---------------------------------------------------------------------------
// Tests: Auto-dismiss
// ---------------------------------------------------------------------------

describe('Auto-dismiss', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('calls onDismiss after 30 seconds', () => {
    const props = renderDrift();
    expect(props.onDismiss).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(30000);
    });

    expect(props.onDismiss).toHaveBeenCalledTimes(1);
  });

  it('does not call onDismiss before 30 seconds', () => {
    const props = renderDrift();

    act(() => {
      vi.advanceTimersByTime(29999);
    });

    expect(props.onDismiss).not.toHaveBeenCalled();
  });

  it('clears timer when component unmounts', () => {
    const props = renderDrift();

    act(() => {
      root.unmount();
    });

    act(() => {
      vi.advanceTimersByTime(30000);
    });

    expect(props.onDismiss).not.toHaveBeenCalled();
  });

  it('clicking a button before auto-dismiss works correctly', () => {
    const props = renderDrift();

    act(() => {
      vi.advanceTimersByTime(10000);
    });

    const continueBtn = container.querySelector('.drift-notification-btn-primary');
    act(() => {
      continueBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(props.onDismiss).toHaveBeenCalledTimes(1);

    // Timer should be cleared, so advancing further should not trigger again
    act(() => {
      vi.advanceTimersByTime(20000);
    });

    expect(props.onDismiss).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: Button ordering
// ---------------------------------------------------------------------------

describe('Button ordering', () => {
  it('renders buttons in correct order: dismiss, primary, secondary', () => {
    renderDrift();
    const buttons = container.querySelectorAll('button');
    expect(buttons).toHaveLength(3);

    // First button is dismiss
    expect(buttons[0]).toBe(container.querySelector('.drift-notification-dismiss'));
    // Second button is primary
    expect(buttons[1]).toBe(container.querySelector('.drift-notification-btn-primary'));
    // Third button is secondary
    expect(buttons[2]).toBe(container.querySelector('.drift-notification-btn-secondary'));
  });
});

// ---------------------------------------------------------------------------
// End of tests
// ---------------------------------------------------------------------------
