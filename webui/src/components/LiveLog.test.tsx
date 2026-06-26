// SP-009-migration: LiveLog was migrated from local webui/src/components/
// to @sprout/ui. This test verifies the import from @sprout/ui works correctly
// and the component renders without crashing.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { LiveLog } from '@sprout/ui';
import type { LiveLogLine } from '@sprout/ui';

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
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeLine(id: string, text: string, overrides: Partial<LiveLogLine> = {}): LiveLogLine {
  return { id, text, timestamp: new Date('2024-01-01'), taskId: undefined, ...overrides };
}

// ---------------------------------------------------------------------------
// Migration Verification: LiveLog imported from @sprout/ui
// ---------------------------------------------------------------------------

describe('LiveLog (@sprout/ui import)', () => {
  it('imports successfully from @sprout/ui', () => {
    expect(LiveLog).toBeDefined();
    expect(typeof LiveLog).toBe('function');
  });

  it('renders without crashing with valid lines', () => {
    const lines = [makeLine('1', 'First log line'), makeLine('2', 'Second log line')];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
        }),
      );
    });

    expect(container.querySelector('.subagent-feed-log')).not.toBeNull();
  });

  it('returns null when lines array is empty', () => {
    act(() => {
      root.render(
        createElement(LiveLog, {
          lines: [],
          maxLines: 10,
        }),
      );
    });

    expect(container.querySelector('.subagent-feed-log')).toBeNull();
  });

  it('renders each line in a log-line container', () => {
    const lines = [makeLine('1', 'Hello'), makeLine('2', 'World')];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
        }),
      );
    });

    const lineEls = container.querySelectorAll('.subagent-feed-log-line');
    expect(lineEls.length).toBe(2);
  });

  it('renders line text correctly', () => {
    const lines = [makeLine('1', 'Log message here')];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
        }),
      );
    });

    const textEl = container.querySelector('.subagent-feed-log-text');
    expect(textEl?.textContent).toBe('Log message here');
  });

  it('renders taskId badge when present', () => {
    const lines = [makeLine('1', 'Task output', { taskId: 'task-1' })];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
        }),
      );
    });

    const taskEl = container.querySelector('.subagent-feed-log-task');
    expect(taskEl).not.toBeNull();
    expect(taskEl?.textContent).toBe('task-1');
  });

  it('does not render taskId badge when taskId is undefined', () => {
    const lines = [makeLine('1', 'No task')];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
        }),
      );
    });

    expect(container.querySelector('.subagent-feed-log-task')).toBeNull();
  });

  it('respects maxLines by only showing the most recent lines', () => {
    const lines = [
      makeLine('1', 'Line 1'),
      makeLine('2', 'Line 2'),
      makeLine('3', 'Line 3'),
      makeLine('4', 'Line 4'),
      makeLine('5', 'Line 5'),
    ];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 3,
        }),
      );
    });

    const lineEls = container.querySelectorAll('.subagent-feed-log-line');
    expect(lineEls.length).toBe(3);
    // Should show lines 3, 4, 5 (the last 3)
    const textEls = container.querySelectorAll('.subagent-feed-log-text');
    expect(textEls[0]?.textContent).toBe('Line 3');
    expect(textEls[1]?.textContent).toBe('Line 4');
    expect(textEls[2]?.textContent).toBe('Line 5');
  });

  it('applies custom className alongside default classes', () => {
    const lines = [makeLine('1', 'Custom class test')];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
          className: 'my-custom-class',
        }),
      );
    });

    const logEl = container.querySelector('.subagent-feed-log');
    expect(logEl?.className).toContain('my-custom-class');
    expect(logEl?.className).toContain('subagent-feed-log');
  });

  it('renders lines with null className', () => {
    const lines = [makeLine('1', 'No custom class')];

    act(() => {
      root.render(
        createElement(LiveLog, {
          lines,
          maxLines: 10,
        }),
      );
    });

    const logEl = container.querySelector('.subagent-feed-log');
    expect(logEl).not.toBeNull();
  });
});
