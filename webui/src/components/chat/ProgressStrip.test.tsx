/**
 * ProgressStrip tests.
 *
 * Pattern (mirrors EventsContext.test.tsx and useCommandOutput.test.ts):
 * render the strip inside an EventsContextProvider whose provider mock
 * exposes onEvent: vi.fn(), capture the registered callback, and invoke
 * it with synthetic events.
 *
 * The strip is chat-scoped: events carrying a chat_id for another chat
 * are ignored, chatless events pass, and switching the chatId prop clears
 * the stored summary.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, expect, it, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import type { EventsProvider, SproutEventCallback } from '@sprout/events';
import { EventsContextProvider } from '../../contexts/EventsContext';
import ProgressStrip from './ProgressStrip';

// Mock lucide-react icons — the forwardRef pattern breaks in jsdom; a
// plain SVG stands in.
vi.mock('lucide-react', () => {
  const { createElement: h } = require('react');
  const icons = ['Info'];
  const result: Record<string, (props: unknown) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: unknown) => h('svg', { 'data-testid': name.toLowerCase(), ...(props as object) });
  }
  return result;
});

function createMockEventsProvider(): EventsProvider {
  return {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onEvent: vi.fn(),
    removeEvent: vi.fn(),
    sendEvent: vi.fn(),
    isConnected: vi.fn(() => true),
    onReconnect: vi.fn(),
    freeze: vi.fn(),
    resume: vi.fn(),
    resetAndReconnect: vi.fn(),
    getQueuedMessageCount: vi.fn(() => 0),
    flushQueuedMessages: vi.fn(() => 0),
  };
}

let container: HTMLDivElement;
let root: Root | undefined;
let provider: EventsProvider;
let handler: SproutEventCallback | undefined;

function fire(event: { type: string; data?: unknown }) {
  if (!handler) throw new Error('no event handler registered');
  act(() => {
    handler(event);
  });
}

/**
 * Render (or re-render) the strip with a chatId. Calling it again with a
 * different chatId updates the prop in place — React keeps the component
 * mounted, so the subscription is not re-registered and the chatId-clear
 * effect runs — which is exactly the user-visible chat-switch path.
 */
function renderStrip(chatId?: string) {
  act(() => {
    root!.render(createElement(EventsContextProvider, { provider }, createElement(ProgressStrip, { chatId })));
  });
  const calls = provider.onEvent.mock.calls as Array<[SproutEventCallback]>;
  expect(calls.length).toBe(1);
  handler = calls[0][0];
}

const strip = () => container.querySelector('[data-testid="progress-strip"]');

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete globalThis.IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  provider = createMockEventsProvider();
  handler = undefined;
});

afterEach(() => {
  if (root) {
    act(() => {
      root.unmount();
    });
    root = undefined;
  }
  container?.remove();
});

// Flat milestone payload for chat-1's run — the summary line the strip
// shows for a finished scope item.
const chat1Finished = {
  run_id: 'run-1',
  plan_revision: 3,
  elapsed_ms: 1000,
  phase: 'finished',
  scope_title: 'sign-up form',
  files_touched: 4,
  chat_id: 'chat-1',
};

