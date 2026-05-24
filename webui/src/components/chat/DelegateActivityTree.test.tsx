// @ts-nocheck
/**
 * DelegateActivityTree.test.tsx — Tests for the delegate activity tree component.
 *
 * Verifies:
 *   - Status icons appear correctly (running, completed, error)
 *   - Collapsed by default, expands on header click
 *   - Tool calls render when expanded
 *   - "Waiting for tool calls..." for running with no tools
 *   - Token and cost metrics in header
 *   - Depth display in delegate label
 *   - Summary vs action fallback
 */

import { createElement } from 'react';

// Mock lucide-react icons — return SVGs with data-testid for selection in jsdom
vi.mock('lucide-react', () => {
  const { createElement: h } = require('react');
  const icons = ['ChevronRight', 'ChevronDown', 'Loader2', 'CheckCircle2', 'XCircle', 'Wrench'];
  const result: Record<string, (props: any) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: any) =>
      h('svg', { 'data-testid': name.toLowerCase().replace('2', ''), ...props });
  }
  return result;
});

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { DelegateActivityTree } from './DelegateActivityTree';
import type { DelegateActivity, DelegateToolCallInfo } from '@sprout/ui';

let container: HTMLDivElement;
let root: Root;

function makeActivity(overrides: Partial<DelegateActivity> = {}): DelegateActivity {
  return {
    delegateId: overrides.delegateId ?? 'delegate-1',
    action: overrides.action ?? 'started',
    summary: overrides.summary ?? 'Test activity',
    depth: overrides.depth ?? 0,
    tokensUsed: overrides.tokensUsed ?? 0,
    cost: overrides.cost ?? 0,
    toolsCalled: overrides.toolsCalled ?? [],
    status: overrides.status ?? 'running',
  };
}

function makeToolCall(overrides: Partial<DelegateToolCallInfo> = {}): DelegateToolCallInfo {
  return {
    tool_name: overrides.tool_name ?? 'grep',
    input: overrides.input ?? 'search for foo',
    output: overrides.output ?? 'found 3 results',
    timestamp: overrides.timestamp ?? '2024-01-01T00:00:00Z',
    duration_ms: overrides.duration_ms ?? 45,
    success: overrides.success ?? true,
  };
}

