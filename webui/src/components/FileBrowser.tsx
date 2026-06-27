import { Skeleton } from '@sprout/ui';
import { Folder, File, ArrowUp, X, FolderOpen } from 'lucide-react';
import { useState, useEffect, useCallback } from 'react';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';
import './FileBrowser.css';

export interface FileNode {
  id: string;
  name: string;
  path: string;
  type: 'file' | 'directory';
  size?: number;
  modified?: number;
  children?: FileNode[];
}

interface FileBrowserProps {
  isOpen: boolean;
  initialPath?: string;
  onSelect: (files: FileNode[]) => void;
  onCancel: () => void;
  allowDirectories?: boolean;
  allowedExtensions?: string[];
  browseEndpoint?: string;
}

function FileBrowser({
  isOpen,
  initialPath = '/',
  onSelect,
  onCancel,
  allowDirectories = false,
  allowedExtensions = [],
  browseEndpoint = '/api/browse',
}: FileBrowserProps): JSX.Element | null {
  const [currentPath, setCurrentPath] = useState(initialPath);
  const [pathInput, setPathInput] = useState(initialPath);
  const [files, setFiles] = useState<FileNode[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) {
      setCurrentPath(initialPath);
      setPathInput(initialPath);
      setSelectedIds(new Set());
    }
  }, [initialPath, isOpen]);

  const loadDirectory = useCallback(
    async (path: string) => {
      setLoading(true);
      setError(null);

      try {
        const response = await clientFetch(`${browseEndpoint}?path=${encodeURIComponent(path)}`);
        if (!response.ok) {
          throw new Error(`Failed to browse directory: ${response.statusText}`);
        }

        const data = await response.json();
        const directoryFiles: FileNode[] = (data.files || []).map((file: Record<string, unknown>) => ({
          id: String(file.path || `${path}/${file.name}`),
          name: String(file.name),
          path: String(file.path),
          type: file.type === 'directory' ? 'directory' : 'file',
          size: typeof file.size === 'number' ? file.size : undefined,
          modified: typeof file.modified === 'number' ? file.modified : undefined,
        }));

        const sortedFiles = directoryFiles.sort((a, b) => {
          if (a.type !== b.type) {
            return a.type === 'directory' ? -1 : 1;
          }
          return a.name.localeCompare(b.name);
        });

        setFiles(sortedFiles);
        setSelectedIds(new Set());
      } catch (err) {
        debugLog('[FileBrowser] Failed to load directory:', err);
        setError(err instanceof Error ? err.message : 'Failed to load directory');
        setFiles([]);
        setSelectedIds(new Set());
      } finally {
        setLoading(false);
      }
    },
    [browseEndpoint],
  );

  useEffect(() => {
    if (isOpen) {
      loadDirectory(currentPath);
    }
  }, [isOpen, currentPath, loadDirectory]);

  // Resolve a node by id from the flat directory listing (current directory only).
  const findNodeById = useCallback((id: string): FileNode | undefined => files.find((f) => f.id === id), [files]);

  const toggleSelection = (file: FileNode) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(file.id)) {
        next.delete(file.id);
      } else {
        next.add(file.id);
      }
      return next;
    });
  };

  const handleFileClick = (file: FileNode, event: React.MouseEvent) => {
    if (file.type === 'directory' && !event.metaKey && !event.ctrlKey && !event.shiftKey) {
      // Single click on a directory (without modifier) navigates into it.
      setCurrentPath(file.path);
      setPathInput(file.path);
      return;
    }
    toggleSelection(file);
  };

  const handleFileDoubleClick = (file: FileNode) => {
    if (file.type === 'file' || allowDirectories) {
      onSelect([file]);
    }
  };

  const handleSelect = () => {
    const selected = Array.from(selectedIds)
      .map((id) => findNodeById(id))
      .filter((node): node is FileNode => node !== undefined);
    if (selected.length > 0) {
      onSelect(selected);
    }
  };

  const navigateUp = () => {
    const parts = currentPath.split('/').filter(Boolean);
    parts.pop();
    const newPath = `/${parts.join('/')}` || '/';
    setCurrentPath(newPath);
    setPathInput(newPath);
  };

  const formatFileSize = (bytes: number) => {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const filterFiles = (nestedFiles: FileNode[]) => {
    if (allowedExtensions.length === 0) return nestedFiles;
    return nestedFiles.filter((file) => {
      if (file.type === 'directory') return true;
      const ext = file.name.split('.').pop()?.toLowerCase();
      return ext && allowedExtensions.includes(ext);
    });
  };

  const filteredFiles = filterFiles(files);

  if (!isOpen) return null;

  const selectButtonDisabled =
    selectedIds.size === 0 ||
    Array.from(selectedIds)
      .map((id) => findNodeById(id))
      .filter((node): node is FileNode => node !== undefined && node.type === 'directory' && !allowDirectories).length > 0;

  return (
    <div className="filebrowser-overlay" onClick={onCancel}>
      <div className="filebrowser-container" onClick={(e) => e.stopPropagation()}>
        <div className="filebrowser-header">
          <h3>
            <FolderOpen size={16} style={{ marginRight: 6, verticalAlign: 'middle' }} /> File Browser
            {selectedIds.size > 0 && (
              <span className="filebrowser-selection-count">{selectedIds.size} selected</span>
            )}
          </h3>
          <button className="filebrowser-close" onClick={onCancel}>
            <X size={16} />
          </button>
        </div>

        <div className="filebrowser-nav">
          <button className="filebrowser-nav-button" onClick={navigateUp} disabled={currentPath === '/'}>
            <ArrowUp size={14} style={{ marginRight: 4, verticalAlign: 'middle' }} /> Up
          </button>
          <div className="filebrowser-path">
            <input
              type="text"
              value={pathInput}
              onChange={(e) => setPathInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  setCurrentPath(pathInput);
                }
              }}
              className="filebrowser-path-input"
            />
          </div>
        </div>

        <div className="filebrowser-content">
          {loading ? (
            <div className="filebrowser-loading" role="status" aria-label="Loading files">
              <div className="filebrowser-skeleton">
                {Array.from({ length: 8 }, (_, i) => (
                  <div key={i} className="filebrowser-skeleton-item">
                    <Skeleton width="16px" height="16px" radius="4px" />
                    <div className="filebrowser-skeleton-info">
                      <Skeleton width={`${60 + Math.floor((i * 47) % 40)}%`} height="14px" />
                    </div>
                  </div>
                ))}
              </div>
              <span className="sr-only">Loading files...</span>
            </div>
          ) : error ? (
            <div className="filebrowser-error">{error}</div>
          ) : (
            <div className="filebrowser-list">
              {filteredFiles.map((file) => (
                <div
                  key={file.id}
                  className={`filebrowser-item ${selectedIds.has(file.id) ? 'selected' : ''}`}
                  onClick={(e) => handleFileClick(file, e)}
                  onDoubleClick={() => handleFileDoubleClick(file)}
                >
                  <input
                    type="checkbox"
                    className="filebrowser-checkbox"
                    checked={selectedIds.has(file.id)}
                    onChange={() => toggleSelection(file)}
                    onClick={(e) => e.stopPropagation()}
                    aria-label={`Select ${file.name}`}
                  />
                  <div className="filebrowser-icon">
                    {file.type === 'directory' ? <Folder size={16} /> : <File size={16} />}
                  </div>
                  <div className="filebrowser-info">
                    <div className="filebrowser-name">{file.name}</div>
                    <div className="filebrowser-details">
                      {file.type === 'directory' ? 'Directory' : formatFileSize(file.size || 0)}
                      {file.modified && ` • ${new Date(file.modified * 1000).toLocaleDateString()}`}
                    </div>
                  </div>
                </div>
              ))}
              {filteredFiles.length === 0 && <div className="filebrowser-empty">This directory is empty</div>}
            </div>
          )}
        </div>

        <div className="filebrowser-footer">
          <div className="filebrowser-help">
            Click to select, double-click or use Select to confirm. Hold Ctrl/Cmd to add to selection.
          </div>
          <div className="filebrowser-actions">
            <button className="filebrowser-button secondary" onClick={onCancel}>
              Cancel
            </button>
            <button
              className="filebrowser-button primary"
              onClick={handleSelect}
              disabled={selectButtonDisabled}
              title={
                selectedIds.size === 0
                  ? 'Select at least one file'
                  : selectedIds.size === 1
                    ? 'Select 1 file'
                    : `Select ${selectedIds.size} files`
              }
            >
              Select {selectedIds.size > 0 && `(${selectedIds.size})`}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default FileBrowser;