import React, { useCallback, useEffect, useRef, type CSSProperties } from 'react';
import { Columns2, Rows2, X, MessageSquarePlus } from 'lucide-react';
import ErrorBoundary from './ErrorBoundary';
import EditorTabs from './EditorTabs';
import WorkspacePane from './WorkspacePane';
import ResizeHandle from './ResizeHandle';
import EditorWithOutline from './EditorWithOutline';
import { TasksPage, BillingPage, TeamPage } from './platform';
import { useEditorManager, MIN_PANE_WIDTH_PERCENT, normalizePaneSize } from '../contexts/EditorManagerContext';
import type { PerChatState } from '../types/app';

export interface EditorWorkspaceProps {
  currentView: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team';
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;
  onCreateChat?: () => Promise<string | null>;
  chatProps: React.ComponentProps<typeof WorkspacePane>['chatProps'];
  reviewProps: React.ComponentProps<typeof WorkspacePane>['reviewProps'];
  diffState: React.ComponentProps<typeof WorkspacePane>['diffState'];
  handleOutlineNavigateToSymbol: (line: number) => void;
}

const toPaneFlex = (weight: number): CSSProperties => ({
  flexGrow: weight,
  flexShrink: 1,
  flexBasis: 0,
  minWidth: 0,
  minHeight: 0,
});

const PaneWrapper: React.FC<{ children: React.ReactNode, style?: CSSProperties }> = ({ children, style }) => (
  <div className="pane-wrapper" style={style}>{children}</div>
);

const EditorPaneWrapper: React.FC<{ children: React.ReactNode, isActive?: boolean, onClick?: () => void }> = ({ children, isActive, onClick }) => {
  return (
    <div
      className={`editor-pane-wrapper ${isActive ? 'active' : ''}`}
      onClick={!isActive ? onClick : undefined}
      tabIndex={isActive ? -1 : 0}
      onFocus={() => isActive && (onClick?.())}
    >
      {children}
    </div>
  );
};

const EditorPaneComponent: React.FC<{
  paneId: string;
  isActive?: boolean;
  onClick?: () => void;
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;
  chatProps: React.ComponentProps<typeof WorkspacePane>['chatProps'];
  reviewProps: React.ComponentProps<typeof WorkspacePane>['reviewProps'];
  diffState: React.ComponentProps<typeof WorkspacePane>['diffState'];
}> = ({ paneId, onClick, perChatCache, activeChatId, chatProps, reviewProps, diffState }) => {
  return (
    <div className="editor-pane-host" onClick={onClick}>
      <WorkspacePane
        paneId={paneId}
        perChatCache={perChatCache}
        activeChatId={activeChatId}
        chatProps={chatProps}
        reviewProps={reviewProps}
        diffState={diffState}
      />
    </div>
  );
};

