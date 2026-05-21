import { LiveLog } from '@sprout/ui';
import { Bot, ChevronDown, ChevronRight, BarChart3 } from 'lucide-react';
import React from 'react';
import { stripAnsiCodes } from '../../utils/ansi';
import { getSubagentResultPreview, formatToolDetail } from '../../utils/resultSummary';
import { getPersonaColor, getStatusIcon, formatDuration, formatTime } from './helpers';
import type { ContextSubagentRun, LiveLogLine, SubagentLifecycleCounts } from './types';

interface SubagentsTabProps {
  subagentRuns: ContextSubagentRun[];
  lifecycleCounts?: SubagentLifecycleCounts;
  totalLifecycle?: number;
  expandedSubagents: Set<string>;
  toolRefs: React.MutableRefObject<Record<string, HTMLDivElement | null>>;
  expandedTools: Set<string>;
  expandedQueries: Set<number>;
  setActiveToolId: (v: string | null) => void;
  setChatTab: (v: 'subagents' | 'tools' | 'changes' | 'tasks' | 'status' | 'sessions') => void;
  setExpandedTools: React.Dispatch<React.SetStateAction<Set<string>>>;
  setExpandedQueries: React.Dispatch<React.SetStateAction<Set<number>>>;
  toggleSubagentExpansion: (toolId: string) => void;
}

