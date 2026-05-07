import { useState, useEffect, useCallback, useRef, type RefObject } from 'react';
import type { Terminal } from '@xterm/xterm';
import {
  Copy,
  ClipboardPaste,
  Search,
  Trash2,
  Rows2,
  Columns2,
  TextSelect,
  Link2,
} from 'lucide-react';
import { ContextMenu } from '@sprout/ui';

interface TerminalMenuState {
  visible: boolean;
  x: number;
  y: number;
  hasLink: boolean;
  linkUrl: string;
}

interface TerminalContextMenuProps {
  containerRef: RefObject<HTMLDivElement>;
  /** Get the xterm Terminal instance for selection/selection queries */
  getTerminal: () => Terminal | null;
  /** Check if terminal has a selection */
  hasSelection: () => boolean;
  /** Copy text to clipboard */
  onCopy: (text: string) => void;
  /** Paste from clipboard → write to terminal */
  onPaste: (text: string) => void;
  /** Open the search bar in parent */
  onSearch: () => void;
  /** Clear the terminal buffer */
  onClear: () => void;
  /** Select all terminal content */
  onSelectAll: () => void;
  /** Split terminal pane */
  onSplitPane: (direction: 'horizontal' | 'vertical') => void;
}

/**
 * Standalone context menu for terminal panes.
 * Listens for `contextmenu` events on the given container ref and shows
 * a menu with Copy / Paste / Search / Clear / Split / Select All / Copy Link actions.
 */
