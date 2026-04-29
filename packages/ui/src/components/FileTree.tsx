import { useState, useCallback, useMemo, type ReactNode } from 'react';
import {
  Folder,
  FolderOpen,
} from 'lucide-react';
import { fuzzyMatch, getFileIcon, highlightMatch } from '../utils/fileTree';
import './FileTree.css';

export type GitStatus = 'modified' | 'untracked' | 'ignored' | 'added';

export interface TreeNode {
  name: string;
  path: string;
  isDir: boolean;
  children?: TreeNode[];
  gitStatus?: GitStatus;
}

export interface FileTreeProps {
  files: TreeNode[];
  onFileSelect?: (path: string) => void;
  onFileRename?: (oldPath: string, newPath: string) => void;
  onFileDelete?: (path: string) => void;
  onFileCreate?: (parentPath: string, name: string, isDir: boolean) => void;
  onFolderToggle?: (path: string, expanded: boolean) => void;
  selectedPath?: string;
  expandedPaths?: Set<string>;
  searchQuery?: string;
  className?: string;
  children?: ReactNode; // For context menu rendering
}

/**
 * Recursive tree node component.
 */
function TreeNodeComponent({
  node,
  level,
  selectedPath,
  expandedPaths,
  searchQuery,
  onFileSelect,
  onFileRename,
  onFileDelete,
  onFileCreate,
  onFolderToggle,
  editingPath,
  setEditingPath,
}: {
  node: TreeNode;
  level: number;
  selectedPath?: string;
  expandedPaths: Set<string>;
  searchQuery?: string;
  onFileSelect?: (path: string) => void;
  onFileRename?: (oldPath: string, newName: string) => void;
  onFileDelete?: (path: string) => void;
  onFileCreate?: (parentPath: string, name: string, isDir: boolean) => void;
  onFolderToggle?: (path: string, expanded: boolean) => void;
  editingPath: string | null;
  setEditingPath: (path: string | null) => void;
}): JSX.Element | null {
  const isExpanded = expandedPaths.has(node.path);
  const isSelected = selectedPath === node.path;
  const isEditing = editingPath === node.path;

  const handleClick = useCallback(() => {
    if (node.isDir) {
      onFolderToggle?.(node.path, !isExpanded);
    } else {
      onFileSelect?.(node.path);
    }
  }, [node, isExpanded, onFolderToggle, onFileSelect]);

  const handleDoubleClick = useCallback(() => {
    if (!node.isDir) {
      setEditingPath(node.path);
    }
  }, [node, setEditingPath]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') {
        const newName = (e.target as HTMLInputElement).value;
        if (newName && newName !== node.name) {
          onFileRename?.(node.path, newName);
        }
        setEditingPath(null);
      } else if (e.key === 'Escape') {
        setEditingPath(null);
      }
    },
    [node, onFileRename, setEditingPath],
  );

  const handleBlur = useCallback(() => {
    // Don't call rename on blur - it might be accidental
    setEditingPath(null);
  }, [setEditingPath]);

  const FolderIcon = isExpanded ? FolderOpen : Folder;
  const FileIcon = getFileIcon(node.name);

  // Filter children based on search query
  const filteredChildren = useMemo(() => {
    if (!searchQuery || !node.children) return node.children || [];
    return node.children.filter((child) => fuzzyMatch(searchQuery, child.name));
  }, [node.children, searchQuery]);

  // Show node if it matches or has matching children
  const shouldShow = useMemo(() => {
    if (!searchQuery) return true;
    if (fuzzyMatch(searchQuery, node.name)) return true;
    if (node.children && filteredChildren.length > 0) return true;
    return false;
  }, [node, searchQuery, filteredChildren.length]);

  if (!shouldShow) return null;

  return (
    <div className="filetree-node">
      {/* Node row */}
      <div
        className={`filetree-row ${isSelected ? 'filetree-row-selected' : ''} ${isEditing ? 'filetree-row-editing' : ''}`}
        style={{ paddingLeft: `${level * 16 + 8}px` }}
        onClick={handleClick}
        onDoubleClick={handleDoubleClick}
        role="treeitem"
        aria-expanded={node.isDir ? isExpanded : undefined}
        aria-selected={isSelected}
        tabIndex={0}
      >
        {/* Expand/collapse arrow for folders */}
        {node.isDir && (
          <span
            className={`filetree-arrow ${isExpanded ? 'filetree-arrow-expanded' : ''}`}
            aria-hidden="true"
          >
            ▶
          </span>
        )}

        {/* Icon */}
        <span className="filetree-icon" aria-hidden="true">
          {node.isDir ? <FolderIcon size={14} /> : <FileIcon size={14} />}
        </span>

        {/* Name or input for editing */}
        {isEditing ? (
          <input
            type="text"
            className="filetree-edit-input"
            defaultValue={node.name}
            autoFocus
            onKeyDown={handleKeyDown}
            onBlur={handleBlur}
            onClick={(e) => e.stopPropagation()}
          />
        ) : (
          <span className="filetree-name">{highlightMatch(node.name, searchQuery || '')}</span>
        )}

        {/* Git status indicator */}
        {node.gitStatus && (
          <span className={`filetree-git-status filetree-git-${node.gitStatus}`} aria-hidden="true">
            ●
          </span>
        )}
      </div>

      {/* Children */}
      {node.isDir && isExpanded && filteredChildren.length > 0 && (
        <div className="filetree-children">
          {filteredChildren.map((child) => (
            <TreeNodeComponent
              key={child.path}
              node={child}
              level={level + 1}
              selectedPath={selectedPath}
              expandedPaths={expandedPaths}
              searchQuery={searchQuery}
              onFileSelect={onFileSelect}
              onFileRename={onFileRename}
              onFileDelete={onFileDelete}
              onFileCreate={onFileCreate}
              onFolderToggle={onFolderToggle}
              editingPath={editingPath}
              setEditingPath={setEditingPath}
            />
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * A file tree view component with expand/collapse, selection, search,
 * git status indicators, and inline rename support.
 */
function FileTree({
  files,
  onFileSelect,
  onFileRename,
  onFileDelete,
  onFileCreate,
  onFolderToggle,
  selectedPath,
  expandedPaths = new Set(),
  searchQuery,
  className,
  children,
}: FileTreeProps): JSX.Element {
  const [editingPath, setEditingPath] = useState<string | null>(null);

  // Adapt callback signatures
  const handleFileRename = useCallback(
    (oldPath: string, newName: string) => {
      const dirPath = oldPath.substring(0, oldPath.lastIndexOf('/'));
      const newPath = dirPath ? `${dirPath}/${newName}` : newName;
      onFileRename?.(oldPath, newPath);
    },
    [onFileRename],
  );

  return (
    <div className={`filetree ${className || ''}`} role="tree">
      {files.map((node) => (
        <TreeNodeComponent
          key={node.path}
          node={node}
          level={0}
          selectedPath={selectedPath}
          expandedPaths={expandedPaths}
          searchQuery={searchQuery}
          onFileSelect={onFileSelect}
          onFileRename={handleFileRename}
          onFileDelete={onFileDelete}
          onFileCreate={onFileCreate}
          onFolderToggle={onFolderToggle}
          editingPath={editingPath}
          setEditingPath={setEditingPath}
        />
      ))}
      {children}
    </div>
  );
}

export default FileTree;
