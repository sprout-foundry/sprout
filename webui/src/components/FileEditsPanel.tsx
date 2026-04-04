/**
 * File Edits Panel
 *
 * Displays files edited in this session and revision checkpoints with rollback.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { FC } from 'react';
import { useLog } from '../utils/log';
import {
  Pencil,
  Plus,
  Trash2,
  ArrowLeftRight,
  CircleCheck,
  ArrowDown,
  Undo2,
  File,
  FilePen,
  RotateCcw,
  X,
  TriangleAlert,
  ScrollText,
  Inbox,
  ChevronDown,
  ChevronRight,
} from 'lucide-react';
import { ApiService } from '../services/api';
import { showThemedConfirm, showThemedAlert } from './ThemedDialog';
import './FileEditsPanel.css';

interface FileEdit {
  path: string;
  action: string; // 'edited', 'created', 'deleted', 'renamed'
  timestamp: Date;
  linesAdded?: number;
  linesDeleted?: number;
}

interface RevisionFile {
  path: string;
  operation: string;
  lines_added: number;
  lines_deleted: number;
}

interface Revision {
  revision_id: string;
  timestamp: string;
  files: RevisionFile[];
  description: string;
}

interface FileEditsPanelProps {
  edits: FileEdit[];
  onFileClick?: (filePath: string) => void;
}

const MAX_RECENT_FILE_ROWS = 12;

const normalizeRevision = (raw: unknown): Revision => {
  const r = raw as Record<string, unknown> | null | undefined;
  if (!r) {
    return {
      revision_id: 'unknown',
      timestamp: new Date().toISOString(),
      files: [],
      description: '',
    };
  }
  const files = Array.isArray(r.files)
    ? (r.files as Array<Record<string, unknown>>).map((file: Record<string, unknown>) => ({
        path: typeof file?.path === 'string' ? file.path : 'Unknown',
        operation: typeof file?.operation === 'string' ? file.operation : 'edited',
        lines_added: Number(file?.lines_added || 0),
        lines_deleted: Number(file?.lines_deleted || 0),
      }))
    : [];

  return {
    revision_id: typeof r.revision_id === 'string' ? r.revision_id : 'unknown',
    timestamp: typeof r.timestamp === 'string' ? r.timestamp : new Date().toISOString(),
    files,
    description: typeof r.description === 'string' ? r.description : '',
  };
};

const sortRevisionsNewestFirst = (revisions: Revision[]): Revision[] => {
  return [...revisions].sort((a, b) => {
    const aTime = new Date(a.timestamp).getTime();
    const bTime = new Date(b.timestamp).getTime();
    return bTime - aTime;
  });
};

const FileEditsPanel: FC<FileEditsPanelProps> = ({ edits, onFileClick }) => {
  const log = useLog();
  const [showHistory, setShowHistory] = useState(false);
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [expandedRevisionIds, setExpandedRevisionIds] = useState<Set<string>>(new Set());
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [rollbackError, setRollbackError] = useState<string | null>(null);
  const historyLoadRequestRef = useRef(0);

  const apiService = ApiService.getInstance();

  const openHistory = useCallback(async () => {
    const requestId = ++historyLoadRequestRef.current;
    setIsLoadingHistory(true);
    setRollbackError(null);

    try {
      const response = await apiService.getChangelog();
      if (requestId !== historyLoadRequestRef.current) {
        return;
      }
      const normalized = sortRevisionsNewestFirst((response.revisions || []).map(normalizeRevision));
      setRevisions(normalized);

      // Expand the newest revision by default.
      if (normalized.length > 0) {
        setExpandedRevisionIds(new Set([normalized[0].revision_id]));
      } else {
        setExpandedRevisionIds(new Set());
      }

      setShowHistory(true);
    } catch (error) {
      if (requestId !== historyLoadRequestRef.current) {
        return;
      }
      log.error(`Failed to fetch changelog: ${error instanceof Error ? error.message : String(error)}`, { title: 'Changelog Error' });
      setRollbackError('Failed to fetch revision history');
      setShowHistory(true);
    } finally {
      if (requestId === historyLoadRequestRef.current) {
        setIsLoadingHistory(false);
      }
    }
  }, [apiService]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    const onOpenHistory = () => {
      openHistory();
    };

    window.addEventListener('ledit:open-revision-history', onOpenHistory);
    return () => window.removeEventListener('ledit:open-revision-history', onOpenHistory);
  }, [openHistory]);

  const handleRollback = async (revisionId: string) => {
    if (
      !(await showThemedConfirm(
        `Rollback to revision ${revisionId}?\n\nThis will undo all changes made after this revision.`,
        { title: 'Confirm Rollback', type: 'danger' },
      ))
    ) {
      return;
    }

    setIsLoadingHistory(true);
    setRollbackError(null);

    try {
      await apiService.rollbackToRevision(revisionId);
      await showThemedAlert(`Successfully rolled back to revision ${revisionId}`, {
        title: 'Rollback Complete',
        type: 'success',
      });
      setShowHistory(false);
      window.location.reload();
    } catch (error) {
      log.error(`Rollback failed: ${error instanceof Error ? error.message : String(error)}`, { title: 'Rollback Error' });
      setRollbackError(error instanceof Error ? error.message : 'Rollback failed');
    } finally {
      setIsLoadingHistory(false);
    }
  };

  const toggleRevisionExpanded = (revisionId: string) => {
    setExpandedRevisionIds((prev) => {
      const next = new Set(prev);
      if (next.has(revisionId)) {
        next.delete(revisionId);
      } else {
        next.add(revisionId);
      }
      return next;
    });
  };

  const getActionIcon = (action: string) => {
    switch (action) {
      case 'edited':
        return <Pencil size={14} />;
      case 'created':
        return <Plus size={14} />;
      case 'deleted':
        return <Trash2 size={14} />;
      case 'renamed':
        return <ArrowLeftRight size={14} />;
      case 'git_stage':
        return <CircleCheck size={14} />;
      case 'git_unstage':
        return <ArrowDown size={14} />;
      case 'git_discard':
        return <Undo2 size={14} />;
      default:
        return <File size={14} />;
    }
  };

  const getActionText = (action: string) => {
    switch (action) {
      case 'edited':
        return 'Modified';
      case 'created':
        return 'Created';
      case 'deleted':
        return 'Deleted';
      case 'renamed':
        return 'Renamed';
      case 'git_stage':
        return 'Staged';
      case 'git_unstage':
        return 'Unstaged';
      case 'git_discard':
        return 'Discarded';
      default:
        return action;
    }
  };

  const getOperationText = (operation: string) => {
    switch (operation) {
      case 'edited':
        return 'Modified';
      case 'created':
        return 'Created';
      case 'deleted':
        return 'Deleted';
      case 'renamed':
        return 'Renamed';
      default:
        return operation;
    }
  };

  const formatRelativeTime = (value: Date | string) => {
    const date = value instanceof Date ? value : new Date(value);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSecs = Math.max(0, Math.floor(diffMs / 1000));
    const diffMins = Math.floor(diffSecs / 60);
    const diffHours = Math.floor(diffMins / 60);

    if (diffSecs < 60) return `${diffSecs}s ago`;
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;

    return date.toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const latestEditsByFile = useMemo(() => {
    const latest = new Map<string, FileEdit>();
    edits.forEach((edit) => {
      const existing = latest.get(edit.path);
      if (!existing || edit.timestamp > existing.timestamp) {
        latest.set(edit.path, edit);
      }
    });
    return latest;
  }, [edits]);

  const sortedEdits = useMemo(() => {
    return Array.from(latestEditsByFile.values()).sort((a, b) => b.timestamp.getTime() - a.timestamp.getTime());
  }, [latestEditsByFile]);

  const visibleEdits = sortedEdits.slice(0, MAX_RECENT_FILE_ROWS);
  const hiddenEditCount = Math.max(0, sortedEdits.length - visibleEdits.length);

  const summarizeRevision = (revision: Revision) => {
    let additions = 0;
    let deletions = 0;
    revision.files.forEach((file) => {
      additions += Number(file.lines_added || 0);
      deletions += Number(file.lines_deleted || 0);
    });

    return {
      fileCount: revision.files.length,
      additions,
      deletions,
    };
  };

  return (
    <div className="file-edits-panel">
      <div className="edits-header">
        <h4>
          <FilePen size={14} /> File Edits
        </h4>
        <div className="edits-header-meta">{sortedEdits.length} tracked files</div>
        <button
          onClick={openHistory}
          disabled={isLoadingHistory}
          className="history-button"
          title="Open revision history"
        >
          {isLoadingHistory ? (
            'Loading...'
          ) : (
            <>
              <RotateCcw size={14} /> Revision History
            </>
          )}
        </button>
      </div>

      {sortedEdits.length === 0 ? (
        <div className="edits-list">
          <div className="edit-item muted">
            <span className="edit-icon">
              <Inbox size={16} />
            </span>
            <span className="edit-info">
              <span className="file-name">No file edits yet</span>
              <span className="file-dir">Start editing to build a revision trail</span>
            </span>
          </div>
        </div>
      ) : (
        <div className="edits-list">
          {visibleEdits.map((edit) => {
            const fileName = edit.path.split('/').pop() || edit.path;
            const slashIndex = edit.path.lastIndexOf('/');
            const fileDir = slashIndex >= 0 ? edit.path.substring(0, slashIndex) : '';

            return (
              <div
                key={edit.path}
                className="edit-item"
                onClick={() => onFileClick?.(edit.path)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onFileClick?.(edit.path);
                  }
                }}
                role="button"
                tabIndex={0}
                title={edit.path}
              >
                <span className="edit-icon">{getActionIcon(edit.action)}</span>
                <span className="edit-info">
                  <span className="file-name">{fileName}</span>
                  {fileDir && <span className="file-dir">{fileDir}</span>}
                </span>
                <span className="edit-action">{getActionText(edit.action)}</span>
                {(edit.linesAdded !== undefined || edit.linesDeleted !== undefined) && (
                  <span className="edit-diff">
                    {edit.linesAdded !== undefined && edit.linesAdded > 0 && (
                      <span className="lines-added">+{edit.linesAdded}</span>
                    )}
                    {edit.linesDeleted !== undefined && edit.linesDeleted > 0 && (
                      <span className="lines-deleted">-{edit.linesDeleted}</span>
                    )}
                  </span>
                )}
                <span className="edit-time">{formatRelativeTime(edit.timestamp)}</span>
              </div>
            );
          })}

          {hiddenEditCount > 0 && (
            <div className="edits-overflow-note">
              +{hiddenEditCount} more tracked file{hiddenEditCount === 1 ? '' : 's'}
            </div>
          )}
        </div>
      )}

      {showHistory && (
        <div className="history-modal-overlay" onClick={() => setShowHistory(false)}>
          <div className="history-modal" onClick={(e) => e.stopPropagation()}>
            <div className="history-modal-header">
              <h3>
                <RotateCcw size={16} /> Revision History
              </h3>
              <button className="close-button" onClick={() => setShowHistory(false)} title="Close">
                <X size={14} />
              </button>
            </div>

            {rollbackError && (
              <div className="history-error">
                <span className="error-icon">
                  <TriangleAlert size={14} />
                </span>
                <span>{rollbackError}</span>
              </div>
            )}

            <div className="history-content">
              {revisions.length === 0 ? (
                <div className="history-empty">
                  <span className="empty-icon">
                    <ScrollText size={16} />
                  </span>
                  <p>No revision history available</p>
                  <p className="empty-hint">Make some changes to create checkpoints</p>
                </div>
              ) : (
                <div className="revisions-list">
                  {revisions.map((revision) => {
                    const summary = summarizeRevision(revision);
                    const isExpanded = expandedRevisionIds.has(revision.revision_id);

                    return (
                      <div key={revision.revision_id} className="revision-item">
                        <button
                          className="revision-summary"
                          onClick={() => toggleRevisionExpanded(revision.revision_id)}
                          aria-expanded={isExpanded}
                        >
                          <span className="revision-expand-icon">
                            {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                          </span>
                          <span className="revision-main">
                            <span className="revision-id">{revision.revision_id}</span>
                            <span className="revision-time">{formatRelativeTime(revision.timestamp)}</span>
                          </span>
                          <span className="revision-stats">
                            <span className="stats-chip">{summary.fileCount} files</span>
                            {summary.additions > 0 && (
                              <span className="stats-chip additions">+{summary.additions}</span>
                            )}
                            {summary.deletions > 0 && (
                              <span className="stats-chip deletions">-{summary.deletions}</span>
                            )}
                          </span>
                        </button>

                        {isExpanded && (
                          <div className="revision-details">
                            {revision.description && <div className="revision-description">{revision.description}</div>}
                            <div className="revision-file-list">
                              {revision.files.map((file, fileIndex) => (
                                <div
                                  key={`${revision.revision_id}-${file.path}-${fileIndex}`}
                                  className="revision-file-row"
                                >
                                  <span className={`file-badge file-${file.operation}`}>
                                    {getOperationText(file.operation)}
                                  </span>
                                  <span className="file-path-small" title={file.path}>
                                    {file.path}
                                  </span>
                                  <span className="file-diff-small">
                                    {file.lines_added > 0 && <span className="additions">+{file.lines_added}</span>}
                                    {file.lines_deleted > 0 && <span className="deletions">-{file.lines_deleted}</span>}
                                  </span>
                                </div>
                              ))}
                            </div>
                            <div className="revision-actions">
                              <button
                                onClick={() => handleRollback(revision.revision_id)}
                                className="rollback-button"
                                disabled={isLoadingHistory}
                              >
                                <RotateCcw size={14} /> Rollback To This Revision
                              </button>
                            </div>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default FileEditsPanel;
