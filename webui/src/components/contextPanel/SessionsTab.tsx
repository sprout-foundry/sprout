import { Skeleton } from '@sprout/ui';
import { RotateCcw } from 'lucide-react';
import { formatRelativeTime } from './helpers';
import type { SessionEntry } from './types';

interface SessionsTabProps {
  sessions: SessionEntry[];
  currentSessionId: string;
  isLoadingSessions: boolean;
  sessionRestoreError: string | null;
  loadSessions: () => Promise<void>;
  handleRestoreSession: (sessionId: string) => Promise<void>;
}

export function SessionsTab({
  sessions,
  currentSessionId,
  isLoadingSessions,
  sessionRestoreError,
  loadSessions,
  handleRestoreSession,
}: SessionsTabProps) {
  return (
    <div className="context-panel-tools-list" data-testid="context-panel-sessions">
      <div className="history-toolbar">
        <button className="history-refresh-btn" onClick={loadSessions} disabled={isLoadingSessions}>
          <RotateCcw size={12} /> Refresh
        </button>
      </div>

      {sessionRestoreError && <div className="history-error-inline">{sessionRestoreError}</div>}

      {isLoadingSessions ? (
        <div className="context-panel-loading" role="status" aria-label="Loading sessions">
          <div className="sessions-skeleton">
            {Array.from({ length: 5 }, (_, i) => (
              <div key={i} className="sessions-skeleton-item">
                <div className="sessions-skeleton-summary">
                  <div className="sessions-skeleton-main">
                    <Skeleton width={`${40 + Math.floor((i * 53) % 50)}%`} height="14px" />
                    <Skeleton width="48px" height="12px" />
                  </div>
                  <div className="sessions-skeleton-stats">
                    <Skeleton width="48px" height="12px" />
                    <Skeleton width="52px" height="12px" />
                  </div>
                </div>
                <div className="sessions-skeleton-meta">
                  <Skeleton width={`${30 + Math.floor((i * 37) % 40)}%`} height="12px" />
                </div>
              </div>
            ))}
          </div>
          <span className="sr-only">Loading sessions...</span>
        </div>
      ) : sessions.length === 0 ? (
        <div className="context-panel-empty">No saved sessions found.</div>
      ) : (
        sessions.map((session) => {
          const isCurrent = session.session_id === currentSessionId;
          const dirName = session.working_directory.split('/').filter(Boolean).slice(-2).join('/');
          return (
            <div key={session.session_id} className={`history-item ${isCurrent ? 'session-current' : ''}`}>
              <div className="session-summary">
                <span className="history-main">
                  <span className="history-id" title={session.session_id}>
                    {session.name || session.session_id}
                  </span>
                  <span className="history-time">{formatRelativeTime(session.last_updated)}</span>
                </span>
                <span className="history-stats">
                  <span>{session.message_count} msgs</span>
                  {session.total_tokens > 0 && <span>{session.total_tokens.toLocaleString()} tok</span>}
                </span>
              </div>
              <div className="session-meta">
                <span className="session-dir" title={session.working_directory}>
                  {dirName}
                </span>
                {isCurrent ? (
                  <span className="session-current-badge">Current</span>
                ) : (
                  <button
                    className="history-rollback-btn"
                    onClick={() => handleRestoreSession(session.session_id)}
                    disabled={isLoadingSessions}
                  >
                    Restore
                  </button>
                )}
              </div>
            </div>
          );
        })
      )}
    </div>
  );
}
