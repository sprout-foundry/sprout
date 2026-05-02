import { useState, useMemo, CSSProperties } from 'react';
import {
  Bot,
  CheckCircle2,
  XCircle,
  ChevronDown,
  ChevronRight,
  Loader2,
} from 'lucide-react';
import { LiveLog } from '@sprout/ui';
import type { SubagentActivity, SubagentRun } from './types';
import { MAX_ACTIVE_LINES, MAX_COMPLETED_SUMMARIES } from './types';

// ── Utility Functions ────────────────────────────────────────────────

export const groupSubagentRuns = (activities: SubagentActivity[]): SubagentRun[] => {
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
  }

  return Array.from(runMap.values());
};

const PERSONA_COLORS: Record<string, string> = {
  coder: '#58a6ff',
  reviewer: '#d2a8ff',
  code_reviewer: '#d2a8ff',
  tester: '#7ee787',
  debugger: '#f0883e',
  refactor: '#79c0ff',
  researcher: '#ff7b72',
  general: '#8b949e',
};

export const getPersonaColor = (persona?: string): string => {
  return PERSONA_COLORS[persona || ''] || '#8b949e';
};

export const formatDuration = (start: Date, end?: Date): string => {
  const ms = (end || new Date()).getTime() - start.getTime();
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
};

// ── Active Subagent Card ─────────────────────────────────────────────

interface ActiveSubagentCardProps {
  run: SubagentRun;
}

function ActiveSubagentCard({ run }: ActiveSubagentCardProps): JSX.Element {
  const [expanded, setExpanded] = useState(true);
  const color = getPersonaColor(run.persona);
  const startTime = run.spawnActivity?.timestamp || run.activities[0]?.timestamp;
  const hasOutput = run.outputLines.length > 0;

  return (
    <div
      className="subagent-feed-card subagent-feed-card--active"
      style={{ '--feed-persona-color': color } as CSSProperties}
    >
      <button
        className="subagent-feed-card-header"
        onClick={() => hasOutput && setExpanded((prev) => !prev)}
        type="button"
        aria-expanded={expanded}
      >
        <span className="subagent-feed-card-left">
          <span className="subagent-feed-status-dot subagent-feed-status-dot--active">
            <Loader2 size={8} />
          </span>
          <Bot size={13} className="subagent-feed-card-icon" />
          <span className="subagent-feed-persona">{run.persona}</span>
          {run.isParallel && <span className="subagent-feed-badge">parallel</span>}
        </span>
        <span className="subagent-feed-card-right">
          {run.outputLines.length > 0 && (
            <span className="subagent-feed-line-count">{run.outputLines.length} lines</span>
          )}
          {startTime && <span className="subagent-feed-duration">{formatDuration(startTime)}</span>}
          {hasOutput && (
            <span className="subagent-feed-toggle">
              {expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
            </span>
          )}
        </span>
      </button>

      {expanded && hasOutput && <LiveLog lines={run.outputLines} maxLines={MAX_ACTIVE_LINES} />}
    </div>
  );
}

// ── Completed Subagent Card ──────────────────────────────────────────

interface CompletedSubagentCardProps {
  run: SubagentRun;
}

function CompletedSubagentCard({ run }: CompletedSubagentCardProps): JSX.Element {
  const hasFailures =
    run.completionMessage?.toLowerCase().includes('fail') || run.completionMessage?.toLowerCase().includes('error');

  return (
    <div className="subagent-feed-card subagent-feed-card--completed">
      <span className="subagent-feed-status-dot subagent-feed-status-dot--completed">
        {hasFailures ? <XCircle size={9} /> : <CheckCircle2 size={9} />}
      </span>
      <Bot size={13} className="subagent-feed-card-icon" style={{ color: getPersonaColor(run.persona) }} />
      <span className="subagent-feed-persona">{run.persona}</span>
      {run.isParallel && <span className="subagent-feed-badge">parallel</span>}
      <span className="subagent-feed-sep">·</span>
      <span className={`subagent-feed-result ${hasFailures ? 'subagent-feed-result--fail' : ''}`}>
        {run.completionMessage || 'Completed'}
      </span>
      {run.spawnActivity?.timestamp && (
        <>
          <span className="subagent-feed-sep">·</span>
          <span className="subagent-feed-duration">
            {formatDuration(run.spawnActivity.timestamp, run.completionTimestamp || undefined)}
          </span>
        </>
      )}
    </div>
  );
}

// ── Subagent Activity Feed ───────────────────────────────────────────

interface SubagentActivityFeedProps {
  activities: SubagentActivity[];
}

export function SubagentActivityFeed({ activities }: SubagentActivityFeedProps): JSX.Element | null {
  const [visible, setVisible] = useState(true);

  const runs = useMemo(() => groupSubagentRuns(activities), [activities]);

  const activeRuns = useMemo(() => runs.filter((r) => !r.isComplete), [runs]);
  const completedRuns = useMemo(() => runs.filter((r) => r.isComplete).slice(-MAX_COMPLETED_SUMMARIES), [runs]);

  const hasContent = activeRuns.length > 0 || completedRuns.length > 0;
  if (!hasContent) return null;

  return (
    <div className={`subagent-feed ${visible ? '' : 'subagent-feed--collapsed'}`}>
      <button
        className="subagent-feed-toggle-bar"
        onClick={() => setVisible((prev) => !prev)}
        type="button"
        aria-expanded={visible}
      >
        <span className="subagent-feed-toggle-left">
          <Bot size={14} />
          <span className="subagent-feed-toggle-label">Subagent Activity</span>
          {activeRuns.length > 0 && (
            <span className="subagent-feed-active-badge">
              {activeRuns.length === 1 ? '1 active' : `${activeRuns.length} active`}
            </span>
          )}
        </span>
        <span className="subagent-feed-toggle-right">
          {visible ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </span>
      </button>

      {visible && (
        <div className="subagent-feed-body">
          {activeRuns.map((run) => (
            <ActiveSubagentCard key={run.toolCallId} run={run} />
          ))}
          {completedRuns.map((run) => (
            <CompletedSubagentCard key={run.toolCallId} run={run} />
          ))}
        </div>
      )}
    </div>
  );
}
