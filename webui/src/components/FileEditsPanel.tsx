/**
 * File Edits Panel
 *
 * Displays files that have been edited during the session
 */

import React, { useState } from 'react';
import { ApiService } from '../services/api';
import './FileEditsPanel.css';

interface FileEdit {
  path: string;
  action: string; // 'edited', 'created', 'deleted', 'renamed'
  timestamp: Date;
  linesAdded?: number;
  linesDeleted?: number;
}

interface FileEditsPanelProps {
  edits: FileEdit[];
  onFileClick?: (filePath: string) => void;
}

const FileEditsPanel: React.FC<FileEditsPanelProps> = ({ edits, onFileClick }) => {
  const [showHistory, setShowHistory] = useState(false);
  const [revisions, setRevisions] = useState<any[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [rollbackError, setRollbackError] = useState<string | null>(null);

  const apiService = ApiService.getInstance();

  const handleViewHistory = async () => {
    setIsLoadingHistory(true);
    setRollbackError(null);
    try {
      const response = await apiService.getChangelog();
      setRevisions(response.revisions);
      setShowHistory(true);
    } catch (error) {
      console.error('Failed to fetch changelog:', error);
      setRollbackError('Failed to fetch revision history');
    } finally {
      setIsLoadingHistory(false);
    }
  };

  const handleRollback = async (revisionId: string) => {
    if (!window.confirm(`Are you sure you want to rollback to revision ${revisionId}?\n\nThis will undo all changes made after this revision.`)) {
      return;
    }

    setIsLoadingHistory(true);
    setRollbackError(null);
    try {
      await apiService.rollbackToRevision(revisionId);
      alert(`Successfully rolled back to revision ${revisionId}`);
      setShowHistory(false);
      // Reload the page to show the rolled back state
      window.location.reload();
    } catch (error) {
      console.error('Rollback failed:', error);
      setRollbackError(error instanceof Error ? error.message : 'Rollback failed');
    } finally {
      setIsLoadingHistory(false);
    }
  };
  const getActionIcon = (action: string) => {
    switch (action) {
      case 'edited': return '📝';
      case 'created': return '➕';
      case 'deleted': return '🗑️';
      case 'renamed': return '🔄';
      case 'git_stage': return '✅';
      case 'git_unstage': return '⬇️';
      case 'git_discard': return '↩️';
      default: return '📄';
    }
  };

  const getActionText = (action: string) => {
    switch (action) {
      case 'edited': return 'Modified';
      case 'created': return 'Created';
      case 'deleted': return 'Deleted';
      case 'renamed': return 'Renamed';
      case 'git_stage': return 'Staged';
      case 'git_unstage': return 'Unstaged';
      case 'git_discard': return 'Discarded';
      default: return action;
    }
  };

  const formatTime = (date: Date) => {
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSecs = Math.floor(diffMs / 1000);
    const diffMins = Math.floor(diffSecs / 60);

    if (diffSecs < 60) {
      return `${diffSecs}s ago`;
    } else if (diffMins < 60) {
      return `${diffMins}m ago`;
    } else {
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }
  };

  if (edits.length === 0) {
    return (
      <div className="file-edits-panel empty">
        <div className="empty-state">
          <span className="empty-icon">📝</span>
          <span className="empty-text">No edits yet</span>
        </div>
      </div>
    );
  }

  // Group edits by file path, showing only the most recent action
  const latestEditsByFile = new Map<string, FileEdit>();
  edits.forEach(edit => {
    const existing = latestEditsByFile.get(edit.path);
    if (!existing || edit.timestamp > existing.timestamp) {
      latestEditsByFile.set(edit.path, edit);
    }
  });

  const sortedEdits = Array.from(latestEditsByFile.values()).sort(
    (a, b) => b.timestamp.getTime() - a.timestamp.getTime()
  );

  return (
    <div className="file-edits-panel">
      <div className="edits-header">
        <h4>📝 File Edits ({sortedEdits.length})</h4>
        <button
          onClick={handleViewHistory}
          disabled={isLoadingHistory}
          className="history-button"
          title="View revision history and rollback"
        >
          {isLoadingHistory ? 'Loading...' : '⏪ History'}
        </button>
      </div>
      <div className="edits-list">
        {sortedEdits.map((edit, index) => {
          const fileName = edit.path.split('/').pop() || edit.path;
          const fileDir = edit.path.substring(0, edit.path.lastIndexOf('/'));

          return (
            <div
              key={index}
              className="edit-item"
              onClick={() => onFileClick?.(edit.path)}
              role="button"
              tabIndex={0}
              title={edit.path}
            >
              <span className="edit-icon">{getActionIcon(edit.action)}</span>
              <span className="edit-info">
                <span className="file-name">{fileName}</span>
                {fileDir && (
                  <span className="file-dir">{fileDir}</span>
                )}
              </span>
              <span className="edit-action">{getActionText(edit.action)}</span>
              <span className="edit-time">{formatTime(edit.timestamp)}</span>
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
            </div>
          );
        })}
      </div>

      {/* History/Rollback Modal */}
      {showHistory && (
        <div className="history-modal-overlay" onClick={() => setShowHistory(false)}>
          <div className="history-modal" onClick={(e) => e.stopPropagation()}>
            <div className="history-modal-header">
              <h3>⏪ Revision History</h3>
              <button
                className="close-button"
                onClick={() => setShowHistory(false)}
                title="Close"
              >
                ✕
              </button>
            </div>

            {rollbackError && (
              <div className="history-error">
                <span className="error-icon">⚠️</span>
                <span>{rollbackError}</span>
              </div>
            )}

            <div className="history-content">
              {revisions.length === 0 ? (
                <div className="history-empty">
                  <span className="empty-icon">📜</span>
                  <p>No revision history available</p>
                  <p className="empty-hint">Make some changes to see revisions here</p>
                </div>
              ) : (
                <div className="revisions-list">
                  {revisions.map((revision, index) => (
                    <div key={revision.revision_id || index} className="revision-item">
                      <div className="revision-header">
                        <span className="revision-id">{revision.revision_id}</span>
                        <span className="revision-timestamp">
                          {new Date(revision.timestamp).toLocaleString()}
                        </span>
                      </div>
                      <div className="revision-files">
                        <strong>Files:</strong>
                        <ul>
                          {revision.files?.map((file: any, fileIndex: number) => (
                            <li key={fileIndex}>
                              <span className={`file-badge file-${file.operation}`}>
                                {file.operation}
                              </span>
                              <span className="file-path-small">{file.path}</span>
                              {(file.lines_added > 0 || file.lines_deleted > 0) && (
                                <span className="file-diff-small">
                                  {file.lines_added > 0 && <span className="additions">+{file.lines_added}</span>}
                                  {file.lines_deleted > 0 && <span className="deletions">-{file.lines_deleted}</span>}
                                </span>
                              )}
                            </li>
                          ))}
                        </ul>
                      </div>
                      <div className="revision-actions">
                        <button
                          onClick={() => handleRollback(revision.revision_id)}
                          className="rollback-button"
                          disabled={isLoadingHistory}
                        >
                          ⏪ Rollback to this revision
                        </button>
                      </div>
                    </div>
                  ))}
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
