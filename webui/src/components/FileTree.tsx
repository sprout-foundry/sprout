import { useState, useEffect, useCallback, useRef, useMemo, forwardRef, useImperativeHandle, Fragment } from 'react';
import type { KeyboardEvent, ReactNode, DragEvent } from 'react';
import { showThemedConfirm } from './ThemedDialog';
import ContextMenu from './ContextMenu';
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
  Search,
  FilePlus,
  FolderPlus,
  X,
  Check,
  Eye,
  EyeOff,
} from 'lucide-react';
import './FileTree.css';
import { ApiService } from '../services/api';
import { clientFetch } from '../services/clientSession';
import { copyToClipboard } from '../utils/clipboard';
import { fuzzyScore, highlightMatches } from '../utils/fuzzyMatch';

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
  files: Array<
    FileInfo & {
      is_dir?: boolean;
      mod_time?: number;
      git_status?: string;
    }
  >;
}

interface FileTreeProps {
  onFileSelect: (file: FileInfo) => void;
  selectedFile?: string;
  rootPath?: string;
  onRefresh?: () => void;
  onItemCreated?: () => void;
  onDeleteItem?: (path: string) => void;
  workspaceRoot?: string;
}

interface FileTreeHandle {
  refresh: () => void;
  revealFile: (filePath: string) => void;
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

const FileTree = forwardRef<FileTreeHandle, FileTreeProps>(
  ({ onFileSelect, selectedFile, rootPath = '.', onRefresh, onItemCreated, onDeleteItem, workspaceRoot }, ref) => {
    const apiService = ApiService.getInstance();

    const [files, setFiles] = useState<FileInfo[]>([]);
    const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set([rootPath]));
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [draft, setDraft] = useState<DraftState | null>(null);
    const [draftValue, setDraftValue] = useState('');
    const [draftError, setDraftError] = useState<string | null>(null);
    const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
    const [bgContextMenu, setBgContextMenu] = useState<{ x: number; y: number } | null>(null);
    const filesRef = useRef<FileInfo[]>([]);
    const inputRef = useRef<HTMLInputElement>(null);
    const fileListRef = useRef<HTMLDivElement>(null);
    const [internalSelectedFile, setInternalSelectedFile] = useState<string | null>(null);
    const [filterQuery, setFilterQuery] = useState('');
    const [isFilterFocused, setIsFilterFocused] = useState(false);
    const [showIgnoredFiles, setShowIgnoredFiles] = useState<boolean>(() => {
      try {
        return localStorage.getItem('filetree-show-ignored') !== 'false';
      } catch {
        return true;
      }
    });

    // ── Drag-and-drop state ────────────────────────────────────────────
    const [draggedPath, setDraggedPath] = useState<string | null>(null);
    const [dropTargetPath, setDropTargetPath] = useState<string | null>(null);
    const [isDropOnRoot, setIsDropOnRoot] = useState(false);

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

    const updateFileChildren = useCallback(
      (fileList: FileInfo[], dirPath: string, children: FileInfo[]): FileInfo[] =>
        fileList.map((file) => {
          if (file.path === dirPath) {
            return { ...file, children: children.length > 0 ? children : undefined };
          }
          if (file.children) {
            return { ...file, children: updateFileChildren(file.children, dirPath, children) };
          }
          return file;
        }),
      [],
    );

    const fetchFiles = useCallback(async (path: string): Promise<FileInfo[]> => {
      try {
        const response = await clientFetch(`/api/files?path=${encodeURIComponent(path)}`);
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
            ext:
              (file.isDir ?? file.is_dir) ? '' : file.name.includes('.') ? `.${file.name.split('.').pop() || ''}` : '',
            gitStatus: (file.git_status as FileInfo['gitStatus']) || undefined,
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
          throw new Error('Backend not connected. Start with: ./ledit agent');
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
      revealFile,
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

    // Persist showIgnoredFiles preference to localStorage
    useEffect(() => {
      try {
        localStorage.setItem('filetree-show-ignored', String(showIgnoredFiles));
      } catch {
        // Ignore storage errors (e.g. quota exceeded, private browsing)
      }
    }, [showIgnoredFiles]);

    // ContextMenu handles its own dismissal

    const startDraft = useCallback(
      (nextDraft: DraftState, initialValue = '') => {
        setContextMenu(null);
        setBgContextMenu(null);
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
      },
      [rootPath],
    );

    const handleCreateItem = useCallback(
      (type: 'file' | 'folder', parentPath = rootPath) => {
        startDraft({
          mode: type === 'file' ? 'create-file' : 'create-folder',
          parentPath,
        });
      },
      [rootPath, startDraft],
    );

    const handleStartRename = useCallback(
      (file: FileInfo) => {
        const segments = file.path.split('/').filter(Boolean);
        segments.pop();
        const parentPath = segments.length > 0 ? segments.join('/') : rootPath;
        startDraft(
          {
            mode: 'rename',
            parentPath,
            targetPath: file.path,
          },
          file.name,
        );
      },
      [rootPath, startDraft],
    );

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

    const handleDraftKeyDown = useCallback(
      (event: KeyboardEvent<HTMLInputElement>) => {
        if (event.key === 'Enter') {
          event.preventDefault();
          handleConfirmDraft();
          return;
        }
        if (event.key === 'Escape') {
          event.preventDefault();
          handleCancelDraft();
        }
      },
      [handleCancelDraft, handleConfirmDraft],
    );

    const handleDeleteTreeItem = useCallback(
      async (file: FileInfo) => {
        if (
          !(await showThemedConfirm(`Delete "${file.name}"?\n\nThis action cannot be undone.`, {
            title: 'Confirm Delete',
            type: 'danger',
          }))
        ) {
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
      },
      [apiService, onDeleteItem, refreshTree],
    );

    const toggleDir = useCallback(
      async (dirPath: string) => {
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
      },
      [expandedDirs, fetchFiles, findFileByPath, updateFileChildren],
    );

    const getAncestors = useCallback((filePath: string, _dirRoot: string): string[] => {
      const segments = filePath.split('/').filter(Boolean);
      const ancestors: string[] = [];
      // Build ancestor paths: e.g. "pkg/webui/server.go" -> ["pkg", "pkg/webui"]
      for (let i = 0; i < segments.length - 1; i++) {
        const ancestorPath = segments.slice(0, i + 1).join('/');
        ancestors.push(ancestorPath);
      }
      return ancestors;
    }, []);

    const revealFile = useCallback(
      async (filePath: string) => {
        // Skip empty paths
        if (!filePath) {
          return;
        }

        // If the target file is ignored and we're hiding ignored files,
        // temporarily show them so the reveal can work.
        if (!showIgnoredFiles) {
          const target = findFileByPath(filesRef.current, filePath);
          if (target?.gitStatus === 'ignored') {
            setShowIgnoredFiles(true);
          }
        }

        // Compute all ancestor directories
        const ancestors = getAncestors(filePath, rootPath);
        const newAncestors = ancestors.filter((a) => !expandedDirs.has(a));

        // Add new ancestors to expandedDirs
        if (newAncestors.length > 0) {
          setExpandedDirs((prev) => {
            const next = new Set(prev);
            newAncestors.forEach((a) => next.add(a));
            return next;
          });
        }

        // Fetch children for newly expanded directories that don't have children loaded
        // Fetch from deepest to shallowest so parent structures are in place
        if (newAncestors.length > 0) {
          // Sort by depth (deepest first)
          const sortedAncestors = [...newAncestors].sort((a, b) => b.split('/').length - a.split('/').length);

          for (const dirPath of sortedAncestors) {
            const dir = findFileByPath(filesRef.current, dirPath);
            if (!dir || dir.isDir) {
              const children = await fetchFiles(dirPath);
              setFiles((prev) => updateFileChildren(prev, dirPath, children));
            }
          }
        }

        // Set the selected file
        setInternalSelectedFile(filePath);

        // Scroll the selected element into view after state updates
        setTimeout(() => {
          const selectedElement = fileListRef.current?.querySelector('.file-tree-item.selected');
          if (selectedElement) {
            // Add flash animation class
            selectedElement.classList.add('revealed');

            // Scroll into view
            selectedElement.scrollIntoView({ behavior: 'smooth', block: 'nearest' });

            // Remove flash animation class after animation
            setTimeout(() => {
              selectedElement.classList.remove('revealed');
            }, 1500);
          }
        }, 100);
      },
      [getAncestors, rootPath, expandedDirs, findFileByPath, fetchFiles, updateFileChildren, showIgnoredFiles],
    );

    // ── Filter / fuzzy search ────────────────────────────────────────────

    const isFiltering = filterQuery.trim().length > 0;

    // Recursively remove ignored files but keep directories visible (even when empty)
    const filterIgnoredFiles = useCallback((items: FileInfo[]): FileInfo[] => {
      return items.reduce<FileInfo[]>((acc, item) => {
        if (item.gitStatus === 'ignored') {
          return acc;
        }
        if (item.isDir && item.children) {
          const filteredChildren = filterIgnoredFiles(item.children);
          acc.push({
            ...item,
            children: filteredChildren.length > 0 ? filteredChildren : undefined,
          });
          return acc;
        }
        acc.push(item);
        return acc;
      }, []);
    }, []);

    // Shared pre-filtered source: remove ignored files when toggle is off
    const visibleFiles = useMemo(
      () => (showIgnoredFiles ? files : filterIgnoredFiles(files)),
      [files, filterIgnoredFiles, showIgnoredFiles],
    );

    // Flatten tree into all items for filtering
    const flatFiles = useMemo(() => {
      const result: FileInfo[] = [];
      const flatten = (items: FileInfo[]) => {
        for (const item of items) {
          result.push(item);
          if (item.children) flatten(item.children);
        }
      };
      flatten(visibleFiles);
      return result;
    }, [visibleFiles]);

    // Compute filter matches: path -> { score, matches }
    const filterMatches = useMemo(() => {
      if (!filterQuery.trim()) return new Map<string, { score: number; matches: Array<[number, number]> }>();

      const matches = new Map<string, { score: number; matches: Array<[number, number]> }>();

      for (const file of flatFiles) {
        const { score, matches: m } = fuzzyScore(filterQuery, file.path);
        if (score >= 0) {
          matches.set(file.path, { score, matches: m });
        }
      }

      return matches;
    }, [filterQuery, flatFiles]);

    // Build filtered tree: keep dirs that transitively contain matching files.
    // Returns an empty array when nothing matches (the caller decides whether
    // to show the unfiltered tree or the "no results" message).
    const filteredFiles = useMemo(() => {
      if (filterMatches.size === 0) return [];

      // Set of all paths that either match or are ancestors of matching files
      const visiblePaths = new Set<string>();

      filterMatches.forEach((_match, path) => {
        visiblePaths.add(path);
        // Add all ancestor directories
        const segments = path.split('/');
        for (let i = 1; i < segments.length; i++) {
          const ancestor = segments.slice(0, i).join('/');
          visiblePaths.add(ancestor);
        }
      });

      // Filter tree recursively
      const filterTree = (items: FileInfo[]): FileInfo[] => {
        return items
          .filter((item) => visiblePaths.has(item.path))
          .map((item) => {
            if (item.isDir && item.children) {
              const filteredChildren = filterTree(item.children);
              return {
                ...item,
                children: filteredChildren.length > 0 ? filteredChildren : undefined,
              };
            }
            return item;
          });
      };

      return filterTree(visibleFiles);
    }, [filterMatches, visibleFiles]);

    const treeData = isFiltering ? filteredFiles : visibleFiles;

    const handleFilterKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setFilterQuery('');
        e.currentTarget.blur();
      }
    }, []);

    const handleClick = useCallback(
      async (file: FileInfo) => {
        if (file.isDir && !isFiltering) {
          await toggleDir(file.path);
          return;
        }
        // During filtering, clicking a directory does nothing visible
        if (!file.isDir) {
          onFileSelect(file);
        }
      },
      [isFiltering, onFileSelect, toggleDir],
    );

    const getFileIcon = (file: FileInfo): ReactNode => {
      if (file.isDir) {
        return expandedDirs.has(file.path) ? (
          <FolderOpen size={16} className="icon-folder icon-folder-open" />
        ) : (
          <Folder size={16} className="icon-folder" />
        );
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

    // ── Drag-and-drop handlers ───────────────────────────────────────
    // Returns true if candidatePath is the same as, or is a descendant of, ancestorPath
    const isAncestorOrSelf = useCallback((ancestorPath: string, candidatePath: string): boolean => {
      if (ancestorPath === candidatePath) return true;
      return candidatePath.startsWith(`${ancestorPath}/`);
    }, []);

    const getParentPath = useCallback(
      (filePath: string): string => {
        const segments = filePath.split('/').filter(Boolean);
        segments.pop();
        return segments.length > 0 ? segments.join('/') : rootPath;
      },
      [rootPath],
    );

    const handleDragStart = useCallback(
      (e: DragEvent, filePath: string) => {
        // Don't allow drag while in draft mode (creating/renaming)
        if (draft) return;
        setDraggedPath(filePath);
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('application/x-ledit-filepath', filePath);
        // Set a minimal drag image for better UX
        if (e.currentTarget instanceof HTMLElement) {
          e.dataTransfer.setDragImage(e.currentTarget, 0, 0);
        }
      },
      [draft],
    );

    const handleDragEnd = useCallback(() => {
      setDraggedPath(null);
      setDropTargetPath(null);
      setIsDropOnRoot(false);
    }, []);

    const handleDragOver = useCallback(
      (e: DragEvent, filePath: string, file: FileInfo) => {
        if (!draggedPath) return;

        // Non-directory items: don't handle — let the event bubble to .file-list
        // so the background root drop zone can activate.
        if (!file.isDir) return;

        // This item will handle the drag — stop it from reaching the background
        e.preventDefault();
        e.stopPropagation();

        // Clear root drop zone when hovering a specific valid directory target
        setIsDropOnRoot(false);

        // Can't drop onto self or descendants
        if (isAncestorOrSelf(draggedPath, filePath)) {
          setDropTargetPath(null);
          e.dataTransfer.dropEffect = 'none';
          return;
        }

        // Can't drop into same parent (no-op)
        const currentParent = getParentPath(draggedPath);
        if (currentParent === filePath) {
          setDropTargetPath(null);
          e.dataTransfer.dropEffect = 'none';
          return;
        }

        setDropTargetPath(filePath);
        e.dataTransfer.dropEffect = 'move';
      },
      [draggedPath, isAncestorOrSelf, getParentPath],
    );

    const handleDragLeave = useCallback((e: DragEvent) => {
      // Only clear if we're truly leaving this element (not entering a child)
      const relatedTarget = e.relatedTarget as Node | null;
      if (relatedTarget && e.currentTarget.contains(relatedTarget)) {
        return;
      }
      setDropTargetPath(null);
    }, []);

    const executeMove = useCallback(
      async (sourcePath: string, targetDirPath: string, targetDirName: string, existingChildren?: FileInfo[]) => {
        const sourceName = sourcePath.split('/').pop() || '';
        const existingChild = existingChildren?.find((child) => child.name === sourceName && child.path !== sourcePath);

        if (existingChild) {
          const confirmed = await showThemedConfirm(
            `"${sourceName}" already exists in "${targetDirName}".\n\nReplace it? This cannot be undone.`,
            { title: 'Confirm Replace', type: 'danger' },
          );
          if (!confirmed) return;
        }

        const targetPrefix = targetDirPath === rootPath ? '' : `${targetDirPath}/`;
        const newPath = `${targetPrefix}${sourceName}`;

        try {
          setLoading(true);
          await apiService.renameItem(sourcePath, newPath);

          setExpandedDirs((prev) => {
            const next = new Set(prev);
            if (targetDirPath !== rootPath) {
              next.add(targetDirPath);
            }
            return next;
          });

          await refreshTree();
          setInternalSelectedFile(newPath);
          onItemCreated?.();
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Failed to move item');
        } finally {
          setLoading(false);
        }
      },
      [apiService, refreshTree, onItemCreated, rootPath],
    );

    const handleDrop = useCallback(
      async (e: DragEvent, targetDirPath: string) => {
        e.preventDefault();
        e.stopPropagation();
        setDropTargetPath(null);
        setIsDropOnRoot(false);

        const sourcePath = draggedPath || e.dataTransfer.getData('application/x-ledit-filepath');
        if (!sourcePath) {
          setDraggedPath(null);
          return;
        }
        if (sourcePath === targetDirPath) {
          setDraggedPath(null);
          return;
        }
        setDraggedPath(null);

        // Validate: target must be a directory
        const targetDir = findFileByPath(filesRef.current, targetDirPath);
        if (!targetDir?.isDir) return;

        // Validate: not dropping onto self or descendant
        if (isAncestorOrSelf(sourcePath, targetDirPath)) return;

        // Validate: not same parent (no-op)
        const currentParent = getParentPath(sourcePath);
        if (currentParent === targetDirPath) return;

        await executeMove(sourcePath, targetDirPath, targetDir.name, targetDir.children);
      },
      [draggedPath, findFileByPath, isAncestorOrSelf, getParentPath, executeMove],
    );

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
            <button className="create-btn create-cancel" onClick={handleCancelDraft} aria-label="Cancel">
              <X size={14} />
            </button>
          </div>
        </div>
      );
    };

    const renderFileTree = (fileList: FileInfo[], depth = 0): JSX.Element[] =>
      fileList.map((file) => {
        const isExpanded = isFiltering ? true : expandedDirs.has(file.path);
        const isSelected = (internalSelectedFile ?? selectedFile) === file.path;
        const hasChildren = file.isDir && Array.isArray(file.children) && file.children.length > 0;
        const isRenaming = draft?.mode === 'rename' && draft.targetPath === file.path;
        const matchInfo = isFiltering ? filterMatches.get(file.path) : undefined;

        // Compute name-level matches from path-level matches for highlighting
        const nameMatches = matchInfo
          ? (() => {
              const nameStart = file.path.lastIndexOf('/') + 1;
              const ranges = matchInfo.matches
                .filter(([_s, e]) => e > nameStart) // match must touch the name
                .map(([s, e]) => {
                  const clampedStart = Math.max(s, nameStart);
                  return [clampedStart - nameStart, e - nameStart] as [number, number];
                })
                .filter(([s, e]) => s < e);
              return ranges.length > 0 ? ranges : undefined;
            })()
          : undefined;

        return (
          <Fragment key={file.path}>
            <div
              className={`file-tree-item ${file.isDir ? 'directory' : 'file'} ${isSelected ? 'selected' : ''}${file.gitStatus ? ` git-${file.gitStatus}` : ''}${dropTargetPath === file.path ? ' drop-target' : ''}${draggedPath === file.path ? ' dragging' : ''}`}
              style={{ paddingLeft: `${depth * 16 + 8}px` }}
              data-ext={file.ext || ''}
              data-git-status={file.gitStatus || ''}
              draggable={!draft}
              onClick={() => handleClick(file)}
              onDragStart={(event) => handleDragStart(event, file.path)}
              onDragEnd={handleDragEnd}
              onDragOver={(event) => handleDragOver(event, file.path, file)}
              onDragLeave={handleDragLeave}
              onDrop={(event) => handleDrop(event, file.path)}
              onContextMenu={(event) => {
                event.preventDefault();
                event.stopPropagation();
                setBgContextMenu(null);
                setContextMenu({
                  x: event.clientX,
                  y: event.clientY,
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
                    <button className="create-btn create-cancel" onClick={handleCancelDraft} aria-label="Cancel rename">
                      <X size={14} />
                    </button>
                  </div>
                </div>
              ) : nameMatches ? (
                <span
                  className="file-tree-name"
                  dangerouslySetInnerHTML={{ __html: highlightMatches(file.name, nameMatches) }}
                />
              ) : (
                <span className="file-tree-name">{file.name}</span>
              )}
              {file.isDir && hasChildren ? <span className="file-tree-count">({file.children?.length})</span> : null}
            </div>

            {file.isDir && isExpanded && (
              <div className="file-tree-children">
                {renderDraftRow(file.path, depth + 1)}
                {Array.isArray(file.children) ? renderFileTree(file.children, depth + 1) : null}
              </div>
            )}
          </Fragment>
        );
      });

    return (
      <div className="file-tree">
        <div className="file-tree-header">
          <div className="header-left">
            <span className="header-title">Files</span>
            <div className={`file-tree-filter-wrapper ${isFilterFocused || isFiltering ? 'focused' : ''}`}>
              <Search size={13} className="file-tree-filter-icon" />
              <input
                type="text"
                role="searchbox"
                className="file-tree-filter-input"
                placeholder="Filter files..."
                value={filterQuery}
                onChange={(e) => setFilterQuery(e.target.value)}
                onFocus={() => setIsFilterFocused(true)}
                onBlur={() => setIsFilterFocused(false)}
                onKeyDown={handleFilterKeyDown}
                aria-label="Filter files"
              />
              {isFiltering && (
                <button
                  className="file-tree-filter-clear"
                  onClick={() => setFilterQuery('')}
                  aria-label="Clear filter"
                  type="button"
                >
                  <X size={12} />
                </button>
              )}
            </div>
          </div>
          <div className="header-actions">
            <button
              className={`action-button toggle-ignored-btn ${showIgnoredFiles ? 'active' : ''}`}
              onClick={() => setShowIgnoredFiles((prev) => !prev)}
              aria-label={showIgnoredFiles ? 'Hide ignored files' : 'Show ignored files'}
              title={showIgnoredFiles ? 'Hide ignored files' : 'Show ignored files'}
            >
              {showIgnoredFiles ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
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
            <button className="refresh-button" onClick={refreshTree} disabled={loading} aria-label="Refresh">
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
            <div className="spinner">
              <Zap size={16} />
            </div>
            <span>Loading...</span>
          </div>
        ) : null}

        {error ? (
          <div className="error-message">
            <span className="error-icon">
              <AlertTriangle size={16} />
            </span>
            <span className="error-text">{error}</span>
          </div>
        ) : null}

        <div
          className={`file-list ${isDropOnRoot ? 'drop-on-root' : ''}`}
          ref={fileListRef}
          role="tree"
          aria-label="File tree"
          onContextMenu={(event) => {
            event.preventDefault();
            event.stopPropagation();
            setContextMenu(null);
            setBgContextMenu({ x: event.clientX, y: event.clientY });
          }}
          onDragOver={(e) => {
            if (!draggedPath) return;
            const currentParent = getParentPath(draggedPath);
            if (currentParent === rootPath) return;
            e.preventDefault();
            e.stopPropagation();
            e.dataTransfer.dropEffect = 'move';
            setIsDropOnRoot(true);
            setDropTargetPath(null);
          }}
          onDragLeave={(e) => {
            const relatedTarget = e.relatedTarget as Node | null;
            if (relatedTarget && fileListRef.current?.contains(relatedTarget)) return;
            setIsDropOnRoot(false);
          }}
          onDrop={async (e) => {
            e.preventDefault();
            e.stopPropagation();
            setIsDropOnRoot(false);
            setDropTargetPath(null);

            const sourcePath = draggedPath || e.dataTransfer.getData('application/x-ledit-filepath');
            if (!sourcePath) return;
            setDraggedPath(null);

            const currentParent = getParentPath(sourcePath);
            if (currentParent === rootPath) return;

            await executeMove(sourcePath, rootPath, 'root directory', filesRef.current);
          }}
        >
          {renderDraftRow(rootPath, 0)}
          {renderFileTree(treeData)}
          {treeData.length === 0 && !loading && !error && !isFiltering ? (
            <div className="empty-directory">
              <span className="empty-icon">
                <FolderOpen size={16} />
              </span>
              <span className="empty-text">Empty directory</span>
            </div>
          ) : null}
          {isFiltering && treeData.length === 0 && !loading && !error ? (
            <div className="file-tree-no-results" role="status">
              <span>No files matching &quot;{filterQuery}&quot;</span>
            </div>
          ) : null}
        </div>

        <ContextMenu
          isOpen={contextMenu !== null}
          x={contextMenu?.x ?? 0}
          y={contextMenu?.y ?? 0}
          onClose={() => setContextMenu(null)}
        >
          {contextMenu?.file.isDir ? (
            <>
              <button
                className="context-menu-item"
                onClick={() => {
                  if (!contextMenu) return;
                  setContextMenu(null);
                  handleCreateItem('file', contextMenu.file.path);
                }}
              >
                Add file
              </button>
              <button
                className="context-menu-item"
                onClick={() => {
                  if (!contextMenu) return;
                  setContextMenu(null);
                  handleCreateItem('folder', contextMenu.file.path);
                }}
              >
                Add folder
              </button>
            </>
          ) : null}
          {contextMenu && (
            <button
              className="context-menu-item"
              onClick={() => {
                setContextMenu(null);
                handleStartRename(contextMenu.file);
              }}
            >
              Rename
            </button>
          )}
          {contextMenu && !contextMenu.file.isDir && (
            <>
              <div className="context-menu-divider" />
              <button
                className="context-menu-item"
                onClick={() => {
                  copyToClipboard(contextMenu.file.path);
                  setContextMenu(null);
                }}
              >
                Copy relative path
              </button>
              {workspaceRoot && (
                <button
                  className="context-menu-item"
                  onClick={() => {
                    copyToClipboard(`${workspaceRoot.replace(/\/+$/, '')}/${contextMenu.file.path}`);
                    setContextMenu(null);
                  }}
                >
                  Copy absolute path
                </button>
              )}
              <button
                className="context-menu-item"
                onClick={() => {
                  setContextMenu(null);
                  onFileSelect(contextMenu.file);
                }}
              >
                Open in editor
              </button>
              <div className="context-menu-divider" />
            </>
          )}
          {contextMenu && (
            <button
              className="context-menu-item danger"
              onClick={() => {
                setContextMenu(null);
                handleDeleteTreeItem(contextMenu.file);
              }}
            >
              Delete
            </button>
          )}
        </ContextMenu>

        <ContextMenu
          isOpen={bgContextMenu !== null}
          x={bgContextMenu?.x ?? 0}
          y={bgContextMenu?.y ?? 0}
          onClose={() => setBgContextMenu(null)}
        >
          <button
            className="context-menu-item"
            onClick={() => {
              setBgContextMenu(null);
              handleCreateItem('file');
            }}
          >
            New File
          </button>
          <button
            className="context-menu-item"
            onClick={() => {
              setBgContextMenu(null);
              handleCreateItem('folder');
            }}
          >
            New Folder
          </button>
        </ContextMenu>
      </div>
    );
  },
);

FileTree.displayName = 'FileTree';

export default FileTree;
