// @ts-nocheck
/**
 * SubagentActivityFeed.test.tsx — Tests for the subagent activity feed component.
 *
 * Verifies depth indicator rendering across active and completed subagent
 * cards, as well as feed-level behavior (empty state, multiple runs).
 *
 * Tests:
 *   - Active card with depth=0 has NO depth badge
 *   - Active card with depth=1 shows "D1" depth badge
 *   - Active card with depth=2 shows "D2" depth badge
 *   - Active card with depth>0 has data-depth attribute
 *   - Completed card with depth=0 has no depth badge
 *   - Completed card with depth=1 shows "D1" depth badge
 *   - Completed card with depth=2 has data-depth="2" attribute
 *   - Feed with multiple runs at different depths renders all cards
 *   - Feed returns null when no activities
 */

import { createElement } from 'react';

// ── Mock lucide-react icons ──────────────────────────────────────────

vi.mock('lucide-react', () => {
  const { createElement: h } = require('react');
  const icons = [
    'Bot',
    'CheckCircle2',
    'XCircle',
    'ChevronDown',
    'ChevronRight',
    'Loader2',
  ];
  const result: Record<string, (props: any) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: any) =>
      h('svg', { 'data-testid': name.toLowerCase().replace('2', ''), ...props });
  }
  return result;
});

// ── Mock @sprout/ui ──────────────────────────────────────────────────
// @sprout/ui is a workspace package — vi.mock's importOriginal() can't resolve
// it in this context. We inline all needed exports.
//
// Exports the component needs (at runtime):
//   - LiveLog        → mocked as a simple <div>
//   - groupSubagentRuns → real logic inlined here
//   - getPersonaColor   → real logic inlined here
//   - MAX_ACTIVE_LINES  → constant
// The types.ts re-export also needs these, so they must all be present.

vi.mock('@sprout/ui', () => {
  const { createElement: h } = require('react');

  // ── Inline persona color logic ───────────────────────────────────
  const PERSONA_COLORS = {
    coder: '#58a6ff',
    reviewer: '#d2a8ff',
    code_reviewer: '#d2a8ff',
    tester: '#7ee787',
    debugger: '#f0883e',
    refactor: '#79c0ff',
    researcher: '#ff7b72',
    orchestrator: '#d29922',
    executive_assistant: '#58a6ff',
    general: '#6e7681',
  };
  const DEFAULT_PERSONA_COLOR = '#6e7681';

  function getPersonaColor(persona) {
    if (!persona) return DEFAULT_PERSONA_COLOR;
    return PERSONA_COLORS[persona] || DEFAULT_PERSONA_COLOR;
  }

  // ── Inline groupSubagentRuns logic ──────────────────────────────
  function groupSubagentRuns(activities) {
    const runMap = new Map();

    for (const activity of activities) {
      const key = activity.toolCallId || activity.id;
      let run = runMap.get(key);
      if (!run) {
        run = {
          toolCallId: activity.toolCallId,
          persona: activity.persona || 'subagent',
          isParallel: activity.isParallel || false,
          isComplete: false,
          completionMessage: '',
          completionTimestamp: null,
          activities: [],
          spawnActivity: null,
          completeActivity: null,
          outputLines: [],
          depth: activity.depth ?? 0,
          tokensUsed: 0,
          cost: 0,
        };
        runMap.set(key, run);
      }

      run.activities.push(activity);
      if (activity.tokensUsed) {
        run.tokensUsed += activity.tokensUsed;
      }
      if (activity.cost) {
        run.cost += activity.cost;
      }
      if (activity.persona && (!run.spawnActivity || activity.phase === 'spawn')) {
        run.persona = activity.persona;
      }
      if (activity.isParallel) {
        run.isParallel = true;
      }
      if (activity.phase === 'spawn') {
        run.spawnActivity = activity;
      }
      if (activity.phase === 'complete') {
        run.isComplete = true;
        run.completeActivity = activity;
        run.completionMessage = activity.message;
        run.completionTimestamp = activity.timestamp;
      }
      if (activity.phase === 'output' || activity.phase === 'step') {
        const lines = activity.message.split('\n').filter((l) => l.trim());
        for (const line of lines) {
          run.outputLines.push({
            id: activity.id + '-' + run.outputLines.length,
            text: line.trim(),
            timestamp: activity.timestamp,
            taskId: activity.taskId,
          });
        }
      }
    }

    return Array.from(runMap.values());
  }

  // ── Inline formatResourceUsage ──────────────────────────────────
  function formatCost(cost) {
    return '$' + cost.toFixed(4);
  }
  function formatTokens(tokens) {
    if (tokens >= 1000) {
      return (tokens / 1000).toFixed(1) + 'k';
    }
    return String(tokens);
  }

  // ── Mock LiveLog ────────────────────────────────────────────────
  function LiveLog({ lines, maxLines }) {
    return h('div', { 'data-testid': 'live-log', 'data-max-lines': maxLines },
      (lines || []).map((line, i) =>
        h('div', { key: i, 'data-testid': 'live-log-line' }, line.text)
      )
    );
  }

  return {
    LiveLog,
    groupSubagentRuns,
    getPersonaColor,
    formatCost,
    formatTokens,
    PERSONA_COLORS,
    MAX_ACTIVE_LINES: 50,
    MAX_COMPLETED_SUMMARIES: 3,
  };
});

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { SubagentActivityFeed } from './SubagentActivityFeed';
import type { SubagentActivity } from '@sprout/ui';