const EditorWorkspace: React.FC<EditorWorkspaceProps> = ({
  currentView,
  perChatCache,
  activeChatId,
  onCreateChat,
  chatProps,
  reviewProps,
  diffState,
  handleOutlineNavigateToSymbol,
}) => {
  const {
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    buffers,
    switchPane,
    splitPane,
    closePane,
    closeSplit,
    paneSizes,
    updatePaneSize,
    maxPanes,
    openWorkspaceBuffer,
  } = useEditorManager();

  const currentBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
  const canSplit = panes.length < maxPanes;
  const canCloseSplit = panes.length > 1;

  const [nestedSplit, setNestedSplit] = React.useState<{ hostPaneId: string; nestedPaneId: string; direction: 'vertical' | 'horizontal' } | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const dragStartSizeRef = useRef<Map<string, number>>(new Map());
  const isPaneDraggingRef = useRef<Set<string>>(new Set());

  React.useEffect(() => {
    if (panes.length !== 3 && nestedSplit) {
      setNestedSplit(null);
    }
  }, [nestedSplit, panes.length]);

  const handlePaneResize = useCallback((sizeKey: string, axis: 'horizontal' | 'vertical', invert = false) => (_deltaPixels: number, totalDeltaPixels: number) => {
    if (!containerRef.current) return;

    const containerRect = containerRef.current.getBoundingClientRect();
    const isVertical = axis === 'horizontal';
    const containerSize = isVertical ? containerRect.width : containerRect.height;
    const deltaPercent = ((invert ? -totalDeltaPixels : totalDeltaPixels) / containerSize) * 100;

    // Capture size at drag start to avoid accumulation bugs.
    if (!isPaneDraggingRef.current.has(sizeKey)) {
      isPaneDraggingRef.current.add(sizeKey);
      dragStartSizeRef.current.set(sizeKey, paneSizes[sizeKey] || 50);
    }
    const sizeAtDragStart = dragStartSizeRef.current.get(sizeKey)!;
    const maxAllowed = 100 - MIN_PANE_WIDTH_PERCENT * Math.max(0, Object.keys(paneSizes).filter(k => !k.startsWith('group:') && !k.startsWith('nested:') && !k.startsWith('grid:')).length - 1);
    const newSize = Math.max(MIN_PANE_WIDTH_PERCENT, Math.min(maxAllowed, sizeAtDragStart + deltaPercent));
    updatePaneSize(sizeKey, newSize);
  }, [paneSizes, updatePaneSize]);

  const handlePaneResizeEnd = useCallback((sizeKey: string) => () => {
    isPaneDraggingRef.current.delete(sizeKey);
    dragStartSizeRef.current.delete(sizeKey);
  }, []);

  const showResizeHandles = panes.length > 1;

  const renderSplitControls = (paneId: string) => {
    return (
      <div className="split-controls split-controls-embedded">
        {paneId === activePaneId && onCreateChat && (
          <button
            onClick={async () => {
              const newId = await onCreateChat();
              if (newId) {
                openWorkspaceBuffer({
                  kind: 'chat',
                  path: `__workspace/chat/${newId}`,
                  title: 'New Chat',
                  isPinned: false,
                  isClosable: true,
                  metadata: { chatId: newId },
                });
              }
            }}
            className="pane-control-btn compact"
            title="New chat"
            aria-label="New chat"
          >
            <MessageSquarePlus size={13} />
          </button>
        )}
        {paneId === activePaneId && canCloseSplit && (
          <button
            onClick={handleCloseAllSplits}
            className="pane-control-btn compact"
            title="Close split panes"
            aria-label="Close split panes"
          >
            <X size={13} />
          </button>
        )}
        {paneId === activePaneId && canSplit && (
          <button
            onClick={() => handleSplitRequest('vertical')}
            className="pane-control-btn compact"
            title="Split vertically"
            aria-label="Split vertically"
          >
            <Columns2 size={14} />
          </button>
        )}
        {paneId === activePaneId && canSplit && (
          <button
            onClick={() => handleSplitRequest('horizontal')}
            className="pane-control-btn compact"
            title="Split horizontally"
            aria-label="Split horizontally"
          >
            <Rows2 size={14} />
          </button>
        )}
      </div>
    );
  };

  const renderPaneById = (paneId: string, style?: CSSProperties) => {
    const pane = panes.find((item) => item.id === paneId);
    if (!pane) {
      return null;
    }

    return (
      <PaneWrapper key={pane.id} style={style}>
        <div className="pane-shell">
          <EditorTabs
            paneId={pane.id}
            compact
            actions={renderSplitControls(pane.id)}
          />
          <EditorPaneWrapper
            isActive={pane.id === activePaneId}
            onClick={() => switchPane(pane.id)}
          >
            <EditorPaneComponent
              paneId={pane.id}
              isActive={pane.id === activePaneId}
              onClick={() => switchPane(pane.id)}
              perChatCache={perChatCache}
              activeChatId={activeChatId}
              chatProps={chatProps}
              reviewProps={reviewProps}
              diffState={diffState}
            />
          </EditorPaneWrapper>
        </div>
      </PaneWrapper>
    );
  };

  const renderPaneLayout = () => {
    if (panes.length === 0) {
      return null;
    }

    if (panes.length < 3 || !nestedSplit) {
      if (panes.length === 1) {
        return renderPaneById(panes[0].id, toPaneFlex(1));
      }

      if (panes.length === 2) {
        const [firstPane, secondPane] = panes;
        const splitAxis = paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal';
        const firstPaneSize = Math.max(MIN_PANE_WIDTH_PERCENT, Math.min(100 - MIN_PANE_WIDTH_PERCENT, paneSizes[firstPane.id] || 50));
        const secondPaneSize = 100 - firstPaneSize;

        return (
          <>
            {renderPaneById(firstPane.id, toPaneFlex(firstPaneSize))}
            <ResizeHandle
              direction={splitAxis}
              onResize={handlePaneResize(firstPane.id, splitAxis)}
              onResizeEnd={handlePaneResizeEnd(firstPane.id)}
            />
            {renderPaneById(secondPane.id, toPaneFlex(secondPaneSize))}
          </>
        );
      }

      return (
        <>
          {(() => {
            const rawSizes = panes.map(p => paneSizes[p.id] || (100 / panes.length));
            const totalSize = rawSizes.reduce((a, b) => a + b, 0);
            return (
              <>
                {panes.map((pane, index) => {
                  const paneSize = normalizePaneSize(rawSizes[index], totalSize);
                  const isLast = index === panes.length - 1;
                  const splitAxis = paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal';

                  return (
                    <React.Fragment key={pane.id}>
                      {renderPaneById(pane.id, toPaneFlex(paneSize))}
                      {showResizeHandles && !isLast && (
                        <ResizeHandle
                          direction={splitAxis}
                          onResize={handlePaneResize(pane.id, splitAxis)}
                          onResizeEnd={handlePaneResizeEnd(pane.id)}
                        />
                      )}
                    </React.Fragment>
                  );
                })}
              </>
            );
          })()}
        </>
      );
    }

    // 3+ panes with nested split
    const hostPane = panes.find((pane) => pane.id === nestedSplit.hostPaneId);
    const nestedPane = panes.find((pane) => pane.id === nestedSplit.nestedPaneId);
    const siblingPane = panes.find((pane) => pane.id !== nestedSplit.hostPaneId && pane.id !== nestedSplit.nestedPaneId);
    if (!hostPane || !nestedPane || !siblingPane) {
      return null;
    }

    const rootDirection = paneLayout === 'split-horizontal' ? 'column' : 'row';
    const nestedDirection = nestedSplit.direction === 'horizontal' ? 'column' : 'row';
    const hostIsFirst = panes.findIndex((pane) => pane.id === hostPane.id) < panes.findIndex((pane) => pane.id === siblingPane.id);
    const rootSizeKey = `group:${hostPane.id}`;
    const nestedSizeKey = `nested:${hostPane.id}`;
    const groupSize = paneSizes[rootSizeKey] || 50;
    const nestedSize = paneSizes[nestedSizeKey] || 50;
    const rootHandleDirection = rootDirection === 'row' ? 'horizontal' : 'vertical';
    const nestedHandleDirection = nestedDirection === 'row' ? 'horizontal' : 'vertical';

    const nestedGroup = (
      <div
        className={`nested-pane-group nested-pane-group-${nestedDirection}`}
        style={toPaneFlex(groupSize)}
      >
        {renderPaneById(hostPane.id, toPaneFlex(nestedSize))}
        <ResizeHandle
          direction={nestedHandleDirection}
          onResize={handlePaneResize(nestedSizeKey, nestedHandleDirection)}
          onResizeEnd={handlePaneResizeEnd(nestedSizeKey)}
        />
        {renderPaneById(nestedPane.id, toPaneFlex(100 - nestedSize))}
      </div>
    );

    return (
      <div className={`nested-pane-layout nested-pane-layout-${rootDirection}`}>
        {hostIsFirst ? nestedGroup : renderPaneById(siblingPane.id, toPaneFlex(100 - groupSize))}
        <ResizeHandle
          direction={rootHandleDirection}
          onResize={handlePaneResize(rootSizeKey, rootHandleDirection, !hostIsFirst)}
          onResizeEnd={handlePaneResizeEnd(rootSizeKey)}
        />
        {hostIsFirst ? renderPaneById(siblingPane.id, toPaneFlex(100 - groupSize)) : nestedGroup}
      </div>
    );
  };

  const handleSplitRequest = useCallback((direction: 'vertical' | 'horizontal') => {
    if (!activePaneId) {
      return;
    }

    const previousPaneCount = panes.length;
    const newPaneId = splitPane(activePaneId, direction);
    if (!newPaneId) {
      return;
    }

    if (previousPaneCount === 2) {
      setNestedSplit({
        hostPaneId: activePaneId,
        nestedPaneId: newPaneId,
        direction,
      });
      updatePaneSize(`group:${activePaneId}`, 50);
      updatePaneSize(`nested:${activePaneId}`, 50);
    }
  }, [activePaneId, panes.length, splitPane, updatePaneSize]);

  const handleCloseAllSplits = useCallback(() => {
    if (nestedSplit) {
      // When a nested split is active, close just the nested pane (3 → 2 panes)
      closePane(nestedSplit.nestedPaneId);
      setNestedSplit(null);
    } else {
      // No nested split — close all splits (2 → 1 pane)
      closeSplit();
    }
  }, [closeSplit, closePane, nestedSplit]);

  // Handle focus_split hotkeys (focus_split_1 through focus_split_6)
  const handleFocusPaneIndex = useCallback((index: number) => {
    if (index < 0) {
      return;
    }

    // If the pane exists, switch to it
    if (index < panes.length) {
      switchPane(panes[index].id);
      return;
    }

    // If we can create a new pane, do so
    if (panes.length < maxPanes) {
      const sourcePaneId = activePaneId || panes[panes.length - 1]?.id;
      if (!sourcePaneId) return;

      const direction = panes.length === 1 ? 'vertical' : 'horizontal';
      const newPaneId = splitPane(sourcePaneId, direction);

      if (newPaneId) {
        if (panes.length === 1) {
          updatePaneSize(`group:${sourcePaneId}`, 50);
          updatePaneSize(`nested:${sourcePaneId}`, 50);
        }
        switchPane(newPaneId);
      }
    }
  }, [panes, activePaneId, splitPane, switchPane, updatePaneSize, maxPanes]);

  // Listen for focus_split hotkeys
  React.useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail?.commandId) return;

      const commandId = detail.commandId;
      const match = commandId.match(/^focus_split_(\d+)$/);

      if (match) {
        const index = parseInt(match[1], 10) - 1; // Convert to 0-based index
        handleFocusPaneIndex(index);
      }
    };

    window.addEventListener('sprout:hotkey', handleHotkey);
    return () => window.removeEventListener('sprout:hotkey', handleHotkey);
  }, [handleFocusPaneIndex]);

  if (currentView === 'tasks') {
    return <TasksPage />;
  }

  if (currentView === 'billing') {
    return <BillingPage />;
  }

  if (currentView === 'team') {
    return <TeamPage />;
  }

  return (
    <EditorWithOutline
      content={currentBuffer?.content || ''}
      fileExtension={currentBuffer?.file?.ext}
      cursorLine={currentBuffer?.cursorPosition?.line || 0}
      isFileOpen={currentBuffer?.kind === 'file'}
      onNavigateToSymbol={handleOutlineNavigateToSymbol}
    >
      <div className={`editor-workspace ${paneLayout}`}>
        <div
          ref={containerRef}
          className={`panes-container layout-${paneLayout}`}
        >
          {renderPaneLayout()}
        </div>
      </div>
    </EditorWithOutline>
  );
};

export default EditorWorkspace;
