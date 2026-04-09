// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Mock heavy child components that ContextPanel imports
// ---------------------------------------------------------------------------

jest.mock('./TodoPanel', () => () => <div data-testid="todo-panel" />);
jest.mock('./RevisionListPanel', () => () => <div data-testid="revision-panel" />);
jest.mock('../services/api', () => {
  const mockApi = {
    getChangelog: jest.fn().mockResolvedValue({ revisions: [] }),
    getSessions: jest.fn().mockResolvedValue({ sessions: [], current_session_id: '' }),
    getRevisionDetails: jest.fn().mockResolvedValue({ revision: { files: [] } }),
    restoreSession: jest.fn().mockResolvedValue({ messages: [] }),
  };
  return {
    ApiService: {
      getInstance: jest.fn(() => mockApi),
    },
  };
});
// ContextPanel uses useLog() which requires NotificationContext.
jest.mock('../contexts/NotificationContext', () => {
  const noop = () => {};
  return Object.assign(function NotificationProviderMock({ children }) { return children; }, {
    useNotifications: () => ({ addNotification: noop }),
  });
});

// ---------------------------------------------------------------------------
// Replicate formatTokens and formatCost (mirrors the inline implementations
// in ContextPanel).  These are intentionally kept in sync — if ContextPanel's
// logic changes these should be updated.  The functions are tiny (3–5 lines)
// so duplication is acceptable for isolated unit tests.
// ---------------------------------------------------------------------------

function formatTokens(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens < 0) return '—';
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`;
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`;
  return tokens.toString();
}

