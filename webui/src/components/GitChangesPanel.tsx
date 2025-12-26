import React, { useState, useEffect } from 'react';
import './GitChangesPanel.css';

export enum GitFileStatus {
  Modified = 'M',
  Added = 'A',
  Deleted = 'D',
  Renamed = 'R',
  Copied = 'C',
  Updated = 'U',
  Unchanged = ' ',
  Untracked = '?',
  Ignored = '!'
}

export interface GitFile {
  path: string;
  status: GitFileStatus;
  staged: boolean;
}

interface GitChangesPanelProps {
  onFileClick?: (file: GitFile) => void;
}

const GitChangesPanel: React.FC<GitChangesPanelProps> = ({ onFileClick }) => {
  const [files, setFiles] = useState<GitFile[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedSections, setExpandedSections] = useState({
    unstaged: true,
    staged: true
  });

  // Fetch git status
  const fetchGitStatus = async () => {
    setLoading(true);
    setError(null);

    try {
      const response = await fetch('/api/git/status');
      if (!response.ok) {
        throw new Error(`Failed to fetch git status: ${response.statusText}`);
      }

      const data = await response.json();
      if (data.status === 'success') {
        setFiles(data.files || []);
      } else {
        throw new Error(data.message || 'Unknown error');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  // Stage a file
  const stageFile = async (file: GitFile) => {
    try {
      const response = await fetch('/api/git/stage', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: file.path })
      });

      if (response.ok) {
        await fetchGitStatus(); // Refresh status
      }
    } catch (err) {
      console.error('Failed to stage file:', err);
      alert('Failed to stage file');
    }
  };

  // Unstage a file
  const unstageFile = async (file: GitFile) => {
    try {
      const response = await fetch('/api/git/unstage', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: file.path })
      });

      if (response.ok) {
        await fetchGitStatus(); // Refresh status
      }
    } catch (err) {
      console.error('Failed to unstage file:', err);
      alert('Failed to unstage file');
    }
  };

  // Discard changes in a file
  const discardChanges = async (file: GitFile) => {
    if (!file.path) return;

    const confirmMsg = `Are you sure you want to discard all changes in "${file.path}"? This cannot be undone.`;
    if (!confirm(confirmMsg)) {
      return;
    }

    try {
      const response = await fetch('/api/git/discard', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: file.path })
      });

      if (response.ok) {
        await fetchGitStatus(); // Refresh status
      }
    } catch (err) {
      console.error('Failed to discard changes:', err);
      alert('Failed to discard changes');
    }
  };

  // Stage all changes
  const stageAll = async () => {
    try {
      const response = await fetch('/api/git/stage-all', { method: 'POST' });
      if (response.ok) {
        await fetchGitStatus();
      }
    } catch (err) {
      alert('Failed to stage all changes');
    }
  };

  // Unstage all changes
  const unstageAll = async () => {
    try {
      const response = await fetch('/api/git/unstage-all', { method: 'POST' });
      if (response.ok) {
        await fetchGitStatus();
      }
    } catch (err) {
      alert('Failed to unstage all changes');
    }
  };

  // Toggle section expand/collapse
  const toggleSection = (section: 'unstaged' | 'staged') => {
    setExpandedSections(prev => ({
      ...prev,
      [section]: !prev[section]
    }));
  };

  // Get status display info
  const getStatusInfo = (status: GitFileStatus) => {
    switch (status) {
      case GitFileStatus.Modified:
        return { icon: 'M', color: '#e2c08d', label: 'Modified' };
      case GitFileStatus.Added:
        return { icon: 'A', color: '#73c991', label: 'Added' };
      case GitFileStatus.Deleted:
        return { icon: 'D', color: '#f48771', label: 'Deleted' };
      case GitFileStatus.Renamed:
        return { icon: 'R', color: '#a5d6ff', label: 'Renamed' };
      case GitFileStatus.Copied:
        return { icon: 'C', color: '#a5d6ff', label: 'Copied' };
      case GitFileStatus.Updated:
        return { icon: 'U', color: '#e2c08d', label: 'Updated (merged)' };
      case GitFileStatus.Untracked:
        return { icon: '?', color: '#6a9955', label: 'Untracked' };
      default:
        return { icon: '-', color: '#858585', label: 'Unknown' };
    }
  };

  // Get file name from path
  const getFileName = (path: string) => {
    const parts = path.split('/');
    return parts[parts.length - 1] || path;
  };

  // Get file icon
  const getFileIcon = (path: string) => {
    const ext = path.split('.').pop()?.toLowerCase();
    switch (ext) {
      case 'js':
      case 'jsx':
        return '🟨';
      case 'ts':
      case 'tsx':
        return '🔷';
      case 'go':
        return '🐹';
      case 'py':
        return '🐍';
      case 'json':
        return '📋';
      case 'html':
        return '🌐';
      case 'css':
        return '🎨';
      case 'md':
        return '📝';
      default:
        return '📄';
    }
  };

  useEffect(() => {
    fetchGitStatus();
    // Poll for changes every 5 seconds
    const interval = setInterval(fetchGitStatus, 5000);
    return () => clearInterval(interval);
  }, []);

  // Separate into staged and unstaged
  const stagedFiles = files.filter(f => f.staged);
  const unstagedFiles = files.filter(f => !f.staged);

  return (
    <div className="git-changes-panel">
      <div className="panel-header">
        <div className="header-left">
          <span className="panel-icon">📊</span>
          <span className="panel-title">Source Control</span>
        </div>
        <button
          className="refresh-btn"
          onClick={fetchGitStatus}
          disabled={loading}
          title="Refresh status"
        >
          {loading ? '⚡' : '🔄'}
        </button>
      </div>

      {error && (
        <div className="panel-error">
          <span className="error-icon">⚠️</span>
          <span className="error-text">{error}</span>
        </div>
      )}

      {/* Changes section */}
      {unstagedFiles.length > 0 && (
        <div className="changes-section">
          <div
            className={`section-header ${expandedSections.unstaged ? 'expanded' : ''}`}
            onClick={() => toggleSection('unstaged')}
          >
            <span className="chevron">{expandedSections.unstaged ? '▼' : '▶'}</span>
            <span className="section-title">Changes</span>
            <span className="section-count">{unstagedFiles.length}</span>
            <button
              className="stage-all-btn"
              onClick={(e) => {
                e.stopPropagation();
                stageAll();
              }}
              title="Stage all changes"
            >
              Stage All
            </button>
          </div>

          {expandedSections.unstaged && (
            <div className="file-list">
              {unstagedFiles.map((file) => {
                const statusInfo = getStatusInfo(file.status);
                return (
                  <div
                    key={file.path}
                    className={`file-item ${file.status}`}
                    onClick={() => onFileClick?.(file)}
                  >
                    <span
                      className={`file-status-badge ${file.status}`}
                      title={statusInfo.label}
                      style={{ color: statusInfo.color }}
                    >
                      {statusInfo.icon}
                    </span>
                    <span className="file-icon">{getFileIcon(file.path)}</span>
                    <span className="file-name" title={file.path}>
                      {getFileName(file.path)}
                    </span>
                    <div className="file-actions">
                      <button
                        className="action-btn stage"
                        onClick={(e) => {
                          e.stopPropagation();
                          stageFile(file);
                        }}
                        title="Stage file"
                      >
                        +
                      </button>
                      <button
                        className="action-btn discard"
                        onClick={(e) => {
                          e.stopPropagation();
                          discardChanges(file);
                        }}
                        title="Discard changes"
                      >
                        ✕
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* Staged Changes section */}
      {stagedFiles.length > 0 && (
        <div className="changes-section">
          <div
            className={`section-header ${expandedSections.staged ? 'expanded' : ''}`}
            onClick={() => toggleSection('staged')}
          >
            <span className="chevron">{expandedSections.staged ? '▼' : '▶'}</span>
            <span className="section-title">Staged Changes</span>
            <span className="section-count">{stagedFiles.length}</span>
            <button
              className="stage-all-btn unstage"
              onClick={(e) => {
                e.stopPropagation();
                unstageAll();
              }}
              title="Unstage all changes"
            >
              Unstage All
            </button>
          </div>

          {expandedSections.staged && (
            <div className="file-list">
              {stagedFiles.map((file) => {
                const statusInfo = getStatusInfo(file.status);
                return (
                  <div
                    key={file.path}
                    className={`file-item ${file.status} staged`}
                    onClick={() => onFileClick?.(file)}
                  >
                    <span
                      className={`file-status-badge ${file.status}`}
                      title={statusInfo.label}
                      style={{ color: statusInfo.color }}
                    >
                      {statusInfo.icon}
                    </span>
                    <span className="file-icon">{getFileIcon(file.path)}</span>
                    <span className="file-name" title={file.path}>
                      {getFileName(file.path)}
                    </span>
                    <div className="file-actions">
                      <button
                        className="action-btn unstage"
                        onClick={(e) => {
                          e.stopPropagation();
                          unstageFile(file);
                        }}
                        title="Unstage file"
                      >
                        −
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* Empty state */}
      {unstagedFiles.length === 0 && stagedFiles.length === 0 && !loading && !error && (
        <div className="empty-state">
          <span className="empty-icon">✅</span>
          <span className="empty-text">No changes detected</span>
          <span className="empty-subtext">Working directory is clean</span>
        </div>
      )}

      {/* Footer with commit button */}
      {stagedFiles.length > 0 && (
        <div className="panel-footer">
          <button className="commit-btn">
            ✎ Commit {stagedFiles.length} {stagedFiles.length === 1 ? 'change' : 'changes'}
          </button>
        </div>
      )}
    </div>
  );
};

export default GitChangesPanel;
