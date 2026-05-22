import { useMemo, useCallback } from 'react';
import { stripAnsiCodes } from '../../utils/ansi';
import { isSubagentTool, getSubagentPrompt } from './helpers';
import type {
  ChatContextPanelProps,
  ContextSubagentRun,
  ContextNormalizedActivity,
  SubagentResourceCounts,
} from './types';

export interface UseSubagentRunsResult {
  subagentRuns: ContextSubagentRun[];
  resourceCounts: SubagentResourceCounts;
}

export function useSubagentRuns(chatProps: ChatContextPanelProps | null) {
  const subagentToolExecutions = useMemo(() => chatProps?.toolExecutions ?? [], [chatProps]);
  const subagentLogs = useMemo(() => chatProps?.logs ?? [], [chatProps]);
  const subagentActivities = useMemo(() => chatProps?.subagentActivities ?? [], [chatProps]);

  const getSubagentLogMessage = useCallback((logEntry: (typeof subagentLogs)[number]): string | null => {
    if (logEntry.type !== 'agent_message' || !logEntry.data || typeof logEntry.data !== 'object') {
      return null;
    }
    const d = logEntry.data as Record<string, unknown>;
    const raw = typeof d.message === 'string' ? d.message : '';
    if (!raw || (!raw.includes('Subagent:') && !/Spawning subagent/i.test(raw))) {
      return null;
    }
    return stripAnsiCodes(raw).trim() || null;
  }, []);

  const summarizeExecutionTarget = useCallback((message: string): string => {
    const match = message.match(/executing tool \[([^\]]+)\]/i);
    if (!match) {
      return message;
    }
    const rawTarget = match[1].trim();
    if (!rawTarget) {
      return message;
    }
    const parts = rawTarget.split(/\s+/);
    const toolName = parts[0] || 'tool';
    const argPreview = parts.slice(1).join(' ').trim();
    const suffix = argPreview ? ` ${argPreview.slice(0, 56)}${argPreview.length > 56 ? '...' : ''}` : '';
    return message.replace(/executing tool \[[^\]]+\]/i, `Running ${toolName}${suffix}`);
  }, []);

  const normalizeSubagentActivity = useCallback(
    (rawMessage: string): ContextNormalizedActivity => {
      const cleaned = stripAnsiCodes(rawMessage).trim();
      const taskMatch = cleaned.match(/^→\s+\[([^\]]+)\]\s+Subagent:\s+(.*)$/);
      if (taskMatch) {
        const body = summarizeExecutionTarget(taskMatch[2].trim())
          .replace(/^\[\d+\s*-\s*\d+%\]\s*/i, '')
          .trim();
        return {
          taskId: taskMatch[1],
          label: body,
          isSpawn: false,
        };
      }

      const spawnMatch = cleaned.match(/Spawning subagent \[([^\]]+)\]:\s*(.*)$/i);
      if (spawnMatch) {
        const spawnDetails = spawnMatch[2].trim();
        return {
          taskId: undefined,
          label: spawnDetails ? `Starting ${spawnMatch[1]} (${spawnDetails})` : `Starting ${spawnMatch[1]}`,
          isSpawn: true,
        };
      }

      const inlineMatch = cleaned.match(/^→\s+Subagent:\s+(.*)$/);
      if (inlineMatch) {
        const body = summarizeExecutionTarget(inlineMatch[1].trim())
          .replace(/^\[\d+\s*-\s*\d+%\]\s*/i, '')
          .trim();
        return {
          taskId: undefined,
          label: body,
          isSpawn: false,
        };
      }

      return {
        taskId: undefined,
        label: summarizeExecutionTarget(cleaned),
        isSpawn: false,
      };
    },
    [summarizeExecutionTarget],
  );

  // ── Compute subagent runs + resource counts in a single useMemo ──────────────

  return useMemo<UseSubagentRunsResult>(
    () => {
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
        const structuredActivities = subagentActivities
          .filter((activity) => {
            if (activity.toolCallId) {
              return activity.toolCallId === tool.id;
            }
            const ts =
              activity.timestamp instanceof Date
                ? activity.timestamp.getTime()
                : new Date(activity.timestamp).getTime();
            const startMs = tool.startTime.getTime() - 500;
            const endMs = (tool.endTime || new Date()).getTime() + 500;
            return ts >= startMs && ts <= endMs;
          })
          .map((activity) => ({
            id: activity.id,
            timestamp: activity.timestamp,
            taskId: activity.taskId,
            label: activity.message,
            isSpawn: activity.phase === 'spawn',
          }));

        const startMs = tool.startTime.getTime() - 500;
        const endMs = (tool.endTime || new Date()).getTime() + 500;
        const fallbackActivities = subagentLogs
          .filter((logEntry) => {
            const message = getSubagentLogMessage(logEntry);
            if (!message) {
              return false;
            }
            const ts =
              logEntry.timestamp instanceof Date
                ? logEntry.timestamp.getTime()
                : new Date(logEntry.timestamp).getTime();
            return ts >= startMs && ts <= endMs;
          })
          .map((logEntry) => {
            const message = getSubagentLogMessage(logEntry) || '';
            const normalized = normalizeSubagentActivity(message);
            return {
              id: logEntry.id,
              timestamp: logEntry.timestamp,
              taskId: normalized.taskId,
              label: normalized.label,
              isSpawn: normalized.isSpawn,
            };
          })
          .filter((item, index, items) => {
            if (!item.label) {
              return false;
            }
            const previous = items[index - 1];
            return !previous || previous.label !== item.label;
          });
        const activities = structuredActivities.length > 0 ? structuredActivities : fallbackActivities;

        const taskGroups = activities.reduce<Record<string, typeof activities>>(
          (acc, item) => {
            const key = item.taskId || '__main__';
            if (!acc[key]) {
              acc[key] = [];
            }
            acc[key].push(item);
            return acc;
          },
          {},
        );

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
    },
    [
      subagentToolExecutions,
      subagentActivities,
      subagentLogs,
      getSubagentLogMessage,
      normalizeSubagentActivity,
    ],
  );
}