beforeAll(() => {
  // @ts-expect-error
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

describe('DelegateActivityTree', () => {
  it('renders delegate header with correct status icon for running', () => {
    act(() => {
      root.render(createElement(DelegateActivityTree, { activity: makeActivity({ status: 'running' }) }));
    });

    expect(container.querySelector('.delegate-activity-header')).not.toBeNull();
    expect(container.querySelector('.delegate-spinner')).not.toBeNull();
    expect(container.querySelector('.delegate-status-completed')).toBeNull();
    expect(container.querySelector('.delegate-status-error')).toBeNull();
  });

  it('renders delegate header with correct status icon for completed', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ status: 'completed' }),
        }),
      );
    });

    expect(container.querySelector('.delegate-activity-header')).not.toBeNull();
    expect(container.querySelector('.delegate-spinner')).toBeNull();
    expect(container.querySelector('.delegate-status-completed')).not.toBeNull();
    expect(container.querySelector('.delegate-status-error')).toBeNull();
  });

  it('renders delegate header with correct status icon for error', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ status: 'error' }),
        }),
      );
    });

    expect(container.querySelector('.delegate-activity-header')).not.toBeNull();
    expect(container.querySelector('.delegate-spinner')).toBeNull();
    expect(container.querySelector('.delegate-status-completed')).toBeNull();
    expect(container.querySelector('.delegate-status-error')).not.toBeNull();
  });

  it('is collapsed by default', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ toolsCalled: [makeToolCall()] }),
        }),
      );
    });

    expect(container.querySelector('.delegate-activity-body')).toBeNull();
    expect(container.querySelector('.delegate-tools-list')).toBeNull();
  });

  it('expands on header click', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ toolsCalled: [makeToolCall()] }),
        }),
      );
    });

    // Initially collapsed
    expect(container.querySelector('.delegate-activity-body')).toBeNull();

    // Click header to expand
    act(() => {
      const header = container.querySelector('.delegate-activity-header');
      header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-activity-body')).not.toBeNull();
  });

  it('collapses on second header click', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ toolsCalled: [makeToolCall()] }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('.delegate-activity-body')).not.toBeNull();

    // Collapse
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('.delegate-activity-body')).toBeNull();
  });

  it('shows tool calls when expanded', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [
              makeToolCall({ tool_name: 'grep' }),
              makeToolCall({ tool_name: 'cat' }),
            ],
          }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const toolCalls = container.querySelectorAll('.delegate-tool-call');
    expect(toolCalls.length).toBe(2);
    expect(container.querySelectorAll('.delegate-tool-name')[0]?.textContent).toBe('grep');
    expect(container.querySelectorAll('.delegate-tool-name')[1]?.textContent).toBe('cat');
  });

  it('shows Waiting for tool calls when running with no tools', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ status: 'running', toolsCalled: [] }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const emptyMsg = container.querySelector('.delegate-empty');
    expect(emptyMsg).not.toBeNull();
    expect(emptyMsg?.textContent).toBe('Waiting for tool calls...');
  });

  it('does NOT show waiting message for completed with no tools', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ status: 'completed', toolsCalled: [] }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-empty')).toBeNull();
  });

  it('does NOT show waiting message for error with no tools', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ status: 'error', toolsCalled: [] }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-empty')).toBeNull();
  });

  it('displays token metrics in header when tokensUsed > 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ tokensUsed: 1500 }),
        }),
      );
    });

    const metrics = container.querySelectorAll('.delegate-metric');
    expect(metrics.length).toBe(1);
    expect(metrics[0]?.textContent).toContain('1.5k');
    expect(metrics[0]?.textContent).toContain('tok');
  });

  it('displays cost metrics in header when cost > 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ cost: 0.0123 }),
        }),
      );
    });

    const metrics = container.querySelectorAll('.delegate-metric');
    expect(metrics.length).toBe(1);
    expect(metrics[0]?.textContent).toContain('$0.0123');
  });

  it('displays both token and cost metrics when both > 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ tokensUsed: 500, cost: 0.01 }),
        }),
      );
    });

    const metrics = container.querySelectorAll('.delegate-metric');
    expect(metrics.length).toBe(2);
    expect(metrics[0]?.textContent).toContain('500');
    expect(metrics[1]?.textContent).toContain('$0.0100');
  });

  it('does NOT display metrics when tokens and cost are zero', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ tokensUsed: 0, cost: 0 }),
        }),
      );
    });

    const metrics = container.querySelectorAll('.delegate-metric');
    expect(metrics.length).toBe(0);
  });

  it('displays depth in delegate label when depth > 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ depth: 2 }),
        }),
      );
    });

    const idEl = container.querySelector('.delegate-id');
    expect(idEl).not.toBeNull();
    expect(idEl?.textContent).toContain('depth 2');
  });

  it('displays Delegate without depth when depth is 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ depth: 0 }),
        }),
      );
    });

    const idEl = container.querySelector('.delegate-id');
    expect(idEl?.textContent).toBe('Delegate');
  });

  it('shows summary when available', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ summary: 'My summary text' }),
        }),
      );
    });

    expect(container.querySelector('.delegate-summary-text')?.textContent).toBe('My summary text');
  });

  it('shows action as fallback when summary is not provided', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ summary: '', action: 'tool_call' }),
        }),
      );
    });

    expect(container.querySelector('.delegate-summary-text')?.textContent).toBe('tool_call');
  });

  it('shows tool call success indicator for successful tools', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ success: true })],
          }),
        }),
      );
    });

    // Expand to show tool calls
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-tool-status.success')).not.toBeNull();
  });

  it('shows tool call error indicator for failed tools', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ success: false })],
          }),
        }),
      );
    });

    // Expand to show tool calls
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-tool-status.error')).not.toBeNull();
  });

  it('shows duration for tool call when duration_ms > 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ duration_ms: 123 })],
          }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-tool-duration')?.textContent).toBe('123ms');
  });

  it('does NOT show duration for tool call when duration_ms is 0', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ duration_ms: 0 })],
          }),
        }),
      );
    });

    // Expand
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.delegate-tool-duration')).toBeNull();
  });

  it('expands tool call to show input and output on click', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ tool_name: 'grep', input: 'search query', output: '3 matches' })],
          }),
        }),
      );
    });

    // Expand the main tree
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Tool call body should be hidden initially
    expect(container.querySelector('.delegate-tool-call-body')).toBeNull();

    // Click the tool call header
    act(() => {
      const toolHeader = container.querySelector('.delegate-tool-call-header');
      toolHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Now the body should be visible with input and output
    expect(container.querySelector('.delegate-tool-call-body')).not.toBeNull();
    expect(container.querySelectorAll('.delegate-tool-label')).toHaveLength(2);
    expect(container.querySelectorAll('.delegate-tool-label')[0]?.textContent).toBe('Input:');
    expect(container.querySelectorAll('.delegate-tool-label')[1]?.textContent).toBe('Output:');
    expect(container.querySelectorAll('.delegate-tool-pre')[0]?.textContent).toBe('search query');
    expect(container.querySelectorAll('.delegate-tool-pre')[1]?.textContent).toBe('3 matches');
  });

  it('omits input section when input is empty', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ input: '', output: 'some output' })],
          }),
        }),
      );
    });

    // Expand tree then tool
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('.delegate-tool-call-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Only Output section should be present
    expect(container.querySelectorAll('.delegate-tool-section')).toHaveLength(1);
    expect(container.querySelector('.delegate-tool-label')?.textContent).toBe('Output:');
  });

  it('omits output section when output is empty', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({
            toolsCalled: [makeToolCall({ input: 'some input', output: '' })],
          }),
        }),
      );
    });

    // Expand tree then tool
    act(() => {
      container.querySelector('.delegate-activity-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('.delegate-tool-call-header')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Only Input section should be present
    expect(container.querySelectorAll('.delegate-tool-section')).toHaveLength(1);
    expect(container.querySelector('.delegate-tool-label')?.textContent).toBe('Input:');
  });

  it('applies depth-based color styling', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ depth: 1 }),
        }),
      );
    });

    const tree = container.querySelector('.delegate-activity-tree');
    expect(tree).not.toBeNull();
    expect(tree?.className).toContain('depth-1');
  });

  it('caps depth class at 3 for very deep nesting', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity({ depth: 10 }),
        }),
      );
    });

    const tree = container.querySelector('.delegate-activity-tree');
    expect(tree?.className).toContain('depth-3');
  });

  it('toggles aria-expanded on header button', () => {
    act(() => {
      root.render(
        createElement(DelegateActivityTree, {
          activity: makeActivity(),
        }),
      );
    });

    const header = container.querySelector('.delegate-activity-header');
    expect(header?.getAttribute('aria-expanded')).toBe('false');

    act(() => {
      header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(header?.getAttribute('aria-expanded')).toBe('true');

    act(() => {
      header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(header?.getAttribute('aria-expanded')).toBe('false');
  });
});
