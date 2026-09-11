import { useMemo } from 'react';
import { isSubagentTool, getSubagentPrompt } from './helpers';
import type { ChatContextPanelProps, ContextSubagentRun, SubagentResourceCounts } from './types';

export interface UseSubagentRunsResult {
  subagentRuns: ContextSubagentRun[];
  resourceCounts: SubagentResourceCounts;
}

/**
 * Builds subagent run view-models from structured activity events.
 *
 * Correlation is exact: every subagent_activity event carries the parent
 * run_subagent tool call's ID (seed v1.4.0 attaches it to handler
 * contexts; sprout's lifecycle/progress/llm_output publishers propagate
 * it). The previous ±500ms timestamp-window matching against raw logs —
 * racy under parallel runs and coupled to backend log copy — is gone.
 */
export function useSubagentRuns(chatProps: ChatContextPanelProps | null) {
  const subagentToolExecutions = useMemo(() => chatProps?.toolExecutions ?? [], [chatProps]);
  const subagentActivities = useMemo(() => chatProps?.subagentActivities ?? [], [chatProps]);

  return useMemo<UseSubagentRunsResult>(() => {
    // ── Resource counts from lifecycle status events ──
    const latestStatusByTask = new Map<string, { status: string; failures?: number; message: string }>();

    for (const activity of subagentActivities) {
      if (!activity.status || !activity.taskId) continue;
      latestStatusByTask.set(activity.taskId, {
        status: activity.status,
        failures: activity.failures,
        message: activity.message,
      });
    }

    let active = 0;
    let queued = 0;
    let completed = 0;
    let failed = 0;
    let cancelled = 0;

    for (const { status, failures, message } of latestStatusByTask.values()) {
      switch (status) {
        case 'started':
          active++;
          break;
        case 'queued':
          queued++;
          break;
        case 'completed':
          if ((failures ?? 0) > 0 || /\b(failed|failure|errors?|fail)\b/i.test(message)) {
            failed++;
          } else {
            completed++;
          }
          break;
        case 'cancelled':
          cancelled++;
          break;
      }
    }

    const resourceCounts = { active, queued, completed, failed, cancelled };

    // ── Subagent runs ──
    const subagentRuns: ContextSubagentRun[] = subagentToolExecutions.filter(isSubagentTool).map((tool) => {
      const activities = subagentActivities
        .filter((activity) => activity.toolCallId === tool.id)
        .map((activity) => ({
          id: activity.id,
          timestamp: activity.timestamp,
          taskId: activity.taskId,
          label: activity.message,
          isSpawn: activity.phase === 'spawn',
        }));

      const taskGroups = activities.reduce<Record<string, typeof activities>>((acc, item) => {
        const key = item.taskId || '__main__';
        if (!acc[key]) {
          acc[key] = [];
        }
        acc[key].push(item);
        return acc;
      }, {});

      const orderedTaskGroups = Object.entries(taskGroups).map(([taskId, items]) => ({
        taskId: taskId === '__main__' ? null : taskId,
        items,
        latest: items[items.length - 1],
      }));

      return {
        tool,
        prompt: getSubagentPrompt(tool),
        latestActivity: activities[activities.length - 1],
        activities,
        orderedTaskGroups,
      };
    });

    return { subagentRuns, resourceCounts };
  }, [subagentToolExecutions, subagentActivities]);
}
