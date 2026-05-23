// @ts-nocheck
/**
 * SP-053-2c: tests for the live tool timeline bar that renders above
 * the chat input. Pins the rules from SP-053:
 *   - Empty input → renders nothing (caller doesn't have to gate).
 *   - Running tool → spinner + tool name visible.
 *   - Completed tool → green check + final duration visible.
 *   - Error tool → red X + sticks past the 3s fade window.
 *   - Mix → most-recent N visible (older tools clipped).
 *   - Persona badge appears in the expected color when persona is set.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import { ToolTimelineBar } from './ToolTimelineBar';
import type { ToolExecution } from '@sprout/ui';

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
  container?.remove();
  vi.useRealTimers();
});

function tool(overrides: Partial<ToolExecution> = {}): ToolExecution {
  return {
    id: overrides.id ?? 'tool-1',
    tool: 'read_file',
    status: 'running',
    startTime: new Date(Date.now() - 500),
    ...overrides,
  } as ToolExecution;
}

describe('ToolTimelineBar', () => {
  it('renders nothing when there are no tool executions', () => {
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [] }));
    });
    expect(container.querySelector('.tool-timeline-bar')).toBeNull();
  });

  it('shows a spinner card for a running tool', () => {
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [tool({ status: 'running' })] }));
    });
    const card = container.querySelector('.tool-timeline-card--running');
    expect(card).not.toBeNull();
    expect(container.querySelector('.tool-timeline-spinner')).not.toBeNull();
    expect(container.querySelector('.tool-timeline-name')?.textContent).toBe('read_file');
  });

  it('shows a green check card for a recently-completed tool', () => {
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [
        tool({ status: 'completed', startTime: new Date(Date.now() - 200), endTime: new Date() }),
      ]}));
    });
    expect(container.querySelector('.tool-timeline-card--completed')).not.toBeNull();
    expect(container.querySelector('.tool-timeline-icon-ok')).not.toBeNull();
  });

  it('shows a red X card for an error tool', () => {
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [
        tool({ status: 'error', startTime: new Date(Date.now() - 200), endTime: new Date() }),
      ]}));
    });
    expect(container.querySelector('.tool-timeline-card--error')).not.toBeNull();
    expect(container.querySelector('.tool-timeline-icon-error')).not.toBeNull();
  });

  it('completed tool fades out after FADE_MS', () => {
    const completed = tool({ status: 'completed', startTime: new Date(Date.now() - 200), endTime: new Date() });
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [completed] }));
    });
    // Initially visible
    expect(container.querySelector('.tool-timeline-card--completed')).not.toBeNull();
    // Advance past the 3s fade window
    act(() => {
      vi.advanceTimersByTime(3500);
    });
    expect(container.querySelector('.tool-timeline-card--completed')).toBeNull();
    expect(container.querySelector('.tool-timeline-bar')).toBeNull();
  });

  it('error tool sticks past the fade window', () => {
    const errored = tool({ status: 'error', startTime: new Date(Date.now() - 200), endTime: new Date() });
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [errored] }));
    });
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(container.querySelector('.tool-timeline-card--error')).not.toBeNull();
  });

  it('renders persona badge with the persona color when persona is set', () => {
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: [
        tool({ persona: 'coder', status: 'running' }),
      ]}));
    });
    const badge = container.querySelector('.tool-timeline-persona') as HTMLElement | null;
    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe('[coder]');
    // getPersonaColor('coder') → #58a6ff → rgb(88, 166, 255) in DOM
    expect(badge?.style.color.replace(/\s/g, '').toLowerCase()).toBe('rgb(88,166,255)');
  });

  it('caps the visible cards at maxVisible (most recent wins)', () => {
    const tools = Array.from({ length: 8 }, (_, i) =>
      tool({ id: `t${i}`, tool: `tool_${i}`, status: 'running' }),
    );
    act(() => {
      root.render(createElement(ToolTimelineBar, { toolExecutions: tools, maxVisible: 3 }));
    });
    const cards = container.querySelectorAll('.tool-timeline-card');
    expect(cards.length).toBe(3);
    // Most-recent N is the tail of the input array
    expect(cards[0].getAttribute('data-tool-name')).toBe('tool_5');
    expect(cards[2].getAttribute('data-tool-name')).toBe('tool_7');
  });
});
