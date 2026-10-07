// @ts-nocheck
/**
 * MessageItem.languageGuardOriginal.test.tsx — Tests for the language
 * guard's "view original" affordance.
 *
 * Pins the rules:
 *   - An assistant message carrying `languageGuardOriginal` renders a
 *     collapsed "Original reply" expander (the notice's promise made
 *     real).
 *   - Expanding it reveals the held original text; the default message
 *     content still shows alongside it.
 *   - A message WITHOUT `languageGuardOriginal` renders no expander.
 */

import type { Message } from '@sprout/ui';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { MessageItem } from './MessageItem';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  vi.useFakeTimers();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  vi.useRealTimers();
});

const baseProps = {
  findMatchingToolExecution: () => undefined,
  getToolStatus: () => undefined,
  formatTime: () => '12:00',
};

const originalText = 'The build succeeded after applying the patch, so the tests can run and the release is ready.';

function makeGuardedMessage(content: string, original?: string): Message {
  const message: Message = {
    id: 'msg-1',
    type: 'assistant',
    content,
    timestamp: new Date(),
  };
  if (original !== undefined) {
    message.languageGuardOriginal = original;
  }
  return message;
}

function summary(): HTMLSummaryElement | null {
  return container.querySelector<HTMLElement>('details[data-testid="language-guard-original"] > summary');
}

describe('MessageItem language guard "view original"', () => {
  it('renders the expander for a message carrying languageGuardOriginal', () => {
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message: makeGuardedMessage(
            'La respuesta llegó en un idioma diferente en lugar de en español. Puedes ver el texto original.',
            originalText,
          ),
          messageIndex: 0,
        }),
      );
    });
    const details = container.querySelector('details[data-testid="language-guard-original"]');
    expect(details).not.toBeNull();
    expect(details?.hasAttribute('open')).toBe(false);
    expect(summary()?.getAttribute('aria-label')).toBe('Original reply');
    // The expander sits below the replaced reply, which still renders.
    expect(container.textContent).toContain('Puedes ver el texto original');
  });

  it('reveals the original text when expanded', () => {
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message: makeGuardedMessage('The reply came back in another language.', originalText),
          messageIndex: 0,
        }),
      );
    });
    // Collapsible keeps the body mounted and hidden via CSS while closed;
    // the meaningful toggle assertion is the `open` attribute flip.
    const details = container.querySelector('details[data-testid="language-guard-original"]');
    expect(details?.hasAttribute('open')).toBe(false);
    act(() => {
      summary()?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const opened = container.querySelector('details[data-testid="language-guard-original"]');
    expect(opened?.hasAttribute('open')).toBe(true);
    expect(container.textContent).toContain(originalText);
  });

  it('renders no expander for a message without languageGuardOriginal', () => {
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message: makeGuardedMessage('A normal reply in the user’s language.'),
          messageIndex: 0,
        }),
      );
    });
    expect(container.querySelector('details[data-testid="language-guard-original"]')).toBeNull();
    expect(container.querySelector('.language-guard-original')).toBeNull();
  });

  it('renders no expander for a whitespace-only payload', () => {
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message: makeGuardedMessage('A normal reply.', '   '),
          messageIndex: 0,
        }),
      );
    });
    expect(container.querySelector('details[data-testid="language-guard-original"]')).toBeNull();
  });

  it('does not render the expander for user messages', () => {
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message: {
            id: 'msg-u1',
            type: 'user',
            content: 'hola',
            timestamp: new Date(),
          },
          messageIndex: 0,
        }),
      );
    });
    expect(container.querySelector('details[data-testid="language-guard-original"]')).toBeNull();
  });
});
