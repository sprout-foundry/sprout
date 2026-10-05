/**
 * ProgressStrip tests — SP-151 §151c (item 151.7).
 *
 * Pattern (mirrors EventsContext.test.tsx and useCommandOutput.test.ts):
 * render the strip inside an EventsContextProvider whose provider mock
 * exposes onEvent: vi.fn(), capture the registered callback, and invoke
 * it with synthetic events.
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

function renderStrip() {
  act(() => {
    root!.render(createElement(EventsContextProvider, { provider }, createElement(ProgressStrip)));
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
});
