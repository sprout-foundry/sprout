/**
 * RepoFileTree — Recursive file tree component for cloned repos.
 *
 * Reads from lightning-fs (via GitClient) and renders expandable directories.
 * Click on a file → calls onFileClick(filepath, content).
 */

import React, { useState, useEffect, useCallback } from 'react';
import { ChevronRight, ChevronDown, File, Folder, FolderOpen, Loader2 } from 'lucide-react';
import { gitClient, FileEntry } from '../../services/gitClient';
import './RepoFileTree.css';

interface RepoFileTreeProps {
  dir: string;
  onFileClick: (filepath: string, content: string) => void;
  onCreateFile?: (name: string) => Promise<void>;
  onCreateFolder?: (name: string) => Promise<void>;
  onRefresh?: () => Promise<void>;
}

const MAX_FILE_SIZE = 1_000_000; // 1MB

export const RepoFileTree: React.FC<RepoFileTreeProps> = ({
  dir,
  onFileClick,
  onCreateFile,
  onCreateFolder,
  onRefresh,
}) => {
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateInput, setShowCreateInput] = useState<'file' | 'folder' | null>(null);
  const [createName, setCreateName] = useState('');

  const loadRoot = useCallback(async () => {
    try {
      setLoading(true);
      const items = await gitClient.listDir(dir, '/');
      setEntries(items);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to read directory');
    } finally {
      setLoading(false);
    }
  }, [dir]);

  useEffect(() => {
    loadRoot();
  }, [loadRoot]);

  const handleCreate = useCallback(
    async (type: 'file' | 'folder') => {
      const name = createName.trim();
      if (!name) return;
      try {
        if (type === 'file' && onCreateFile) {
          await onCreateFile(name);
        } else if (type === 'folder' && onCreateFolder) {
          await onCreateFolder(name);
        }
        setShowCreateInput(null);
        setCreateName('');
        await loadRoot();
      } catch (err) {
        setError(err instanceof Error ? err.message : `Failed to create ${type}`);
      }
    },
    [createName, onCreateFile, onCreateFolder, loadRoot],
  );

  const renderCreateInput = () => (
    <div className="tree-create-input-row">
      <input
        className="tree-create-input"
        type="text"
        placeholder={showCreateInput === 'file' ? 'filename.ts' : 'folder-name'}
        value={createName}
        onChange={(e) => setCreateName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') handleCreate(showCreateInput!);
          if (e.key === 'Escape') {
            setShowCreateInput(null);
            setCreateName('');
          }
        }}
        autoFocus
      />
      <button className="btn btn-sm btn-primary" onClick={() => handleCreate(showCreateInput!)}>
        Create
      </button>
    </div>
  );

  if (loading) {
    return (
      <div className="repo-file-tree-loading">
        <Loader2 size={16} className="spinner" /> Loading files…
      </div>
    );
  }

  if (error) {
    return <div className="repo-file-tree-error">{error}</div>;
  }

  if (entries.length === 0 && !showCreateInput) {
    return (
      <div className="repo-file-tree">
        <div className="repo-file-tree-toolbar">
          {onCreateFile && (
            <button className="tree-toolbar-btn" onClick={() => setShowCreateInput('file')} title="New file">
              + File
            </button>
          )}
          {onCreateFolder && (
            <button className="tree-toolbar-btn" onClick={() => setShowCreateInput('folder')} title="New folder">
              + Folder
            </button>
          )}
        </div>
        <div className="repo-file-tree-empty">No files in this repo.</div>
        {showCreateInput && renderCreateInput()}
      </div>
    );
  }

  return (
    <div className="repo-file-tree">
      <div className="repo-file-tree-toolbar">
        {onCreateFile && (
          <button className="tree-toolbar-btn" onClick={() => setShowCreateInput('file')} title="New file">
            + File
          </button>
        )}
        {onCreateFolder && (
          <button className="tree-toolbar-btn" onClick={() => setShowCreateInput('folder')} title="New folder">
            + Folder
          </button>
        )}
      </div>
      {showCreateInput && renderCreateInput()}
      {entries.map((entry) => (
        <TreeNode key={entry.path} entry={entry} repoDir={dir} depth={0} onFileClick={onFileClick} />
      ))}
    </div>
  );
};

interface TreeNodeProps {
  entry: FileEntry;
  repoDir: string;
  depth: number;
  onFileClick: (filepath: string, content: string) => void;
}

const TreeNode: React.FC<TreeNodeProps> = ({ entry, repoDir, depth, onFileClick }) => {
  const [expanded, setExpanded] = useState(false);
  const [children, setChildren] = useState<FileEntry[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [fileContent, setFileContent] = useState<string | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);

  const isDir = entry.type === 'dir';

  const toggleDir = useCallback(async () => {
    if (!isDir) return;
    if (!expanded && children === null) {
      setLoading(true);
      try {
        const items = await gitClient.listDir(repoDir, entry.path);
        setChildren(items);
      } catch (err) {
        setFileError(err instanceof Error ? err.message : 'Failed to read directory');
      } finally {
        setLoading(false);
      }
    }
    setExpanded((e) => !e);
  }, [expanded, children, isDir, repoDir, entry.path]);

  const handleFileClick = useCallback(async () => {
    if (entry.size > MAX_FILE_SIZE) {
      setFileError(`File too large (${Math.round(entry.size / 1024)}KB). Max ${MAX_FILE_SIZE / 1024}KB.`);
      return;
    }
    try {
      const content = await gitClient.readFile(repoDir, entry.path);
      setFileContent(content);
      setFileError(null);
      onFileClick(entry.path, content);
    } catch (err) {
      setFileError(err instanceof Error ? err.message : 'Failed to read file');
    }
  }, [entry, repoDir, onFileClick]);

  return (
    <div className="tree-node" style={{ paddingLeft: depth * 16 }}>
      <div
        className={`tree-node-row ${isDir ? 'tree-node-dir' : 'tree-node-file'}`}
        onClick={isDir ? toggleDir : handleFileClick}
        role={isDir ? 'button' : 'button'}
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            isDir ? toggleDir() : handleFileClick();
          }
        }}
      >
        {isDir ? (
          <>
            {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            {expanded ? <FolderOpen size={14} /> : <Folder size={14} />}
          </>
        ) : (
          <>
            <span style={{ width: 14 }} />
            <File size={14} />
          </>
        )}
        <span className="tree-node-name">{entry.name}</span>
        {!isDir && entry.size > 0 && <span className="tree-node-size">{formatSize(entry.size)}</span>}
      </div>

      {isDir && expanded && (
        <div className="tree-node-children">
          {loading && (
            <div className="tree-node-loading" style={{ paddingLeft: (depth + 1) * 16 }}>
              <Loader2 size={14} className="spinner" /> Loading…
            </div>
          )}
          {children?.map((child) => (
            <TreeNode key={child.path} entry={child} repoDir={repoDir} depth={depth + 1} onFileClick={onFileClick} />
          ))}
          {fileError && (
            <div className="tree-node-error" style={{ paddingLeft: (depth + 1) * 16 }}>
              {fileError}
            </div>
          )}
        </div>
      )}

      {fileError && !isDir && (
        <div className="tree-node-error" style={{ paddingLeft: (depth + 1) * 16 }}>
          {fileError}
        </div>
      )}
      {/* fileContent is just for show — the parent handles it via onFileClick */}
      {fileContent !== null && null}
    </div>
  );
};

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

export default RepoFileTree;
