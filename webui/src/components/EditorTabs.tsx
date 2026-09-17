import { ContextMenu } from '@sprout/ui';
import {
  X,
  FolderOpen,
  ArrowRightLeft,
  PanelRightOpen,
  Eye,
  Pin,
  Plus,
  GitBranch,
  Pencil,
  RefreshCw,
  Trash2,
  Loader2,
} from 'lucide-react';
import { useEffect, useMemo, useRef, useState, useCallback, memo } from 'react';
import type { MouseEvent, KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useTabDragReorder } from '../hooks/useTabDragReorder';
import { readFileWithConsent } from '../services/fileAccess';
import { notificationBus } from '../services/notificationBus';
import { type EditorBuffer } from '../types/editor';
import { isSharedMode } from '../utils/sharedMode';
import { catchIfAsync, getBufferIcon, getChatId, getFileIcon, getFileIconColor } from './editorTabIcons';
import { showThemedConfirm } from './ThemedDialog';
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
  /** Create a new chat. May return the new chat id (directly or via Promise)
   * so the caller can open a buffer for it immediately. */
  onCreateChat?: () => string | null | Promise<string | null> | void;
  onCreateChatInWorktree?: () => void;
  /** Delete a chat session (and optionally its worktree) server-side. */
  onDeleteChat?: (id: string, options?: { removeWorktree?: boolean }) => void;
  /** Legacy shape kept for callers that only delete worktree-backed chats. */
  onDeleteChatWithWorktree?: (id: string) => void;
  onRenameChat?: (id: string, name: string) => void;
  onDeleteAllChats?: () => void;
  chatSessions?: Array<{ id: string; name?: string; is_pinned?: boolean; is_default?: boolean }>;
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
  onDeleteChat,
  onDeleteChatWithWorktree,
  onRenameChat,
  onDeleteAllChats,
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
    openWorkspaceBuffer,
    reloadBufferFromDisk,
  } = useEditorManager();
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; bufferId: string } | null>(null);
  const [emptyAreaContextMenu, setEmptyAreaContextMenu] = useState<{ x: number; y: number } | null>(null);
  const tabRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const buffersRef = useRef(buffers);
  useEffect(() => {
    buffersRef.current = buffers;
  }, [buffers]);

  // Drop tabRef entries for buffers that have been closed so the map
  // doesn't grow unbounded across long sessions.
  useEffect(() => {
    const live = new Set<string>();
    for (const id of buffers.keys()) live.add(id);
    for (const id of Object.keys(tabRefs.current)) {
      if (!live.has(id)) delete tabRefs.current[id];
    }
  }, [buffers]);

  // ── Drag-and-drop tab reorder ─────────────────────────────────
  const { handleDragStart, handleDrop, resolveDraggedBufferId, handlePaneDrop, handleDragEnd } = useTabDragReorder({
    paneId,
    reorderBuffers,
    moveBufferToPane,
  });

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

  // Preserve insertion order, filter by paneId.
  // Uses `buffers` directly — Array.from + filter is trivially cheap.
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
    // Use 'instant' so rapid Cmd+Option+→ tab cycling doesn't trigger a
    // smooth-scroll queue that jumps behind the user.
    if (activeTab) {
      activeTab.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' as ScrollBehavior });
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

  const handleTabClose = async (e: MouseEvent, buffer: EditorBuffer) => {
    e.stopPropagation();

    if (buffer.isPinned && buffer.kind !== 'chat') return;

    if (buffer.isModified) {
      const ok = await showThemedConfirm(`You have unsaved changes in "${buffer.file.name}". Close without saving?`, {
        title: 'Unsaved changes',
        type: 'danger',
        confirmLabel: 'Close without saving',
      });
      if (!ok) return;
      closeBuffer(buffer.id);
      return;
    }

    if (buffer.isPinned && buffer.kind === 'chat') {
      const ok = await showThemedConfirm(`Close pinned chat "${buffer.file.name}"?`, {
        title: 'Close pinned chat',
        type: 'warning',
        confirmLabel: 'Close',
      });
      if (!ok) return;
      closeBuffer(buffer.id);
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
  const startRename = useCallback(
    (buffer: EditorBuffer) => {
      // Only allow renaming chat tabs that are not default sessions
      const chatId = getChatId(buffer);
      if (!chatId) return;
      const session = chatSessions?.find((s) => s.id === chatId);
      if (session?.is_default) return;
      setRenamingBufferId(buffer.id);
      setRenameValue(buffer.file.name);
    },
    [chatSessions],
  );

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

  const handleTabDoubleClick = useCallback(
    (buffer: EditorBuffer) => {
      if (buffer.kind === 'chat') {
        startRename(buffer);
      }
    },
    [startRename],
  );

  // Bulk close uses showThemedConfirm so the confirmation surface matches the
  // rest of the app. The list of dirty files is captured at the moment the
  // dialog opens so any new modifications made while the dialog is open
  // don't sneak into the batch.
  const closeRelatedBuffers = async (predicate: (buffer: EditorBuffer) => boolean) => {
    const closeTargets = Array.from(buffersRef.current.values()).filter(
      (buffer) => buffer.isClosable !== false && !buffer.isPinned && predicate(buffer),
    );
    const modifiedTargets = closeTargets.filter((buffer) => buffer.isModified);

    if (modifiedTargets.length > 0) {
      const names = modifiedTargets.map((b) => b.file.name).join(', ');
      const display = names.length > 60 ? `${names.slice(0, 60)}… and ${modifiedTargets.length - 1} more` : names;
      const ok = await showThemedConfirm(`You have unsaved changes in:\n${display}\n\nClose all without saving?`, {
        title: 'Unsaved changes',
        type: 'danger',
        confirmLabel: 'Close without saving',
      });
      if (!ok) return;
    }

    // Re-read at the moment of confirm so the snapshot reflects the latest
    // buffer state (a file might have been saved while the dialog was open).
    const finalTargets = Array.from(buffersRef.current.values()).filter(
      (buffer) => buffer.isClosable !== false && !buffer.isPinned && predicate(buffer),
    );
    finalTargets.forEach((buffer) => closeBuffer(buffer.id));
  };

  // ── Safe callbacks for chat actions (fire-and-forget with error handling) ──
  // These catch unhandled rejections when the prop is actually an async function
  // but typed as () => void (e.g. PaneLayoutManager's wrapper that discards the Promise).
  // Optimistic UX: a placeholder chat tab opens IMMEDIATELY (spinner in the
  // tab), then claims the real chatId when the create resolves and closes
  // itself on failure. Previously nothing appeared until create+list
  // round-trips finished, which read as "button did nothing".
  const handleNewChat = useCallback(() => {
    if (!onCreateChat) return;
    // Shared-mode servers reject creates outright — don't show a placeholder
    // that's about to vanish.
    if (isSharedMode()) return;
    const placeholderId = openWorkspaceBuffer({
      kind: 'chat',
      path: `__workspace/chat/creating-${Date.now()}`,
      title: 'Creating…',
      isPinned: false,
      isClosable: true,
      metadata: { creating: true },
    });
    // On success open the REAL buffer (path-keyed, so useChatSessionsSync's
    // open of __workspace/chat/<id> dedupes against it) and drop the
    // placeholder. Mutating the placeholder's metadata in place instead
    // would race the session-list sync, which matches buffers by chatId —
    // losing that race produced two tabs for one chat.
    const swapInReal = (newId: string) => {
      openWorkspaceBuffer({
        kind: 'chat',
        path: `__workspace/chat/${newId}`,
        title: 'New Chat',
        isPinned: false,
        isClosable: true,
        metadata: { chatId: newId },
      });
      void closeBuffer(placeholderId);
      // Creating a chat lands you IN it: the buffer is already active, so
      // switch the chat-level state to match (otherwise the tab renders the
      // previous active chat's transcript).
      onActiveChatChange?.(newId);
    };
    try {
      Promise.resolve(onCreateChat())
        .then((newId) => {
          if (typeof newId === 'string' && newId) {
            swapInReal(newId);
          } else {
            // Create failed — remove the placeholder.
            void closeBuffer(placeholderId);
          }
        })
        .catch((err: unknown) => {
          console.warn('[EditorTabs] Failed to create chat:', err);
          void closeBuffer(placeholderId);
        });
    } catch (err) {
      console.warn('[EditorTabs] Failed to create chat:', err);
      void closeBuffer(placeholderId);
    }
  }, [onCreateChat, openWorkspaceBuffer, closeBuffer, onActiveChatChange]);

  const handleNewWorktreeChat = useCallback(() => {
    if (onCreateChatInWorktree) {
      try {
        const result = onCreateChatInWorktree();
        catchIfAsync(result, (err) => console.warn('[EditorTabs] Failed to create worktree chat:', err));
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
  const contextIsDefaultChat = contextChatId ? (defaultChatIds?.has(contextChatId) ?? false) : false;
  const contextHasWorktree = contextChatId ? chatWorktreePaths?.has(contextChatId) : false;

  const handleContextAction = (action: () => void | Promise<void>) => {
    setContextMenu(null);
    action();
  };

  // ── Reload from disk ──────────────────────────────────────────
  // Badge click / context-menu action: take the on-disk version of the
  // file, discarding this buffer's unsaved edits. Two regimes:
  //  - clean buffer: read + reloadBufferFromDisk (silent, cursor-free —
  //    the buffer is not mounted in any view if inactive; if active, the
  //    `file:auto-reloaded` pipeline in useEditorFileIO applies a ranged
  //    CodeMirror replacement that maps the cursor through the diff).
  //  - modified buffer with an external conflict: same reload, plus the
  //    conflict flag clears — this click IS the arbitration. Guarded so
  //    it never runs for a modified buffer WITHOUT a pending conflict,
  //    where it would silently destroy typed work (that case belongs to
  //    the conflict dialog).
  const reloadFromDisk = useCallback(
    async (bufferId: string) => {
      const buf = buffersRef.current.get(bufferId);
      if (!buf || buf.kind !== 'file') return;
      if (buf.file.path.startsWith('__workspace/')) return;
      if (buf.isModified && !buf.externallyModified) return;

      try {
        const response = await readFileWithConsent(buf.file.path);
        if (!response.ok) {
          notificationBus.notify('warning', 'Reload Failed', `Could not read ${buf.file.name} from disk.`, 5000);
          return;
        }
        const content = await response.text();

        // Re-validate after the async read: the buffer may have been
        // closed, switched, or typed into meanwhile.
        const fresh = buffersRef.current.get(bufferId);
        if (!fresh || fresh.kind !== 'file') return;
        if (fresh.isModified && !fresh.externallyModified) return;

        reloadBufferFromDisk(bufferId, content);
        document.dispatchEvent(
          new CustomEvent('file:auto-reloaded', {
            detail: { bufferId, content },
          }),
        );
        if (fresh.isModified) {
          notificationBus.notify(
            'info',
            'Reloaded from disk',
            `${fresh.file.name} reloaded — unsaved changes were discarded in favor of the disk version.`,
            5000,
          );
        }
      } catch (err) {
        notificationBus.notify('error', 'Reload Failed', String(err instanceof Error ? err.message : err), 5000);
      }
    },
    [buffersRef, reloadBufferFromDisk],
  );

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
              const hasActiveQuery = chatId ? (activeChatQueries?.has(chatId) ?? false) : false;
              const worktreePath = chatId ? chatWorktreePaths?.get(chatId) : undefined;
              const isDefaultChat = chatId ? (defaultChatIds?.has(chatId) ?? false) : false;

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
                  onDragEnd={handleDragEnd}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    const droppedBufferId = resolveDraggedBufferId(e);
                    if (!droppedBufferId || droppedBufferId === buffer.id) {
                      handleDragEnd();
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
                    {buffer.metadata?.creating ? (
                      <Loader2 size={13} className="tab-creating-spinner" aria-label="Creating chat" />
                    ) : (
                      <span className="tab-icon" style={{ color: getFileIconColor(buffer.file.ext) }}>
                        {getBufferIcon(buffer)}
                      </span>
                    )}
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
                      <span className="tab-name" title={buffer.file.path}>
                        {buffer.file.name}
                      </span>
                    )}
                    {buffer.isModified && (
                      <span className="tab-modified" aria-label="Unsaved changes">
                        <span className="tab-modified-dot" />
                      </span>
                    )}
                    {buffer.externallyModified && (
                      <button
                        className="tab-externally-modified"
                        title="File changed on disk — click to reload from disk"
                        aria-label={`Reload ${buffer.file.name} from disk (changed externally)`}
                        onClick={(e) => {
                          e.stopPropagation();
                          void reloadFromDisk(buffer.id);
                        }}
                      >
                        <RefreshCw size={11} aria-hidden="true" />
                      </button>
                    )}
                    {/* Pin button hidden on chat tabs — pin/unpin a chat
                     * isn't a typical workflow, and the pinned-default-chat
                     * gets a forced pinned state via context anyway. */}
                    {buffer.kind !== 'chat' && (
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
                    )}
                    {buffer.isClosable !== false && (buffer.kind === 'chat' || !buffer.isPinned) && !isDefaultChat && (
                      <button
                        className="tab-close"
                        onClick={(e) => handleTabClose(e, buffer)}
                        title={`Close ${buffer.file.name}`}
                        aria-label={`Close ${buffer.file.name}`}
                      >
                        <X size={14} />
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
            {/* New Chat button (Plus icon) — hidden in shared mode */}
            {!compact && onCreateChat && !isSharedMode() && (
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
            {/* New Chat in Worktree button — hidden in shared mode */}
            {!compact && onCreateChatInWorktree && !isSharedMode() && (
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

      {/* Unsaved-changes confirm uses the same themed dialog as the rest of
       * the app (see SettingsPanel etc.) — surfaced via a useEffect just
       * below when `showConfirm` flips truthy. The bespoke .close-confirm
       * dialog used to live here and was the only place in the editor
       * that didn't use showThemedConfirm. */}

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
            {/* Take the on-disk version: shown for file buffers with an
             * external-change conflict (click = arbitration) or a clean
             * buffer (harmless refresh). Deliberately NOT shown for a
             * modified buffer without a conflict — that would silently
             * destroy unsaved edits; the conflict dialog owns that case. */}
            {activeContextBuffer.kind === 'file' &&
              !activeContextBuffer.file.path.startsWith('__workspace/') &&
              (!activeContextBuffer.isModified || activeContextBuffer.externallyModified) && (
                <button
                  className="context-menu-item"
                  onClick={() => handleContextAction(() => void reloadFromDisk(activeContextBuffer.id))}
                >
                  <RefreshCw size={14} />
                  <span>Reload from disk{activeContextBuffer.isModified ? ' (discard my changes)' : ''}</span>
                </button>
              )}
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
            {/* Pin is meaningless for chat tabs (defaults are forced
             * pinned by the context). Only surface the action on real
             * file tabs. */}
            {activeContextBuffer.kind !== 'chat' && (
              <button
                className="context-menu-item"
                onClick={() => handleContextAction(() => toggleBufferPin(activeContextBuffer.id))}
                disabled={!activeContextBuffer.isPinned && activeContextBuffer.isClosable === false}
              >
                <Pin size={14} fill={activeContextBuffer.isPinned ? 'currentColor' : 'none'} />
                <span>{activeContextBuffer.isPinned ? 'Unpin tab' : 'Pin tab'}</span>
              </button>
            )}

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
            {activeContextBuffer.isClosable !== false &&
            (activeContextBuffer.kind === 'chat' || !activeContextBuffer.isPinned) ? (
              <>
                <button
                  className="context-menu-item danger"
                  onClick={() => handleContextAction(() => closeBuffer(activeContextBuffer.id))}
                >
                  <Trash2 size={14} />
                  <span>Close</span>
                </button>
                {/* Delete Chat — every non-default chat */}
                {activeContextBuffer.kind === 'chat' && !contextIsDefaultChat && onDeleteChat && contextChatId && (
                  <button
                    className="context-menu-item danger"
                    onClick={() =>
                      handleContextAction(async () => {
                        const hasWorktree = contextHasWorktree === true;
                        const confirmed = await showThemedConfirm(
                          hasWorktree
                            ? 'This will permanently delete the chat session and remove the git worktree directory from disk. Are you sure?'
                            : 'This will permanently delete the chat session and its messages. Are you sure?',
                          { type: 'danger' },
                        );
                        if (!confirmed) return;
                        onDeleteChat(contextChatId, { removeWorktree: hasWorktree });
                      })
                    }
                  >
                    <Trash2 size={14} />
                    <span>{contextHasWorktree ? 'Delete Chat and Worktree' : 'Delete Chat'}</span>
                  </button>
                )}
                {/* Delete Chat and Worktree — legacy callback shape, for
                    callers that only support the worktree variant */}
                {activeContextBuffer.kind === 'chat' &&
                  contextHasWorktree &&
                  !onDeleteChat &&
                  onDeleteChatWithWorktree && (
                    <button
                      className="context-menu-item danger"
                      onClick={() =>
                        handleContextAction(async () => {
                          const confirmed = await showThemedConfirm(
                            'This will permanently delete the chat session and remove the git worktree directory from disk. Are you sure?',
                            { type: 'danger' },
                          );
                          if (!confirmed) return;
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
            {!compact && onCreateChat && !isSharedMode() && (
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
            {!compact && onCreateChatInWorktree && !isSharedMode() && (
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
            {onDeleteAllChats && (
              <button
                className="context-menu-item danger"
                onClick={async () => {
                  setEmptyAreaContextMenu(null);
                  const confirmed = await showThemedConfirm('Close all chat sessions except the active one?', {
                    type: 'warning',
                  });
                  if (confirmed) {
                    onDeleteAllChats();
                  }
                }}
              >
                <Trash2 size={14} />
                <span>Close All Chats</span>
              </button>
            )}
          </>
        )}
      </ContextMenu>
    </div>
  );
}

export default memo(EditorTabs);
