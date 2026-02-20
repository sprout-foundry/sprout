import React, { useState, useEffect } from 'react';
import './GitView.css';

interface GitStatus {
  branch: string;
  ahead: number;
  behind: number;
  staged: GitFile[];
  modified: GitFile[];
  untracked: GitFile[];
  deleted: GitFile[];
  renamed: GitFile[];
  clean: boolean;
}

interface GitFile {
  path: string;
  status: string;
  changes?: {
    additions: number;
    deletions: number;
  };
}

interface GitViewProps {
  onCommit?: (message: string, files: string[]) => void;
  onStage?: (files: string[]) => void;
  onUnstage?: (files: string[]) => void;
  onDiscard?: (files: string[]) => void;
}

const GitView: React.FC<GitViewProps> = ({
  onCommit,
  onStage,
  onUnstage,
  onDiscard
}) => {
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [commitMessage, setCommitMessage] = useState('');
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load git status from API
  useEffect(() => {
    const loadGitStatus = async () => {
      setIsLoading(true);
      setError(null);
      
      try {
        const response = await fetch('/api/git/status');
        if (!response.ok) {
          throw new Error(`Failed to load git status: ${response.statusText}`);
        }
        
        const data = await response.json();
        if (data.message === 'success') {
          // Handle null values from API
          const status = data.status || {};
          setGitStatus({
            branch: status.branch || 'main',
            ahead: status.ahead || 0,
            behind: status.behind || 0,
            staged: status.staged || [],
            modified: status.modified || [],
            untracked: status.untracked || [],
            deleted: status.deleted || [],
            renamed: status.renamed || [],
            clean: !(status.staged?.length || status.modified?.length || status.untracked?.length || status.deleted?.length)
          });
        } else {
          throw new Error(data.message || 'Unknown error');
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load git status');
        // Fallback to mock data for development
        const mockStatus: GitStatus = {
          branch: 'main',
          ahead: 0,
          behind: 0,
          staged: [
            { path: 'src/components/CommandInput.tsx', status: 'M', changes: { additions: 15, deletions: 5 } },
            { path: 'src/components/Sidebar.tsx', status: 'M', changes: { additions: 8, deletions: 3 } }
          ],
          modified: [
            { path: 'src/App.tsx', status: 'M', changes: { additions: 12, deletions: 4 } },
            { path: 'webui/src/components/GitView.css', status: 'A', changes: { additions: 45, deletions: 0 } }
          ],
          untracked: [
            { path: 'src/components/NewComponent.tsx', status: '??' }
          ],
          deleted: [],
          renamed: [],
          clean: false
        };
        setGitStatus(mockStatus);
      } finally {
        setIsLoading(false);
      }
    };

    loadGitStatus();
  }, []);

  const handleFileSelect = (filePath: string) => {
    setSelectedFiles(prev => {
      const newSet = new Set(prev);
      if (newSet.has(filePath)) {
        newSet.delete(filePath);
      } else {
        newSet.add(filePath);
      }
      return newSet;
    });
  };

  const handleSelectAll = () => {
    if (!gitStatus) return;
    
    const allFiles = [
      ...gitStatus.staged.map(f => f.path),
      ...gitStatus.modified.map(f => f.path),
      ...gitStatus.untracked.map(f => f.path)
    ];
    
    setSelectedFiles(new Set(allFiles));
  };

  const handleDeselectAll = () => {
    setSelectedFiles(new Set());
  };

  const handleStageSelected = () => {
    if (selectedFiles.size === 0) return;
    onStage?.(Array.from(selectedFiles));
  };

  const handleUnstageSelected = () => {
    if (selectedFiles.size === 0) return;
    onUnstage?.(Array.from(selectedFiles));
  };

  const handleCommit = () => {
    if (!commitMessage.trim() || !gitStatus?.staged.length) return;
    
    const stagedFiles = gitStatus.staged.map(f => f.path);
    onCommit?.(commitMessage, stagedFiles);
    setCommitMessage('');
  };

  const handleDiscardSelected = () => {
    if (selectedFiles.size === 0) return;
    onDiscard?.(Array.from(selectedFiles));
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'M': return '📝';
      case 'A': return '➕';
      case 'D': return '🗑️';
      case 'R': return '🔄';
      case '??': return '❓';
      default: return '📄';
    }
  };

  const getStatusText = (status: string) => {
    switch (status) {
      case 'M': return 'Modified';
      case 'A': return 'Added';
      case 'D': return 'Deleted';
      case 'R': return 'Renamed';
      case '??': return 'Untracked';
      default: return 'Unknown';
    }
  };

  if (isLoading) {
    return (
      <div className="git-view">
        <div className="git-loading">
          <div className="spinner"></div>
          <p>Loading git status...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="git-view">
        <div className="git-error">
          <span className="error-icon">❌</span>
          <p>{error}</p>
          <button onClick={() => window.location.reload()}>Retry</button>
        </div>
      </div>
    );
  }

  if (!gitStatus) {
    return (
      <div className="git-view">
        <div className="git-empty">
          <span className="empty-icon">🔀</span>
          <p>No git repository found</p>
        </div>
      </div>
    );
  }

  return (
    <div className="git-view">
      {/* Header */}
      <div className="git-header">
        <div className="git-branch">
          <span className="branch-icon">🌿</span>
          <span className="branch-name">{gitStatus.branch}</span>
          {gitStatus.ahead > 0 && <span className="ahead">↑{gitStatus.ahead}</span>}
          {gitStatus.behind > 0 && <span className="behind">↓{gitStatus.behind}</span>}
        </div>
        <div className="git-status">
          {gitStatus.clean ? (
            <span className="clean">✅ Clean</span>
          ) : (
            <span className="dirty">🔄 Changes</span>
          )}
        </div>
      </div>

      {/* Actions Bar */}
      <div className="git-actions">
        <div className="selection-actions">
          <button onClick={handleSelectAll} className="action-btn">
            Select All
          </button>
          <button onClick={handleDeselectAll} className="action-btn">
            Deselect All
          </button>
          <span className="selected-count">
            {selectedFiles.size} selected
          </span>
        </div>
        <div className="file-actions">
          <button 
            onClick={handleStageSelected}
            disabled={selectedFiles.size === 0}
            className="action-btn primary"
          >
            Stage Selected
          </button>
          <button 
            onClick={handleUnstageSelected}
            disabled={selectedFiles.size === 0}
            className="action-btn"
          >
            Unstage Selected
          </button>
          <button 
            onClick={handleDiscardSelected}
            disabled={selectedFiles.size === 0}
            className="action-btn danger"
          >
            Discard Selected
          </button>
        </div>
      </div>

      {/* File Sections */}
      <div className="git-files">
        {/* Staged Files */}
        {gitStatus.staged.length > 0 && (
          <div className="file-section staged">
            <h3>Staged Files ({gitStatus.staged.length})</h3>
            <div className="file-list">
              {gitStatus.staged.map((file, index) => (
                <div 
                  key={`staged-${index}`}
                  className={`file-item ${selectedFiles.has(file.path) ? 'selected' : ''}`}
                  onClick={() => handleFileSelect(file.path)}
                >
                  <input 
                    type="checkbox" 
                    checked={selectedFiles.has(file.path)}
                    onChange={() => handleFileSelect(file.path)}
                  />
                  <span className="file-icon">{getStatusIcon(file.status)}</span>
                  <span className="file-path">{file.path}</span>
                  <span className="file-status">{getStatusText(file.status)}</span>
                  {file.changes && (
                    <span className="file-changes">
                      <span className="additions">+{file.changes.additions}</span>
                      <span className="deletions">-{file.changes.deletions}</span>
                    </span>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Modified Files */}
        {gitStatus.modified.length > 0 && (
          <div className="file-section modified">
            <h3>Modified Files ({gitStatus.modified.length})</h3>
            <div className="file-list">
              {gitStatus.modified.map((file, index) => (
                <div 
                  key={`modified-${index}`}
                  className={`file-item ${selectedFiles.has(file.path) ? 'selected' : ''}`}
                  onClick={() => handleFileSelect(file.path)}
                >
                  <input 
                    type="checkbox" 
                    checked={selectedFiles.has(file.path)}
                    onChange={() => handleFileSelect(file.path)}
                  />
                  <span className="file-icon">{getStatusIcon(file.status)}</span>
                  <span className="file-path">{file.path}</span>
                  <span className="file-status">{getStatusText(file.status)}</span>
                  {file.changes && (
                    <span className="file-changes">
                      <span className="additions">+{file.changes.additions}</span>
                      <span className="deletions">-{file.changes.deletions}</span>
                    </span>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Untracked Files */}
        {gitStatus.untracked.length > 0 && (
          <div className="file-section untracked">
            <h3>Untracked Files ({gitStatus.untracked.length})</h3>
            <div className="file-list">
              {gitStatus.untracked.map((file, index) => (
                <div 
                  key={`untracked-${index}`}
                  className={`file-item ${selectedFiles.has(file.path) ? 'selected' : ''}`}
                  onClick={() => handleFileSelect(file.path)}
                >
                  <input 
                    type="checkbox" 
                    checked={selectedFiles.has(file.path)}
                    onChange={() => handleFileSelect(file.path)}
                  />
                  <span className="file-icon">{getStatusIcon(file.status)}</span>
                  <span className="file-path">{file.path}</span>
                  <span className="file-status">{getStatusText(file.status)}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* No Changes */}
        {gitStatus.clean && (
          <div className="no-changes">
            <span className="no-changes-icon">✨</span>
            <p>Working directory clean</p>
          </div>
        )}
      </div>

      {/* Commit Section */}
      <div className="git-commit">
        <h3>Commit Changes</h3>
        <div className="commit-form">
          <textarea
            value={commitMessage}
            onChange={(e) => setCommitMessage(e.target.value)}
            placeholder="Enter commit message..."
            className="commit-input"
            rows={3}
          />
          <button 
            onClick={handleCommit}
            disabled={!commitMessage.trim() || gitStatus.staged.length === 0}
            className="commit-btn primary"
          >
            Commit {gitStatus.staged.length} file{gitStatus.staged.length !== 1 ? 's' : ''}
          </button>
        </div>
      </div>
    </div>
  );
};

export default GitView;