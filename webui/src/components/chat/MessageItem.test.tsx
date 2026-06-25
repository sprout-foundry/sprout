// @ts-nocheck
/**
 * MessageItem.test.tsx — Tests for the SP-076 verbosity filter.
 *
 * Pins the rules:
 *   - `compact` mode hides short inter-tool narration (assistant message
 *     with toolRefs, < 120 chars, not the terminal answer).
 *   - `compact` mode does NOT hide the terminal answer (no next
 *     assistant message after it), even if short.
 *   - `compact` mode does NOT hide messages without toolRefs.
 *   - `default` mode shows everything (no filtering).
 *   - `verbose` mode does NOT hide inter-tool narration.
 *   - Reasoning details: `verbose` expands inline (`open={true}`),
 *     `default` and `compact` keep the collapsed toggle.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { MessageItem } from './MessageItem';
import type { Message } from '@sprout/ui';

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

function makeNarration(content: string, toolRefs?: Message['toolRefs']): Message {
  return {
    id: 'msg-1',
    type: 'assistant',
    content,
    timestamp: new Date(),
    toolRefs,
  };
}

describe('MessageItem verbosity filter (SP-076)', () => {
  it('hides short inter-tool narration in compact mode', () => {
    const message = makeNarration('Let me check the file', [
      { id: 't1', name: 'read_file' },
    ]);
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'compact',
          hasNextAssistantMessage: true,
        }),
      );
    });
    expect(container.querySelector('.message-bubble')).toBeNull();
  });

  it('keeps the terminal answer visible in compact mode', () => {
    const message = makeNarration('Done.', [{ id: 't1', name: 'read_file' }]);
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'compact',
          hasNextAssistantMessage: false,
        }),
      );
    });
    expect(container.querySelector('.message-bubble')).not.toBeNull();
  });

  it('does not hide messages without toolRefs in compact mode', () => {
    const message = makeNarration('A short message with no tools.');
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'compact',
          hasNextAssistantMessage: true,
        }),
      );
    });
    expect(container.querySelector('.message-bubble')).not.toBeNull();
  });

  it('does not hide long narration in compact mode', () => {
    const longContent =
      'I need to read the file, parse its contents, and then check the syntax tree to make sure everything is consistent before I make any changes to the codebase.';
    const message = makeNarration(longContent, [{ id: 't1', name: 'read_file' }]);
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'compact',
          hasNextAssistantMessage: true,
        }),
      );
    });
    expect(container.querySelector('.message-bubble')).not.toBeNull();
  });

  it('shows inter-tool narration in default mode', () => {
    const message = makeNarration('Let me check the file', [
      { id: 't1', name: 'read_file' },
    ]);
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'default',
          hasNextAssistantMessage: true,
        }),
      );
    });
    expect(container.querySelector('.message-bubble')).not.toBeNull();
  });

  it('shows inter-tool narration in verbose mode', () => {
    const message = makeNarration('Let me check the file', [
      { id: 't1', name: 'read_file' },
    ]);
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'verbose',
          hasNextAssistantMessage: true,
        }),
      );
    });
    expect(container.querySelector('.message-bubble')).not.toBeNull();
  });

  it('expands reasoning inline in verbose mode', () => {
    const message: Message = {
      id: 'msg-1',
      type: 'assistant',
      content: 'Final answer.',
      reasoning: 'Let me think about this...',
      timestamp: new Date(),
    };
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'verbose',
          hasNextAssistantMessage: false,
        }),
      );
    });
    const details = container.querySelector('details.reasoning-block');
    expect(details).not.toBeNull();
    expect(details?.hasAttribute('open')).toBe(true);
  });

  it('keeps reasoning collapsed in default mode', () => {
    const message: Message = {
      id: 'msg-1',
      type: 'assistant',
      content: 'Final answer.',
      reasoning: 'Let me think about this...',
      timestamp: new Date(),
    };
    act(() => {
      root.render(
        createElement(MessageItem, {
          ...baseProps,
          message,
          messageIndex: 0,
          outputVerbosity: 'default',
          hasNextAssistantMessage: false,
        }),
      );
    });
    const details = container.querySelector('details.reasoning-block');
    expect(details).not.toBeNull();
    expect(details?.hasAttribute('open')).toBe(false);
  });
});