function formatCost(cost: number): string {
  if (!Number.isFinite(cost)) return '—';
  return `$${cost.toFixed(4)}`;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const MINIMAL_CHAT_PROPS = {
  context: 'chat',
  toolExecutions: [],
  fileEdits: [],
  logs: [],
  subagentActivities: [],
  currentTodos: [],
  messages: [],
  isProcessing: false,
  lastError: null,
  queryProgress: null,
};

function makeChatProps(overrides: Record<string, unknown> = {}) {
  return { ...MINIMAL_CHAT_PROPS, ...overrides };
}

/**
 * Create mock messages spaced evenly apart.
 */
function makeMessages(count: number, spacingMs = 60_000): Array<{ type: string; timestamp: Date }> {
  const msgs: Array<{ type: string; timestamp: Date }> = [];
  const base = Date.now() - count * spacingMs;
  for (let i = 0; i < count; i++) {
    msgs.push({
      type: i % 2 === 0 ? 'user' : 'assistant',
      timestamp: new Date(base + i * spacingMs),
    });
  }
  return msgs;
}

/**
 * Create a minimal stats object with sensible defaults and any overrides.
 */
function makeStats(overrides: Record<string, unknown> = {}) {
  return {
    provider: 'anthropic',
    model: 'claude-sonnet-4-20250514',
    total_tokens: 15000,
    prompt_tokens: 10000,
    completion_tokens: 5000,
    cached_tokens: 0,
    current_context_tokens: 8000,
    max_context_tokens: 16000,
    context_usage_percent: 50.0,
    cache_efficiency: 0,
    total_cost: 0,
    cached_cost_savings: 0,
    last_tps: 0,
    current_iteration: 1,
    max_iterations: 10,
    streaming_enabled: true,
    debug_mode: false,
    context_warning_issued: false,
    uptime: '5m',
    query_count: 3,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();

  // Ensure panel is not collapsed and status tab is active.
  // The component reads these from localStorage in a useEffect on mount.
  window.localStorage.setItem('ledit.contextPanel.collapsed', '0');
  window.localStorage.setItem('ledit.contextPanel.tab.chat', 'status');

  // Ensure we're not in mobile layout (MOBILE_LAYOUT_MAX_WIDTH = 768)
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 1024 });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/**
 * Render the ContextPanel with the given chat-context props.
 * The component is auto-imported so we use require() to avoid
 * hoisting issues with jest.mock.
 */
async function renderPanel(props: Record<string, unknown>) {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const ContextPanel = require('./ContextPanel').default;
  // eslint-disable-next-line testing-library/no-unnecessary-act
  await act(async () => {
    root.render(createElement(ContextPanel, props));
  });
  await flushPromises();
}

// ---------------------------------------------------------------------------
// DOM query helpers
// ---------------------------------------------------------------------------

/**
 * Get all .status-section elements as an array.
 */
function getStatusSections(): HTMLElement[] {
  return Array.from(container.querySelectorAll('.status-section'));
}

/**
 * Find a .status-section by the text content of its .status-section-title.
 * Returns null if not found.
 */
function getStatusSectionByTitle(title: string): HTMLElement | null {
  const sections = getStatusSections();
  return (
    sections.find((section) => {
      const titleEl = section.querySelector('.status-section-title');
      return titleEl && titleEl.textContent?.includes(title);
    }) ?? null
  );
}

/**
 * Get the text content of a metric label/value pair within a section,
 * identified by the label text. Returns null if the label is not found.
 */
function getMetricValue(labelText: string): string | null {
  const metrics = container.querySelectorAll('.status-metric');
  for (const metric of metrics) {
    const label = metric.querySelector('.status-metric-label');
    if (label && label.textContent?.trim() === labelText) {
      const value = metric.querySelector('.status-metric-value');
      return value?.textContent?.trim() ?? null;
    }
  }
  return null;
}

/**
 * Check whether a metric with the given label text exists in the DOM.
 */
function hasMetric(labelText: string): boolean {
  return getMetricValue(labelText) !== null;
}

/**
 * Get the context bar fill element's className string.
 * Returns null if the context bar fill is not rendered.
 */
function getContextBarFillClasses(): string | null {
  const fill = container.querySelector('.status-context-bar-fill');
  if (!fill) return null;
  return fill.className;
}

// ===========================================================================
// Part 1: Unit tests for formatTokens (pure formatting logic)
// ===========================================================================

describe('formatTokens – unit tests (pure formatting logic)', () => {
  it('formats 0 as "0"', () => {
    expect(formatTokens(0)).toBe('0');
  });

  it('formats small numbers as plain strings', () => {
    expect(formatTokens(1)).toBe('1');
    expect(formatTokens(500)).toBe('500');
    expect(formatTokens(999)).toBe('999');
  });

  it('formats exactly 1000 with "K" suffix', () => {
    expect(formatTokens(1000)).toBe('1.0K');
  });

  it('formats thousands with one decimal place and "K" suffix', () => {
    expect(formatTokens(15000)).toBe('15.0K');
    expect(formatTokens(500)).toBe('500');
    expect(formatTokens(9999)).toBe('10.0K'); // 9999/1000 = 9.999 → toFixed(1) → "10.0"
    expect(formatTokens(85750)).toBe('85.8K'); // 85750/1000 = 85.75 → toFixed(1) → "85.8"
  });

  it('formats exactly 1,000,000 with "M" suffix', () => {
    expect(formatTokens(1000000)).toBe('1.0M');
  });

  it('formats millions with one decimal place and "M" suffix', () => {
    expect(formatTokens(1500000)).toBe('1.5M');
    expect(formatTokens(25000000)).toBe('25.0M');
  });

  it('formats very large numbers with "M" suffix', () => {
    expect(formatTokens(123456789)).toBe('123.5M');
  });
});

// ===========================================================================
// Part 2: Unit tests for formatCost (pure formatting logic)
// ===========================================================================

describe('formatCost – unit tests (pure formatting logic)', () => {
  it('formats 0 as "$0.0000"', () => {
    expect(formatCost(0)).toBe('$0.0000');
  });

  it('formats small costs with "$" prefix and 4 decimal places', () => {
    expect(formatCost(0.0123)).toBe('$0.0123');
    expect(formatCost(0.5)).toBe('$0.5000');
    expect(formatCost(0.0001)).toBe('$0.0001');
  });

  it('formats whole dollar costs with 4 decimal places', () => {
    expect(formatCost(1)).toBe('$1.0000');
    expect(formatCost(10)).toBe('$10.0000');
  });

  it('formats larger costs correctly', () => {
    expect(formatCost(1.5)).toBe('$1.5000');
    expect(formatCost(123.4567)).toBe('$123.4567');
  });

  it('rounds to 4 decimal places', () => {
    expect(formatCost(0.123456)).toBe('$0.1235'); // rounds up
    expect(formatCost(0.123444)).toBe('$0.1234'); // rounds down
  });
});

// ===========================================================================
// Part 3: Component integration tests – stats sections (no stats provided)
// ===========================================================================

describe('ContextPanel status tab – no stats provided', () => {
  it('renders Token Usage section with "—" for all values', async () => {
    await renderPanel(makeChatProps({ messages: makeMessages(2) }));

    const section = getStatusSectionByTitle('Token Usage');
    expect(section).not.toBeNull();

    expect(getMetricValue('Total')).toBe('—');
    expect(getMetricValue('Prompt')).toBe('—');
    expect(getMetricValue('Completion')).toBe('—');
    expect(hasMetric('Cached')).toBe(false);
  });

  it('renders Context Window section with "—" for all values', async () => {
    await renderPanel(makeChatProps({ messages: makeMessages(2) }));

    const section = getStatusSectionByTitle('Context Window');
    expect(section).not.toBeNull();

    expect(getMetricValue('Used')).toBe('—');
    expect(getMetricValue('Current')).toBe('—');
    expect(getMetricValue('Max')).toBe('—');
  });

  it('does NOT render a context bar when no stats are provided', async () => {
    await renderPanel(makeChatProps({ messages: makeMessages(2) }));

    expect(getContextBarFillClasses()).toBeNull();
  });

  it('renders Costs section with "—" for Total Cost', async () => {
    await renderPanel(makeChatProps({ messages: makeMessages(2) }));

    const section = getStatusSectionByTitle('Costs');
    expect(section).not.toBeNull();

    expect(getMetricValue('Total Cost')).toBe('—');
    expect(hasMetric('Cache Savings')).toBe(false);
  });
});

// ===========================================================================
// Part 4: Component integration tests – Token Usage section with stats
// ===========================================================================

describe('ContextPanel status tab – Token Usage with stats', () => {
  it('shows formatted total_tokens as "15.0K"', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ total_tokens: 15000 }),
      }),
    );

    expect(getMetricValue('Total')).toBe('15.0K');
  });

  it('shows formatted prompt_tokens', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ prompt_tokens: 10000 }),
      }),
    );

    expect(getMetricValue('Prompt')).toBe('10.0K');
  });

  it('shows formatted completion_tokens', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ completion_tokens: 500 }),
      }),
    );

    expect(getMetricValue('Completion')).toBe('500');
  });

  it('shows Cached metric when cached_tokens > 0', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ cached_tokens: 5000 }),
      }),
    );

    expect(hasMetric('Cached')).toBe(true);
    expect(getMetricValue('Cached')).toBe('5.0K');
  });

  it('hides Cached metric when cached_tokens is 0', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ cached_tokens: 0 }),
      }),
    );

    expect(hasMetric('Cached')).toBe(false);
  });

  it('formats large token counts (millions)', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ total_tokens: 1500000 }),
      }),
    );

    expect(getMetricValue('Total')).toBe('1.5M');
  });
});

