import { LiveLog } from '@sprout/ui';
import { Square } from 'lucide-react';
import { useState } from 'react';
import { cancelSubagent } from '../../services/api/subagentApi';
import { stripAnsiCodes } from '../../utils/ansi';
import { formatToolDetail } from '../../utils/resultSummary';
import { FilePathPre } from '../contextPanel/FilePathPre';
import type { ContextSubagentRun, LiveLogLine } from '../contextPanel/types';
import './SubagentRunBody.css';

/**
 * A delegated run inside a tool's inline detail: the task it was given, what
 * it is doing now (per task for parallel runs), its live output while it
 * runs, a Stop control, and its result once done.
 */
export function SubagentRunBody({ run }: { run: ContextSubagentRun }) {
  const { tool, prompt, latestActivity, activities, orderedTaskGroups } = run;
  const [cancelling, setCancelling] = useState(false);
  const isActive = tool.status === 'started' || tool.status === 'running';
  const tasks = orderedTaskGroups.filter((group) => group.taskId);
  // One runner task per single run; one per task for a parallel run.
  const runnerTaskIds = Array.from(new Set(activities.map((a) => a.taskId).filter((v): v is string => !!v)));

  const stop = async () => {
    if (cancelling || runnerTaskIds.length === 0) return;
    setCancelling(true);
    try {
      await Promise.allSettled(runnerTaskIds.map((id) => cancelSubagent(window.fetch.bind(window), id)));
    } finally {
      setCancelling(false);
    }
  };

  const outputLines: LiveLogLine[] = activities
    .filter((a) => !a.isSpawn)
    .map((a) => ({
      id: a.id,
      text: a.label,
      timestamp: a.timestamp instanceof Date ? a.timestamp : new Date(a.timestamp),
      taskId: a.taskId,
    }));

  return (
    <div className="subagent-run-body">
      {prompt && (
        <div className="subagent-run-section">
          <div className="subagent-run-label">Task</div>
          <pre className="subagent-run-task">{stripAnsiCodes(prompt)}</pre>
        </div>
      )}
      {isActive && latestActivity && tasks.length === 0 && (
        <div className="subagent-run-now">
          <span className="subagent-run-label">Now</span>
          <span>{latestActivity.label}</span>
        </div>
      )}
      {tasks.length > 0 && (
        <ul className="subagent-run-tasks">
          {tasks.map((group) => (
            <li key={group.taskId}>
              <span className="subagent-run-task-name">{group.taskId}</span>
              <span>{group.latest?.label || 'Waiting…'}</span>
            </li>
          ))}
        </ul>
      )}
      {isActive && outputLines.length > 0 && <LiveLog lines={outputLines} maxLines={50} />}
      {tool.result && (
        <div className="subagent-run-section">
          <div className="subagent-run-label">Result</div>
          <div className="tool-detail-output">
            <FilePathPre text={formatToolDetail(tool.result)} />
          </div>
        </div>
      )}
      {isActive && runnerTaskIds.length > 0 && (
        <div className="subagent-run-actions">
          <button type="button" className="subagent-run-stop" onClick={() => void stop()} disabled={cancelling}>
            <Square size={11} />
            {cancelling ? 'Stopping…' : runnerTaskIds.length > 1 ? `Stop all ${runnerTaskIds.length}` : 'Stop'}
          </button>
        </div>
      )}
    </div>
  );
}
