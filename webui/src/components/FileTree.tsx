import React, { useState, useEffect } from 'react';
import './FileTree.css';

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
}

interface FileTreeResponse {
  message: string;
  path: string;
  files: FileInfo[];
}

interface FileTreeProps {
  onFileSelect: (file: FileInfo) => void;
  selectedFile?: string;
}

const FileTree: React.FC<FileTreeProps> = ({ onFileSelect, selectedFile }) => {
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [currentPath, setCurrentPath] = useState<string>('.');
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set(['.']));
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  // Fetch files for a given path
  const fetchFiles = async (path: string) => {
    setLoading(true);
    setError(null);

    try {
      const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch files: ${response.statusText}`);
      }

      const data: FileTreeResponse = await response.json();
      if (data.message === 'success') {
        setFiles(data.files);
        setCurrentPath(data.path);
      } else {
        throw new Error(data.message);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
      setFiles([]);
    } finally {
      setLoading(false);
    }
  };

  // Initial load and path changes
  useEffect(() => {
    fetchFiles(currentPath);
  }, [currentPath]);

  // Toggle directory expansion
  const toggleDir = (path: string) => {
    const newExpanded = new Set(expandedDirs);
    if (newExpanded.has(path)) {
      newExpanded.delete(path);
    } else {
      newExpanded.add(path);
    }
    setExpandedDirs(newExpanded);
  };

  // Handle file/directory click
  const handleClick = (file: FileInfo) => {
    if (file.isDir) {
      // Navigate into directory or toggle expansion
      if (expandedDirs.has(file.path)) {
        toggleDir(file.path);
      } else {
        toggleDir(file.path);
        fetchFiles(file.path);
      }
    } else {
      // Select file
      onFileSelect(file);
    }
  };

  // Navigate to parent directory
  const navigateToParent = () => {
    const parentPath = currentPath.split('/').slice(0, -1).join('.') || '.';
    setCurrentPath(parentPath);
  };

  // Get file icon based on extension or type
  const getFileIcon = (file: FileInfo): string => {
    if (file.isDir) {
      return expandedDirs.has(file.path) ? '📂' : '📁';
    }

    const ext = file.ext?.toLowerCase();
    switch (ext) {
      case '.js':
      case '.jsx':
        return '🟨'; // JavaScript
      case '.ts':
      case '.tsx':
        return '🔷'; // TypeScript
      case '.go':
        return '🐹'; // Go
      case '.py':
        return '🐍'; // Python
      case '.json':
        return '📋'; // JSON
      case '.html':
        return '🌐'; // HTML
      case '.css':
        return '🎨'; // CSS
      case '.md':
        return '📝'; // Markdown
      case '.txt':
        return '📄'; // Text
      case '.yml':
      case '.yaml':
        return '⚙️'; // YAML
      case '.sh':
        return '🐚'; // Shell
      case '.gitignore':
        return '🚫'; // Git ignore
      default:
        return '📄'; // Default file
    }
  };

  // Format file size
  const formatSize = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  // Format modified time
  const formatDate = (timestamp: number): string => {
    return new Date(timestamp * 1000).toLocaleDateString();
  };

  return (
    <div className="file-tree">
      <div className="file-tree-header">
        <h3>📁 File Explorer</h3>
        <div className="file-tree-controls">
          <button
            onClick={() => fetchFiles(currentPath)}
            disabled={loading}
            className="refresh-button"
            title="Refresh"
          >
            🔄
          </button>
          {currentPath !== '.' && (
            <button
              onClick={navigateToParent}
              className="parent-button"
              title="Parent directory"
            >
              ⬆️
            </button>
          )}
        </div>
      </div>

      <div className="current-path">
        <span className="path-label">Path:</span>
        <span className="path-value">{currentPath}</span>
      </div>

      {loading && (
        <div className="loading-indicator">
          <div className="spinner">⚡</div>
          <span>Loading...</span>
        </div>
      )}

      {error && (
        <div className="error-message">
          <span className="error-icon">⚠️</span>
          <span className="error-text">{error}</span>
        </div>
      )}

      <div className="file-list">
        {files.map((file) => (
          <div
            key={file.path}
            className={`file-item ${file.isDir ? 'directory' : 'file'} ${selectedFile === file.path ? 'selected' : ''}`}
            onClick={() => handleClick(file)}
          >
            <div className="file-icon">
              {getFileIcon(file)}
            </div>
            <div className="file-info">
              <span className="file-name">{file.name}</span>
              <div className="file-details">
                {!file.isDir && (
                  <span className="file-size">{formatSize(file.size)}</span>
                )}
                <span className="file-modified">{formatDate(file.modified)}</span>
              </div>
            </div>
          </div>
        ))}

        {files.length === 0 && !loading && !error && (
          <div className="empty-directory">
            <span className="empty-icon">📂</span>
            <span className="empty-text">Empty directory</span>
          </div>
        )}
      </div>
    </div>
  );
};

export default FileTree;