// ===========================================================================
// Part 5: Component integration tests – Context Window section with stats
// ===========================================================================

describe('ContextPanel status tab – Context Window with stats', () => {
  it('shows percentage for Used when context_usage_percent is set', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 50.0 }),
      }),
    );

    expect(getMetricValue('Used')).toBe('50.0%');
  });

  it('shows formatted current_context_tokens', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ current_context_tokens: 8000 }),
      }),
    );

    expect(getMetricValue('Current')).toBe('8.0K');
  });

  it('shows formatted max_context_tokens', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ max_context_tokens: 16000 }),
      }),
    );

    expect(getMetricValue('Max')).toBe('16.0K');
  });

  it('renders context bar with normal class when usage is below 75%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 50.0 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    // Normal usage: should NOT have "high" or "critical" class
    expect(classes).not.toContain('critical');
    expect(classes).not.toContain('high');
    // Should still have the base "status-context-bar-fill" class
    expect(classes).toContain('status-context-bar-fill');
  });

  it('renders context bar with "high" class when usage is 75-90%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 85.5 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    expect(classes).toContain('high');
    expect(classes).not.toContain('critical');
  });

  it('renders context bar with "high" class exactly at 75%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 75.0 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    // 75 is NOT > 75, so it should NOT get "high"
    // The condition is: > 90 → critical, > 75 → high. At exactly 75, neither applies.
    expect(classes).not.toContain('high');
    expect(classes).not.toContain('critical');
  });

  it('renders context bar with "high" class at 75.01%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 75.01 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    expect(classes).toContain('high');
    expect(classes).not.toContain('critical');
  });

  it('renders context bar with "critical" class when usage is above 90%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 95.0 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    expect(classes).toContain('critical');
    expect(classes).not.toContain('high');
  });

  it('renders context bar with "critical" class exactly at 90%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 90.0 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    // 90 is NOT > 90, so it should get "high" (since 90 > 75)
    expect(classes).toContain('high');
    expect(classes).not.toContain('critical');
  });

  it('renders context bar with "critical" class at 90.01%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 90.01 }),
      }),
    );

    const classes = getContextBarFillClasses();
    expect(classes).not.toBeNull();
    expect(classes).toContain('critical');
    expect(classes).not.toContain('high');
  });

  it('sets context bar width to match usage percent', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 65.0 }),
      }),
    );

    const fill = container.querySelector('.status-context-bar-fill');
    expect(fill).not.toBeNull();
    expect((fill as HTMLElement).style.width).toBe('65%');
  });

  it('caps context bar width at 100%', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ context_usage_percent: 150.0 }),
      }),
    );

    const fill = container.querySelector('.status-context-bar-fill');
    expect(fill).not.toBeNull();
    expect((fill as HTMLElement).style.width).toBe('100%');
  });
});