export function SubagentsTab({
  subagentRuns,
  lifecycleCounts,
  totalLifecycle,
  expandedSubagents,
  toolRefs,
  expandedTools: _expandedTools,
  expandedQueries: _expandedQueries,
  setActiveToolId,
  setChatTab,
  setExpandedTools,
  setExpandedQueries,
  toggleSubagentExpansion,
}: SubagentsTabProps) {
  // Auto-scroll live subagent activity lists
  const liveActivityListRef = React.useRef<HTMLDivElement | null>(null);
  const liveActivityScrollTimeoutRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    const el = liveActivityListRef.current;
    if (!el) return;
    if (liveActivityScrollTimeoutRef.current) {
      clearTimeout(liveActivityScrollTimeoutRef.current);
    }
    liveActivityScrollTimeoutRef.current = setTimeout(() => {
      el.scrollTop = el.scrollHeight;
    }, 150);
    return () => {
      if (liveActivityScrollTimeoutRef.current) {
        clearTimeout(liveActivityScrollTimeoutRef.current);
      }
    };
  }, [subagentRuns]);

  return (
    <div className="context-panel-tools-list">
      {totalLifecycle != null && totalLifecycle > 0 && lifecycleCounts && (
        <div
          className="subagent-lifecycle-summary"
          style={{
            background: 'rgba(255,255,255,0.03)',
            borderRadius: '6px',
            padding: '8px 12px',
            marginBottom: '12px',
            fontSize: '12px',
            color: 'rgba(255,255,255,0.6)',
            display: 'flex',
            gap: '12px',
            flexWrap: 'wrap',
          }}
        >
          {lifecycleCounts.active > 0 && (
            <span style={{ color: '#4ade80' }}>
              ▶ {lifecycleCounts.active} active
            </span>
          )}
          {lifecycleCounts.queued > 0 && (
            <span style={{ color: '#fbbf24' }}>
              ⏳ {lifecycleCounts.queued} queued
            </span>
          )}
          {lifecycleCounts.completed > 0 && (
            <span style={{ color: '#4ade80' }}>
              ✓ {lifecycleCounts.completed} completed
            </span>
          )}
          {lifecycleCounts.cancelled > 0 && (
            <span style={{ color: '#f87171' }}>
              ✕ {lifecycleCounts.cancelled} cancelled
            </span>
          )}
        </div>
      )}
      {subagentRuns.length === 0 ? (
        <div className="context-panel-empty">
          Delegated work will appear here when the orchestrator runs <code>run_subagent</code> or{' '}
          <code>run_parallel_subagents</code>.
        </div>
      ) : (
        subagentRuns.map(({ tool, prompt, latestActivity, activities, orderedTaskGroups }) => {
          const isActive = tool.status === 'started' || tool.status === 'running';
          const expanded = expandedSubagents.has(tool.id) || isActive;
          const isParallel = tool.subagentType === 'parallel';
          const collapsedActivities = activities.slice(isActive ? -10 : -3);
          const visibleActivities = expanded ? activities : collapsedActivities;
          const taskCount = orderedTaskGroups.filter((group) => group.taskId).length;
          const hiddenActivityCount = Math.max(0, activities.length - visibleActivities.length);
          const resultPreview = getSubagentResultPreview(tool.result);
          const lastUpdatedAt = latestActivity?.timestamp || tool.endTime || tool.startTime;

          const outputLines: LiveLogLine[] = activities
            .filter((a) => !a.isSpawn)
            .map((a) => ({
              id: a.id,
              text: a.label,
              timestamp: a.timestamp instanceof Date ? a.timestamp : new Date(a.timestamp),
              taskId: a.taskId,
            }));

          return (
            <section key={tool.id} className={`subagent-card tool-${tool.status}`}>
              <button
                className="subagent-card-header"
                onClick={() => toggleSubagentExpansion(tool.id)}
                aria-expanded={expanded}
              >
                <span className="subagent-card-title-row">
                  <span className="subagent-card-icon" style={{ color: getPersonaColor(tool.persona) }}>
                    <Bot size={14} />
                  </span>
                  <span className="subagent-card-title">
                    {tool.persona || (isParallel ? 'parallel subagents' : 'subagent')}
                  </span>
                  {isParallel && <span className="subagent-kind-badge">parallel</span>}
                </span>
                <span className="subagent-card-meta">
                  <span className="subagent-card-status">{getStatusIcon(tool.status)}</span>
                  <span className="tool-duration">{formatDuration(tool.startTime, tool.endTime)}</span>
                  <span className="tool-expand">
                    {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                  </span>
                </span>
              </button>

              {prompt && <div className="subagent-prompt-preview">{stripAnsiCodes(prompt)}</div>}

              <div className="subagent-card-stats">
                <span className="subagent-stat-chip">
                  {activities.length} {activities.length === 1 ? 'update' : 'updates'}
                </span>
                {isParallel && taskCount > 0 && (
                  <span className="subagent-stat-chip">
                    {taskCount} {taskCount === 1 ? 'task' : 'tasks'}
                  </span>
                )}
                <span className="subagent-stat-chip">Updated {formatTime(lastUpdatedAt)}</span>
              </div>

              {latestActivity && (
                <div className="subagent-current-step">
                  <span className="subagent-current-label">Now</span>
                  <span className="subagent-current-text">{latestActivity.label}</span>
                </div>
              )}

              {isParallel && orderedTaskGroups.filter((group) => group.taskId).length > 0 && (
                <div className="subagent-task-groups">
                  {orderedTaskGroups
                    .filter((group) => group.taskId)
                    .map((group) => (
                      <div key={group.taskId || 'main'} className="subagent-task-card">
                        <div className="subagent-task-name">{group.taskId}</div>
                        <div className="subagent-task-summary">{group.latest?.label || 'Waiting...'}</div>
                      </div>
                    ))}
                </div>
              )}

              {isActive && outputLines.length > 0 && (
                <LiveLog lines={outputLines} maxLines={50} className="subagent-sidebar-log" />
              )}

              {visibleActivities.length > 0 && (
                <div
                  ref={isActive ? liveActivityListRef : undefined}
                  className={`subagent-activity-list ${isActive ? 'subagent-activity-live' : ''}`}
                >
                  {visibleActivities.map((activity) => (
                    <div key={activity.id} className="subagent-activity-item">
                      <span className={`subagent-activity-dot ${activity.isSpawn ? 'spawn' : ''}`} />
                      <div className="subagent-activity-body">
                        <div className="subagent-activity-text">
                          {activity.taskId && <span className="subagent-task-pill">{activity.taskId}</span>}
                          <span>{activity.label}</span>
                        </div>
                        <div className="subagent-activity-time">{formatTime(activity.timestamp)}</div>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {hiddenActivityCount > 0 && !expanded && (
                <div className="subagent-collapsed-note">
                  Showing the latest {visibleActivities.length} of {activities.length} updates
                </div>
              )}

              {resultPreview && (
                <div className="subagent-result-preview">
                  <div className="tool-detail-label">
                    <BarChart3 size={12} className="inline-icon" /> Result preview
                  </div>
                  <div className="subagent-result-preview-text">{resultPreview}</div>
                </div>
              )}

              {tool.result && expanded && (
                <div className="subagent-result-snippet">
                  <div className="tool-detail-label">
                    <BarChart3 size={12} className="inline-icon" /> Result
                  </div>
                  <pre>{formatToolDetail(tool.result)}</pre>
                </div>
              )}

              <div className="subagent-card-actions">
                {activities.length > 3 && (
                  <button className="subagent-link-btn" onClick={() => toggleSubagentExpansion(tool.id)}>
                    {expanded ? 'Show fewer updates' : 'Show all updates'}
                  </button>
                )}
                <button
                  className="subagent-link-btn"
                  onClick={() => {
                    setChatTab('tools');
                    setActiveToolId(tool.id);
                    setExpandedTools((prev) => new Set(prev).add(tool.id));
                    const qid = tool.queryId ?? 0;
                    setExpandedQueries((prev) => {
                      if (prev.has(qid)) return prev;
                      const next = new Set(prev);
                      next.add(qid);
                      return next;
                    });
                    setTimeout(() => {
                      const el = toolRefs.current[tool.id];
                      if (el != null) {
                        el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
                      }
                    }, 150);
                  }}
                >
                  View raw tool details
                </button>
              </div>
            </section>
          );
        })
      )}
    </div>
  );
}
