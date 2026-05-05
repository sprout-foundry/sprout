// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import MessageBubble from './MessageBubble';
import * as clipboard from '../utils/clipboard';

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
  vi.spyOn(clipboard, 'copyToClipboard').mockResolvedValue();
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MessageBubble', () => {
  it('renders with default type (assistant)', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test message',
        children: 'Hello',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble).not.toBeNull();
    expect(bubble?.classList.contains('assistant')).toBe(true);
    expect(bubble?.getAttribute('role')).toBe('assistant-message');
  });

  it('renders with user type', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        type: 'user',
        ariaLabel: 'User message',
        children: 'Hi there',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble).not.toBeNull();
    expect(bubble?.classList.contains('user')).toBe(true);
    expect(bubble?.getAttribute('role')).toBe('user-message');
  });

  it('applies aria-label', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Custom label',
        children: 'Content',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble?.getAttribute('aria-label')).toBe('Custom label');
  });

  it('renders children content inside message-content', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'My content',
      }));
    });

    const content = container.querySelector('.message-content');
    expect(content).not.toBeNull();
    expect(content?.textContent).toBe('My content');
  });

  it('renders children as React elements', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: createElement('span', { 'data-testid': 'child-span' }, 'Nested'),
      }));
    });

    expect(container.querySelector('[data-testid="child-span"]')).not.toBeNull();
  });

  it('shows copy button when copyText is provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Text to copy',
        children: 'Content',
      }));
    });

    const copyBtn = container.querySelector('.copy-button');
    expect(copyBtn).not.toBeNull();
    expect(copyBtn?.getAttribute('title')).toBe('Copy message');
    expect(copyBtn?.getAttribute('aria-label')).toBe('Copy message');
  });

  it('hides copy button when copyText is not provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'Content',
      }));
    });

    expect(container.querySelector('.copy-button')).toBeNull();
  });

  it('hides copy button when copyText is empty string', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: '',
        children: 'Content',
      }));
    });

    expect(container.querySelector('.copy-button')).toBeNull();
  });

  it('calls copyToClipboard with copyText on copy button click', async () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Copied content',
        children: 'Content',
      }));
    });

    const copyBtn = container.querySelector('.copy-button');
    await act(async () => {
      copyBtn?.click();
    });

    expect(clipboard.copyToClipboard).toHaveBeenCalledWith('Copied content');
  });

  it('shows timestamp when provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        timestamp: '2024-01-15 10:30:00',
        children: 'Content',
      }));
    });

    const ts = container.querySelector('.message-timestamp');
    expect(ts).not.toBeNull();
    expect(ts?.textContent).toBe('2024-01-15 10:30:00');
  });

  it('hides timestamp when not provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'Content',
      }));
    });

    expect(container.querySelector('.message-timestamp')).toBeNull();
  });

  it('sets data-message-content attribute on bubble when copyText is provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Copiable text',
        children: 'Content',
      }));
    });

    const bubbleInner = container.querySelector('[data-message-content]');
    expect(bubbleInner).not.toBeNull();
    expect(bubbleInner?.getAttribute('data-message-content')).toBe('Copiable text');
  });

  it('sets data-message-content to empty string when no copyText', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'Content',
      }));
    });

    const bubbleInner = container.querySelector('[data-message-content]');
    expect(bubbleInner).not.toBeNull();
    expect(bubbleInner?.getAttribute('data-message-content')).toBe('');
  });

  it('renders copy button with SVG icon', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Text',
        children: 'Content',
      }));
    });

    const copyBtn = container.querySelector('.copy-button');
    expect(copyBtn?.querySelector('svg')).not.toBeNull();
  });

  it('combines all features: user type, copyText, timestamp', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        type: 'user',
        ariaLabel: 'User copyable message',
        copyText: 'Copiable',
        timestamp: 'Now',
        children: 'Full example',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble?.classList.contains('user')).toBe(true);
    expect(bubble?.getAttribute('role')).toBe('user-message');
    expect(container.querySelector('.copy-button')).not.toBeNull();
    expect(container.querySelector('.message-timestamp')?.textContent).toBe('Now');
    expect(container.querySelector('.message-content')?.textContent).toBe('Full example');
  });
});