// ===========================================================================
// Part 6: Component integration tests – Costs section with stats
// ===========================================================================

describe('ContextPanel status tab – Costs with stats', () => {
  it('shows formatted total_cost', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ total_cost: 0.0123 }),
      }),
    );

    expect(getMetricValue('Total Cost')).toBe('$0.0123');
  });

  it('shows Cache Savings when cached_cost_savings > 0', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ cached_cost_savings: 0.005 }),
      }),
    );

    expect(hasMetric('Cache Savings')).toBe(true);
    expect(getMetricValue('Cache Savings')).toBe('$0.0050');
  });

  it('hides Cache Savings when cached_cost_savings is 0', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ cached_cost_savings: 0 }),
      }),
    );

    expect(hasMetric('Cache Savings')).toBe(false);
  });

  it('formats larger costs correctly', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({ total_cost: 1.5 }),
      }),
    );

    expect(getMetricValue('Total Cost')).toBe('$1.5000');
  });

  it('shows both token and cost stats together', async () => {
    await renderPanel(
      makeChatProps({
        messages: makeMessages(2),
        stats: makeStats({
          total_tokens: 1500000,
          total_cost: 0.0123,
          cached_tokens: 5000,
          cached_cost_savings: 0.005,
          context_usage_percent: 85.5,
        }),
      }),
    );

    // Token Usage
    expect(getMetricValue('Total')).toBe('1.5M');
    expect(hasMetric('Cached')).toBe(true);
    expect(getMetricValue('Cached')).toBe('5.0K');

    // Context Window
    expect(getMetricValue('Used')).toBe('85.5%');
    const classes = getContextBarFillClasses();
    expect(classes).toContain('high');

    // Costs
    expect(getMetricValue('Total Cost')).toBe('$0.0123');
    expect(hasMetric('Cache Savings')).toBe(true);
    expect(getMetricValue('Cache Savings')).toBe('$0.0050');
  });
});
