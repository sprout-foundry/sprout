import { Copy, FileText, Ban, FolderOpen } from 'lucide-react';
import { useCallback } from 'react';
import type { MouseEvent } from 'react';
import { copyToClipboard } from '../../utils/clipboard';
import { ContextMenu } from '@sprout/ui';
import type { SearchContextMenuState } from './types';
import { getRelativePath, getParentDirectory } from './useSearchState';

interface SearchContextMenuProps {
  contextMenu: SearchContextMenuState | null;
  excludePatterns: string;
  onClose: () => void;
  onFileClick: (filePath: string, lineNumber?: number) => void;
  onExcludePatternsChange: (patterns: string) => void;
}

/**
 * Right-click context menu for search results with actions:
 * - Copy line text
 * - Open in editor
 * - Copy file path
 * - Exclude file/folder from search
 */
function SearchContextMenu({
  contextMenu,
  excludePatterns,
  onClose,
  onFileClick,
  onExcludePatternsChange,
}: SearchContextMenuProps): JSX.Element {
  // ── Action handlers ──────────────────────────────────────────

  const handleCopyMatchText = useCallback(() => {
    if (contextMenu?.matchText !== undefined) {
      copyToClipboard(contextMenu.matchText);
    }
    onClose();
  }, [contextMenu?.matchText, onClose]);

  const handleOpenInEditor = useCallback(() => {
    if (contextMenu) {
      onFileClick(contextMenu.filePath, contextMenu.lineNumber);
    }
    onClose();
  }, [contextMenu, onFileClick, onClose]);

  const handleCopyFilePath = useCallback(() => {
    if (contextMenu) {
      copyToClipboard(getRelativePath(contextMenu.filePath));
    }
    onClose();
  }, [contextMenu, onClose]);

  const handleExcludeFromSearch = useCallback(() => {
    if (!contextMenu) return;

    let patternToExclude: string;
    if (contextMenu.isFileHeader) {
      patternToExclude = getRelativePath(contextMenu.filePath);
    } else {
      patternToExclude = getParentDirectory(contextMenu.filePath);
    }

    const existingPatterns = excludePatterns
      .split(',')
      .map((p) => p.trim())
      .filter((p) => p.length > 0);

    if (!existingPatterns.includes(patternToExclude)) {
      const newExclude = existingPatterns.length > 0 ? `${excludePatterns},${patternToExclude}` : patternToExclude;
      onExcludePatternsChange(newExclude);
    }

    onClose();
  }, [contextMenu, excludePatterns, onClose, onExcludePatternsChange]);

  // ── Helpers ──────────────────────────────────────────────────

  const getExcludeLabel = (): string => {
    if (!contextMenu) return '';
    if (contextMenu.isFileHeader) {
      return getRelativePath(contextMenu.filePath);
    }
    return getParentDirectory(contextMenu.filePath);
  };

  const isAlreadyExcluded = (): boolean => {
    if (!contextMenu) return false;
    const pattern = contextMenu.isFileHeader
      ? getRelativePath(contextMenu.filePath)
      : getParentDirectory(contextMenu.filePath);
    const existing = excludePatterns
      .split(',')
      .map((p) => p.trim())
      .filter((p) => p.length > 0);
    return existing.includes(pattern);
  };

  // ── Render ───────────────────────────────────────────────────

  return (
    <ContextMenu isOpen={contextMenu !== null} x={contextMenu?.x ?? 0} y={contextMenu?.y ?? 0} onClose={onClose}>
      {!contextMenu?.isFileHeader && (
        <>
          <button className="context-menu-item" onClick={handleCopyMatchText} type="button">
            <Copy size={13} />
            <span className="menu-item-label">Copy line text</span>
          </button>
          <button className="context-menu-item" onClick={handleOpenInEditor} type="button">
            <FileText size={13} />
            <span className="menu-item-label">Open in editor</span>
          </button>
          <div className="context-menu-divider" />
        </>
      )}
      <button className="context-menu-item" onClick={handleCopyFilePath} type="button">
        <FileText size={13} />
        <span className="menu-item-label">Copy file path</span>
      </button>
      <div className="context-menu-divider" />
      <button
        className={`context-menu-item ${isAlreadyExcluded() ? 'disabled' : ''}`}
        onClick={handleExcludeFromSearch}
        type="button"
        disabled={isAlreadyExcluded()}
      >
        {contextMenu?.isFileHeader ? <Ban size={13} /> : <FolderOpen size={13} />}
        <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: 1 }}>
          <span className="menu-item-label">
            {contextMenu?.isFileHeader ? 'Exclude file from search' : 'Exclude folder from search'}
          </span>
          <span
            style={{
              fontSize: 10,
              color: 'var(--text-tertiary)',
              fontFamily: 'var(--font-mono)',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {getExcludeLabel()}
          </span>
        </div>
      </button>
    </ContextMenu>
  );
}

// ── Context menu trigger handlers (used by parent) ──────────

/** Create a context menu trigger for a match row. */
export function createRowContextMenuHandler(setContextMenu: (state: SearchContextMenuState) => void) {
  return (e: MouseEvent, filePath: string, lineNumber: number, lineText: string) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      filePath,
      lineNumber,
      matchText: lineText,
      isFileHeader: false,
    });
  };
}

/** Create a context menu trigger for a file header. */
export function createFileHeaderContextMenuHandler(setContextMenu: (state: SearchContextMenuState) => void) {
  return (e: MouseEvent, filePath: string) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      filePath,
      isFileHeader: true,
    });
  };
}

export default SearchContextMenu;
