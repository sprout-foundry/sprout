import { useState, useCallback, useEffect } from 'react';
import type { CSSProperties } from 'react';
import DocumentOutlinePanel from './DocumentOutlinePanel';
import ResizeHandle from './ResizeHandle';
import './EditorWithOutline.css';

interface EditorWithOutlineProps {
  /** The editor-workspace content (children) */
  children: React.ReactNode;
  /** The file content to extract symbols from */
  content: string;
  /** File extension for language detection (e.g., '.ts', '.go', '.py') */
  fileExtension?: string;
  /** The current cursor line (1-based) for sync highlighting */
  cursorLine: number;
  /** Whether a real file is open (not chat, diff, or welcome) */
  isFileOpen: boolean;
  /** Callback when user clicks a symbol — navigate editor to this line */
  onNavigateToSymbol: (line: number) => void;
}

/**
 * EditorWithOutline wraps the editor workspace with a collapsible/resizable outline panel.
 * The outline panel shows a document outline (functions, classes, etc.) and enables
 * navigation to symbols in the code.
 */
function EditorWithOutline({
  children,
  content,
  fileExtension,
  cursorLine,
  isFileOpen,
  onNavigateToSymbol,
}: EditorWithOutlineProps): JSX.Element {
  // Persist collapsed state to localStorage
  const [isCollapsed, setIsCollapsed] = useState(() => {
    if (typeof window === 'undefined') return false;
    return window.localStorage.getItem('sprout.outline-panel.collapsed') === '1';
  });

  // Persist panel width to localStorage
  const [panelWidth, setPanelWidth] = useState(() => {
    if (typeof window === 'undefined') return 240;
    const storedWidth = Number(window.localStorage.getItem('sprout.outline-panel.width'));
    if (Number.isFinite(storedWidth) && storedWidth >= 180 && storedWidth <= 500) {
      return storedWidth;
    }
    return 240;
  });

  // Sync collapsed state to localStorage
  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem('sprout.outline-panel.collapsed', isCollapsed ? '1' : '0');
  }, [isCollapsed]);

  // Sync panel width to localStorage
  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem('sprout.outline-panel.width', String(Math.round(panelWidth)));
  }, [panelWidth]);

  // Handle panel resize. The outline panel is right-anchored with the
  // resize handle on its LEFT edge — dragging the handle leftward must
  // grow the panel. ResizeHandle reports `deltaPixels = clientX - startX`
  // (positive when moving right), so we subtract to invert the sign.
  const handleResize = useCallback((deltaPixels: number) => {
    setPanelWidth((prev) => Math.max(180, Math.min(500, prev - deltaPixels)));
  }, []);

  // Handle toggle collapse
  const handleToggleCollapse = useCallback(() => {
    setIsCollapsed((prev) => !prev);
  }, []);

  // Panel style: let DocumentOutlinePanel's own CSS handle collapsed state (40px)
  // Only set explicit width when expanded
  const outlinePanelStyle: CSSProperties = isCollapsed ? {} : { width: `${panelWidth}px`, minWidth: `${panelWidth}px` };

  return (
    <div className="editor-with-outline">
      <div className="editor-workspace-wrapper">{children}</div>

      {isFileOpen && (
        <>
          {!isCollapsed && (
            <ResizeHandle direction="horizontal" onResize={handleResize} className="outline-resize-handle" />
          )}
          <div className="outline-panel-container" style={outlinePanelStyle}>
            <DocumentOutlinePanel
              content={content}
              fileExtension={fileExtension}
              cursorLine={cursorLine}
              onNavigateToSymbol={onNavigateToSymbol}
              isFileOpen={isFileOpen}
              isCollapsed={isCollapsed}
              onToggleCollapse={handleToggleCollapse}
              panelWidth={panelWidth}
            />
          </div>
        </>
      )}
    </div>
  );
}

export default EditorWithOutline;
