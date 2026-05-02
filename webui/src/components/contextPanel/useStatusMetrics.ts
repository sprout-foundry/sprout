import { useMemo } from 'react';
import { debugLog } from '../../utils/log';
import type {
  ChatContextPanelProps,
  StatusMetrics,
  ToolExecution,
} from './types';
import { isSubagentTool } from './helpers';

export function useStatusMetrics(
  chatProps: ChatContextPanelProps | null,
  toolExecutions: ToolExecution[],
  maxQueryId: number,
) {
  const currentTurnTools = useMemo(
    () => toolExecutions.filter((t) => (t.queryId ?? 0) === maxQueryId),
    [toolExecutions, maxQueryId],
  );

  const chatMessages = useMemo(() => chatProps?.messages ?? [], [chatProps]);
  const chatFileEdits = useMemo(() => chatProps?.fileEdits ?? [], [chatProps]);

  return useMemo((): StatusMetrics => {
    if (!chatProps) {
      return {
        userMsgs: 0,
        assistantMsgs: 0,
        totalMsgs: 0,
        completedTools: 0,
        failedTools: 0,
        activeTools: 0,
        totalTools: 0,
        totalAdditions: 0,
        totalDeletions: 0,
        filesTouched: 0,
        topTools: [],
        maxToolCount: 1,
        duration: 0,
      };
    }

    const msgs = chatMessages;
    const userMsgs = msgs.filter((m) => m.type === 'user').length;
    const assistantMsgs = msgs.filter((m) => m.type === 'assistant').length;
    const completedTools = currentTurnTools.filter((t) => t.status === 'completed').length;
    const failedTools = currentTurnTools.filter((t) => t.status === 'error').length;
    const activeTools = currentTurnTools.filter((t) => t.status === 'running' || t.status === 'started').length;

    const totalAdditions = chatFileEdits.reduce((sum, edit) => sum + (edit.linesAdded || 0), 0);
    const totalDeletions = chatFileEdits.reduce((sum, edit) => sum + (edit.linesDeleted || 0), 0);

    const touchedFiles = new Set<string>();
    currentTurnTools.forEach((t) => {
      if (t.tool === 'write_file' || t.tool === 'edit_file') {
        try {
          const args = t.arguments ? JSON.parse(t.arguments) : {};
          if (args.path) touchedFiles.add(args.path);
        } catch (err) {
          debugLog('[summaryStats] parse tool arguments failed:', err);
        }
      }
    });
    chatFileEdits.forEach((edit) => {
      if (edit.path) {
        touchedFiles.add(edit.path);
      }
    });

    const toolCounts: Record<string, number> = {};
    currentTurnTools.forEach((t) => {
      const name = isSubagentTool(t) ? 'subagent' : t.tool;
      toolCounts[name] = (toolCounts[name] || 0) + 1;
    });
    const sortedTools = Object.entries(toolCounts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 6);
    const maxToolCount = sortedTools.length > 0 ? sortedTools[0][1] : 1;

    let duration = 0;
    if (msgs.length >= 2) {
      duration = msgs[msgs.length - 1].timestamp.getTime() - msgs[0].timestamp.getTime();
    }

    return {
      userMsgs,
      assistantMsgs,
      totalMsgs: userMsgs + assistantMsgs,
      completedTools,
      failedTools,
      activeTools,
      totalTools: currentTurnTools.length,
      totalAdditions,
      totalDeletions,
      filesTouched: touchedFiles.size,
      topTools: sortedTools,
      maxToolCount,
      duration,
    };
  }, [currentTurnTools, chatMessages, chatFileEdits]);
}