describe('ProgressStrip', () => {
  it('renders nothing initially', () => {
    renderStrip();
    expect(strip()).toBeNull();
  });

  it('renders the verified complete summary', () => {
    renderStrip();
    fire({
      type: 'progress_complete',
      data: {
        run_id: 'run-1',
        plan_revision: 3,
        verified: true,
        verification: {
          run_id: 'run-1',
          plan_revision: 3,
          passed: true,
          checks: [
            { kind: 'build', passed: true },
            { kind: 'test', passed: true },
          ],
        },
      },
    });
    const el = strip();
    expect(el).not.toBeNull();
    expect(el!.textContent).toContain('Run complete — verified (Checks: 2/2 passed)');
  });

  it('clears the summary on query_started (new run)', () => {
    renderStrip();
    fire({
      type: 'progress_milestone',
      data: {
        run_id: 'run-1',
        plan_revision: 3,
        elapsed_ms: 1000,
        phase: 'finished',
        scope_title: 'sign-up form',
        files_touched: 4,
      },
    });
    expect(strip()!.textContent).toContain('Finished: sign-up form (4 files)');

    fire({ type: 'query_started' });
    expect(strip()).toBeNull();
  });

  it('renders the milestone summary (latest wins)', () => {
    renderStrip();
    fire({
      type: 'progress_milestone',
      data: {
        run_id: 'run-1',
        plan_revision: 3,
        elapsed_ms: 1000,
        phase: 'started',
        scope_title: 'billing',
      },
    });
    expect(strip()!.textContent).toContain('Started: billing');

    fire({
      type: 'progress_milestone',
      data: {
        run_id: 'run-1',
        plan_revision: 3,
        elapsed_ms: 2000,
        phase: 'finished',
        scope_title: 'billing',
        files_touched: 2,
      },
    });
    const el = strip();
    expect(el!.textContent).toContain('Finished: billing (2 files)');
    expect(el!.textContent).not.toContain('Started:');
  });

  it('ignores progress events whose template says nothing', () => {
    renderStrip();
    fire({
      type: 'progress_milestone',
      data: { run_id: 'run-1', plan_revision: 3, elapsed_ms: 1, phase: 'paused' },
    });
    expect(strip()).toBeNull();
  });

  it('removes the subscription on unmount', () => {
    renderStrip();
    expect(provider.onEvent).toHaveBeenCalledTimes(1);
    expect(provider.removeEvent).not.toHaveBeenCalled();
    act(() => {
      root!.unmount();
    });
    root = undefined;
    expect(provider.removeEvent).toHaveBeenCalledTimes(1);
  });

  // ── chat scoping ────────────────────────────────────────────────────

  it('accepts an event whose chat_id matches the active chat', () => {
    renderStrip('chat-1');
    fire({ type: 'progress_milestone', data: chat1Finished });
    expect(strip()!.textContent).toContain('Finished: sign-up form (4 files)');
  });

  it('ignores an event whose chat_id names a different chat', () => {
    renderStrip('chat-1');
    fire({
      type: 'progress_milestone',
      data: { ...chat1Finished, chat_id: 'chat-2', scope_title: 'other chat work' },
    });
    expect(strip()).toBeNull();
  });

  it('accepts a chatless event (no chat_id) in any chat', () => {
    renderStrip('chat-1');
    fire({ type: 'progress_milestone', data: { ...chat1Finished, chat_id: undefined } });
    expect(strip()!.textContent).toContain('Finished: sign-up form (4 files)');
  });

  it('ignores another chat’s query_started (does not clear the summary)', () => {
    renderStrip('chat-1');
    fire({ type: 'progress_milestone', data: chat1Finished });
    expect(strip()).not.toBeNull();

    fire({ type: 'query_started', data: { query: 'other chat’s turn', chat_id: 'chat-2' } });
    expect(strip()).not.toBeNull();
    expect(strip()!.textContent).toContain('Finished: sign-up form (4 files)');

    // This chat's own new run still clears it.
    fire({ type: 'query_started', data: { query: 'this chat’s turn', chat_id: 'chat-1' } });
    expect(strip()).toBeNull();
  });

  it('clears the summary when the chatId prop changes', () => {
    renderStrip('chat-1');
    fire({ type: 'progress_milestone', data: chat1Finished });
    expect(strip()!.textContent).toContain('Finished: sign-up form (4 files)');

    // User switches chats — the previous chat's progress line must not
    // follow them into the new chat.
    renderStrip('chat-2');
    expect(strip()).toBeNull();
  });

  it('renders a coalesced batch with a matching chat_id as the count line', () => {
    renderStrip('chat-1');
    fire({
      type: 'progress_milestone',
      data: {
        run_id: 'run-1',
        milestones: [
          { run_id: 'run-1', plan_revision: 3, phase: 'finished', scope_id: 'a', elapsed_ms: 1 },
          { run_id: 'run-1', plan_revision: 3, phase: 'finished', scope_id: 'b', elapsed_ms: 2 },
          { run_id: 'run-1', plan_revision: 3, phase: 'started', scope_id: 'c' },
        ],
        client_id: 'client-1',
        chat_id: 'chat-1',
        user_id: 'user-1',
      },
    });
    expect(strip()!.textContent).toContain('Milestones: 3');
  });

  it('ignores a coalesced batch routed to a different chat', () => {
    renderStrip('chat-1');
    fire({
      type: 'progress_milestone',
      data: {
        run_id: 'run-9',
        milestones: [
          { run_id: 'run-9', plan_revision: 1, phase: 'finished', scope_id: 'a' },
          { run_id: 'run-9', plan_revision: 1, phase: 'started', scope_id: 'b' },
        ],
        chat_id: 'chat-2',
      },
    });
    expect(strip()).toBeNull();
  });
});
