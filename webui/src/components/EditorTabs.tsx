import { useEffect, useMemo, useRef, useState, useCallback, type ReactNode } from 'react';
import type { DragEvent, MouseEvent, KeyboardEvent as ReactKeyboardEvent } from 'react';
import {
  X,
  AlertTriangle,
  FolderOpen,
  FileCode,
  FileText,
  File,
  Code2,
  Globe,
  Palette,
  Settings,
  Terminal,
  Braces,
  MessageSquareText,
  GitCompareArrows,
  ShieldCheck,
  ArrowRightLeft,
  PanelRightOpen,
  Eye,
  Sparkles,
  Pin,
  Plus,
  GitBranch,
  Pencil,
  Trash2,
  ImageIcon,
  Video,
  Headphones,
  FileWarning,
  type LucideIcon,
} from 'lucide-react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { type EditorBuffer } from '../types/editor';
import ContextMenu from './ContextMenu';
import './EditorTabs.css';

interface EditorTabsProps {
  paneId?: string;
  actions?: ReactNode;
  compact?: boolean;

  // Chat-specific props (added when removing ChatTabBar)
  onActiveChatChange?: (id: string) => void;
  activeChatQueries?: Set<string>;
  defaultChatIds?: Set<string>;
  chatWorktreePaths?: Map<string, string>;
  onCreateChat?: () => void;
  onCreateChatInWorktree?: () => void;
  onDeleteChatWithWorktree?: (id: string) => void;
  onRenameChat?: (id: string, name: string) => void;
  chatSessions?: Array<{ id: string; name?: string; is_pinned?: boolean; is_default?: boolean }>;
}

// ── File icon helpers (defined before component to avoid use-before-define) ──

const FILE_ICON_SIZE = 16;

const getFileIconComponent = (ext?: string): LucideIcon => {
  if (!ext) return File;

  switch (ext.toLowerCase()) {
    case '.js':
    case '.jsx':
      return FileCode;
    case '.ts':
    case '.tsx':
      return Braces;
    case '.go':
      return Code2;
    case '.py':
      return FileCode;
    case '.json':
      return Braces;
    case '.html':
      return Globe;
    case '.css':
      return Palette;
    case '.md':
      return FileText;
    case '.txt':
      return FileText;
    case '.yml':
    case '.yaml':
      return Settings;
    case '.sh':
      return Terminal;
    // Image files
    case '.png':
    case '.jpg':
    case '.jpeg':
    case '.gif':
    case '.bmp':
    case '.webp':
    case '.ico':
    case '.tiff':
    case '.tif':
    case '.avif':
      return ImageIcon;
    // Audio files
    case '.mp3':
    case '.wav':
    case '.ogg':
    case '.flac':
    case '.aac':
    case '.m4a':
    case '.wma':
    case '.opus':
    case '.weba':
      return Headphones;
    // Video files
    case '.mp4':
    case '.webm':
    case '.mov':
    case '.avi':
    case '.mkv':
    case '.m4v':
    case '.flv':
    case '.wmv':
      return Video;
    // Binary/compressed/compiled files
    case '.zip':
    case '.tar':
    case '.gz':
    case '.rar':
    case '.pdf':
    case '.exe':
    case '.dll':
    case '.so':
    case '.wasm':
    case '.jar':
    case '.woff':
    case '.woff2':
    case '.ttf':
    case '.db':
    case '.sqlite':
      return FileWarning;
    default:
      return File;
  }
};

const getFileIcon = (ext?: string): ReactNode => {
  const Icon = getFileIconComponent(ext);
  return <Icon size={FILE_ICON_SIZE} />;
};

