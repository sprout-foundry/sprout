/**
 * Tests for useSubagentRuns resource-usage counts computation (SP-037-3c).
 *
 * These tests verify that resourceCounts are computed correctly from
 * subagentActivities that carry a status field. The logic finds the
 * LATEST status event per unique taskId, then aggregates those into
 * active/queued/completed/failed/cancelled counts.
 */

import { renderHook } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useSubagentRuns } from './useSubagentRuns';
import type { ChatContextPanelProps } from './types';
import type { SubagentActivity, ToolExecution } from '@sprout/ui';

// ── Helpers ─────────────────────────────────────────────────────────

const makeActivity = (overrides: Partial<SubagentActivity> = {}): SubagentActivity => ({
  id: `act-${Math.random().toString(36).slice(2)}`,
  toolCallId: '',
  toolName: 'run_subagent',
  phase: 'output',
  message: 'activity message',
  timestamp: new Date(),
  taskId: undefined,
  status: undefined,
  ...overrides,
});

const makeTool = (overrides: Partial<ToolExecution> = {}): ToolExecution => ({
  id: `tool-${Math.random().toString(36).slice(2)}`,
  tool: 'run_subagent',
  status: 'completed',
  startTime: new Date(),
  ...overrides,
});

const makeBaseProps = (overrides: Partial<ChatContextPanelProps> = {}): ChatContextPanelProps => ({
  context: 'chat',
  toolExecutions: [],
  fileEdits: [],
  logs: [],
  subagentActivities: [],
  delegateActivities: [],
  currentTodos: [],
  messages: [],
  isProcessing: false,
  lastError: null,
  queryProgress: null,
  onLoadRevisionHistory: vi.fn().mockResolvedValue({ revisions: [] }),
  onLoadSessions: vi.fn().mockResolvedValue({ sessions: [], current_session_id: '' }),
  onRestoreSession: vi.fn().mockResolvedValue({ messages: [] }),
  onLoadRevisionDetails: vi.fn().mockResolvedValue({}),
  ...overrides,
});

// ── Tests ───────────────────────────────────────────────────────────

