// @ts-nocheck
/**
 * SP-140-10d: tests for the inline tool details that replace the ContextPanel
 * dependency when a tool pill is clicked in the chat. Pins the contract:
 *   - Renders an accessible region labelled for the tool.
 *   - Escape collapses and returns focus to the controlling pill (a11y).
 *   - Focusing outside the block collapses it.
 *   - Focusing the controlling pill does NOT collapse (the pill's re-press
 *     toggle owns that path, so it must not double-collapse).
 */

import type { ToolExecution } from '@sprout/ui';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import { ToolDetailInline } from './ToolDetailInline';

const tool: ToolExecution = {
  id: 't1',
  tool: 'read_file',
  status: 'completed',
  startTime: new Date('2026-01-01T00:00:00Z'),
  endTime: new Date('2026-01-01T00:00:02Z'),
  arguments: '{"path":"src/foo.ts"}',
  result: 'line 1\nline 2',
};

let container: HTMLDivElement;
let root: Root;
let pill: HTMLButtonElement;

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
  // The pill that opened this detail (the element whose re-press / focus
  // toggles it). Identified to the component by its aria-controls attribute.
  pill = document.createElement('button');
  pill.setAttribute('aria-controls', 'tool-detail-t1');
  pill.textContent = 'read_file';
  document.body.appendChild(pill);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  pill?.remove();
});

describe('ToolDetailInline', () => {
  it('renders an accessible region labelled for the tool', () => {
    act(() => {
      root.render(createElement(ToolDetailInline, { tool, onToggle: vi.fn() }));
    });
    const region = container.querySelector('.tool-detail-inline');
    expect(region).not.toBeNull();
    expect(region!.getAttribute('role')).toBe('region');
    expect(region!.getAttribute('aria-label')).toContain('read_file');
    expect(container.querySelector('.tool-name')?.textContent).toBe('read_file');
  });

  it('collapses on Escape and returns focus to the controlling pill', () => {
    const onToggle = vi.fn();
    act(() => {
      root.render(createElement(ToolDetailInline, { tool, onToggle }));
    });
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onToggle).toHaveBeenCalledWith('t1');
    expect(document.activeElement).toBe(pill);
  });

  it('collapses when focus moves outside the block', () => {
    const onToggle = vi.fn();
    act(() => {
      root.render(createElement(ToolDetailInline, { tool, onToggle }));
    });
    const block = container.querySelector('.tool-detail-inline')!;
    const outside = document.createElement('div');
    document.body.appendChild(outside);
    act(() => {
      block.dispatchEvent(new FocusEvent('focusout', { relatedTarget: outside, bubbles: true }));
    });
    expect(onToggle).toHaveBeenCalledTimes(1);
    outside.remove();
  });

  it('does not collapse when focus moves to the controlling pill', () => {
    const onToggle = vi.fn();
    act(() => {
      root.render(createElement(ToolDetailInline, { tool, onToggle }));
    });
    const block = container.querySelector('.tool-detail-inline')!;
    act(() => {
      block.dispatchEvent(new FocusEvent('focusout', { relatedTarget: pill, bubbles: true }));
    });
    expect(onToggle).not.toHaveBeenCalled();
  });
});