const getFileIconColor = (ext?: string): string => {
  if (ext === '.chat') return 'var(--accent-primary)';
  if (ext === '.diff') return '#22c55e';
  if (ext === '.review') return '#f59e0b';
  if (ext === '.welcome') return 'var(--accent-color)';
  if (!ext) return '#9ca3af';

  switch (ext.toLowerCase()) {
    case '.js':
    case '.jsx':
      return '#f1e05a';
    case '.ts':
    case '.tsx':
      return '#519aba';
    case '.go':
      return '#00add8';
    case '.py':
      return '#5b9bd5';
    case '.json':
      return '#f1e05a';
    case '.html':
      return '#e44d26';
    case '.css':
      return '#5b8def';
    case '.md':
      return '#519aba';
    // Image files
    case '.png':
    case '.jpg':
    case '.jpeg':
    case '.gif':
    case '.bmp':
    case '.webp':
    case '.ico':
    case '.tiff':
    case '.tif':
    case '.avif':
      return '#a855f7';
    // Audio files
    case '.mp3':
    case '.wav':
    case '.ogg':
    case '.flac':
    case '.aac':
    case '.m4a':
    case '.wma':
    case '.opus':
    case '.weba':
      return '#3b82f6';
    // Video files
    case '.mp4':
    case '.webm':
    case '.mov':
    case '.avi':
    case '.mkv':
    case '.m4v':
    case '.flv':
    case '.wmv':
      return '#ef4444';
    // Binary/compressed/compiled files
    case '.zip':
    case '.tar':
    case '.gz':
    case '.rar':
    case '.pdf':
    case '.exe':
    case '.dll':
    case '.so':
    case '.wasm':
    case '.jar':
    case '.woff':
    case '.woff2':
    case '.ttf':
    case '.db':
    case '.sqlite':
      return '#f59e0b';
    default:
      return '#9ca3af';
  }
};

const getBufferIcon = (buffer: EditorBuffer): ReactNode => {
  switch (buffer.kind) {
    case 'chat':
      return <MessageSquareText size={FILE_ICON_SIZE} />;
    case 'diff':
      return <GitCompareArrows size={FILE_ICON_SIZE} />;
    case 'review':
      return <ShieldCheck size={FILE_ICON_SIZE} />;
    case 'welcome':
      return <Sparkles size={FILE_ICON_SIZE} />;
    default:
      return getFileIcon(buffer.file.ext);
  }
};

/**
 * Extract the chat session ID from a buffer's metadata or path.
 * Returns undefined if the buffer is not a chat or has no identifiable ID.
 */
function getChatId(buffer: EditorBuffer): string | undefined {
  if (buffer.kind !== 'chat') return undefined;
  return (buffer.metadata?.chatId as string | undefined);
}

