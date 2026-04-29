import { useState, useRef, useCallback, useEffect } from 'react';
import type { KeyboardEvent, MouseEvent } from 'react';
import { Plus, X, Pencil, Pin } from 'lucide-react';
import ContextMenu from './ContextMenu';
import './TerminalTabBar.css';

export interface TerminalSession {
  id: string;
  name: string;
  is_pinned: boolean;
}

interface TerminalTabBarProps {
  sessions: TerminalSession[];
  activeSessionId: string;
  onSwitch: (id: string) => void;
  onCreate?: () => void;
  onClose: (id: string) => void;
  onRename: (id: string, name: string) => void;
  onTogglePin?: (id: string) => void;
}

interface ContextMenuState {
  visible: boolean;
  x: number;
  y: number;
  sessionId: string | null;
  canClose: boolean;
}

function TerminalTabBar({
  sessions,
  activeSessionId,
  onSwitch,
  onCreate,
  onClose,
  onRename,
  onTogglePin,
}: TerminalTabBarProps): JSX.Element {
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [contextMenu, setContextMenu] = useState<ContextMenuState>({
    visible: false,
    x: 0,
    y: 0,
    sessionId: null,
    canClose: false,
  });
  const renameInputRef = useRef<HTMLInputElement>(null);
  const barRef = useRef<HTMLDivElement>(null);

  // Focus rename input when it appears
  useEffect(() => {
    if (renamingId && renameInputRef.current) {
      renameInputRef.current.focus();
      renameInputRef.current.select();
    }
  }, [renamingId]);

  const handleDoubleClick = useCallback((session: TerminalSession) => {
    setRenamingId(session.id);
    setRenameValue(session.name);
  }, []);

  const commitRename = useCallback(() => {
    if (!renamingId || !renameValue.trim()) {
      setRenamingId(null);
      return;
    }
    onRename(renamingId, renameValue.trim());
    setRenamingId(null);
  }, [renamingId, renameValue, onRename]);

  const cancelRename = useCallback(() => {
    setRenamingId(null);
    setRenameValue('');
  }, []);

  const handleRenameKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Enter') {
        commitRename();
      } else if (e.key === 'Escape') {
        cancelRename();
      }
      e.stopPropagation();
    },
    [commitRename, cancelRename],
  );

  const closeContextMenu = useCallback(() => {
    setContextMenu((prev) => ({ ...prev, visible: false }));
  }, []);

  const handleContextMenu = useCallback(
    (e: MouseEvent, session: TerminalSession) => {
      e.preventDefault();
      e.stopPropagation();
      setContextMenu({
        visible: true,
        x: e.clientX,
        y: e.clientY,
        sessionId: session.id,
        canClose: sessions.length > 1,
      });
    },
    [sessions.length],
  );

  const handleMenuRename = useCallback(() => {
    const id = contextMenu.sessionId;
    if (!id) return;
    const session = sessions.find((s) => s.id === id);
    if (!session) return;
    setRenamingId(id);
    setRenameValue(session.name);
    closeContextMenu();
  }, [contextMenu.sessionId, sessions, closeContextMenu]);

  const handleMenuClose = useCallback(() => {
    const id = contextMenu.sessionId;
    if (!id || !contextMenu.canClose) return;
    onClose(id);
    closeContextMenu();
  }, [contextMenu.sessionId, contextMenu.canClose, onClose, closeContextMenu]);

  const showCloseButtons = sessions.length > 1;

  return (
    <>
      <div className="terminal-tab-bar" ref={barRef} onContextMenu={(e) => e.preventDefault()} role="tablist">
        {sessions.map((session) => {
          const isActive = session.id === activeSessionId;
          const isRenaming = session.id === renamingId;

          return (
            <button
              key={session.id}
              className={`terminal-tab ${isActive ? 'active' : ''}`}
              onClick={() => onSwitch(session.id)}
              onDoubleClick={(e) => {
                e.stopPropagation();
                handleDoubleClick(session);
              }}
              onContextMenu={(e) => handleContextMenu(e, session)}
              title={session.name}
              type="button"
              role="tab"
              aria-selected={isActive}
            >
              {isRenaming ? (
                <input
                  ref={renameInputRef}
                  className="terminal-tab-rename-input"
                  value={renameValue}
                  onChange={(e) => setRenameValue(e.target.value)}
                  onKeyDown={handleRenameKeyDown}
                  onBlur={commitRename}
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <span className="terminal-tab-name">{session.name}</span>
              )}
              {showCloseButtons && !isRenaming && (
                <span
                  className="terminal-tab-close"
                  role="button"
                  tabIndex={-1}
                  aria-label={`Close ${session.name}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    onClose(session.id);
                  }}
                >
                  <X size={12} />
                </span>
              )}
            </button>
          );
        })}
        {onCreate && (
          <button
            className="terminal-tab-new"
            onClick={onCreate}
            title="New terminal session"
            type="button"
            aria-label="New terminal session"
          >
            <Plus size={14} />
          </button>
        )}
      </div>

      <ContextMenu isOpen={contextMenu.visible} x={contextMenu.x} y={contextMenu.y} onClose={closeContextMenu}>
        <button className="context-menu-item" onClick={handleMenuRename} type="button">
          <Pencil size={13} />
          <span className="menu-item-label">Rename</span>
        </button>
        <button
          className="context-menu-item"
          onClick={() => {
            if (onTogglePin && contextMenu.sessionId) {
              onTogglePin(contextMenu.sessionId);
              closeContextMenu();
            }
          }}
          type="button"
          disabled={!onTogglePin || !contextMenu.sessionId}
        >
          <Pin size={13} />
          <span className="menu-item-label">{sessions.find((s) => s.id === contextMenu.sessionId)?.is_pinned ? 'Unpin' : 'Pin'}</span>
        </button>
        <div className="context-menu-divider" />
        <button
          className={`context-menu-item ${contextMenu.canClose ? '' : 'disabled'}`}
          onClick={handleMenuClose}
          type="button"
          disabled={!contextMenu.canClose}
        >
          <X size={13} />
          <span className="menu-item-label">Close Tab</span>
        </button>
      </ContextMenu>
    </>
  );
}

export default TerminalTabBar;
