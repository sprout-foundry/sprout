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
  ChevronRight,
  ChevronDown,
  Zap,
  AlertTriangle,
  ImageIcon,
  FilePlus,
  FolderPlus,
  X,
  Check,
} from 'lucide-react';
import './FileTree.css';
import { ApiService } from '../services/api';

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
  gitStatus?: 'modified' | 'untracked' | 'ignored';
  children?: FileInfo[];
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
  onItemCreated?: () => void;
  onDeleteItem?: (path: string) => void;
}

interface FileTreeHandle {
  refresh: () => void;
}

type DraftMode = 'create-file' | 'create-folder' | 'rename';

interface DraftState {
  mode: DraftMode;
  parentPath: string;
  targetPath?: string;
}

interface ContextMenuState {
  x: number;
  y: number;
  file: FileInfo;
}

const FileTree = forwardRef<FileTreeHandle, FileTreeProps>(({
  onFileSelect,
  selectedFile,
  rootPath = '.',
  onRefresh,
  onItemCreated,
  onDeleteItem
}, ref) => {
  const apiService = ApiService.getInstance();
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set([rootPath]));
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState<DraftState | null>(null);
  const [draftValue, setDraftValue] = useState('');
  const [draftError, setDraftError] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const filesRef = useRef<FileInfo[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const treeRef = useRef<HTMLDivElement>(null);

  const findFileByPath = useCallback((fileList: FileInfo[], targetPath: string): FileInfo | null => {
    for (const file of fileList) {
      if (file.path === targetPath) {
        return file;
      }
      if (file.children) {
        const found = findFileByPath(file.children, targetPath);
        if (found) {
          return found;
        }
      }
    }
    return null;
  }, []);

  const updateFileChildren = useCallback((fileList: FileInfo[], dirPath: string, children: FileInfo[]): FileInfo[] => (
    fileList.map((file) => {
      if (file.path === dirPath) {
        return { ...file, children: children.length > 0 ? children : undefined };
      }
      if (file.children) {
        return { ...file, children: updateFileChildren(file.children, dirPath, children) };
      }
      return file;
    })
  ), []);

  const fetchFiles = useCallback(async (path: string): Promise<FileInfo[]> => {
    try {
      const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch files: ${response.statusText}`);
      }

      const data: FileTreeResponse = await response.json();
      if (data.message !== 'success') {
        throw new Error(data.message);
      }

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
          if ((a.gitStatus === 'ignored') !== (b.gitStatus === 'ignored')) {
            return a.gitStatus === 'ignored' ? 1 : -1;
          }
          return a.name.localeCompare(b.name);
        });
    } catch (err) {
      if (err instanceof Error && err.message.includes('Unexpected token')) {
        throw new Error('Backend not connected. Start with: ./ledit agent --web-port 54421');
      }
      throw err instanceof Error ? err : new Error('Unknown error');
    }
  }, []);

  const refreshTree = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      let nextFiles = await fetchFiles(rootPath);
      const expanded = Array.from(expandedDirs)
        .filter((dirPath) => dirPath !== rootPath)
        .sort((a, b) => a.split('/').length - b.split('/').length);

      for (const dirPath of expanded) {
        const dir = findFileByPath(nextFiles, dirPath);
        if (!dir?.isDir) {
          continue;
        }
        const children = await fetchFiles(dirPath);
        nextFiles = updateFileChildren(nextFiles, dirPath, children);
      }

      setFiles(nextFiles);
      onRefresh?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
      setFiles([]);
    } finally {
      setLoading(false);
    }
  }, [expandedDirs, fetchFiles, findFileByPath, onRefresh, rootPath, updateFileChildren]);

  useImperativeHandle(ref, () => ({
    refresh: refreshTree,
  }));

  useEffect(() => {
    filesRef.current = files;
  }, [files]);

  useEffect(() => {
    refreshTree();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rootPath]);

  useEffect(() => {
    if (!draft) {
      return;
    }
    const timer = window.setTimeout(() => inputRef.current?.focus(), 0);
    return () => window.clearTimeout(timer);
  }, [draft]);

  useEffect(() => {
    if (!contextMenu) {
      return;
    }

    const handleClose = () => setContextMenu(null);
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null);
      }
    };

    window.addEventListener('click', handleClose);
    window.addEventListener('contextmenu', handleClose);
    window.addEventListener('keydown', handleEscape);
    return () => {
      window.removeEventListener('click', handleClose);
      window.removeEventListener('contextmenu', handleClose);
      window.removeEventListener('keydown', handleEscape);
    };
  }, [contextMenu]);

  const startDraft = useCallback((nextDraft: DraftState, initialValue = '') => {
    setContextMenu(null);
    setDraft(nextDraft);
    setDraftValue(initialValue);
    setDraftError(null);
    if (nextDraft.parentPath !== rootPath) {
      setExpandedDirs((prev) => {
        const next = new Set(prev);
        next.add(nextDraft.parentPath);
        return next;
      });
    }
  }, [rootPath]);

  const handleCreateItem = useCallback((type: 'file' | 'folder', parentPath = rootPath) => {
    startDraft({
      mode: type === 'file' ? 'create-file' : 'create-folder',
      parentPath,
    });
  }, [rootPath, startDraft]);

  const handleStartRename = useCallback((file: FileInfo) => {
    const segments = file.path.split('/').filter(Boolean);
    segments.pop();
    const parentPath = segments.length > 0 ? segments.join('/') : rootPath;
    startDraft({
      mode: 'rename',
      parentPath,
      targetPath: file.path,
    }, file.name);
  }, [rootPath, startDraft]);

  const handleCancelDraft = useCallback(() => {
    setDraft(null);
    setDraftValue('');
    setDraftError(null);
  }, []);

  const handleConfirmDraft = useCallback(async () => {
    if (!draft || !draftValue.trim()) {
      setDraftError('Please enter a name');
      return;
    }

    setLoading(true);
    setDraftError(null);

    try {
      const parentPrefix = draft.parentPath === '.' ? '' : `${draft.parentPath}/`;
      const targetPath = `${parentPrefix}${draftValue.trim()}`;

      if (draft.mode === 'rename' && draft.targetPath) {
        await apiService.renameItem(draft.targetPath, targetPath);
      } else {
        await apiService.createItem(targetPath, draft.mode === 'create-folder');
      }

      await refreshTree();
      onItemCreated?.();
      setDraft(null);
      setDraftValue('');
    } catch (err) {
      setDraftError(err instanceof Error ? err.message : 'Failed to save item');
    } finally {
      setLoading(false);
    }
  }, [apiService, draft, draftValue, onItemCreated, refreshTree]);

  const handleDraftKeyDown = useCallback((event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      handleConfirmDraft();
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      handleCancelDraft();
    }
  }, [handleCancelDraft, handleConfirmDraft]);

  const handleDeleteTreeItem = useCallback(async (file: FileInfo) => {
    if (!window.confirm(`Delete "${file.name}"?\n\nThis action cannot be undone.`)) {
      return;
    }

    setLoading(true);
    setContextMenu(null);

    try {
      await apiService.deleteItem(file.path);
      await refreshTree();
      onDeleteItem?.(file.path);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete item');
    } finally {
      setLoading(false);
    }
  }, [apiService, onDeleteItem, refreshTree]);

  const toggleDir = useCallback(async (dirPath: string) => {
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

    const dir = findFileByPath(filesRef.current, dirPath);
    const needsLoad = Boolean(dir && (!dir.children || dir.children.length === 0));
    if (!needsLoad) {
      return;
    }

    const children = await fetchFiles(dirPath);
    setFiles((prev) => updateFileChildren(prev, dirPath, children));
  }, [expandedDirs, fetchFiles, findFileByPath, updateFileChildren]);

  const handleClick = useCallback(async (file: FileInfo) => {
    if (file.isDir) {
      await toggleDir(file.path);
      return;
    }
    onFileSelect(file);
  }, [onFileSelect, toggleDir]);

  const getFileIcon = (file: FileInfo): React.ReactNode => {
    if (file.isDir) {
      return expandedDirs.has(file.path)
        ? <FolderOpen size={16} className="icon-folder icon-folder-open" />
        : <Folder size={16} className="icon-folder" />;
    }

    switch (file.ext?.toLowerCase()) {
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
      case '.txt':
        return <FileText size={16} className="icon-file-text" />;
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

  const renderDraftRow = (parentPath: string, depth: number): JSX.Element | null => {
    if (!draft || draft.mode === 'rename' || draft.parentPath !== parentPath) {
      return null;
    }

    return (
      <div className="file-tree-draft-row" style={{ paddingLeft: `${depth * 16 + 24}px` }}>
        <input
          ref={inputRef}
          type="text"
          value={draftValue}
          onChange={(event) => setDraftValue(event.target.value)}
          onKeyDown={handleDraftKeyDown}
          placeholder={draft.mode === 'create-file' ? 'filename.ext or nested/path.ext' : 'folder or nested/path'}
          className="create-input"
          aria-label="Enter name for new item"
        />
        <div className="create-actions">
          <button
            className="create-btn create-confirm"
            onClick={handleConfirmDraft}
            disabled={loading || !draftValue.trim()}
            aria-label="Create item"
          >
            <Check size={14} />
          </button>
          <button
            className="create-btn create-cancel"
            onClick={handleCancelDraft}
            aria-label="Cancel"
          >
            <X size={14} />
          </button>
        </div>
      </div>
    );
  };

  const renderFileTree = (fileList: FileInfo[], depth = 0): JSX.Element[] => (
    fileList.map((file) => {
      const isExpanded = expandedDirs.has(file.path);
      const isSelected = selectedFile === file.path;
      const hasChildren = file.isDir && Array.isArray(file.children) && file.children.length > 0;
      const isRenaming = draft?.mode === 'rename' && draft.targetPath === file.path;

      return (
        <React.Fragment key={file.path}>
          <div
            className={`file-tree-item ${file.isDir ? 'directory' : 'file'} ${isSelected ? 'selected' : ''}${file.gitStatus ? ` git-${file.gitStatus}` : ''}`}
            style={{ paddingLeft: `${depth * 16 + 8}px` }}
            data-ext={file.ext || ''}
            data-git-status={file.gitStatus || ''}
            onClick={() => handleClick(file)}
            onContextMenu={(event) => {
              event.preventDefault();
              const rect = treeRef.current?.getBoundingClientRect();
              setContextMenu({
                x: event.clientX - (rect?.left || 0),
                y: event.clientY - (rect?.top || 0),
                file,
              });
            }}
            role="treeitem"
            tabIndex={0}
            aria-selected={isSelected}
            aria-expanded={file.isDir ? isExpanded : undefined}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                handleClick(file);
                return;
              }
              if (event.key === 'Delete') {
                event.preventDefault();
                handleDeleteTreeItem(file);
              }
            }}
          >
            <div className="file-tree-icon">{getFileIcon(file)}</div>
            {file.isDir && (
              <span className="file-tree-expand">
                {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              </span>
            )}
            {isRenaming ? (
              <div className="file-tree-inline-editor" onClick={(event) => event.stopPropagation()}>
                <input
                  ref={inputRef}
                  type="text"
                  value={draftValue}
                  onChange={(event) => setDraftValue(event.target.value)}
                  onKeyDown={handleDraftKeyDown}
                  className="create-input"
                  aria-label={`Rename ${file.name}`}
                />
                <div className="create-actions">
                  <button
                    className="create-btn create-confirm"
                    onClick={handleConfirmDraft}
                    disabled={loading || !draftValue.trim()}
                    aria-label="Rename item"
                  >
                    <Check size={14} />
                  </button>
                  <button
                    className="create-btn create-cancel"
                    onClick={handleCancelDraft}
                    aria-label="Cancel rename"
                  >
                    <X size={14} />
                  </button>
                </div>
              </div>
            ) : (
              <span className="file-tree-name">{file.name}</span>
            )}
            {file.isDir && hasChildren ? (
              <span className="file-tree-count">({file.children?.length})</span>
            ) : null}
          </div>

          {file.isDir && isExpanded && (
            <div className="file-tree-children">
              {renderDraftRow(file.path, depth + 1)}
              {Array.isArray(file.children) ? renderFileTree(file.children, depth + 1) : null}
            </div>
          )}
        </React.Fragment>
      );
    })
  );

  return (
    <div className="file-tree" ref={treeRef}>
      <div className="file-tree-header">
        <div className="header-left">
          <span className="header-title">Files</span>
        </div>
        <div className="header-actions">
          <button
            className="action-button create-file-btn"
            onClick={() => handleCreateItem('file')}
            disabled={loading}
            aria-label="Create new file"
            title="Create new file"
          >
            <FilePlus size={14} />
          </button>
          <button
            className="action-button create-folder-btn"
            onClick={() => handleCreateItem('folder')}
            disabled={loading}
            aria-label="Create new folder"
            title="Create new folder"
          >
            <FolderPlus size={14} />
          </button>
          <button
            className="refresh-button"
            onClick={refreshTree}
            disabled={loading}
            aria-label="Refresh"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" />
              <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" />
              <path d="M16 21h5v-5" />
            </svg>
          </button>
        </div>
      </div>

      {draftError ? (
        <div className="create-error-message">
          <AlertTriangle size={14} />
          <span>{draftError}</span>
        </div>
      ) : null}

      {loading ? (
        <div className="loading-indicator">
          <div className="spinner"><Zap size={16} /></div>
          <span>Loading...</span>
        </div>
      ) : null}

      {error ? (
        <div className="error-message">
          <span className="error-icon"><AlertTriangle size={16} /></span>
          <span className="error-text">{error}</span>
        </div>
      ) : null}

      <div className="file-list">
        {renderDraftRow(rootPath, 0)}
        {renderFileTree(files)}
        {files.length === 0 && !loading && !error ? (
          <div className="empty-directory">
            <span className="empty-icon"><FolderOpen size={16} /></span>
            <span className="empty-text">Empty directory</span>
          </div>
        ) : null}
      </div>

      {contextMenu ? (
        <div
          className="file-tree-context-menu"
          style={{ left: `${contextMenu.x}px`, top: `${contextMenu.y}px` }}
          onClick={(event) => event.stopPropagation()}
        >
          {contextMenu.file.isDir ? (
            <>
              <button className="file-tree-context-item" onClick={() => handleCreateItem('file', contextMenu.file.path)}>Add file</button>
              <button className="file-tree-context-item" onClick={() => handleCreateItem('folder', contextMenu.file.path)}>Add folder</button>
            </>
          ) : null}
          <button className="file-tree-context-item" onClick={() => handleStartRename(contextMenu.file)}>Rename</button>
          <button className="file-tree-context-item danger" onClick={() => handleDeleteTreeItem(contextMenu.file)}>Delete</button>
        </div>
      ) : null}
    </div>
  );
});

export default FileTree;