let container: HTMLDivElement;
let root: Root;

// ── Activity factory helpers ─────────────────────────────────────────

function makeActivity(overrides: Partial<SubagentActivity> = {}): SubagentActivity {
  return {
    id: overrides.id ?? 'activity-1',
    toolCallId: overrides.toolCallId ?? 'call-1',
    toolName: overrides.toolName ?? 'subagent',
    phase: overrides.phase ?? 'spawn',
    message: overrides.message ?? 'Activity message',
    timestamp: overrides.timestamp ?? new Date('2024-01-01T10:00:00Z'),
    persona: overrides.persona ?? 'coder',
    depth: overrides.depth ?? undefined,
    isParallel: overrides.isParallel ?? false,
  };
}

/**
 * Build a set of activities for a single run that will group into one SubagentRun.
 * For active runs, provide spawn + output (no complete).
 * For completed runs, provide spawn + output + complete.
 */
function makeRunActivities(opts: {
  toolCallId?: string;
  persona?: string;
  depth?: number;
  isComplete?: boolean;
  isParallel?: boolean;
  completionMessage?: string;
  outputMessage?: string;
  tokensUsed?: number;
  cost?: number;
}): SubagentActivity[] {
  const tc = opts.toolCallId ?? 'call-test';
  const persona = opts.persona ?? 'coder';
  const depth = opts.depth ?? undefined;
  const outputMsg = opts.outputMessage ?? 'Working on task...';
  const completeMsg = opts.completionMessage ?? 'Task completed successfully';

  const activities: SubagentActivity[] = [
    {
      id: `${tc}-spawn`,
      toolCallId: tc,
      toolName: 'subagent',
      phase: 'spawn',
      message: `Starting ${persona}`,
      timestamp: new Date('2024-01-01T10:00:00Z'),
      persona,
      depth,
      isParallel: opts.isParallel ?? false,
      tokensUsed: opts.tokensUsed,
      cost: opts.cost,
    },
    {
      id: `${tc}-output`,
      toolCallId: tc,
      toolName: 'subagent',
      phase: 'output',
      message: outputMsg,
      timestamp: new Date('2024-01-01T10:00:01Z'),
      persona,
      depth,
    },
  ];

  if (opts.isComplete) {
    activities.push({
      id: `${tc}-complete`,
      toolCallId: tc,
      toolName: 'subagent',
      phase: 'complete',
      message: completeMsg,
      timestamp: new Date('2024-01-01T10:00:02Z'),
      persona,
      depth,
    });
  }

  return activities;
}

// ── Lifecycle ─────────────────────────────────────────────────────────

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

// ── Tests ─────────────────────────────────────────────────────────────