function EditorTabs({
  paneId,
  actions,
  compact = false,
  onActiveChatChange,
  activeChatQueries,
  defaultChatIds,
  chatWorktreePaths,
  onCreateChat,
  onCreateChatInWorktree,
  onDeleteChatWithWorktree,
  onRenameChat,
  chatSessions,
}: EditorTabsProps): JSX.Element {
  const {
    buffers,
    panes,
    activeBufferId,
    activePaneId: _activePaneId,
    switchPane,
    switchToBuffer,
    closeBuffer,
    reorderBuffers,
    moveBufferToPane,
    toggleBufferPin,
  } = useEditorManager();
  const [showConfirm, setShowConfirm] = useState<{ bufferId: string; fileName: string } | null>(null);
  const [draggingBufferId, setDraggingBufferId] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; bufferId: string } | null>(null);
  const [emptyAreaContextMenu, setEmptyAreaContextMenu] = useState<{ x: number; y: number } | null>(null);
  const tabRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const batchCloseTargetsRef = useRef<string[]>([]);

  // ── Inline rename state for chat tabs ─────────────────────────
  const [renamingBufferId, setRenamingBufferId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const renameInputRef = useRef<HTMLInputElement>(null);

  // Focus rename input when it appears
  useEffect(() => {
    if (renamingBufferId && renameInputRef.current) {
      renameInputRef.current.focus();
      renameInputRef.current.select();
    }
  }, [renamingBufferId]);

  const paneOrder = useMemo(() => {
    const order = new Map<string, number>();
    panes.forEach((pane, index) => {
      order.set(pane.id, index + 1);
    });
    return order;
  }, [panes]);

  // Preserve insertion order, filter by paneId with pinned-chat exception.
  const bufferList = useMemo(() => {
    const values = Array.from(buffers.values());
    if (!paneId) return values;
    return values.filter((buffer) => buffer.paneId === paneId);
  }, [buffers, paneId]);

  useEffect(() => {
    if (!activeBufferId) {
      return;
    }
    const activeTab = tabRefs.current[activeBufferId];
    if (activeTab) {
      activeTab.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'smooth' });
    }
  }, [activeBufferId]);

  // ── Tab click: also sync activeChatId for chat buffers ────────
  const handleTabClick = (buffer: EditorBuffer) => {
    if (renamingBufferId) return; // don't switch during rename
    if (buffer.id !== activeBufferId) {
      switchToBuffer(buffer.id);
    }
    // Sync the global active chat ID so parent state stays consistent
    if (buffer.kind === 'chat') {
      const chatId = getChatId(buffer);
      if (chatId) {
        onActiveChatChange?.(chatId);
      }
    }
  };

  const handleTabClose = (e: MouseEvent, buffer: EditorBuffer) => {
    e.stopPropagation();

    if (buffer.isPinned && buffer.kind !== 'chat') return;

    if (buffer.isModified) {
      setShowConfirm({ bufferId: buffer.id, fileName: buffer.file.name });
      return;
    }

    if (buffer.isPinned && buffer.kind === 'chat') {
      setShowConfirm({ bufferId: buffer.id, fileName: buffer.file.name });
      return;
    }

    closeBuffer(buffer.id);
  };

  const handleTabAuxClick = (e: MouseEvent, buffer: EditorBuffer) => {
    if (e.button !== 1 || buffer.isClosable === false || (buffer.isPinned && buffer.kind !== 'chat')) {
      return;
    }
    e.preventDefault();
    handleTabClose(e, buffer);
  };

  const handleTabContextMenu = (e: MouseEvent, buffer: EditorBuffer) => {
    e.preventDefault();
    e.stopPropagation();
    setEmptyAreaContextMenu(null);
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      bufferId: buffer.id,
    });
  };

  const handleTabsContainerContextMenu = (e: MouseEvent) => {
    if ((e.target as HTMLElement).closest('.tab')) {
      return;
    }
    e.preventDefault();
    setContextMenu(null);
    setEmptyAreaContextMenu({ x: e.clientX, y: e.clientY });
  };

  // ── Inline rename handlers ────────────────────────────────────
  const startRename = useCallback((buffer: EditorBuffer) => {
    // Only allow renaming chat tabs that are not default sessions
    const chatId = getChatId(buffer);
    if (!chatId) return;
    const session = chatSessions?.find((s) => s.id === chatId);
    if (session?.is_default) return;
    setRenamingBufferId(buffer.id);
    setRenameValue(buffer.file.name);
  }, [chatSessions]);

  const commitRename = useCallback(() => {
    if (!renamingBufferId || !renameValue.trim()) {
      setRenamingBufferId(null);
      return;
    }
    // Extract the actual chat session ID from the buffer metadata
    const buffer = buffers.get(renamingBufferId);
    const chatId = buffer ? getChatId(buffer) : undefined;
    if (chatId) {
      onRenameChat?.(chatId, renameValue.trim());
    }
    setRenamingBufferId(null);
  }, [renamingBufferId, renameValue, onRenameChat, buffers]);

  const cancelRename = useCallback(() => {
    setRenamingBufferId(null);
    setRenameValue('');
  }, []);

  const handleRenameKeyDown = useCallback(
    (e: ReactKeyboardEvent) => {
      if (e.key === 'Enter') {
        commitRename();
      } else if (e.key === 'Escape') {
        cancelRename();
      }
      // Stop propagation so global Ctrl+T doesn't fire during rename
      e.stopPropagation();
    },
    [commitRename, cancelRename],
  );

  const handleTabDoubleClick = useCallback((buffer: EditorBuffer) => {
    if (buffer.kind === 'chat') {
      startRename(buffer);
    }
  }, [startRename]);

  // ── Drag and drop ─────────────────────────────────────────────
  const handleDragStart = (e: DragEvent, bufferId: string) => {
    setDraggingBufferId(bufferId);
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', bufferId);
  };

  const handleDrop = (targetBufferId: string, sourceBufferId?: string | null) => {
    const draggedId = sourceBufferId || draggingBufferId;
    if (!draggedId || draggedId === targetBufferId) {
      setDraggingBufferId(null);
      return;
    }
    reorderBuffers(draggedId, targetBufferId);
    setDraggingBufferId(null);
  };

  const resolveDraggedBufferId = (e: DragEvent) => {
    return draggingBufferId || e.dataTransfer.getData('text/plain') || null;
  };

  const handlePaneDrop = (e: DragEvent) => {
    e.preventDefault();
    const droppedBufferId = resolveDraggedBufferId(e);
    if (!droppedBufferId || !paneId) {
      setDraggingBufferId(null);
      return;
    }
    moveBufferToPane(droppedBufferId, paneId);
    setDraggingBufferId(null);
  };

  // ── Close confirmation ────────────────────────────────────────
  const handleConfirmClose = () => {
    if (showConfirm) {
      if (showConfirm.bufferId === '__batch_close__') {
        batchCloseTargetsRef.current.forEach((bufferId) => closeBuffer(bufferId));
        batchCloseTargetsRef.current = [];
      } else {
        closeBuffer(showConfirm.bufferId);
      }
      setShowConfirm(null);
    }
  };

  const handleCancelClose = () => {
    batchCloseTargetsRef.current = [];
    setShowConfirm(null);
  };

  // Escape key dismisses the confirmation dialog
  useEffect(() => {
    if (!showConfirm) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') handleCancelClose();
    };
    document.addEventListener('keydown', handleEsc);
    return () => document.removeEventListener('keydown', handleEsc);
  }, [showConfirm]); // eslint-disable-line react-hooks/exhaustive-deps -- handleCancelClose is stable enough (close-ref + setState)

  const closeRelatedBuffers = (predicate: (buffer: EditorBuffer) => boolean) => {
    const closeTargets = Array.from(buffers.values()).filter(
      (buffer) => buffer.isClosable !== false && !buffer.isPinned && predicate(buffer),
    );
    const modifiedTargets = closeTargets.filter((buffer) => buffer.isModified);

    if (modifiedTargets.length > 0) {
      const names = modifiedTargets.map((b) => b.file.name).join(', ');
      setShowConfirm({
        bufferId: '__batch_close__',
        fileName: names.length > 60 ? `${names.slice(0, 60)}… and ${modifiedTargets.length - 1} more` : names,
      });
      batchCloseTargetsRef.current = closeTargets.map((b) => b.id);
      return;
    }

    closeTargets.forEach((buffer) => closeBuffer(buffer.id));
  };

  // ── Safe callbacks for chat actions (fire-and-forget with error handling) ──
  // These catch unhandled rejections when the prop is actually an async function
  // but typed as () => void (e.g. PaneLayoutManager's wrapper that discards the Promise).
  const handleNewChat = useCallback(() => {
    if (onCreateChat) {
      try {
        const result = onCreateChat();
        // The prop is typed as () => void but the actual implementation may
        // return a Promise (handleCreateChat is async). Duck-type check.
        if (result != null && typeof (result as unknown as Promise<unknown>).then === 'function') {
          (result as unknown as Promise<unknown>).catch((err: unknown) => console.warn('[EditorTabs] Failed to create chat:', err));
        }
      } catch (err) {
        console.warn('[EditorTabs] Failed to create chat:', err);
      }
    }
  }, [onCreateChat]);

  const handleNewWorktreeChat = useCallback(() => {
    if (onCreateChatInWorktree) {
      try {
        const result = onCreateChatInWorktree();
        if (result != null && typeof (result as unknown as Promise<unknown>).then === 'function') {
          (result as unknown as Promise<unknown>).catch((err: unknown) => console.warn('[EditorTabs] Failed to create worktree chat:', err));
        }
      } catch (err) {
        console.warn('[EditorTabs] Failed to create worktree chat:', err);
      }
    }
  }, [onCreateChatInWorktree]);

  // ── Context menu setup ────────────────────────────────────────
  const activeContextBuffer = contextMenu ? buffers.get(contextMenu.bufferId) || null : null;
  const contextPaneId = activeContextBuffer?.paneId || paneId || null;
  const availablePaneTargets = panes.filter((pane) => pane.id !== contextPaneId);

  // Pre-compute chat-specific context menu data (avoid IIFEs in JSX)
  const contextChatId = activeContextBuffer?.kind === 'chat' ? getChatId(activeContextBuffer) : undefined;
  const contextIsDefaultChat = contextChatId ? defaultChatIds?.has(contextChatId) ?? false : false;
  const contextHasWorktree = contextChatId ? chatWorktreePaths?.has(contextChatId) : false;

  const handleContextAction = (action: () => void) => {
    setContextMenu(null);
    action();
  };

  return (
    <div className={`editor-tabs ${compact ? 'compact' : ''}`}>
      <div
        className="tabs-container"
        onContextMenu={handleTabsContainerContextMenu}
        onDragOver={(e) => {
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
        }}
        onDrop={handlePaneDrop}
      >
        {bufferList.length === 0 && !onCreateChat ? (
          <div className="no-tabs">
            <span className="no-tabs-icon">
              <FolderOpen size={20} />
            </span>
            <span className="no-tabs-text">No open tabs</span>
          </div>
        ) : (
          <div
            className="tabs-list"
            onDragOver={(e) => {
              e.preventDefault();
              e.dataTransfer.dropEffect = 'move';
            }}
            onDrop={handlePaneDrop}
          >
            {bufferList.map((buffer) => {
              const chatId = getChatId(buffer);
              const isActive = buffer.id === activeBufferId;
              const isRenaming = buffer.id === renamingBufferId;

              // Chat-specific data lookups (all O(1))
              const hasActiveQuery = chatId ? activeChatQueries?.has(chatId) ?? false : false;
              const worktreePath = chatId ? chatWorktreePaths?.get(chatId) : undefined;
              const isDefaultChat = chatId ? defaultChatIds?.has(chatId) ?? false : false;

              return (
                <div
                  key={buffer.id}
                  className={`tab ${isActive ? 'active' : ''} ${buffer.isPinned ? 'pinned' : ''} ${buffer.kind === 'chat' ? 'chat-tab' : ''}`}
                  ref={(el) => {
                    tabRefs.current[buffer.id] = el;
                  }}
                  onClick={() => handleTabClick(buffer)}
                  onDoubleClick={(e) => {
                    e.stopPropagation();
                    handleTabDoubleClick(buffer);
                  }}
                  onAuxClick={(e) => handleTabAuxClick(e, buffer)}
                  onContextMenu={(e) => handleTabContextMenu(e, buffer)}
                  title={buffer.file.path}
                  draggable
                  data-buffer-id={buffer.id}
                  onDragStart={(e) => handleDragStart(e, buffer.id)}
                  onDragEnd={() => setDraggingBufferId(null)}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    const droppedBufferId = resolveDraggedBufferId(e);
                    if (!droppedBufferId || droppedBufferId === buffer.id) {
                      setDraggingBufferId(null);
                      return;
                    }
                    if (paneId && buffers.get(droppedBufferId)?.paneId !== paneId) {
                      moveBufferToPane(droppedBufferId, paneId);
                    }
                    handleDrop(buffer.id, droppedBufferId);
                  }}
                >
                  <div className="tab-content">
                    {/* Activity dot for chat tabs with active queries (non-active) */}
                    {buffer.kind === 'chat' && hasActiveQuery && !isActive && (
                      <span className="chat-tab-activity-dot" />
                    )}
                    <span className="tab-icon" style={{ color: getFileIconColor(buffer.file.ext) }}>
                      {getBufferIcon(buffer)}
                    </span>
                    {/* Worktree badge for chat tabs */}
                    {buffer.kind === 'chat' && worktreePath && (
                      <span className="tab-worktree-badge" title={`Worktree: ${worktreePath}`}>
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
                      <span className="tab-name">{buffer.file.name}</span>
                    )}
                    {buffer.isModified && <span className="tab-modified">●</span>}
                    {buffer.externallyModified && (
                      <span className="tab-externally-modified" title="File changed on disk">
                        ↑
                      </span>
                    )}
                    <button
                      className="pin-indicator"
                      aria-label={buffer.isPinned ? 'Unpin tab' : 'Pin tab'}
                      aria-pressed={!!buffer.isPinned}
                      onClick={(e) => {
                        e.stopPropagation();
                        toggleBufferPin(buffer.id);
                      }}
                      title={buffer.isPinned ? 'Unpin tab' : 'Pin tab'}
                      disabled={!buffer.isPinned && buffer.isClosable === false}
                    >
                      <Pin size={12} fill={buffer.isPinned ? 'currentColor' : 'none'} />
                    </button>
                    {buffer.isClosable !== false && (buffer.kind === 'chat' || !buffer.isPinned) && !isDefaultChat && (
                      <button
                        className="tab-close"
                        onClick={(e) => handleTabClose(e, buffer)}
                        title={`Close ${buffer.file.name}`}
                      >
                        <X size={14} />
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
            {/* New Chat button (Plus icon) */}
            {!compact && onCreateChat && (
              <button
                className="tab new-chat"
                onClick={handleNewChat}
                title="New chat"
                type="button"
                aria-label="New chat"
              >
                <Plus size={14} />
              </button>
            )}
            {/* New Chat in Worktree button */}
            {!compact && onCreateChatInWorktree && (
              <button
                className="tab new-worktree"
                onClick={handleNewWorktreeChat}
                title="New chat in worktree"
                type="button"
                aria-label="New chat in worktree"
              >
                <GitBranch size={14} />
              </button>
            )}
          </div>
        )}
      </div>
      <div className="tabs-actions">
        {actions ? <div className="tabs-toolbar-actions">{actions}</div> : null}
        {!compact && (
          <span className="tab-count">
            {bufferList.length} tab{bufferList.length !== 1 ? 's' : ''} open
          </span>
        )}
      </div>

      {/* Confirmation Dialog */}
      {showConfirm && (
        <div className="close-confirm-overlay">
          <div className="close-confirm-dialog">
            <div className="dialog-header">
              <h3>
                <AlertTriangle size={16} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 6 }} />
                Unsaved Changes
              </h3>
            </div>
            <div className="dialog-body">
              {showConfirm.bufferId === '__batch_close__' ? (
                <>
                  <p>You have unsaved changes in the following files:</p>
                  <p>
                    <strong>{showConfirm.fileName}</strong>
                  </p>
                  <p>Are you sure you want to close them?</p>
                </>
              ) : (
                <>
                  <p>
                    You have unsaved changes in <strong>&quot;{showConfirm.fileName}&quot;</strong>.
                  </p>
                  <p>Are you sure you want to close the file?</p>
                </>
              )}
            </div>
            <div className="dialog-actions">
              <button onClick={handleConfirmClose} className="dialog-btn danger">
                <X size={14} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 4 }} />
                Yes, Close
              </button>
              <button onClick={handleCancelClose} className="dialog-btn primary">
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Tab Context Menu ─────────────────────────────────────── */}
      <ContextMenu
        isOpen={contextMenu !== null}
        x={contextMenu?.x ?? 0}
        y={contextMenu?.y ?? 0}
        onClose={() => setContextMenu(null)}
        className="tab-context-menu"
        zIndex={1500}
      >
        {contextMenu && activeContextBuffer && (
          <>
            <button
              className="context-menu-item"
              onClick={() =>
                handleContextAction(() => {
                  if (activeContextBuffer.paneId) {
                    switchPane(activeContextBuffer.paneId);
                  }
                  switchToBuffer(activeContextBuffer.id);
                })
              }
            >
              <Eye size={14} />
              <span>Reveal tab</span>
            </button>
            {availablePaneTargets.map((pane, index) => (
              <button
                key={pane.id}
                className="context-menu-item"
                onClick={() =>
                  handleContextAction(() => {
                    moveBufferToPane(activeContextBuffer.id, pane.id);
                    window.setTimeout(() => {
                      switchPane(pane.id);
                      switchToBuffer(activeContextBuffer.id);
                    }, 0);
                  })
                }
              >
                <ArrowRightLeft size={14} />
                <span>Move to split {paneOrder.get(pane.id) ?? index + 1}</span>
              </button>
            ))}
            <button
              className="context-menu-item"
              onClick={() =>
                handleContextAction(() => toggleBufferPin(activeContextBuffer.id))
              }
              disabled={!activeContextBuffer.isPinned && activeContextBuffer.isClosable === false}
            >
              <Pin size={14} fill={activeContextBuffer.isPinned ? 'currentColor' : 'none'} />
              <span>{activeContextBuffer.isPinned ? 'Unpin tab' : 'Pin tab'}</span>
            </button>

            {/* ── Chat-specific context menu items ───────────────── */}
            {activeContextBuffer.kind === 'chat' && !contextIsDefaultChat && onRenameChat && (
              <button
                className="context-menu-item"
                onClick={() =>
                  handleContextAction(() => {
                    startRename(activeContextBuffer);
                  })
                }
              >
                <Pencil size={14} />
                <span>Rename</span>
              </button>
            )}

            <div className="context-menu-divider" />
            <button
              className="context-menu-item"
              onClick={() =>
                handleContextAction(() => {
                  closeRelatedBuffers((buffer) => buffer.id !== activeContextBuffer.id);
                })
              }
            >
              <PanelRightOpen size={14} />
              <span>Close other tabs</span>
            </button>
            <button
              className="context-menu-item"
              onClick={() =>
                handleContextAction(() => {
                  closeRelatedBuffers(
                    (buffer) => buffer.paneId === contextPaneId && buffer.id !== activeContextBuffer.id,
                  );
                })
              }
            >
              <PanelRightOpen size={14} />
              <span>Close other tabs in split</span>
            </button>
            {activeContextBuffer.isClosable !== false && (activeContextBuffer.kind === 'chat' || !activeContextBuffer.isPinned) ? (
              <>
                <button
                  className="context-menu-item danger"
                  onClick={() => handleContextAction(() => closeBuffer(activeContextBuffer.id))}
                >
                  <Trash2 size={14} />
                  <span>Close</span>
                </button>
                {/* Delete Chat and Worktree — for chat tabs with worktrees */}
                {activeContextBuffer.kind === 'chat' && contextHasWorktree && onDeleteChatWithWorktree && (
                  <button
                    className="context-menu-item danger"
                    onClick={() =>
                      handleContextAction(() => {
                        if (!window.confirm('This will permanently delete the chat session and remove the git worktree directory from disk. Are you sure?')) return;
                        if (contextChatId) {
                          onDeleteChatWithWorktree(contextChatId);
                        }
                      })
                    }
                  >
                    <Trash2 size={14} />
                    <span>Delete Chat and Worktree</span>
                  </button>
                )}
              </>
            ) : null}
          </>
        )}
      </ContextMenu>

      {/* ── Empty area context menu ─────────────────────────────── */}
      <ContextMenu
        isOpen={emptyAreaContextMenu !== null}
        x={emptyAreaContextMenu?.x ?? 0}
        y={emptyAreaContextMenu?.y ?? 0}
        onClose={() => setEmptyAreaContextMenu(null)}
        className="tab-context-menu"
        zIndex={1500}
      >
        {emptyAreaContextMenu && (
          <>
            {!compact && onCreateChat && (
              <button
                className="context-menu-item"
                onClick={() => {
                  setEmptyAreaContextMenu(null);
                  handleNewChat();
                }}
              >
                <Plus size={14} />
                <span>New Chat</span>
              </button>
            )}
            {!compact && onCreateChatInWorktree && (
              <button
                className="context-menu-item"
                onClick={() => {
                  setEmptyAreaContextMenu(null);
                  handleNewWorktreeChat();
                }}
              >
                <GitBranch size={14} />
                <span>New Chat in Worktree</span>
              </button>
            )}
            <div className="context-menu-divider" />
            <button
              className="context-menu-item danger"
              onClick={() => {
                setEmptyAreaContextMenu(null);
                if (paneId) {
                  closeRelatedBuffers((buffer) => buffer.paneId === paneId);
                } else {
                  closeRelatedBuffers(() => true);
                }
              }}
            >
              <X size={14} />
              <span>Close All Tabs</span>
            </button>
          </>
        )}
      </ContextMenu>
    </div>
  );
}

export default EditorTabs;
