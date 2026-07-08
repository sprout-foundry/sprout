import { Skeleton } from '@sprout/ui';
import { Download, Loader2, RotateCcw, Search, X } from 'lucide-react';
import PastSessionsHint from '../PastSessionsHint';
import { supportsExport } from '../../config/mode';
import { formatRelativeTime } from './helpers';
import { formatSessionDate, renderSessionExcerpt } from './sessionSearchHelpers';
import './sessionSearch.css';
import type { SessionEntry, SessionSearchResult } from '../../services/api/types/session';

interface SessionsTabProps {
  sessions: SessionEntry[];
  currentSessionId: string;
  isLoadingSessions: boolean;
  sessionRestoreError: string | null;
  loadSessions: () => Promise<void>;
  handleRestoreSession: (sessionId: string) => Promise<void>;
  // Session search (relocated from Sidebar.tsx pinned header)
  sessionSearchQuery: string;
  sessionSearchResults: SessionSearchResult[];
  sessionSearchLoading: boolean;
  sessionSearchError: string | null;
  showSessionSearchDropdown: boolean;
  handleSessionSearchChange: (e: React.ChangeEvent<HTMLInputElement>) => void;
  handleSessionSearchClear: () => void;
  handleSessionSearchBlur: () => void;
  handleSessionSearchFocus: () => void;
  handleSessionSearchResultClick: (sessionId: string) => void;
  // Export-all (relocated from Sidebar.tsx pinned header)
  isExportingAll: boolean;
  exportAllError: string | null;
  handleExportAllSessions: () => Promise<void>;
}

export function SessionsTab({
  sessions,
  currentSessionId,
  isLoadingSessions,
  sessionRestoreError,
  loadSessions,
  handleRestoreSession,
  sessionSearchQuery,
  sessionSearchResults,
  sessionSearchLoading,
  sessionSearchError,
  showSessionSearchDropdown,
  handleSessionSearchChange,
  handleSessionSearchClear,
  handleSessionSearchBlur,
  handleSessionSearchFocus,
  handleSessionSearchResultClick,
  isExportingAll,
  exportAllError,
  handleExportAllSessions,
}: SessionsTabProps) {
  return (
    <div className="context-panel-tools-list context-panel-sessions" data-testid="context-panel-sessions">
      {/* ── Session search (relocated from sidebar pinned header) ── */}
      <div className="sidebar-session-search" data-testid="sidebar-session-search-wrapper">
        <Search size={14} className="sidebar-session-search-icon" strokeWidth={2} />
        <input
          type="text"
          className="sidebar-session-search-input"
          placeholder="Search sessions..."
          value={sessionSearchQuery}
          onChange={handleSessionSearchChange}
          onFocus={handleSessionSearchFocus}
          onBlur={handleSessionSearchBlur}
          data-testid="sidebar-session-search-input"
          aria-label="Search sessions"
        />
        {sessionSearchQuery && (
          <button
            type="button"
            className="sidebar-session-search-clear"
            onClick={handleSessionSearchClear}
            aria-label="Clear search"
            data-testid="sidebar-session-search-clear"
          >
            <X size={12} strokeWidth={2} />
          </button>
        )}
      </div>

      {/* ── Search results dropdown (relocated from sidebar pinned header) ── */}
      {showSessionSearchDropdown && (
        <div className="sidebar-session-search-dropdown" data-testid="sidebar-session-search-dropdown">
          {sessionSearchLoading ? (
            <div className="sidebar-session-search-loading" data-testid="sidebar-session-search-loading">
              <Loader2 size={14} className="sidebar-session-search-spinner" />
              <span>Searching...</span>
            </div>
          ) : sessionSearchError ? (
            <div className="sidebar-session-search-error" data-testid="sidebar-session-search-error">
              {sessionSearchError}
            </div>
          ) : sessionSearchResults.length === 0 && sessionSearchQuery.trim().length > 0 ? (
            <div className="sidebar-session-search-no-results" data-testid="chat-sessions-empty">
              No matching sessions
            </div>
          ) : (
            sessionSearchResults.map((result) => (
              <button
                key={result.session_id}
                type="button"
                className="sidebar-session-search-result"
                onClick={() => handleSessionSearchResultClick(result.session_id)}
                data-testid="chat-item"
                data-session-id={result.session_id}
              >
                <div className="sidebar-session-search-result-header">
                  <span className="sidebar-session-search-result-name" title={result.name || result.session_id}>
                    {result.name || result.session_id}
                  </span>
                  <span className="sidebar-session-search-result-date" title={result.last_updated}>
                    {formatSessionDate(result.last_updated)}
                  </span>
                </div>
                {result.excerpt && (
                  <div className="sidebar-session-search-result-excerpt">
                    {renderSessionExcerpt(result.excerpt)}
                  </div>
                )}
                {result.match_score >= 2 && (
                  <span
                    className="sidebar-session-search-result-score"
                    title={`Match score: ${result.match_score}`}
                  >
                    {result.match_score === 3 ? '★' : '☆'}
                  </span>
                )}
              </button>
            ))
          )}
        </div>
      )}

      {/* ── Export-all button (relocated from sidebar pinned header) ── */}
      {supportsExport && (
        <div className="sidebar-export-all-wrapper">
          <button
            type="button"
            className="sidebar-export-all-btn"
            onClick={handleExportAllSessions}
            disabled={isExportingAll}
            data-testid="sidebar-export-all"
            aria-label="Export all sessions"
            title="Export all sessions"
          >
            {isExportingAll ? (
              <>
                <Loader2 size={14} className="sidebar-export-all-spinner" strokeWidth={2} />
                Exporting...
              </>
            ) : (
              <>
                <Download size={14} strokeWidth={2} />
                Export all
              </>
            )}
          </button>
          {exportAllError && (
            <div className="sidebar-export-all-error" role="alert">
              {exportAllError}
            </div>
          )}
        </div>
      )}

      {/* ── Past-sessions semantic recall (relocated from sidebar pinned header) ── */}
      <PastSessionsHint />

      {/* ── Existing session list ── */}
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