describe('SubagentActivityFeed', () => {
  describe('active card depth rendering', () => {
    it('active card with depth=0 has NO depth badge (depth 0 is primary)', () => {
      const activities = makeRunActivities({ toolCallId: 'call-0', depth: 0, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      // The card should exist
      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();

      // No depth badge should be rendered for depth 0
      const badge = activeCard?.querySelector('.subagent-feed-depth-badge');
      expect(badge).toBeNull();
    });

    it('active card with depth=1 shows D1 depth badge', () => {
      const activities = makeRunActivities({ toolCallId: 'call-1', depth: 1, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();

      const badge = activeCard?.querySelector('.subagent-feed-depth-badge');
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toBe('D1');
    });

    it('active card with depth=2 shows D2 depth badge', () => {
      const activities = makeRunActivities({ toolCallId: 'call-2', depth: 2, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();

      const badge = activeCard?.querySelector('.subagent-feed-depth-badge');
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toBe('D2');
    });

    it('active card with depth>0 has data-depth attribute set correctly', () => {
      const activities = makeRunActivities({ toolCallId: 'call-3', depth: 2, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();
      expect(activeCard?.getAttribute('data-depth')).toBe('2');
    });

    it('active card with depth=0 still has data-depth="0" attribute', () => {
      const activities = makeRunActivities({ toolCallId: 'call-0b', depth: 0, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();
      expect(activeCard?.getAttribute('data-depth')).toBe('0');
    });
  });

  describe('completed card depth rendering', () => {
    it('completed card with depth=0 has no depth badge', () => {
      const activities = makeRunActivities({ toolCallId: 'call-c0', depth: 0, isComplete: true });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();

      const badge = completedCard?.querySelector('.subagent-feed-depth-badge');
      expect(badge).toBeNull();
    });

    it('completed card with depth=1 shows D1 depth badge', () => {
      const activities = makeRunActivities({ toolCallId: 'call-c1', depth: 1, isComplete: true });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();

      const badge = completedCard?.querySelector('.subagent-feed-depth-badge');
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toBe('D1');
    });

    it('completed card with depth=2 has data-depth="2" attribute', () => {
      const activities = makeRunActivities({ toolCallId: 'call-c2', depth: 2, isComplete: true });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();
      expect(completedCard?.getAttribute('data-depth')).toBe('2');
    });

    it('completed card with depth=0 has data-depth="0" attribute', () => {
      const activities = makeRunActivities({ toolCallId: 'call-c0b', depth: 0, isComplete: true });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();
      expect(completedCard?.getAttribute('data-depth')).toBe('0');
    });
  });

  describe('feed-level behavior', () => {
    it('feed with multiple runs at different depths renders all cards', () => {
      const activities: SubagentActivity[] = [
        ...makeRunActivities({ toolCallId: 'call-d1', depth: 1, isComplete: false }),
        ...makeRunActivities({ toolCallId: 'call-d2', depth: 2, isComplete: true }),
        ...makeRunActivities({ toolCallId: 'call-d0', depth: 0, isComplete: false }),
      ];

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      // Should have all 3 cards
      const allCards = container.querySelectorAll('.subagent-feed-card');
      expect(allCards.length).toBe(3);

      // Check depth attributes on each card
      const depths = Array.from(allCards).map((c) => c.getAttribute('data-depth'));
      expect(depths).toContain('1');
      expect(depths).toContain('2');
      expect(depths).toContain('0');
    });

    it('feed returns null when no activities (empty array)', () => {
      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities: [] }));
      });

      expect(container.querySelector('.subagent-feed')).toBeNull();
      expect(container.innerHTML).toBe('');
    });

    it('feed renders toggle bar when there is content', () => {
      const activities = makeRunActivities({ toolCallId: 'call-toggle', depth: 1, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      expect(container.querySelector('.subagent-feed-toggle-bar')).not.toBeNull();
    });

    it('feed shows active badge with correct count for active runs', () => {
      const activities: SubagentActivity[] = [
        ...makeRunActivities({ toolCallId: 'call-a1', depth: 0, isComplete: false }),
        ...makeRunActivities({ toolCallId: 'call-a2', depth: 1, isComplete: false }),
      ];

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeBadge = container.querySelector('.subagent-feed-active-badge');
      expect(activeBadge).not.toBeNull();
      expect(activeBadge?.textContent).toContain('2 active');
    });

    it('feed body is hidden when collapsed', () => {
      const activities = makeRunActivities({ toolCallId: 'call-collapse', depth: 1, isComplete: false });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      // Initially expanded
      expect(container.querySelector('.subagent-feed-body')).not.toBeNull();

      // Click toggle to collapse
      act(() => {
        const toggleBar = container.querySelector('.subagent-feed-toggle-bar');
        toggleBar?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      // Feed should have collapsed class
      const feed = container.querySelector('.subagent-feed');
      expect(feed?.className).toContain('subagent-feed--collapsed');
    });

    it('completed run with failure message shows error indicator', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-fail',
        depth: 1,
        isComplete: true,
        completionMessage: 'Task failed with error',
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();

      // Should show the error/fail styling
      const resultEl = completedCard?.querySelector('.subagent-feed-result--fail');
      expect(resultEl).not.toBeNull();
    });
  });

  describe('resource usage display', () => {
    it('active card shows token count when tokensUsed > 0', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-tok',
        depth: 0,
        isComplete: false,
        tokensUsed: 1500,
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();

      const metrics = activeCard?.querySelectorAll('.subagent-feed-metric');
      expect(metrics?.length).toBeGreaterThanOrEqual(1);
      // Should show formatted tokens like "1.5k tok"
      const metricTexts = Array.from(metrics || []).map((m) => m.textContent);
      expect(metricTexts.some((t) => t?.includes('tok'))).toBe(true);
      expect(metricTexts.some((t) => t?.includes('1.5k'))).toBe(true);
    });

    it('active card shows cost when cost > 0', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-cost',
        depth: 0,
        isComplete: false,
        cost: 0.0023,
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();

      const metrics = activeCard?.querySelectorAll('.subagent-feed-metric');
      expect(metrics?.length).toBeGreaterThanOrEqual(1);
      const metricTexts = Array.from(metrics || []).map((m) => m.textContent);
      expect(metricTexts.some((t) => t?.includes('$0.0023'))).toBe(true);
    });

    it('active card shows neither tokens nor cost when both are 0 or missing', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-notok',
        depth: 0,
        isComplete: false,
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const activeCard = container.querySelector('.subagent-feed-card--active');
      expect(activeCard).not.toBeNull();

      const metrics = activeCard?.querySelectorAll('.subagent-feed-metric');
      expect(metrics?.length).toBe(0);
    });

    it('completed card shows token count when tokensUsed > 0', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-ctok',
        depth: 0,
        isComplete: true,
        tokensUsed: 850,
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();

      const metrics = completedCard?.querySelectorAll('.subagent-feed-metric');
      expect(metrics?.length).toBeGreaterThanOrEqual(1);
      const metricTexts = Array.from(metrics || []).map((m) => m.textContent);
      expect(metricTexts.some((t) => t?.includes('850 tok'))).toBe(true);
    });

    it('completed card shows cost when cost > 0', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-ccost',
        depth: 0,
        isComplete: true,
        cost: 0.0150,
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const completedCard = container.querySelector('.subagent-feed-card--completed');
      expect(completedCard).not.toBeNull();

      const metrics = completedCard?.querySelectorAll('.subagent-feed-metric');
      expect(metrics?.length).toBeGreaterThanOrEqual(1);
      const metricTexts = Array.from(metrics || []).map((m) => m.textContent);
      expect(metricTexts.some((t) => t?.includes('$0.0150'))).toBe(true);
    });

    it('feed shows total summary when runs have tokens or cost', () => {
      const activities: SubagentActivity[] = [
        ...makeRunActivities({ toolCallId: 'call-s1', depth: 0, isComplete: true, tokensUsed: 1200, cost: 0.01 }),
        ...makeRunActivities({ toolCallId: 'call-s2', depth: 0, isComplete: true, tokensUsed: 800, cost: 0.005 }),
      ];

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const summary = container.querySelector('.subagent-feed-summary');
      expect(summary).not.toBeNull();

      const summaryMetrics = summary?.querySelectorAll('.subagent-feed-summary-metric');
      expect(summaryMetrics?.length).toBeGreaterThanOrEqual(1);

      const texts = Array.from(summaryMetrics || []).map((m) => m.textContent);
      // Should show aggregated totals: 2000 tok, $0.0150
      expect(texts.some((t) => t?.includes('2.0k'))).toBe(true);
      expect(texts.some((t) => t?.includes('$0.0150'))).toBe(true);
    });

    it('feed does NOT show total summary when all runs have 0 tokens and 0 cost', () => {
      const activities = makeRunActivities({
        toolCallId: 'call-nosum',
        depth: 0,
        isComplete: true,
      });

      act(() => {
        root.render(createElement(SubagentActivityFeed, { activities }));
      });

      const summary = container.querySelector('.subagent-feed-summary');
      expect(summary).toBeNull();
    });
  });
});
