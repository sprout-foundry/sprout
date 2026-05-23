import { LiveLog, groupSubagentRuns, getPersonaColor } from '@sprout/ui';
import { Bot, CheckCircle2, XCircle, ChevronDown, ChevronRight, Loader2 } from 'lucide-react';
import type { CSSProperties } from 'react';
import { useState, useMemo } from 'react';
import type { SubagentActivity, SubagentRun } from './types';
import { MAX_ACTIVE_LINES, MAX_COMPLETED_SUMMARIES } from './types';

// SP-053-1a: getPersonaColor + PERSONA_COLORS now live in @sprout/ui so the
// chat-bubble badges, the tool timeline, and this activity feed all read
// from one source. Re-exported here for any older importers in this package
// that referenced this module directly.
export { getPersonaColor };

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
