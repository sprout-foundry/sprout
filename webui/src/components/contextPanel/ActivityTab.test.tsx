// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';

vi.mock('../AgentChangesPanel', () => ({ default: () => null }));
vi.mock('../../services/api', () => ({
  // Not used directly by ActivityTab; present for transitive imports
}));
vi.mock('./ToolCard', () => ({
  ToolCard: (props) =>
    createElement(
      'div',
      { 'data-testid': 'tool-card', 'data-tool-id': props.tool.id, onClick: props.onToggleExpansion },
      props.tool.tool,
    ),
}));

import { ActivityTab } from './ActivityTab';
import type { ToolExecution } from './types';

function makeTool(overrides: Partial<ToolExecution> = {}): ToolExecution {
  return {
    id: overrides.id ?? 't1',
    tool: overrides.tool ?? 'read_file',
    status: overrides.status ?? 'completed',
    startTime: new Date('2024-01-01T10:00:00Z'),
    ...(overrides.arguments !== undefined ? { arguments: overrides.arguments } : {}),
    ...(overrides.queryId !== undefined ? { queryId: overrides.queryId } : {}),
    ...(overrides.result !== undefined ? { result: overrides.result } : {}),
  } as ToolExecution;
}

function makeSubagentRun(id: string, status = 'completed'): Record<string, unknown> {
  return {
    tool: makeTool({ id, tool: 'run_subagent', status }),
    activities: [],
    orderedTaskGroups: [],
    latestActivity: undefined,
  };
}

let container: HTMLDivElement;
let root: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

async function render(props: Record<string, unknown>) {
  await act(async () => {
    root.render(createElement(ActivityTab, props));
  });
}

const BASE = {
  groupedByQuery: new Map(),
  maxQueryId: 0,
  expandedQueries: new Set(),
  expandedTools: new Set(),
  expandedSubagents: new Set(),
  activeToolId: null,
  toolRefs: { current: {} },
  toggleQueryGroup: vi.fn(),
  toggleToolExpansion: vi.fn(),
  toggleSubagentExpansion: vi.fn(),
  setActiveToolId: vi.fn(),
  setExpandedTools: vi.fn(),
  setExpandedQueries: vi.fn(),
};

function activityRoot(): HTMLElement | null {
  return container.querySelector('[data-testid="context-panel-activity"]');
}

describe('ActivityTab', () => {
  it('shows the empty state when there are no tools or subagents', async () => {
    await render({ ...BASE, toolExecutions: [], subagentRuns: [], resourceCounts: { active: 0, queued: 0, completed: 0, failed: 0, cancelled: 0 } });
    expect(activityRoot()).not.toBeNull();
    expect(container.textContent).toContain('will appear here');
  });

  it('renders subagent runs in the delegated-work section', async () => {
    await render({
      ...BASE,
      toolExecutions: [makeTool({ id: 'sa1', tool: 'run_subagent' })],
      subagentRuns: [makeSubagentRun('sa1')],
      resourceCounts: { active: 0, queued: 0, completed: 1, failed: 0, cancelled: 0 },
    });
    expect(container.textContent).toContain('Delegated work');
    expect(container.querySelector('[data-testid="context-panel-subagents"]')).not.toBeNull();
  });

  it('excludes subagent tools from the plain tool rows (no duplication)', async () => {
    const tools = [
      makeTool({ id: 'sa1', tool: 'run_subagent', queryId: 1 }),
      makeTool({ id: 'p1', tool: 'read_file', queryId: 1 }),
      makeTool({ id: 'p2', tool: 'shell_command', queryId: 1 }),
    ];
    await render({
      ...BASE,
      toolExecutions: tools,
      groupedByQuery: new Map([[1, tools]]),
      maxQueryId: 1,
      subagentRuns: [makeSubagentRun('sa1')],
      resourceCounts: { active: 0, queued: 0, completed: 1, failed: 0, cancelled: 0 },
    });
    const cards = Array.from(container.querySelectorAll('[data-testid="tool-card"]'));
    expect(cards.map((c) => c.getAttribute('data-tool-id'))).toEqual(['p1', 'p2']);
  });

  it('renders turn groups with only plain tools after filtering', async () => {
    const tools = [makeTool({ id: 'p1', tool: 'edit_file', queryId: 3 })];
    await render({
      ...BASE,
      toolExecutions: tools,
      groupedByQuery: new Map([[3, tools]]),
      maxQueryId: 3,
      subagentRuns: [],
      resourceCounts: { active: 0, queued: 0, completed: 0, failed: 0, cancelled: 0 },
    });
    expect(container.textContent).toContain('Current turn');
    expect(container.querySelectorAll('[data-testid="tool-card"]').length).toBe(1);
  });
});
