import React, { useState, useRef, useCallback, useEffect } from 'react';
import { Plus, X, Pencil, Trash2 } from 'lucide-react';
import type { ChatSession } from '../services/chatSessions';
import './ChatTabBar.css';

interface ChatTabBarProps {
  sessions: ChatSession[];
  activeChatId: string;
  onSwitch: (id: string) => void;
  onCreate: () => void;
  onDelete: (id: string) => void;
  onRename: (id: string, name: string) => void;
}

interface ContextMenuState {
  visible: boolean;
  x: number;
  y: number;
  sessionId: string | null;
  canDelete: boolean;
}

const ChatTabBar: React.FC<ChatTabBarProps> = ({
  sessions,
  activeChatId,
  onSwitch,
  onCreate,
  onDelete,
  onRename,
}) => {
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
  const contextMenuRef = useRef<HTMLDivElement>(null);

  // Close context menu on click outside or scroll
  useEffect(() => {
    if (!contextMenu.visible) return;

    const handleClickOutside = (e: MouseEvent) => {
      if (contextMenuRef.current && !contextMenuRef.current.contains(e.target as Node)) {
        setContextMenu((prev) => ({ ...prev, visible: false }));
      }
    };

    const handleScroll = () => {
      setContextMenu((prev) => ({ ...prev, visible: false }));
    };

    document.addEventListener('mousedown', handleClickOutside);
    window.addEventListener('scroll', handleScroll, true);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      window.removeEventListener('scroll', handleScroll, true);
    };
  }, [contextMenu.visible]);

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

  const handleRenameKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      commitRename();
    } else if (e.key === 'Escape') {
      cancelRename();
    }
    // Don't let the event propagate to the global Ctrl+T handler
    e.stopPropagation();
  }, [commitRename, cancelRename]);

  const handleContextMenu = useCallback((e: React.MouseEvent, session: ChatSession) => {
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

  const handleMenuRename = useCallback(() => {
    const id = contextMenu.sessionId;
    if (!id) return;
    const session = sessions.find((s) => s.id === id);
    if (!session || session.is_default) return;
    setRenamingId(id);
    setRenameValue(session.name);
    setContextMenu((prev) => ({ ...prev, visible: false }));
  }, [contextMenu.sessionId, sessions]);

  const handleMenuDelete = useCallback(() => {
    const id = contextMenu.sessionId;
    if (!id || !contextMenu.canDelete) return;
    onDelete(id);
    setContextMenu((prev) => ({ ...prev, visible: false }));
  }, [contextMenu.sessionId, contextMenu.canDelete, onDelete]);

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
              {session.active_query && !isActive && (
                <span className="chat-tab-activity-dot" />
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
      </div>

      {contextMenu.visible && (
        <div
          ref={contextMenuRef}
          className="chat-tab-context-menu"
          style={{ left: contextMenu.x, top: contextMenu.y }}
        >
          <button
            className={`chat-tab-context-menu-item ${contextMenu.sessionId === sessions.find((s) => s.is_default)?.id ? 'disabled' : ''}`}
            onClick={handleMenuRename}
            type="button"
            disabled={contextMenu.sessionId === sessions.find((s) => s.is_default)?.id}
            aria-disabled={contextMenu.sessionId === sessions.find((s) => s.is_default)?.id}
          >
            <Pencil size={13} />
            <span className="menu-item-label">Rename</span>
          </button>
          <div className="chat-tab-context-menu-divider" />
          <button
            className={`chat-tab-context-menu-item ${contextMenu.canDelete ? '' : 'disabled'}`}
            onClick={handleMenuDelete}
            type="button"
            disabled={!contextMenu.canDelete}
            aria-disabled={!contextMenu.canDelete}
          >
            <Trash2 size={13} />
            <span className="menu-item-label">Delete</span>
          </button>
        </div>
      )}
    </>
  );
};

export default ChatTabBar;
