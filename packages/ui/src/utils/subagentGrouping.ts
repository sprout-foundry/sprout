/**
 * Subagent grouping utilities for chat activity feeds.
 *
 * This module provides functions for grouping individual subagent activities
 * into coherent "runs" that track the lifecycle of a subagent execution.
 */

import type { SubagentActivity, SubagentRun } from '../types/chat';

/**
 * Groups an array of SubagentActivity objects into SubagentRun objects.
 *
 * Activities are grouped by their `toolCallId` (or `id` as fallback).
 * Each run tracks:
 * - All activities in the run
 * - The spawn and complete activities
 * - Output lines (split from multi-line output messages)
 * - Completion status and message
 *
 * @param activities - Array of subagent activities to group
 * @returns Array of grouped SubagentRun objects
 */
export function groupSubagentRuns(activities: SubagentActivity[]): SubagentRun[] {
  const runMap = new Map<string, SubagentRun>();

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
      // Split multi-line batched messages into individual lines
      const lines = activity.message.split('\n').filter((l) => l.trim());
      for (const line of lines) {
        run.outputLines.push({
          id: `${activity.id}-${run.outputLines.length}`,
          text: line.trim(),
          timestamp: activity.timestamp,
          taskId: activity.taskId,
        });
      }
    }

    // Aggregate resource usage from each activity
    if (activity.tokensUsed) {
      run.tokensUsed += activity.tokensUsed;
    }
    if (activity.cost) {
      run.cost += activity.cost;
    }
  }

  return Array.from(runMap.values());
}
