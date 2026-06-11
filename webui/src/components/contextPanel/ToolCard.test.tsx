import { describe, it, expect, beforeEach, afterEach, beforeAll, afterAll, vi } from 'vitest';
import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';

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

    it('applies paddingLeft of 0px on the tool-summary', () => {
      renderToolCard({ depth: 1 });
      const summary = container.querySelector('.tool-summary');
      expect(summary).toBeTruthy();
      expect(summary?.style.paddingLeft).toBe('0px');
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

    it('applies paddingLeft of 16px on the tool-summary', () => {
      renderToolCard({ depth: 2 });
      const summary = container.querySelector('.tool-summary');
      expect(summary).toBeTruthy();
      expect(summary?.style.paddingLeft).toBe('16px');
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

    it('applies paddingLeft of 32px on the tool-summary', () => {
      renderToolCard({ depth: 3 });
      const summary = container.querySelector('.tool-summary');
      expect(summary).toBeTruthy();
      expect(summary?.style.paddingLeft).toBe('32px');
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

    it('applies paddingLeft that scales linearly with depth', () => {
      // depth 5 → (5-1) * 16 = 64px
      renderToolCard({ depth: 5 });
      const summary = container.querySelector('.tool-summary');
      expect(summary?.style.paddingLeft).toBe('64px');
    });
  });

  describe('indentation formula', () => {
    it.each([
      [1, '0px'],
      [2, '16px'],
      [3, '32px'],
      [4, '48px'],
      [5, '64px'],
    ])('paddingLeft = (depth - 1) * 16 → depth=%d → %s', (depth, expectedPadding) => {
      renderToolCard({ depth, id: `tool-${depth}` });
      const summary = container.querySelector('.tool-summary');
      expect(summary?.style.paddingLeft).toBe(expectedPadding);
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
