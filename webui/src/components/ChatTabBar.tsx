import { useState, useRef, useCallback, useEffect } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from 'react';
import { Plus, X, Pencil, Trash2, GitBranch } from 'lucide-react';
import type { ChatSession } from '../services/chatSessions';
import ContextMenu from './ContextMenu';
import './ChatTabBar.css';

interface ChatTabBarProps {
  sessions: ChatSession[];
  activeChatId: string;
  onSwitch: (id: string) => void;
  onCreate: () => void;
  onDelete: (id: string) => void;
  onRename: (id: string, name: string) => void;
  onCreateChatInWorktree?: () => void;
}

interface ContextMenuState {
  visible: boolean;
  x: number;
  y: number;
  sessionId: string | null;
  canDelete: boolean;
}

function ChatTabBar({
  sessions,
  activeChatId,
  onSwitch,
  onCreate,
  onDelete,
  onRename,
  onCreateChatInWorktree,
}: ChatTabBarProps): JSX.Element | null {
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [contextMenu, setContextMenu] = useState<ContextMenuState>({
    visible: false,
    x: 0,
    y: 0,
    sessionId: null,
    canDelete: false,
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

  // Ctrl+T / Cmd+T shortcut to create new chat
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't trigger during rename
      if (renamingId) return;
      if ((e.ctrlKey || e.metaKey) && e.key === 't') {
        e.preventDefault();
        onCreate();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onCreate, renamingId]);

  const handleDoubleClick = useCallback((session: ChatSession) => {
    if (session.is_default) return;
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
    (e: ReactKeyboardEvent) => {
      if (e.key === 'Enter') {
        commitRename();
      } else if (e.key === 'Escape') {
        cancelRename();
      }
      // Don't let the event propagate to the global Ctrl+T handler
      e.stopPropagation();
    },
    [commitRename, cancelRename],
  );

  const closeContextMenu = useCallback(() => {
    setContextMenu((prev) => ({ ...prev, visible: false }));
  }, []);

  const handleContextMenu = useCallback((e: ReactMouseEvent, session: ChatSession) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      sessionId: session.id,
      canDelete: !session.is_default,
    });
  }, []);

  const contextSessionId = contextMenu.sessionId;
  const isDefaultSession = contextSessionId != null && sessions.find((s) => s.id === contextSessionId)?.is_default;

  const handleMenuRename = useCallback(() => {
    const id = contextMenu.sessionId;
    if (!id) return;
    const session = sessions.find((s) => s.id === id);
    if (!session || session.is_default) return;
    setRenamingId(id);
    setRenameValue(session.name);
    closeContextMenu();
  }, [contextMenu.sessionId, sessions, closeContextMenu]);

  const handleMenuDelete = useCallback(() => {
    const id = contextMenu.sessionId;
    if (!id || !contextMenu.canDelete) return;
    onDelete(id);
    closeContextMenu();
  }, [contextMenu.sessionId, contextMenu.canDelete, onDelete, closeContextMenu]);

  if (sessions.length === 0) {
    return null;
  }

  return (
    <>
      <div className="chat-tab-bar" ref={barRef} onContextMenu={(e) => e.preventDefault()}>
        {sessions.map((session) => {
          const isActive = session.id === activeChatId;
          const isRenaming = session.id === renamingId;

          return (
            <button
              key={session.id}
              className={`chat-tab ${isActive ? 'active' : ''}`}
              onClick={() => onSwitch(session.id)}
              onDoubleClick={(e) => {
                e.stopPropagation();
                handleDoubleClick(session);
              }}
              onContextMenu={(e) => handleContextMenu(e, session)}
              title={session.name}
              type="button"
            >
              {session.active_query && !isActive && <span className="chat-tab-activity-dot" />}
              {session.worktree_path && (
                <span className="chat-tab-worktree-badge" title={`Worktree: ${session.worktree_path}`}>
                  <GitBranch size={11} />
                </span>
              )}
              {isRenaming ? (
                <input
                  ref={renameInputRef}
                  className="chat-tab-rename-input"
                  value={renameValue}
                  onChange={(e) => setRenameValue(e.target.value)}
                  onKeyDown={handleRenameKeyDown}
                  onBlur={commitRename}
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <span className="chat-tab-name">{session.name}</span>
              )}
              {!session.is_default && !isRenaming && (
                <span
                  className="chat-tab-close"
                  role="button"
                  tabIndex={-1}
                  aria-label={`Close ${session.name}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete(session.id);
                  }}
                >
                  <X size={12} />
                </span>
              )}
            </button>
          );
        })}
        <button
          className="chat-tab-new"
          onClick={onCreate}
          title="New chat (Ctrl+T)"
          type="button"
          aria-label="New chat"
        >
          <Plus size={14} />
        </button>
        {onCreateChatInWorktree && (
          <button
            className="chat-tab-new-worktree"
            onClick={onCreateChatInWorktree}
            title="New chat in worktree"
            type="button"
            aria-label="New chat in worktree"
          >
            <GitBranch size={14} />
          </button>
        )}
      </div>

      <ContextMenu isOpen={contextMenu.visible} x={contextMenu.x} y={contextMenu.y} onClose={closeContextMenu}>
        <button
          className={`context-menu-item ${isDefaultSession ? 'disabled' : ''}`}
          onClick={handleMenuRename}
          type="button"
          disabled={!!isDefaultSession}
        >
          <Pencil size={13} />
          <span className="menu-item-label">Rename</span>
        </button>
        <div className="context-menu-divider" />
        <button
          className={`context-menu-item ${contextMenu.canDelete ? '' : 'disabled'}`}
          onClick={handleMenuDelete}
          type="button"
          disabled={!contextMenu.canDelete}
        >
          <Trash2 size={13} />
          <span className="menu-item-label">Delete</span>
        </button>
      </ContextMenu>
    </>
  );
}

export default ChatTabBar;
