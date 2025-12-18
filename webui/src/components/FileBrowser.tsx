import React, { useState, useEffect, useCallback } from 'react';
import './FileBrowser.css';

export interface FileNode {
  id: string;
  name: string;
  path: string;
  type: 'file' | 'directory';
  size?: number;
  modified?: string;
  children?: FileNode[];
}

interface FileBrowserProps {
  isOpen: boolean;
  initialPath?: string;
  onSelect: (file: FileNode) => void;
  onCancel: () => void;
  allowDirectories?: boolean;
  allowedExtensions?: string[];
}

const FileBrowser: React.FC<FileBrowserProps> = ({
  isOpen,
  initialPath = '/',
  onSelect,
  onCancel,
  allowDirectories = false,
  allowedExtensions = []
}) => {
  const [currentPath, setCurrentPath] = useState(initialPath);
  const [files, setFiles] = useState<FileNode[]>([]);
  const [selectedFile, setSelectedFile] = useState<FileNode | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Mock file system data - in real implementation, this would come from API
  const mockFileSystem: { [key: string]: FileNode[] } = {
    '/': [
      { id: '1', name: 'src', path: '/src', type: 'directory' },
      { id: '2', name: 'pkg', path: '/pkg', type: 'directory' },
      { id: '3', name: 'cmd', path: '/cmd', type: 'directory' },
      { id: '4', name: 'go.mod', path: '/go.mod', type: 'file', size: 1024 },
      { id: '5', name: 'README.md', path: '/README.md', type: 'file', size: 2048 },
      { id: '6', name: 'package.json', path: '/package.json', type: 'file', size: 512 }
    ],
    '/src': [
      { id: '7', name: 'main.go', path: '/src/main.go', type: 'file', size: 4096 },
      { id: '8', name: 'utils', path: '/src/utils', type: 'directory' },
      { id: '9', name: 'config', path: '/src/config', type: 'directory' }
    ],
    '/pkg': [
      { id: '10', name: 'ui', path: '/pkg/ui', type: 'directory' },
      { id: '11', name: 'agent', path: '/pkg/agent', type: 'directory' },
      { id: '12', name: 'console', path: '/pkg/console', type: 'directory' }
    ],
    '/cmd': [
      { id: '13', name: 'agent.go', path: '/cmd/agent.go', type: 'file', size: 8192 },
      { id: '14', name: 'root.go', path: '/cmd/root.go', type: 'file', size: 2048 },
      { id: '15', name: 'version.go', path: '/cmd/version.go', type: 'file', size: 512 }
    ]
  };

  const loadDirectory = useCallback(async (path: string) => {
    setLoading(true);
    setError(null);

    try {
      // Use the actual API to browse files
      const response = await fetch(`/api/browse?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`Failed to browse directory: ${response.statusText}`);
      }
      
      const data = await response.json();
      const directoryFiles = data.files || [];
      
      const sortedFiles = directoryFiles.sort((a: any, b: any) => {
        // Directories first
        if (a.type !== b.type) {
          return a.type === 'directory' ? -1 : 1;
        }
        // Then alphabetically
        return a.name.localeCompare(b.name);
      });

      setFiles(sortedFiles);
      setSelectedFile(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load directory');
      // Fallback to mock data for development
      const directoryFiles = mockFileSystem[path] || [];
      const sortedFiles = directoryFiles.sort((a, b) => {
        if (a.type !== b.type) {
          return a.type === 'directory' ? -1 : 1;
        }
        return a.name.localeCompare(b.name);
      });
      setFiles(sortedFiles);
    } finally {
      setLoading(false);
    }
  }, [setLoading, setError, setFiles]);

  useEffect(() => {
    if (isOpen) {
      loadDirectory(currentPath);
    }
  }, [isOpen, currentPath, loadDirectory]);

  const handleFileClick = (file: FileNode) => {
    if (file.type === 'directory') {
      setCurrentPath(file.path);
    } else {
      setSelectedFile(file);
    }
  };

  const handleFileDoubleClick = (file: FileNode) => {
    if (file.type === 'file' || allowDirectories) {
      onSelect(file);
    }
  };

  const handleSelect = () => {
    if (selectedFile) {
      handleFileDoubleClick(selectedFile);
    }
  };

  const navigateUp = () => {
    const parts = currentPath.split('/').filter(Boolean);
    parts.pop();
    const parentPath = '/' + parts.join('/');
    setCurrentPath(parentPath || '/');
  };

  const formatFileSize = (bytes: number) => {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const filterFiles = (files: FileNode[]) => {
    if (allowedExtensions.length === 0) return files;
    return files.filter(file => {
      if (file.type === 'directory') return true;
      const ext = file.name.split('.').pop()?.toLowerCase();
      return ext && allowedExtensions.includes(ext);
    });
  };

  const filteredFiles = filterFiles(files);

  if (!isOpen) return null;

  return (
    <div className="filebrowser-overlay" onClick={onCancel}>
      <div className="filebrowser-container" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="filebrowser-header">
          <h3>📁 File Browser</h3>
          <button className="filebrowser-close" onClick={onCancel}>✕</button>
        </div>

        {/* Navigation */}
        <div className="filebrowser-nav">
          <button
            className="filebrowser-nav-button"
            onClick={navigateUp}
            disabled={currentPath === '/'}
          >
            ⬆️ Up
          </button>
          <div className="filebrowser-path">
            <input
              type="text"
              value={currentPath}
              onChange={(e) => setCurrentPath(e.target.value)}
              className="filebrowser-path-input"
            />
          </div>
        </div>

        {/* File List */}
        <div className="filebrowser-content">
          {loading ? (
            <div className="filebrowser-loading">Loading...</div>
          ) : error ? (
            <div className="filebrowser-error">{error}</div>
          ) : (
            <div className="filebrowser-list">
              {filteredFiles.map(file => (
                <div
                  key={file.id}
                  className={`filebrowser-item ${selectedFile?.id === file.id ? 'selected' : ''}`}
                  onClick={() => handleFileClick(file)}
                  onDoubleClick={() => handleFileDoubleClick(file)}
                >
                  <div className="filebrowser-icon">
                    {file.type === 'directory' ? '📁' : '📄'}
                  </div>
                  <div className="filebrowser-info">
                    <div className="filebrowser-name">{file.name}</div>
                    <div className="filebrowser-details">
                      {file.type === 'directory' ? 'Directory' : formatFileSize(file.size || 0)}
                      {file.modified && ` • ${new Date(file.modified).toLocaleDateString()}`}
                    </div>
                  </div>
                </div>
              ))}
              {filteredFiles.length === 0 && (
                <div className="filebrowser-empty">This directory is empty</div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="filebrowser-footer">
          <div className="filebrowser-help">
            Click to select, double-click to choose
          </div>
          <div className="filebrowser-actions">
            <button className="filebrowser-button secondary" onClick={onCancel}>
              Cancel
            </button>
            <button
              className="filebrowser-button primary"
              onClick={handleSelect}
              disabled={!selectedFile || (!allowDirectories && selectedFile.type === 'directory')}
            >
              Select
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default FileBrowser;