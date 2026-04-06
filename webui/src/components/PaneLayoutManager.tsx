import { useCallback, useRef, Fragment } from 'react';
import type { CSSProperties, RefObject, Dispatch, SetStateAction, ReactNode, ComponentProps } from 'react';
import { X, Columns2, Rows2, LayoutGrid, MessageSquarePlus } from 'lucide-react';
import EditorTabs from './EditorTabs';
import WorkspacePane from './WorkspacePane';
import ResizeHandle from './ResizeHandle';
import type { EditorPane, EditorBuffer, PaneLayout } from '../types/editor';
import type { PerChatState, TodoItem, Message, ToolExecution, SubagentActivity } from '../types/app';
import type { OpenWorkspaceBufferFn } from '../hooks/useChatSessionSync';
import type { GitDiffResponse, DeepReviewResult } from '../hooks/useGitWorkspace';

const toPaneFlex = (weight: number): CSSProperties => ({
  flexGrow: weight,
  flexShrink: 1,
  flexBasis: 0,
  minWidth: 0,
  minHeight: 0,
});

/** Shape of nested split configuration for 3-pane layouts */
export interface PaneLayoutManagerProps {
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
  activeBufferId: string | null;
  buffers: Map<string, EditorBuffer>;
  paneSizes: Record<string, number>;
  contextPanelRef: RefObject<unknown>;
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;

  // Chat state for active chat pane
  messages: Message[];
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  onStopProcessing: () => void;
  queuedMessagesCount: number;
  queuedMessages: string[];
  onQueueMessageRemove: (index: number) => void;
  onQueueMessageEdit: (index: number, newText: string) => void;
  onQueueReorder: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages: () => void;
  inputValue: string;
  onInputChange: Dispatch<SetStateAction<string>>;
  isProcessing: boolean;
  lastError: string | null;
  toolExecutions: ToolExecution[];
  queryProgress: unknown;
  currentTodos: TodoItem[];
  subagentActivities: SubagentActivity[];

  // Review state
  deepReview: DeepReviewResult | null;
  reviewError: string | null;
  reviewFixResult: string | null;
  reviewFixLogs: string[];
  reviewFixSessionID: string | null;
  isReviewLoading: boolean;
  isReviewFixing: boolean;
  onFixFromReview: (options?: { fixPrompt?: string; selectedItems?: string[] }) => Promise<void>;

  // Diff state
  activeDiffPath: string | null;
  activeDiff: GitDiffResponse | null;
  diffMode: 'combined' | 'staged' | 'unstaged';
  isDiffLoading: boolean;
  diffError: string | null;
  onDiffModeChange: (mode: 'combined' | 'staged' | 'unstaged') => void;

  // Editor manager actions
  switchPane: (paneId: string) => void;
  switchToBuffer: (bufferId: string) => void;
  updatePaneSize: (paneId: string, size: number) => void;
  openWorkspaceBuffer: OpenWorkspaceBufferFn;
  onOpenCommandPalette: () => void;
  onOpenTerminal: () => void;
  onViewGit: () => void;
  onStartChat: () => void;

  // Split handling
  canSplit: boolean;
  canSplitGrid: boolean;
  canCloseSplit: boolean;
  onSplitRequest: (direction: 'vertical' | 'horizontal' | 'grid') => void;
  onCloseAllSplits: () => void;

  // Chat creation
  onCreateChat?: () => Promise<string | null>;

  // Nested split state (lifted from PaneLayoutManager for split-request coordination)
  nestedSplit: {
    hostPaneId: string;
    nestedPaneId: string;
    direction: 'vertical' | 'horizontal';
  } | null;
  onNestedSplitChange: (
    split: {
      hostPaneId: string;
      nestedPaneId: string;
      direction: 'vertical' | 'horizontal';
    } | null,
  ) => void;

  // Outer layout ref for resize math
  containerRef: RefObject<HTMLDivElement | null>;
}

// ── Sub-components used by PaneLayoutManager ──────────────────────

interface PaneWrapperProps {
  children: ReactNode;
  style?: CSSProperties;
}

export function PaneWrapper({ children, style }: PaneWrapperProps): JSX.Element {
  return (
  <div className="pane-wrapper" style={style}>
    {children}
  </div>
  );
}

