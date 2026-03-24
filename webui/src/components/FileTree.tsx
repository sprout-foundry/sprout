import React, { useState, useEffect, useCallback, useRef, forwardRef, useImperativeHandle } from 'react';
import {
  FolderOpen,
  Folder,
  FileCode,
  Code2,
  Code,
  Braces,
  Globe,
  Palette,
  FileText,
  Settings,
  Terminal,
  FileX,
  File,
  RotateCw,
  ChevronRight,
  ChevronDown,
  Zap,
  AlertTriangle,
  FolderClosed,
  ImageIcon,
} from 'lucide-react';
import './FileTree.css';

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
  gitStatus?: 'modified' | 'untracked' | 'ignored';
  children?: FileInfo[]; // For hierarchical structure
}

interface FileTreeResponse {
  message: string;
  path: string;
  files: Array<FileInfo & {
    is_dir?: boolean;
    mod_time?: number;
    git_status?: string;
  }>;
}

interface FileTreeProps {
  onFileSelect: (file: FileInfo) => void;
  selectedFile?: string;
  rootPath?: string;
  onRefresh?: () => void;
}

interface FileTreeHandle {
  refresh: () => void;
}

const FileTree = forwardRef<FileTreeHandle, FileTreeProps>(({ onFileSelect, selectedFile, rootPath = '.', onRefresh }, ref) => {
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set([rootPath]));
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [currentPath, setCurrentPath] = useState<string>(rootPath);
  const filesRef = useRef<FileInfo[]>([]);

  // Expose refresh method via ref
  useImperativeHandle(ref, () => ({
    refresh: async () => {
      setLoading(true);
      setError(null);
      
      try {
        const rootFiles = await fetchFiles(rootPath);
        setFiles(rootFiles);
        setCurrentPath(rootPath);
        onRefresh?.();
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      } finally {
        setLoading(false);
      }
    }
  }));

  useEffect(() => {
    filesRef.current = files;
  }, [files]);

  // Fetch files for a given path
  const fetchFiles = async (path: string): Promise<FileInfo[]> => {
    try {
      const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch files: ${response.statusText}`);
      }

      const data: FileTreeResponse = await response.json();
      if (data.message === 'success') {
        return (data.files || [])
          .map((file) => ({
            name: file.name,
            path: file.path,
            size: file.size || 0,
            modified: file.modified ?? file.mod_time ?? 0,
            isDir: Boolean(file.isDir ?? file.is_dir),
            ext: (file.isDir ?? file.is_dir)
              ? ''
              : (file.name.includes('.') ? `.${file.name.split('.').pop() || ''}` : ''),
            gitStatus: file.git_status as FileInfo['gitStatus'] || undefined,
          }))
          .sort((a, b) => {
            if (a.isDir !== b.isDir) {
              return a.isDir ? -1 : 1;
            }
            if (a.gitStatus === 'ignored' !== (b.gitStatus === 'ignored')) {
              return a.gitStatus === 'ignored' ? 1 : -1;
            }
            return a.name.localeCompare(b.name);
          });
      } else {
        throw new Error(data.message);
      }
    } catch (err) {
      // Check if it's a JSON parsing error (backend not available)
      if (err instanceof Error && err.message.includes('Unexpected token')) {
        throw new Error('Backend not connected. Start with: ./ledit agent --web-port 54421');
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
    const shouldExpand = !expandedDirs.has(dirPath);

    setExpandedDirs((prev) => {
      const next = new Set(prev);
      if (next.has(dirPath)) {
        next.delete(dirPath);
      } else {
        next.add(dirPath);
      }
      return next;
    });

    if (!shouldExpand) {
      return;
    }

    // Load children if not already loaded.
    const dir = findFileByPath(filesRef.current, dirPath);
    const needsLoad = Boolean(dir && (!dir.children || dir.children.length === 0));

    if (!needsLoad) {
      return;
    }

    const children = await loadDirectoryChildren(dirPath);
    setFiles((prev) => updateFileChildren(prev, dirPath, children));
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

  // Get file icon based on extension or type — returns a React element
  const getFileIcon = (file: FileInfo): React.ReactNode => {
    if (file.isDir) {
      const isExpanded = expandedDirs.has(file.path);
      return isExpanded
        ? <FolderOpen size={16} className="icon-folder icon-folder-open" />
        : <Folder size={16} className="icon-folder" />;
    }

    const ext = file.ext?.toLowerCase();
    switch (ext) {
      case '.js':
      case '.jsx':
        return <FileCode size={16} className="icon-file-code icon-js" style={{ color: '#f7df1e' }} />;
      case '.ts':
      case '.tsx':
        return <Code2 size={16} className="icon-code icon-ts" style={{ color: '#3178c6' }} />;
      case '.go':
        return <Code2 size={16} className="icon-code icon-go" style={{ color: '#00add8' }} />;
      case '.py':
        return <Code size={16} className="icon-code icon-py" style={{ color: '#3776ab' }} />;
      case '.json':
        return <Braces size={16} className="icon-braces icon-json" />;
      case '.html':
        return <Globe size={16} className="icon-globe icon-html" style={{ color: '#e34c26' }} />;
      case '.css':
        return <Palette size={16} className="icon-palette icon-css" style={{ color: '#264de4' }} />;
      case '.md':
        return <FileText size={16} className="icon-file-text icon-md" />;
      case '.txt':
        return <FileText size={16} className="icon-file-text icon-txt" />;
      case '.yml':
      case '.yaml':
        return <Settings size={16} className="icon-settings icon-yaml" />;
      case '.sh':
      case '.bash':
        return <Terminal size={16} className="icon-terminal icon-sh" />;
      case '.gitignore':
        return <FileX size={16} className="icon-file-x icon-gitignore" />;
      case '.png':
      case '.jpg':
      case '.jpeg':
      case '.gif':
      case '.bmp':
      case '.webp':
      case '.svg':
      case '.ico':
      case '.tiff':
      case '.tif':
      case '.avif':
        return <ImageIcon size={16} className="icon-image" style={{ color: '#c084fc' }} />;
      default:
        return <File size={16} className="icon-file" />;
    }
  };

  // Render file tree recursively
  const renderFileTree = (fileList: FileInfo[], depth: number = 0): JSX.Element[] => {
    return fileList.map((file) => {
      const isExpanded = expandedDirs.has(file.path);
      const isSelected = selectedFile === file.path;
      const hasChildren = file.isDir && Array.isArray(file.children) && file.children.length > 0;
      
      return (
        <React.Fragment key={file.path}>
          <div
            className={`file-tree-item ${file.isDir ? 'directory' : 'file'} ${isSelected ? 'selected' : ''}${file.gitStatus ? ` git-${file.gitStatus}` : ''}`}
            style={{ paddingLeft: `${depth * 16 + 8}px` }}
            data-ext={file.ext || ''}
            data-git-status={file.gitStatus || ''}
            onClick={() => handleClick(file)}
            role="treeitem"
            tabIndex={0}
            aria-selected={isSelected}
            aria-expanded={file.isDir ? isExpanded : undefined}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                handleClick(file);
              }
            }}
          >
            <div className="file-tree-icon">
              {getFileIcon(file)}
            </div>
            {file.isDir && (
              <span className="file-tree-expand">
                {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              </span>
            )}
            <span className="file-tree-name">{file.name}</span>
            {file.isDir && hasChildren && (
              <span className="file-tree-count">({file.children?.length})</span>
            )}
          </div>
          
          {/* Render children if directory is expanded */}
          {file.isDir && isExpanded && (
            <div className="file-tree-children">
              {Array.isArray(file.children) ? renderFileTree(file.children, depth + 1) : null}
            </div>
          )}
        </React.Fragment>
      );
    });
  };

  return (
      <div className="file-tree">
        <div className="file-tree-header">
          <h3><FolderClosed size={16} style={{ marginRight: 6, verticalAlign: 'middle' }} /> File Explorer</h3>
        </div>

      {loading && (
        <div className="loading-indicator">
          <div className="spinner"><Zap size={16} /></div>
          <span>Loading...</span>
        </div>
      )}

      {error && (
        <div className="error-message">
          <span className="error-icon"><AlertTriangle size={16} /></span>
          <span className="error-text">{error}</span>
        </div>
      )}

      <div className="file-list">
        {renderFileTree(files)}

        {files.length === 0 && !loading && !error && (
          <div className="empty-directory">
            <span className="empty-icon"><FolderOpen size={16} /></span>
            <span className="empty-text">Empty directory</span>
          </div>
        )}
      </div>
    </div>
  );
});

export default FileTree;
