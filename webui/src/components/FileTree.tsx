import React, { useState, useEffect, useCallback } from 'react';
import './FileTree.css';

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
  children?: FileInfo[]; // For hierarchical structure
}

interface FileTreeResponse {
  message: string;
  path: string;
  files: FileInfo[];
}

interface FileTreeProps {
  onFileSelect: (file: FileInfo) => void;
  selectedFile?: string;
  rootPath?: string;
}

const FileTree: React.FC<FileTreeProps> = ({ onFileSelect, selectedFile, rootPath = '.' }) => {
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set([rootPath]));
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [currentPath, setCurrentPath] = useState<string>(rootPath);

  // Fetch files for a given path
  const fetchFiles = async (path: string): Promise<FileInfo[]> => {
    try {
      const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch files: ${response.statusText}`);
      }

      const data: FileTreeResponse = await response.json();
      if (data.message === 'success') {
        return data.files;
      } else {
        throw new Error(data.message);
      }
    } catch (err) {
      // Check if it's a JSON parsing error (backend not available)
      if (err instanceof Error && err.message.includes('Unexpected token')) {
        throw new Error('Backend not connected. Start with: ./ledit agent --web-port 54321');
      }
      throw err instanceof Error ? err : new Error('Unknown error');
    }
  };

  const loadInitialFiles = useCallback(async () => {
    setLoading(true);
    setError(null);
    
    try {
      const rootFiles = await fetchFiles(rootPath);
      setFiles(rootFiles);
      setCurrentPath(rootPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
      setFiles([]);
    } finally {
      setLoading(false);
    }
  }, [setLoading, setError, setFiles, setCurrentPath, rootPath]);

  // Load initial files
  useEffect(() => {
    loadInitialFiles();
  }, [loadInitialFiles]);

  // Load children for a directory when expanded
  const loadDirectoryChildren = async (dirPath: string): Promise<FileInfo[]> => {
    try {
      return await fetchFiles(dirPath);
    } catch (err) {
      console.error(`Failed to load children for ${dirPath}:`, err);
      return [];
    }
  };

  // Toggle directory expansion
  const toggleDir = async (dirPath: string) => {
    const newExpanded = new Set(expandedDirs);
    
    if (newExpanded.has(dirPath)) {
      // Collapse
      newExpanded.delete(dirPath);
      setExpandedDirs(newExpanded);
    } else {
      // Expand
      newExpanded.add(dirPath);
      setExpandedDirs(newExpanded);
      
      // Load children if not already loaded
      const dir = findFileByPath(files, dirPath);
      if (dir && (!dir.children || dir.children.length === 0)) {
        const children = await loadDirectoryChildren(dirPath);
        const updatedFiles = updateFileChildren(files, dirPath, children);
        setFiles(updatedFiles);
      }
    }
  };

  // Find a file by path in the tree
  const findFileByPath = (fileList: FileInfo[], targetPath: string): FileInfo | null => {
    for (const file of fileList) {
      if (file.path === targetPath) {
        return file;
      }
      if (file.children) {
        const found = findFileByPath(file.children, targetPath);
        if (found) return found;
      }
    }
    return null;
  };

  // Update children of a specific directory
  const updateFileChildren = (fileList: FileInfo[], dirPath: string, children: FileInfo[]): FileInfo[] => {
    return fileList.map(file => {
      if (file.path === dirPath) {
        return { ...file, children: children.length > 0 ? children : undefined };
      }
      if (file.children) {
        return { ...file, children: updateFileChildren(file.children, dirPath, children) };
      }
      return file;
    });
  };

  // Handle file/directory click
  const handleClick = async (file: FileInfo) => {
    if (file.isDir) {
      await toggleDir(file.path);
    } else {
      onFileSelect(file);
    }
  };

  // Refresh the current directory
  const refresh = async () => {
    setLoading(true);
    setError(null);
    
    try {
      const rootFiles = await fetchFiles(rootPath);
      setFiles(rootFiles);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  // Get file icon based on extension or type
  const getFileIcon = (file: FileInfo): string => {
    if (file.isDir) {
      const isExpanded = expandedDirs.has(file.path);
      return isExpanded ? '📂' : '📁';
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

  // Render file tree recursively
  const renderFileTree = (fileList: FileInfo[], depth: number = 0): JSX.Element[] => {
    return fileList.map((file) => {
      const isExpanded = expandedDirs.has(file.path);
      const isSelected = selectedFile === file.path;
      const hasChildren = file.isDir && file.children && file.children.length > 0;
      
      return (
        <React.Fragment key={file.path}>
          <div
            className={`file-item ${file.isDir ? 'directory' : 'file'} ${isSelected ? 'selected' : ''}`}
            style={{ paddingLeft: `${depth * 16 + 8}px` }}
            onClick={() => handleClick(file)}
          >
            <div className="file-icon">
              {getFileIcon(file)}
            </div>
            {file.isDir && (
              <span className="expand-icon">
                {isExpanded ? '▼' : '▶'}
              </span>
            )}
            <span className="file-name">{file.name}</span>
            {file.isDir && hasChildren && (
              <span className="child-count">({file.children?.length})</span>
            )}
          </div>
          
          {/* Render children if directory is expanded */}
          {file.isDir && isExpanded && file.children && (
            <div className="directory-children">
              {renderFileTree(file.children, depth + 1)}
            </div>
          )}
        </React.Fragment>
      );
    });
  };

  return (
    <div className="file-tree">
      <div className="file-tree-header">
        <h3>📁 File Explorer</h3>
        <div className="file-tree-controls">
          <button
            onClick={refresh}
            disabled={loading}
            className="refresh-button"
            title="Refresh"
          >
            🔄
          </button>
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
        {renderFileTree(files)}

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