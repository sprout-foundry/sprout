import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { DragEvent, MouseEvent } from 'react';
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
      return '#f7df1e';
    case '.ts':
    case '.tsx':
      return '#3178c6';
    case '.go':
      return '#00add8';
    case '.py':
      return '#3776ab';
    case '.json':
      return '#cbcb41';
    case '.html':
      return '#e34c26';
    case '.css':
      return '#264de4';
    case '.md':
      return '#083fa1';
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

function EditorTabs({ paneId, actions, compact = false }: EditorTabsProps): JSX.Element {
  const {
    buffers,
    panes,
    activeBufferId,
    activePaneId,
    switchPane,
    switchToBuffer,
    closeBuffer,
    reorderBuffers,
    moveBufferToPane,
  } = useEditorManager();
  const [showConfirm, setShowConfirm] = useState<{ bufferId: string; fileName: string } | null>(null);
  const [draggingBufferId, setDraggingBufferId] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; bufferId: string } | null>(null);
  const [emptyAreaContextMenu, setEmptyAreaContextMenu] = useState<{ x: number; y: number } | null>(null);
  const tabRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const batchCloseTargetsRef = useRef<string[]>([]);

  const paneOrder = useMemo(() => {
    const order = new Map<string, number>();
    panes.forEach((pane, index) => {
      order.set(pane.id, index + 1);
    });
    return order;
  }, [panes]);

  // Preserve insertion order so tabs stay spatially stable.
  const bufferList = useMemo(() => {
    const values = Array.from(buffers.values());
    return paneId ? values.filter((buffer) => buffer.paneId === paneId) : values;
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

  const handleTabClick = (buffer: EditorBuffer) => {
    if (buffer.id !== activeBufferId) {
      switchToBuffer(buffer.id);
    }
  };

  const handleTabClose = (e: MouseEvent, buffer: EditorBuffer) => {
    e.stopPropagation();

    if (buffer.isModified) {
      setShowConfirm({ bufferId: buffer.id, fileName: buffer.file.name });
      return;
    }

    closeBuffer(buffer.id);
  };

  const handleTabAuxClick = (e: MouseEvent, buffer: EditorBuffer) => {
    if (e.button !== 1 || buffer.isClosable === false) {
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

  const closeRelatedBuffers = (predicate: (buffer: EditorBuffer) => boolean) => {
    const closeTargets = Array.from(buffers.values()).filter(
      (buffer) => buffer.isClosable !== false && predicate(buffer),
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

  const activeContextBuffer = contextMenu ? buffers.get(contextMenu.bufferId) || null : null;
  const contextPaneId = activeContextBuffer?.paneId || paneId || null;
  const availablePaneTargets = panes.filter((pane) => pane.id !== contextPaneId);

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
        {bufferList.length === 0 ? (
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
            {bufferList.map((buffer) => (
              <div
                key={buffer.id}
                className={`tab ${buffer.id === activeBufferId ? 'active' : ''}`}
                ref={(el) => {
                  tabRefs.current[buffer.id] = el;
                }}
                onClick={() => handleTabClick(buffer)}
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
                  <span className="tab-icon" style={{ color: getFileIconColor(buffer.file.ext) }}>
                    {getBufferIcon(buffer)}
                  </span>
                  <span className="tab-name">{buffer.file.name}</span>
                  {buffer.paneId && paneOrder.has(buffer.paneId) && (
                    <span className={`tab-pane-badge ${buffer.paneId === activePaneId ? 'active-pane' : ''}`}>
                      {paneOrder.get(buffer.paneId)}
                    </span>
                  )}
                  {buffer.isModified && <span className="tab-modified">●</span>}
                  {buffer.externallyModified && (
                    <span className="tab-externally-modified" title="File changed on disk">
                      ↑
                    </span>
                  )}
                  {buffer.isClosable !== false && (
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
            ))}
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
            {activeContextBuffer.isClosable !== false ? (
              <button
                className="context-menu-item danger"
                onClick={() => handleContextAction(() => closeBuffer(activeContextBuffer.id))}
              >
                <X size={14} />
                <span>Close</span>
              </button>
            ) : null}
          </>
        )}
      </ContextMenu>
      <ContextMenu
        isOpen={emptyAreaContextMenu !== null}
        x={emptyAreaContextMenu?.x ?? 0}
        y={emptyAreaContextMenu?.y ?? 0}
        onClose={() => setEmptyAreaContextMenu(null)}
        className="tab-context-menu"
        zIndex={1500}
      >
        {emptyAreaContextMenu && (
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
        )}
      </ContextMenu>
    </div>
  );
};

export default EditorTabs;