interface EditorPaneWrapperProps {
  children: ReactNode;
  isActive?: boolean;
  onClick?: () => void;
}

export function EditorPaneWrapper({ children, isActive, onClick }: EditorPaneWrapperProps): JSX.Element {
  return (
    <div
      className={`editor-pane-wrapper ${isActive ? 'active' : ''}`}
      onClick={onClick}
      tabIndex={isActive ? -1 : 0}
      onFocus={() => isActive && onClick?.()}
    >
      {children}
    </div>
  );
}

interface EditorPaneComponentProps {
  paneId: string;
  isActive?: boolean;
  onClick?: () => void;
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;
  chatProps: ComponentProps<typeof WorkspacePane>['chatProps'];
  reviewProps: ComponentProps<typeof WorkspacePane>['reviewProps'];
  diffState: ComponentProps<typeof WorkspacePane>['diffState'];
  onOpenCommandPalette?: () => void;
  onOpenTerminal?: () => void;
  onViewGit?: () => void;
  onStartChat?: () => void;
}

export function EditorPaneComponent({ paneId, onClick, perChatCache, activeChatId, chatProps, reviewProps, diffState, onOpenCommandPalette, onOpenTerminal, onViewGit, onStartChat }: EditorPaneComponentProps): JSX.Element {
  return (
    <div className="editor-pane-host" onClick={onClick}>
      <WorkspacePane
        paneId={paneId}
        perChatCache={perChatCache}
        activeChatId={activeChatId}
        chatProps={chatProps}
        reviewProps={reviewProps}
        diffState={diffState}
        onOpenCommandPalette={onOpenCommandPalette}
        onOpenTerminal={onOpenTerminal}
        onViewGit={onViewGit}
        onStartChat={onStartChat}
      />
    </div>
  );
}

/**
 * PaneLayoutManager — owns the nested-split state, pane rendering, resize
 * handles, and the split control buttons for each pane tab bar.
 */
