import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, beforeAll, afterAll, vi } from 'vitest';

// ── Mocks (before imports) ──────────────────────────────────────────

vi.mock('lucide-react', () => {
  const icons = [
    'Wrench',
    'Terminal',
    'BookOpen',
    'Pencil',
    'Search',
    'Eye',
    'FlaskConical',
    'Globe',
    'ArrowDown',
    'ClipboardList',
    'ScrollText',
    'RotateCcw',
    'Bot',
    'Rocket',
    'Zap',
    'CheckCircle2',
    'XCircle',
    'Hourglass',
    'ChevronDown',
    'ChevronRight',
  ];
  const result: Record<string, (props: any) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: any) => <svg data-testid={name.toLowerCase()} {...props} />;
  }
  return result;
});

vi.mock('@sprout/ui', () => ({
  // Prevent the old React bundle inside @sprout/ui from being loaded
  ToolExecution: {},
}));

vi.mock('../../utils/log', () => ({
  debugLog: vi.fn(),
}));

// ── Imports ──────────────────────────────────────────────────────────

import { ToolCard } from './ToolCard';
import type { ToolExecution } from './types';

// ── Helpers ──────────────────────────────────────────────────────────

const createTool = (overrides: Partial<ToolExecution> = {}): ToolExecution => ({
  id: 'tool-123',
  tool: 'read_file',
  status: 'completed',
  startTime: new Date('2024-01-01T00:00:00Z'),
  endTime: new Date('2024-01-01T00:00:01Z'),
  ...overrides,
});

const createProps = (toolOverrides: Partial<ToolExecution> = {}) => ({
  tool: createTool(toolOverrides),
  expandedTools: new Set<string>(),
  activeToolId: null as string | null,
  toolRef: { current: {} } as React.MutableRefObject<Record<string, HTMLDivElement | null>>,
  onToggleExpansion: vi.fn(),
});

// ── Setup / Teardown ─────────────────────────────────────────────────

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot> | null;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  root = null;
});

function renderToolCard(toolOverrides: Partial<ToolExecution> = {}) {
  root = createRoot(container);
  act(() => {
    root.render(createElement(ToolCard, createProps(toolOverrides)));
  });
}

// ── Tests ────────────────────────────────────────────────────────────

describe('ToolCard depth badge', () => {
  describe('no depth', () => {
    it('does not render a depth badge when depth is undefined', () => {
      renderToolCard({ depth: undefined });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeNull();
    });

    it('does not render a depth badge when depth is 0', () => {
      renderToolCard({ depth: 0 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeNull();
    });

    it('does not apply extra paddingLeft on the tool-summary when depth is 0', () => {
      renderToolCard({ depth: 0 });
      const summary = container.querySelector('.tool-summary');
      expect(summary).toBeTruthy();
      expect(summary?.style.paddingLeft).toBe('');
    });

    it('does not apply extra paddingLeft when depth is undefined', () => {
      renderToolCard({ depth: undefined });
      const summary = container.querySelector('.tool-summary');
      expect(summary).toBeTruthy();
      expect(summary?.style.paddingLeft).toBe('');
    });
  });

  describe('depth 1', () => {
    it('renders a "D1" badge', () => {
      renderToolCard({ depth: 1 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent?.trim()).toBe('D1');
    });

    it('tags the depth-1 badge with the orchestrator tier', () => {
      // ToolCard switched from inline backgroundColor to a data-depth-tier
      // attribute that CSS resolves via design tokens (--accent-primary
      // for orchestrator, --accent-warning for deep). Asserting on the
      // attribute decouples the test from token values that vary by theme.
      renderToolCard({ depth: 1 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.getAttribute('data-depth-tier')).toBe('orchestrator');
    });

    it('sets data-depth=1 on the execution row to drive CSS indentation', () => {
      renderToolCard({ depth: 1 });
      const execution = container.querySelector('.tool-execution');
      expect(execution).toBeTruthy();
      expect(execution?.getAttribute('data-depth')).toBe('1');
    });
  });

  describe('depth 2', () => {
    it('renders a "D2" badge', () => {
      renderToolCard({ depth: 2 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent?.trim()).toBe('D2');
    });

    it('tags the depth-2 badge with the deep tier', () => {
      renderToolCard({ depth: 2 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.getAttribute('data-depth-tier')).toBe('deep');
    });

    it('sets data-depth=2 on the execution row to drive CSS indentation', () => {
      renderToolCard({ depth: 2 });
      const execution = container.querySelector('.tool-execution');
      expect(execution).toBeTruthy();
      expect(execution?.getAttribute('data-depth')).toBe('2');
    });
  });

  describe('depth 3', () => {
    it('renders a "D3" badge', () => {
      renderToolCard({ depth: 3 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent?.trim()).toBe('D3');
    });

    it('tags the depth-3 badge with the deep tier', () => {
      renderToolCard({ depth: 3 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.getAttribute('data-depth-tier')).toBe('deep');
    });

    it('sets data-depth=3 on the execution row to drive CSS indentation', () => {
      renderToolCard({ depth: 3 });
      const execution = container.querySelector('.tool-execution');
      expect(execution).toBeTruthy();
      expect(execution?.getAttribute('data-depth')).toBe('3');
    });
  });

  describe('depth 4+', () => {
    it('renders a "D4" badge', () => {
      renderToolCard({ depth: 4 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent?.trim()).toBe('D4');
    });

    it('renders a "D10" badge', () => {
      renderToolCard({ depth: 10 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent?.trim()).toBe('D10');
    });

    it('tags depth 4+ badges with the deep tier', () => {
      renderToolCard({ depth: 5 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.getAttribute('data-depth-tier')).toBe('deep');
    });

    it('sets data-depth that scales with depth to drive CSS indentation', () => {
      // depth 5 → data-depth=5; the CSS indents via .tool-execution[data-depth]
      renderToolCard({ depth: 5 });
      const execution = container.querySelector('.tool-execution');
      expect(execution).toBeTruthy();
      expect(execution?.getAttribute('data-depth')).toBe('5');
    });
  });

  describe('indentation formula', () => {
    it.each([
      [1, '1'],
      [2, '2'],
      [3, '3'],
      [4, '4'],
      [5, '5'],
    ])('data-depth = depth → %s', (depth, expectedDepth) => {
      renderToolCard({ depth, id: `tool-${depth}` });
      const execution = container.querySelector('.tool-execution');
      expect(execution).toBeTruthy();
      expect(execution?.getAttribute('data-depth')).toBe(expectedDepth);
    });
  });

  describe('badge text format', () => {
    it.each([1, 2, 3, 5, 10])('badge text is "D%d" for depth=%d', (depth) => {
      renderToolCard({ depth, id: `tool-${depth}` });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent?.trim()).toBe(`D${depth}`);
    });
  });

  describe('depth tier progression', () => {
    // Depth 1 = orchestrator-level subagent (driven by --accent-primary
    // in CSS). Depth >= 2 = deep / specialist (--accent-warning).
    it('uses the orchestrator tier for depth 1', () => {
      renderToolCard({ depth: 1 });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.getAttribute('data-depth-tier')).toBe('orchestrator');
    });

    it.each([2, 3, 4, 5])('uses the deep tier for depth %d', (depth) => {
      renderToolCard({ depth, id: `tool-${depth}` });
      const badge = container.querySelector('.tool-depth-badge');
      expect(badge).toBeTruthy();
      expect(badge?.getAttribute('data-depth-tier')).toBe('deep');
    });
  });
});