function TerminalContextMenu({
  containerRef,
  getTerminal,
  hasSelection,
  onCopy,
  onPaste,
  onSearch,
  onClear,
  onSelectAll,
  onSplitPane,
}: TerminalContextMenuProps): JSX.Element {
  const timersRef = useRef<number[]>([]);

  // Clean up all pending timers
  const clearTimers = useCallback(() => {
    timersRef.current.forEach(clearTimeout);
    timersRef.current = [];
  }, []);

  const [menu, setMenu] = useState<TerminalMenuState>({
    visible: false,
    x: 0,
    y: 0,
    hasLink: false,
    linkUrl: '',
  });

  const [copiedAction, setCopiedAction] = useState<'text' | 'link' | null>(null);

  const showCopied = useCallback((action: 'text' | 'link') => {
    setCopiedAction(action);
    timersRef.current.push(window.setTimeout(() => setCopiedAction(null), 1200));
  }, []);

  // ── Close helpers ─────────────────────────────────────────

  const close = useCallback(() => {
    clearTimers();
    setMenu((prev) => ({ ...prev, visible: false }));
  }, [clearTimers]);

  // ── Detect link under cursor (same logic as inline implementation) ───

  const detectLinkUnderCursor = useCallback((e: MouseEvent): { hasLink: boolean; linkUrl: string } => {
    const term = getTerminal();
    if (!term) return { hasLink: false, linkUrl: '' };

    const container = containerRef.current;
    if (!container) return { hasLink: false, linkUrl: '' };

    const rect = container.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) {
      return { hasLink: false, linkUrl: '' };
    }

    const cellWidth = rect.width / term.cols;
    const cellHeight = rect.height / term.rows;
    const cellX = Math.floor((e.clientX - rect.left) / cellWidth);
    const cellY = Math.floor((e.clientY - rect.top) / cellHeight);

    const buf = term.buffer.active;
    const lineIdx = buf.baseY + cellY;
    const line = buf.getLine(lineIdx);

    if (!line) return { hasLink: false, linkUrl: '' };

    // Build line text
    let text = '';
    for (let i = 0; i < line.length; i++) {
      text += line.getCell(i)?.getChars() || '';
    }

    // URL regex (same as inline)
    const urlRegex = /https?:\/\/[\w\-._~:/?#[\]@!$&'()*+,;=%]+/g;
    let match;
    while ((match = urlRegex.exec(text)) !== null) {
      const start = match.index;
      const end = start + match[0].length;
      if (cellX >= start && cellX < end) {
        return { hasLink: true, linkUrl: match[0] };
      }
    }

    return { hasLink: false, linkUrl: '' };
  }, [getTerminal, containerRef]);

  // ── Context menu handler ──────────────────────────────────

  const handleContextMenu = useCallback(
    (e: MouseEvent) => {
      const container = containerRef.current;
      if (!container || !container.contains(e.target as Node)) return;

      e.preventDefault();

      // Clear pending timers from previous menu instance to prevent
      // a stale close() timer from closing the newly-opened menu.
      clearTimers();

      const { hasLink, linkUrl } = detectLinkUnderCursor(e);

      setMenu({
        visible: true,
        x: e.clientX,
        y: e.clientY,
        hasLink,
        linkUrl,
      });
    },
    [containerRef, detectLinkUnderCursor, clearTimers],
  );

  // ── Attach / detach contextmenu listener ──────────────────

  useEffect(() => {
    document.addEventListener('contextmenu', handleContextMenu);
    return () => document.removeEventListener('contextmenu', handleContextMenu);
  }, [handleContextMenu]);

  // ── Cleanup pending timers on unmount ─────────────────────

  useEffect(() => {
    return () => {
      clearTimers();
    };
  }, [clearTimers]);

  // ── Action handlers ───────────────────────────────────────

  const handleCopy = useCallback(() => {
    const term = getTerminal();
    if (!term || !hasSelection()) return;

    const selection = term.getSelection();
    if (selection) {
      onCopy(selection);
    }
    close();
  }, [getTerminal, hasSelection, onCopy, close]);

  const handleCopyLink = useCallback(() => {
    if (!menu.linkUrl) return;
    onCopy(menu.linkUrl);
    showCopied('link');
    timersRef.current.push(window.setTimeout(() => close(), 1500));
  }, [menu.linkUrl, onCopy, close, showCopied]);

  const handlePaste = useCallback(async () => {
    try {
      const text = await navigator.clipboard.readText();
      onPaste(text);
      close();
    } catch (err) {
      // Clipboard access denied - silently fail
      close();
    }
  }, [onPaste, close]);

  const handleSearch = useCallback(() => {
    onSearch();
    close();
  }, [onSearch, close]);

  const handleClear = useCallback(() => {
    onClear();
    close();
  }, [onClear, close]);

  const handleSelectAll = useCallback(() => {
    onSelectAll();
    close();
  }, [onSelectAll, close]);

  const handleSplitHorizontal = useCallback(() => {
    onSplitPane('horizontal');
    close();
  }, [onSplitPane, close]);

  const handleSplitVertical = useCallback(() => {
    onSplitPane('vertical');
    close();
  }, [onSplitPane, close]);

  const hasSel = hasSelection();

  return (
    <ContextMenu isOpen={menu.visible} x={menu.x} y={menu.y} onClose={close}>
      <button
        className={`context-menu-item ${!hasSel ? 'disabled' : ''}`}
        onClick={handleCopy}
        disabled={!hasSel}
        type="button"
      >
        <Copy size={13} />
        <span className="menu-item-label">Copy</span>
      </button>
      <button className="context-menu-item" onClick={handlePaste} type="button">
        <ClipboardPaste size={13} />
        <span className="menu-item-label">Paste</span>
      </button>
      <button className="context-menu-item" onClick={handleSearch} type="button">
        <Search size={13} />
        <span className="menu-item-label">Search</span>
        <span className="menu-item-shortcut">Ctrl+Shift+F</span>
      </button>
      <div className="context-menu-divider" />
      <button className="context-menu-item" onClick={handleClear} type="button">
        <Trash2 size={13} />
        <span className="menu-item-label">Clear Terminal</span>
      </button>
      <button className="context-menu-item" onClick={handleSelectAll} type="button">
        <TextSelect size={13} />
        <span className="menu-item-label">Select All</span>
      </button>
      <div className="context-menu-divider" />
      <button className="context-menu-item" onClick={handleSplitHorizontal} type="button">
        <Rows2 size={13} />
        <span className="menu-item-label">Split Horizontally</span>
      </button>
      <button className="context-menu-item" onClick={handleSplitVertical} type="button">
        <Columns2 size={13} />
        <span className="menu-item-label">Split Vertically</span>
      </button>
      {menu.hasLink && (
        <>
          <div className="context-menu-divider" />
          <button className="context-menu-item" onClick={handleCopyLink} type="button">
            <Link2 size={13} />
            <span className="menu-item-label">
              {copiedAction === 'link' ? 'Copied!' : 'Copy Link'}
            </span>
          </button>
        </>
      )}
    </ContextMenu>
  );
}

export default TerminalContextMenu;
