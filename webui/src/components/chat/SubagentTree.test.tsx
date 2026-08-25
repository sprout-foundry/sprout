// @ts-nocheck
/**
 * SubagentTree.test.tsx — Tests for the subagent tree component.
 *
 * Verifies:
 *   - Renders null for empty runs
 *   - Status icons (running, completed, error)
 *   - Persona name, depth badge, parallel badge
 *   - Duration, token, and cost metrics
 *   - Completion message display and truncation
 *   - Tree building: parent-child hierarchy, sorting, multiple roots
 *   - Expand/collapse behavior
 *   - aria-expanded toggling
 *   - data-depth attribute
 */

import { createElement, act } from 'react';

// Mock lucide-react icons — return SVGs with data-testid for selection in jsdom
vi.mock('lucide-react', () => {
  const { createElement: h } = require('react');
  const icons = ['ChevronRight', 'ChevronDown', 'Loader2', 'CheckCircle2', 'XCircle', 'Bot'];
  const result: Record<string, (props: any) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: any) => h('svg', { 'data-testid': name.toLowerCase().replace('2', ''), ...props });
  }
  return result;
});

// Mock @sprout/ui helpers
vi.mock('@sprout/ui', () => ({
  getPersonaColor: (persona: string) => '#6366f1',
  formatCost: (cost: number) => `$${cost.toFixed(4)}`,
  formatTokens: (tokens: number) => {
    if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}k`;
    return `${tokens}`;
  },
}));

import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { SubagentTree, buildTree, type TreeNode } from './SubagentTree';
import type { SubagentRun, SubagentActivity } from '@sprout/ui';

let container: HTMLDivElement;
let root: Root;

function makeActivity(overrides: Partial<SubagentActivity> = {}): SubagentActivity {
  return {
    id: overrides.id ?? 'act-1',
    toolCallId: overrides.toolCallId ?? 'run-1',
    toolName: overrides.toolName ?? 'run_subagent',
    phase: overrides.phase ?? 'spawn',
    message: overrides.message ?? 'started',
    timestamp: overrides.timestamp ?? new Date('2024-01-01T00:00:00Z'),
    persona: overrides.persona ?? undefined,
    isParallel: overrides.isParallel ?? undefined,
    taskId: overrides.taskId ?? undefined,
    provider: overrides.provider ?? undefined,
    model: overrides.model ?? undefined,
    taskCount: overrides.taskCount ?? undefined,
    failures: overrides.failures ?? undefined,
    tool: overrides.tool ?? undefined,
    status: overrides.status ?? undefined,
    reason: overrides.reason ?? undefined,
    tokensUsed: overrides.tokensUsed ?? undefined,
    cost: overrides.cost ?? undefined,
    elapsedMs: overrides.elapsedMs ?? undefined,
    depth: overrides.depth ?? undefined,
  };
}

function makeRun(overrides: Partial<SubagentRun> = {}): SubagentRun {
  const spawnActivity = overrides.spawnActivity ?? makeActivity({ phase: 'spawn' });
  return {
    toolCallId: overrides.toolCallId ?? 'run-1',
    persona: overrides.persona ?? 'coder',
    isParallel: overrides.isParallel ?? false,
    isComplete: overrides.isComplete ?? false,
    completionMessage: overrides.completionMessage ?? '',
    completionTimestamp: overrides.completionTimestamp ?? null,
    activities: overrides.activities ?? [spawnActivity],
    spawnActivity: spawnActivity,
    completeActivity: overrides.completeActivity ?? null,
    outputLines: overrides.outputLines ?? [],
    depth: overrides.depth ?? 0,
    tokensUsed: overrides.tokensUsed ?? 0,
    cost: overrides.cost ?? 0,
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

describe('SubagentTree', () => {
  describe('empty input', () => {
    it('renders null for empty runs', () => {
      act(() => {
        root.render(createElement(SubagentTree, { runs: [] }));
      });

      expect(container.innerHTML).toBe('');
      expect(container.querySelector('.subagent-tree')).toBeNull();
    });
  });

  describe('single node rendering', () => {
    it('renders a single root node with depth 0', () => {
      act(() => {
        root.render(createElement(SubagentTree, { runs: [makeRun()] }));
      });

      expect(container.querySelector('.subagent-tree')).not.toBeNull();
      expect(container.querySelector('.subagent-tree-node')).not.toBeNull();
      expect(container.querySelectorAll('.subagent-tree-node')).toHaveLength(1);
    });

    it('renders persona name', () => {
      act(() => {
        root.render(createElement(SubagentTree, { runs: [makeRun({ persona: 'tester' })] }));
      });

      const personaEl = container.querySelector('.subagent-tree-persona');
      expect(personaEl).not.toBeNull();
      expect(personaEl?.textContent).toBe('tester');
    });

    it('does NOT show depth badge when depth is 0', () => {
      act(() => {
        root.render(createElement(SubagentTree, { runs: [makeRun({ depth: 0 })] }));
      });

      expect(container.querySelector('.subagent-tree-depth-badge')).toBeNull();
    });

    it('shows depth badge when depth > 0', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ depth: 2 })],
          }),
        );
      });

      const badge = container.querySelector('.subagent-tree-depth-badge');
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toBe('D2');
    });

    it('shows parallel badge when isParallel is true', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isParallel: true })],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-parallel-badge')).not.toBeNull();
    });

    it('does NOT show parallel badge when isParallel is false', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isParallel: false })],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-parallel-badge')).toBeNull();
    });
  });

  describe('status icons', () => {
    it('shows spinner for running (isComplete=false)', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isComplete: false })],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-spinner')).not.toBeNull();
      expect(container.querySelector('.subagent-tree-status-ok')).toBeNull();
      expect(container.querySelector('.subagent-tree-status-error')).toBeNull();
    });

    it('shows check icon for completed (isComplete=true, no error in message)', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isComplete: true, completionMessage: 'Done' })],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-status-ok')).not.toBeNull();
      expect(container.querySelector('.subagent-tree-spinner')).toBeNull();
      expect(container.querySelector('.subagent-tree-status-error')).toBeNull();
    });

    it('shows error icon when completionMessage contains "fail"', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                completionMessage: 'Task failed with error',
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-status-error')).not.toBeNull();
      expect(container.querySelector('.subagent-tree-spinner')).toBeNull();
      expect(container.querySelector('.subagent-tree-status-ok')).toBeNull();
    });

    it('shows error icon when completionMessage contains "error"', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                completionMessage: 'An error occurred',
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-status-error')).not.toBeNull();
    });

    it('shows error icon for case-insensitive "FAIL"', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                completionMessage: 'ALL FAIL',
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-status-error')).not.toBeNull();
    });

    it('shows check icon when completionMessage contains neither fail nor error', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                completionMessage: 'Successfully completed all tasks',
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-status-ok')).not.toBeNull();
      expect(container.querySelector('.subagent-tree-status-error')).toBeNull();
    });
  });

  describe('duration display', () => {
    it('shows duration when spawnActivity timestamp is available', () => {
      const now = new Date('2024-01-01T00:01:00Z');
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                spawnActivity: makeActivity({
                  timestamp: new Date('2024-01-01T00:00:00Z'),
                }),
                completionTimestamp: now,
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-duration')).not.toBeNull();
      expect(container.querySelector('.subagent-tree-duration')?.textContent).toBe('1.0m');
    });

    it('shows duration in milliseconds for sub-second duration', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                spawnActivity: makeActivity({
                  timestamp: new Date('2024-01-01T00:00:00.000Z'),
                }),
                completionTimestamp: new Date('2024-01-01T00:00:00.500Z'),
                isComplete: true,
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-duration')?.textContent).toBe('500ms');
    });

    it('shows duration in seconds for durations between 1s and 60s', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                spawnActivity: makeActivity({
                  timestamp: new Date('2024-01-01T00:00:00Z'),
                }),
                completionTimestamp: new Date('2024-01-01T00:00:30Z'),
                isComplete: true,
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-duration')?.textContent).toBe('30.0s');
    });
  });

  describe('metrics display', () => {
    it('shows token metrics when tokensUsed > 0', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ tokensUsed: 1500 })],
          }),
        );
      });

      const metrics = container.querySelectorAll('.subagent-tree-metric');
      expect(metrics.length).toBe(1);
      expect(metrics[0]?.textContent).toContain('1.5k');
      expect(metrics[0]?.textContent).toContain('tok');
    });

    it('shows cost metrics when cost > 0', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ cost: 0.0123 })],
          }),
        );
      });

      const metrics = container.querySelectorAll('.subagent-tree-metric');
      expect(metrics.length).toBe(1);
      expect(metrics[0]?.textContent).toContain('$0.0123');
    });

    it('shows both token and cost metrics when both > 0', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ tokensUsed: 500, cost: 0.01 })],
          }),
        );
      });

      const metrics = container.querySelectorAll('.subagent-tree-metric');
      expect(metrics.length).toBe(2);
      expect(metrics[0]?.textContent).toContain('500');
      expect(metrics[0]?.textContent).toContain('tok');
      expect(metrics[1]?.textContent).toContain('$0.0100');
    });

    it('does NOT show metrics when tokens and cost are zero', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ tokensUsed: 0, cost: 0 })],
          }),
        );
      });

      expect(container.querySelectorAll('.subagent-tree-metric')).toHaveLength(0);
    });
  });

  describe('completion message', () => {
    it('shows completion message for completed runs', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                completionMessage: 'All tasks completed successfully',
              }),
            ],
          }),
        );
      });

      const msgEl = container.querySelector('.subagent-tree-completion-msg');
      expect(msgEl).not.toBeNull();
      expect(msgEl?.textContent).toBe('All tasks completed successfully');
    });

    it('truncates long completion messages at 80 chars', () => {
      const longMsg =
        'This is a very long completion message that exceeds eighty characters and should be truncated in the UI display';
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isComplete: true, completionMessage: longMsg })],
          }),
        );
      });

      const msgEl = container.querySelector('.subagent-tree-completion-msg');
      expect(msgEl).not.toBeNull();
      expect(msgEl?.textContent).toBe(`${longMsg.slice(0, 80)}…`);
    });

    it('does NOT show completion message for running runs', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: false,
                completionMessage: 'Should not appear',
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-completion-msg')).toBeNull();
    });

    it('does NOT show completion message when completionMessage is empty', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [
              makeRun({
                isComplete: true,
                completionMessage: '',
              }),
            ],
          }),
        );
      });

      expect(container.querySelector('.subagent-tree-completion-msg')).toBeNull();
    });
  });

  describe('tree hierarchy and children', () => {
    it('renders children when parent has child runs', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      expect(container.querySelectorAll('.subagent-tree-node')).toHaveLength(2);
      expect(container.querySelector('.subagent-tree-children')).not.toBeNull();
    });

    it('connector line appears for children', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      expect(container.querySelector('.subagent-tree-connector')).not.toBeNull();
    });

    it('root nodes are expanded by default', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      // Root node is expanded by default, so children should be visible
      expect(container.querySelector('.subagent-tree-children')).not.toBeNull();
    });

    it('children are collapsed by default (defaultExpanded=false for child nodes)', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });
      const grandchild = makeRun({
        toolCallId: 'grandchild',
        depth: 2,
        spawnActivity: makeActivity({ id: 'act-grandchild', timestamp: new Date('2024-01-01T00:00:02Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child, grandchild] }));
      });

      // Root is expanded, so child's container is visible
      const rootChildren = container.querySelectorAll('.subagent-tree-children');
      // Only one children container (root's) should be visible; grandchild's is collapsed
      // The child node itself is visible, but its children container should not be
      expect(rootChildren.length).toBe(1);
    });

    it('toggle expands/collapses children', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      // Initially expanded (root default)
      expect(container.querySelector('.subagent-tree-children')).not.toBeNull();

      // Click header to collapse
      act(() => {
        const header = container.querySelector('.subagent-tree-node-header');
        header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(container.querySelector('.subagent-tree-children')).toBeNull();

      // Click header again to expand
      act(() => {
        const header = container.querySelector('.subagent-tree-node-header');
        header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(container.querySelector('.subagent-tree-children')).not.toBeNull();
    });

    it('does not show chevron for leaf nodes (no children)', () => {
      act(() => {
        root.render(createElement(SubagentTree, { runs: [makeRun()] }));
      });

      expect(container.querySelector('.subagent-tree-chevron')).toBeNull();
      expect(container.querySelector('.subagent-tree-chevron-placeholder')).not.toBeNull();
    });

    it('shows chevron for nodes with children', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      expect(container.querySelector('.subagent-tree-chevron')).not.toBeNull();
    });

    it('switches from ChevronRight to ChevronDown when expanded', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      // Start collapsed by clicking first
      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      // Collapse first (it starts expanded as root)
      act(() => {
        const header = container.querySelector('.subagent-tree-node-header');
        header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(container.querySelector('[data-testid="chevronright"]')).not.toBeNull();

      // Expand
      act(() => {
        const header = container.querySelector('.subagent-tree-node-header');
        header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(container.querySelector('[data-testid="chevrondown"]')).not.toBeNull();
    });
  });

  describe('data-depth attribute', () => {
    it('sets data-depth attribute correctly on each node', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      const nodes = container.querySelectorAll('.subagent-tree-node');
      expect(nodes[0].getAttribute('data-depth')).toBe('0');
      expect(nodes[1].getAttribute('data-depth')).toBe('1');
    });

    it('sets data-depth for multiple levels', () => {
      const parent = makeRun({ toolCallId: 'p', depth: 0 });
      const child = makeRun({
        toolCallId: 'c',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-c', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });
      const grandchild = makeRun({
        toolCallId: 'gc',
        depth: 2,
        spawnActivity: makeActivity({ id: 'act-gc', timestamp: new Date('2024-01-01T00:00:02Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child, grandchild] }));
      });

      // All three nodes should be in the DOM (root expanded shows children, but grandchild is collapsed)
      const nodes = container.querySelectorAll('.subagent-tree-node');
      expect(nodes[0].getAttribute('data-depth')).toBe('0');
      expect(nodes[1].getAttribute('data-depth')).toBe('1');
    });
  });

  describe('data-running attribute', () => {
    it('sets data-running="true" for active (isComplete=false) runs', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isComplete: false })],
          }),
        );
      });

      const node = container.querySelector('.subagent-tree-node');
      expect(node?.getAttribute('data-running')).toBe('true');
    });

    it('sets data-running="false" for completed runs', () => {
      act(() => {
        root.render(
          createElement(SubagentTree, {
            runs: [makeRun({ isComplete: true, completionMessage: 'Done' })],
          }),
        );
      });

      const node = container.querySelector('.subagent-tree-node');
      expect(node?.getAttribute('data-running')).toBe('false');
    });
  });

  describe('aria-expanded', () => {
    it('aria-expanded toggles on click for nodes with children', () => {
      const parent = makeRun({ toolCallId: 'parent', depth: 0 });
      const child = makeRun({
        toolCallId: 'child',
        depth: 1,
        spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [parent, child] }));
      });

      const header = container.querySelector('.subagent-tree-node-header');
      // Root is expanded by default
      expect(header?.getAttribute('aria-expanded')).toBe('true');

      act(() => {
        header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      expect(header?.getAttribute('aria-expanded')).toBe('false');

      act(() => {
        header?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      expect(header?.getAttribute('aria-expanded')).toBe('true');
    });

    it('leaf nodes (no children) do NOT have aria-expanded attribute set', () => {
      act(() => {
        root.render(createElement(SubagentTree, { runs: [makeRun()] }));
      });

      const header = container.querySelector('.subagent-tree-node-header');
      // Leaf nodes have no children, so aria-expanded should not be present.
      // The onClick is undefined for leaf nodes, so the user cannot toggle it.
      expect(header?.getAttribute('aria-expanded')).toBeNull();
    });
  });

  describe('multiple root nodes', () => {
    it('renders multiple independent root nodes', () => {
      const run1 = makeRun({
        toolCallId: 'run-1',
        depth: 0,
        spawnActivity: makeActivity({ id: 'act-1', timestamp: new Date('2024-01-01T00:00:00Z') }),
      });
      const run2 = makeRun({
        toolCallId: 'run-2',
        depth: 0,
        spawnActivity: makeActivity({ id: 'act-2', timestamp: new Date('2024-01-01T00:00:01Z') }),
      });

      act(() => {
        root.render(createElement(SubagentTree, { runs: [run1, run2] }));
      });

      const nodes = container.querySelectorAll('.subagent-tree-node');
      expect(nodes).toHaveLength(2);
      expect(container.querySelector('.subagent-tree-children')).toBeNull();
    });
  });
});

describe('buildTree', () => {
  it('returns empty array for empty input', () => {
    const result = buildTree([]);
    expect(result).toEqual([]);
  });

  it('returns single node for single run', () => {
    const run = makeRun();
    const result = buildTree([run]);

    expect(result).toHaveLength(1);
    expect(result[0].run).toBe(run);
    expect(result[0].children).toEqual([]);
  });

  it('creates correct parent-child hierarchy', () => {
    const parent = makeRun({ toolCallId: 'parent', depth: 0 });
    const child = makeRun({
      toolCallId: 'child',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-child', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });

    const result = buildTree([parent, child]);

    expect(result).toHaveLength(1);
    expect(result[0].run).toBe(parent);
    expect(result[0].children).toHaveLength(1);
    expect(result[0].children[0].run).toBe(child);
  });

  it('handles multiple root nodes', () => {
    const root1 = makeRun({ toolCallId: 'r1', depth: 0 });
    const root2 = makeRun({
      toolCallId: 'r2',
      depth: 0,
      spawnActivity: makeActivity({ id: 'act-r2', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });

    const result = buildTree([root1, root2]);

    expect(result).toHaveLength(2);
    expect(result[0].run.toolCallId).toBe('r1');
    expect(result[1].run.toolCallId).toBe('r2');
  });

  it('handles deeply nested hierarchy', () => {
    const grandparent = makeRun({ toolCallId: 'gp', depth: 0 });
    const parent = makeRun({
      toolCallId: 'p',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-p', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });
    const child = makeRun({
      toolCallId: 'c',
      depth: 2,
      spawnActivity: makeActivity({ id: 'act-c', timestamp: new Date('2024-01-01T00:00:02Z') }),
    });

    const result = buildTree([grandparent, parent, child]);

    expect(result).toHaveLength(1);
    expect(result[0].run.toolCallId).toBe('gp');
    expect(result[0].children).toHaveLength(1);
    expect(result[0].children[0].run.toolCallId).toBe('p');
    expect(result[0].children[0].children).toHaveLength(1);
    expect(result[0].children[0].children[0].run.toolCallId).toBe('c');
  });

  it('handles sibling nodes at same depth', () => {
    const parent = makeRun({ toolCallId: 'parent', depth: 0 });
    const child1 = makeRun({
      toolCallId: 'child1',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-c1', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });
    const child2 = makeRun({
      toolCallId: 'child2',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-c2', timestamp: new Date('2024-01-01T00:00:02Z') }),
    });

    const result = buildTree([parent, child1, child2]);

    expect(result).toHaveLength(1);
    expect(result[0].run.toolCallId).toBe('parent');
    expect(result[0].children).toHaveLength(2);
    expect(result[0].children[0].run.toolCallId).toBe('child1');
    expect(result[0].children[1].run.toolCallId).toBe('child2');
  });

  it('sorts runs by spawn timestamp', () => {
    const later = makeRun({
      toolCallId: 'later',
      depth: 0,
      spawnActivity: makeActivity({ id: 'act-later', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });
    const earlier = makeRun({
      toolCallId: 'earlier',
      depth: 0,
      spawnActivity: makeActivity({ id: 'act-earlier', timestamp: new Date('2024-01-01T00:00:00Z') }),
    });

    // Pass in reverse order
    const result = buildTree([later, earlier]);

    expect(result[0].run.toolCallId).toBe('earlier');
    expect(result[1].run.toolCallId).toBe('later');
  });

  it('falls back to activities[0].timestamp when spawnActivity is null', () => {
    const run = makeRun({
      toolCallId: 'fallback',
      depth: 0,
      spawnActivity: null,
      activities: [makeActivity({ id: 'act-fallback', timestamp: new Date('2024-01-01T00:00:05Z') })],
    });

    const result = buildTree([run]);

    expect(result).toHaveLength(1);
    expect(result[0].run.toolCallId).toBe('fallback');
  });

  it('handles runs with null depth', () => {
    const run = { ...makeRun({ toolCallId: 'no-depth' }), depth: undefined as any };
    const result = buildTree([run]);

    expect(result).toHaveLength(1);
  });

  it('handles sibling subtree with children', () => {
    const parent = makeRun({ toolCallId: 'parent', depth: 0 });
    const child1 = makeRun({
      toolCallId: 'child1',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-c1', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });
    const grandchild1 = makeRun({
      toolCallId: 'gc1',
      depth: 2,
      spawnActivity: makeActivity({ id: 'act-gc1', timestamp: new Date('2024-01-01T00:00:02Z') }),
    });
    const child2 = makeRun({
      toolCallId: 'child2',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-c2', timestamp: new Date('2024-01-01T00:00:03Z') }),
    });

    const result = buildTree([parent, child1, grandchild1, child2]);

    expect(result).toHaveLength(1);
    expect(result[0].run.toolCallId).toBe('parent');
    expect(result[0].children).toHaveLength(2);
    expect(result[0].children[0].run.toolCallId).toBe('child1');
    expect(result[0].children[0].children).toHaveLength(1);
    expect(result[0].children[0].children[0].run.toolCallId).toBe('gc1');
    expect(result[0].children[1].run.toolCallId).toBe('child2');
    expect(result[0].children[1].children).toEqual([]);
  });

  it('TreeNode is a proper type with run and children', () => {
    const run = makeRun();
    const node: TreeNode = { run, children: [] };

    expect(node.run).toBe(run);
    expect(node.children).toEqual([]);
  });
});

describe('edge cases', () => {
  it('renders multiple nodes with different statuses', () => {
    const running = makeRun({
      toolCallId: 'running',
      isComplete: false,
    });
    const completed = makeRun({
      toolCallId: 'completed',
      isComplete: true,
      completionMessage: 'Done',
      spawnActivity: makeActivity({ id: 'act-c', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });

    act(() => {
      root.render(createElement(SubagentTree, { runs: [running, completed] }));
    });

    const nodes = container.querySelectorAll('.subagent-tree-node');
    expect(nodes).toHaveLength(2);
  });

  it('renders with both spawnActivity and activities fallback', () => {
    const run = makeRun({
      toolCallId: 'fallback-test',
      spawnActivity: null,
      activities: [makeActivity({ id: 'act-first', timestamp: new Date('2024-01-01T00:00:01Z') })],
    });

    act(() => {
      root.render(createElement(SubagentTree, { runs: [run] }));
    });

    expect(container.querySelector('.subagent-tree-node')).not.toBeNull();
  });

  it('renders parallel runs', () => {
    const run1 = makeRun({
      toolCallId: 'p1',
      isParallel: true,
      depth: 0,
    });
    const run2 = makeRun({
      toolCallId: 'p2',
      isParallel: true,
      depth: 0,
      spawnActivity: makeActivity({ id: 'act-p2', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });

    act(() => {
      root.render(createElement(SubagentTree, { runs: [run1, run2] }));
    });

    const badges = container.querySelectorAll('.subagent-tree-parallel-badge');
    expect(badges).toHaveLength(2);
  });

  it('depth 0 has no depth badge but depth 1+ does', () => {
    const d0 = makeRun({ toolCallId: 'd0', depth: 0 });
    const d1 = makeRun({
      toolCallId: 'd1',
      depth: 1,
      spawnActivity: makeActivity({ id: 'act-d1', timestamp: new Date('2024-01-01T00:00:01Z') }),
    });

    act(() => {
      root.render(createElement(SubagentTree, { runs: [d0, d1] }));
    });

    const badges = container.querySelectorAll('.subagent-tree-depth-badge');
    expect(badges).toHaveLength(1);
    expect(badges[0]?.textContent).toBe('D1');
  });
});