function PaneLayoutManager({
  panes,
  paneLayout,
  activePaneId,
  activeBufferId: _activeBufferId,
  buffers: _buffers,
  paneSizes,
  contextPanelRef,
  perChatCache,
  activeChatId,
  messages,
  onSendMessage,
  onQueueMessage,
  onStopProcessing,
  queuedMessagesCount,
  queuedMessages,
  onQueueMessageRemove,
  onQueueMessageEdit,
  onQueueReorder,
  onClearQueuedMessages,
  inputValue,
  onInputChange,
  isProcessing,
  lastError,
  toolExecutions,
  queryProgress,
  currentTodos,
  subagentActivities,
  deepReview,
  reviewError,
  reviewFixResult,
  reviewFixLogs,
  reviewFixSessionID,
  isReviewLoading,
  isReviewFixing,
  onFixFromReview,
  activeDiffPath,
  activeDiff,
  diffMode,
  isDiffLoading,
  diffError,
  onDiffModeChange,
  switchPane,
  switchToBuffer: _switchToBuffer,
  openWorkspaceBuffer,
  canSplit,
  canSplitGrid,
  canCloseSplit,
  onSplitRequest,
  onCloseAllSplits,
  onCreateChat,
  containerRef,
  updatePaneSize,
  nestedSplit,
  onNestedSplitChange: _onNestedSplitChange,
  onOpenCommandPalette,
  onOpenTerminal,
  onViewGit,
  onStartChat,
}: PaneLayoutManagerProps): JSX.Element | null {
  const dragStartSizeRef = useRef<Map<string, number>>(new Map());
  const isPaneDraggingRef = useRef<Set<string>>(new Set());

  const handlePaneResize = useCallback(
    (sizeKey: string, axis: 'horizontal' | 'vertical', invert = false) =>
      (_deltaPixels: number, totalDeltaPixels: number) => {
        if (!containerRef.current) return;

        const containerRect = containerRef.current.getBoundingClientRect();
        const isVertical = axis === 'horizontal';
        const containerSize = isVertical ? containerRect.width : containerRect.height;
        const deltaPercent = ((invert ? -totalDeltaPixels : totalDeltaPixels) / containerSize) * 100;

        if (!isPaneDraggingRef.current.has(sizeKey)) {
          isPaneDraggingRef.current.add(sizeKey);
          dragStartSizeRef.current.set(sizeKey, paneSizes[sizeKey] || 50);
        }
        const sizeAtDragStart = dragStartSizeRef.current.get(sizeKey) ?? 50;
        const newSize = Math.max(10, Math.min(90, sizeAtDragStart + deltaPercent));
        updatePaneSize(sizeKey, newSize);
      },
    [paneSizes, containerRef, updatePaneSize],
  );

  const handlePaneResizeEnd = useCallback(
    (sizeKey: string) => () => {
      isPaneDraggingRef.current.delete(sizeKey);
      dragStartSizeRef.current.delete(sizeKey);
    },
    [],
  );

  // ── Split control buttons ──────────────────────────────────────

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
            onClick={onCloseAllSplits}
            className="pane-control-btn compact"
            title="Close split panes"
            aria-label="Close split panes"
          >
            <X size={13} />
          </button>
        )}
        {paneId === activePaneId && canSplit && (
          <button
            onClick={() => onSplitRequest('vertical')}
            className="pane-control-btn compact"
            title="Split vertically"
            aria-label="Split vertically"
          >
            <Columns2 size={14} />
          </button>
        )}
        {paneId === activePaneId && canSplit && (
          <button
            onClick={() => onSplitRequest('horizontal')}
            className="pane-control-btn compact"
            title="Split horizontally"
            aria-label="Split horizontally"
          >
            <Rows2 size={14} />
          </button>
        )}
        {paneId === activePaneId && canSplitGrid && (
          <button
            onClick={() => onSplitRequest('grid')}
            className="pane-control-btn compact"
            title="Split into 2×2 grid"
            aria-label="Split into 2×2 grid"
          >
            <LayoutGrid size={14} />
          </button>
        )}
      </div>
    );
  };

  // ── Sub-components ─────────────────────────────────────────────

  const renderPaneById = (paneId: string, style?: CSSProperties) => {
    const pane = panes.find((item) => item.id === paneId);
    if (!pane) {
      return null;
    }

    return (
      <PaneWrapper key={pane.id} style={style}>
        <div className="pane-shell">
          <EditorTabs paneId={pane.id} compact actions={renderSplitControls(pane.id)} />
          <EditorPaneWrapper isActive={pane.id === activePaneId} onClick={() => switchPane(pane.id)}>
            <EditorPaneComponent
              paneId={pane.id}
              isActive={pane.id === activePaneId}
              onClick={() => switchPane(pane.id)}
              perChatCache={perChatCache}
              activeChatId={activeChatId}
              chatProps={{
                messages,
                onSendMessage,
                onQueueMessage,
                queuedMessagesCount,
                queuedMessages,
                onQueueMessageRemove,
                onQueueMessageEdit,
                onQueueReorder,
                onClearQueuedMessages,
                inputValue,
                onInputChange,
                isProcessing,
                lastError,
                toolExecutions,
                queryProgress,
                currentTodos,
                subagentActivities,
                onStopProcessing,
                onToolPillClick: (toolId: string) =>
                  (contextPanelRef.current as { highlightTool?: (id: string) => void } | null)?.highlightTool?.(toolId),
              }}
              reviewProps={{
                review: deepReview,
                reviewError,
                reviewFixResult,
                reviewFixLogs,
                reviewFixSessionID,
                isReviewLoading,
                isReviewFixing,
                onFixFromReview,
              }}
              diffState={{
                activeDiffPath,
                activeDiff,
                diffMode,
                isDiffLoading,
                diffError,
                onDiffModeChange,
              }}
              onOpenCommandPalette={onOpenCommandPalette}
              onOpenTerminal={onOpenTerminal}
              onViewGit={onViewGit}
              onStartChat={onStartChat}
            />
          </EditorPaneWrapper>
        </div>
      </PaneWrapper>
    );
  };

  // ── Layout rendering ───────────────────────────────────────────

  const showResizeHandles = panes.length > 1;

  if (panes.length === 0) {
    return null;
  }

  // ── 2×2 Grid layout ────────────────────────────────────────────
  if (paneLayout === 'split-grid' && panes.length === 4) {
    const colSplit = Math.max(10, Math.min(90, paneSizes['grid:col'] ?? 50));
    const rowSplit = Math.max(10, Math.min(90, paneSizes['grid:row'] ?? 50));

    const positionOrder: Record<string, number> = {
      primary: 0,
      secondary: 1,
      tertiary: 2,
      quaternary: 3,
    };
    const sortedPanes = [...panes].sort(
      (a, b) => (positionOrder[a.position ?? ''] ?? 99) - (positionOrder[b.position ?? ''] ?? 99),
    );
    const [topLeft, topRight, bottomLeft, bottomRight] = sortedPanes;

    return (
      <div
        className="grid-pane-layout"
        style={{
          display: 'grid',
          gridTemplateColumns: `${colSplit}% ${100 - colSplit}%`,
          gridTemplateRows: `${rowSplit}% ${100 - rowSplit}%`,
          flex: 1,
          minWidth: 0,
          minHeight: 0,
          position: 'relative',
        }}
      >
        {renderPaneById(topLeft.id)}
        {topRight && renderPaneById(topRight.id)}
        {bottomLeft && renderPaneById(bottomLeft.id)}
        {bottomRight && renderPaneById(bottomRight.id)}

        <ResizeHandle
          direction="horizontal"
          className="grid-resize-handle-col"
          position="absolute"
          style={{ left: `${colSplit}%` }}
          onResize={handlePaneResize('grid:col', 'horizontal')}
          onResizeEnd={handlePaneResizeEnd('grid:col')}
        />
        <ResizeHandle
          direction="vertical"
          className="grid-resize-handle-row"
          position="absolute"
          style={{ top: `${rowSplit}%` }}
          onResize={handlePaneResize('grid:row', 'vertical')}
          onResizeEnd={handlePaneResizeEnd('grid:row')}
        />
      </div>
    );
  }

  // ── Simple layouts (no nested split) ──────────────────────────
  if (panes.length < 3 || !nestedSplit) {
    if (panes.length === 1) {
      return renderPaneById(panes[0].id, toPaneFlex(1));
    }

    if (panes.length === 2) {
      const [firstPane, secondPane] = panes;
      const splitAxis = paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal';
      const firstPaneSize = Math.max(10, Math.min(90, paneSizes[firstPane.id] || 50));
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
        {panes.map((pane, index) => {
          const paneSize = panes.length === 1 ? 100 : paneSizes[pane.id] || 100 / panes.length;
          const isLast = index === panes.length - 1;
          const splitAxis = paneLayout === 'split-horizontal' ? 'vertical' : 'horizontal';

          return (
            <Fragment key={pane.id}>
              {renderPaneById(pane.id, toPaneFlex(paneSize))}
              {showResizeHandles && !isLast && (
                <ResizeHandle
                  direction={splitAxis}
                  onResize={handlePaneResize(pane.id, splitAxis)}
                  onResizeEnd={handlePaneResizeEnd(pane.id)}
                />
              )}
            </Fragment>
          );
        })}
      </>
    );
  }

  // ── Nested split layout (3 panes) ─────────────────────────────
  const hostPane = panes.find((pane) => pane.id === nestedSplit.hostPaneId);
  const nestedPane = panes.find((pane) => pane.id === nestedSplit.nestedPaneId);
  const siblingPane = panes.find((pane) => pane.id !== nestedSplit.hostPaneId && pane.id !== nestedSplit.nestedPaneId);
  if (!hostPane || !nestedPane || !siblingPane) {
    return null;
  }

  const rootDirection = paneLayout === 'split-horizontal' ? 'column' : 'row';
  const nestedDirection = nestedSplit.direction === 'horizontal' ? 'column' : 'row';
  const hostIsFirst =
    panes.findIndex((pane) => pane.id === hostPane.id) < panes.findIndex((pane) => pane.id === siblingPane.id);
  const rootSizeKey = `group:${hostPane.id}`;
  const nestedSizeKey = `nested:${hostPane.id}`;
  const groupSize = paneSizes[rootSizeKey] || 50;
  const nestedSize = paneSizes[nestedSizeKey] || 50;
  const rootHandleDirection = rootDirection === 'row' ? 'horizontal' : 'vertical';
  const nestedHandleDirection = nestedDirection === 'row' ? 'horizontal' : 'vertical';

  const nestedGroup = (
    <div className={`nested-pane-group nested-pane-group-${nestedDirection}`} style={toPaneFlex(groupSize)}>
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

export default PaneLayoutManager;