describe('useSubagentRuns resource counts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it('returns all zeros when there are no activities', () => {
    const props = makeBaseProps({ subagentActivities: [] });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('returns all zeros when null is passed', () => {
    const { result } = renderHook(() => useSubagentRuns(null));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts a single "started" activity as active', () => {
    const props = makeBaseProps({
      subagentActivities: [makeActivity({ taskId: 'task-1', status: 'started' })],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 1,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts a single "queued" activity as queued', () => {
    const props = makeBaseProps({
      subagentActivities: [makeActivity({ taskId: 'task-1', status: 'queued' })],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 1,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts a single "completed" activity as completed', () => {
    const props = makeBaseProps({
      subagentActivities: [makeActivity({ taskId: 'task-1', status: 'completed' })],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 1,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts a single "cancelled" activity as cancelled', () => {
    const props = makeBaseProps({
      subagentActivities: [makeActivity({ taskId: 'task-1', status: 'cancelled' })],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 1,
    });
  });

  it('aggregates a mix of different statuses correctly', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 't-1', status: 'started' }),
        makeActivity({ taskId: 't-2', status: 'started' }),
        makeActivity({ taskId: 't-3', status: 'queued' }),
        makeActivity({ taskId: 't-4', status: 'completed', message: 'All done' }),
        makeActivity({ taskId: 't-5', status: 'cancelled' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 2,
      queued: 1,
      completed: 1,
      failed: 0,
      cancelled: 1,
    });
  });

  it('uses the LATEST status per task when a task transitions through statuses', () => {
    // Simulates: queued → started → completed for the same task
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'queued', message: 'queued' }),
        makeActivity({ taskId: 'task-1', status: 'started', message: 'started' }),
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'completed' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    // Only the latest (completed) should count — not each transition
    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 1,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts completed with failures > 0 as failed, not completed', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', failures: 2, message: 'finished with retries' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 1,
      cancelled: 0,
    });
  });

  it('counts completed with failures=0 as completed (not failed)', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', failures: 0, message: 'clean run' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 1,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts completed with undefined failures as completed (not failed)', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'finished successfully' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 1,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts completed with "error" in message as failed', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'finished with an error during processing' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 1,
      cancelled: 0,
    });
  });

  it('counts completed with "fail" in message as failed (case-insensitive)', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'TASK FAIL after retries' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 1,
      cancelled: 0,
    });
  });

  it('counts completed with "FAILED" as failed (case-insensitive, with suffix)', () => {
    // The regex \b(error|fail)\w*\b matches "FAILED" — FAIL followed by \w* matches ED
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'TASK FAILED after retries' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 1,
      cancelled: 0,
    });
  });

  it('counts completed with "errors" as failed (plural form)', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'encountered 3 errors during processing' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 1,
      cancelled: 0,
    });
  });

  it('counts completed with "failure" as failed (variant form)', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'subagent failure detected' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 0,
      failed: 1,
      cancelled: 0,
    });
  });

  it('does NOT count completed with substring "error" in unrelated context as failed when it is a word boundary match only', () => {
    // "error" as a whole word should match, but we need word-boundary matching
    // The regex uses \b(error|fail)\b so partial matches like "terror" should NOT match
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'no terror or warfare here' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    // "terror" contains "error" but as part of a word — \b should prevent this
    expect(result.current.resourceCounts).toEqual({
      active: 0,
      queued: 0,
      completed: 1,
      failed: 0,
      cancelled: 0,
    });
  });

  it('ignores activities without a status field for counting', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: undefined, message: 'no status' }),
        makeActivity({ taskId: 'task-2', status: 'started', message: 'has status' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 1,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('ignores activities without a taskId for counting', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: undefined, status: 'started', message: 'no task id' }),
        makeActivity({ taskId: 'task-1', status: 'started', message: 'has task id' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 1,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('ignores activities with empty string status for counting', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-1', status: '', message: 'empty status' }),
        makeActivity({ taskId: 'task-2', status: 'started', message: 'valid status' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 1,
      queued: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('counts each parallel subagent task separately when they have different task_ids', () => {
    const props = makeBaseProps({
      subagentActivities: [
        makeActivity({ taskId: 'task-a', status: 'started', message: 'task a running' }),
        makeActivity({ taskId: 'task-b', status: 'started', message: 'task b running' }),
        makeActivity({ taskId: 'task-c', status: 'queued', message: 'task c waiting' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 2,
      queued: 1,
      completed: 0,
      failed: 0,
      cancelled: 0,
    });
  });

  it('handles multiple tasks with different final statuses correctly', () => {
    const props = makeBaseProps({
      subagentActivities: [
        // task-1: queued → started → completed (with errors)
        makeActivity({ taskId: 'task-1', status: 'queued', message: 'queued' }),
        makeActivity({ taskId: 'task-1', status: 'started', message: 'started' }),
        makeActivity({ taskId: 'task-1', status: 'completed', message: 'finished with error', failures: 1 }),
        // task-2: queued → completed (clean)
        makeActivity({ taskId: 'task-2', status: 'queued', message: 'queued' }),
        makeActivity({ taskId: 'task-2', status: 'completed', message: 'clean run' }),
        // task-3: started (still running)
        makeActivity({ taskId: 'task-3', status: 'started', message: 'running' }),
        // task-4: cancelled
        makeActivity({ taskId: 'task-4', status: 'cancelled', message: 'user cancelled' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.resourceCounts).toEqual({
      active: 1, // task-3
      queued: 0,
      completed: 1, // task-2
      failed: 1, // task-1 (failures > 0)
      cancelled: 1, // task-4
    });
  });

  it('returns empty subagentRuns when no subagent tool executions exist', () => {
    const props = makeBaseProps({ toolExecutions: [] });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.subagentRuns).toEqual([]);
  });

  it('returns non-empty subagentRuns when subagent tool executions exist', () => {
    const tool = makeTool();
    const props = makeBaseProps({
      toolExecutions: [tool],
      subagentActivities: [
        makeActivity({ taskId: 'task-1', toolCallId: tool.id, message: 'working', phase: 'output' }),
      ],
    });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.subagentRuns).toHaveLength(1);
    expect(result.current.subagentRuns[0].tool).toBe(tool);
  });

  it('does not include non-subagent tools in subagentRuns', () => {
    const nonSubagentTool: ToolExecution = {
      id: 'read-file-1',
      tool: 'read_file',
      status: 'completed',
      startTime: new Date(),
      endTime: new Date(),
    };
    const props = makeBaseProps({ toolExecutions: [nonSubagentTool] });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.subagentRuns).toHaveLength(0);
  });

  it('includes run_parallel_subagents in subagentRuns', () => {
    const tool = makeTool({ tool: 'run_parallel_subagents' });
    const props = makeBaseProps({ toolExecutions: [tool] });
    const { result } = renderHook(() => useSubagentRuns(props));

    expect(result.current.subagentRuns).toHaveLength(1);
    expect(result.current.subagentRuns[0].tool.tool).toBe('run_parallel_subagents');
  });
});
