import { File } from 'lucide-react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import Chat from './Chat';
import DiffSurface from './DiffSurface';
import EditorPane from './EditorPane';
import DiffWorkspaceTab from './DiffWorkspaceTab';
import type { GitDiffResponse } from '../hooks/useGitWorkspace';
import ReviewWorkspaceTab from './ReviewWorkspaceTab';
import WelcomeTab from './WelcomeTab';
import './WorkspacePane.css';

interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: unknown;
  arguments?: string;
  result?: string;
  persona?: string;
  subagentType?: 'single' | 'parallel';
}

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string;
  toolRefs?: Array<{ toolId: string; toolName: string; label: string; parallel?: boolean }>;
}

interface DeepReviewResult {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
  warnings?: string[];
}

interface SubagentActivity {
  id: string;
  toolCallId: string;
  toolName: string;
  phase: 'spawn' | 'output' | 'complete' | 'step';
  message: string;
  timestamp: Date;
  taskId?: string;
  persona?: string;
  isParallel?: boolean;
  provider?: string;
  model?: string;
  taskCount?: number;
  failures?: number;
}

interface WorkspacePaneProps {
  paneId: string;
  perChatCache?: Record<
    string,
    {
      messages: Message[];
      toolExecutions?: ToolExecution[];
      fileEdits?: Array<{
        path: string;
        action: string;
        timestamp: Date;
        linesAdded?: number;
        linesDeleted?: number;
      }>;
      subagentActivities?: SubagentActivity[];
      currentTodos?: Array<{
        id: string;
        content: string;
        status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
      }>;
      queryProgress?: unknown;
      lastError?: string | null;
      isProcessing?: boolean;
      worktreePath?: string;
    }
  >;
  activeChatId?: string | null;
  chatProps: {
    messages: Message[];
    onSendMessage: (message: string) => void;
    onQueueMessage: (message: string) => void;
    queuedMessagesCount: number;
    inputValue: string;
    onInputChange: (value: string) => void;
    isProcessing?: boolean;
    lastError?: string | null;
    toolExecutions?: ToolExecution[];
    queryProgress?: unknown;
    currentTodos?: Array<{
      id: string;
      content: string;
      status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
    }>;
    subagentActivities?: SubagentActivity[];
    onToolPillClick?: (toolId: string) => void;
    onStopProcessing?: () => void;
    queuedMessages?: string[];
    onQueueMessageRemove?: (index: number) => void;
    onQueueMessageEdit?: (index: number, newText: string) => void;
    onQueueReorder?: (fromIndex: number, toIndex: number) => void;
    onClearQueuedMessages?: () => void;
    worktreePath?: string;
    workspaceRoot?: string;
    chatId?: string;
    onWorktreeChange?: (worktreePath: string) => void;
    onCreateChatInWorktree?: () => void;
    // Status bar
    isConnected?: boolean;
    stats?: Record<string, unknown>;
    providerAvailable?: boolean;
    onRequestProviderSetup?: () => void;
  };
  reviewProps: {
    review: DeepReviewResult | null;
    reviewError: string | null;
    reviewFixResult: string | null;
    reviewFixLogs: string[];
    reviewFixSessionID: string | null;
    isReviewLoading: boolean;
    isReviewFixing: boolean;
    onFixFromReview: () => void;
  };
  diffState: {
    activeDiffPath: string | null;
    activeDiff: unknown;
    diffMode: 'combined' | 'staged' | 'unstaged';
    isDiffLoading: boolean;
    diffError: string | null;
    onDiffModeChange: (mode: 'combined' | 'staged' | 'unstaged') => void;
  };
  onOpenCommandPalette?: () => void;
  onOpenTerminal?: () => void;
  onViewGit?: () => void;
  onStartChat?: () => void;
}

function WorkspacePane({
  paneId,
  perChatCache,
  activeChatId,
  chatProps,
  reviewProps,
  diffState,
  onOpenCommandPalette,
  onOpenTerminal,
  onViewGit,
  onStartChat,
}: WorkspacePaneProps): JSX.Element {
  const { panes, buffers, dismissWelcomeBuffer } = useEditorManager();
  const pane = panes.find((item) => item.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  if (!buffer) {
    return (
      <div className="editor-pane empty">
        <div className="no-file-selected">
          <div className="no-file-icon">
            <File size={32} />
          </div>
          <div className="no-file-text">Open a file or tab</div>
        </div>
      </div>
    );
  }

  switch (buffer.kind) {
    case 'chat': {
      const bufferChatId = buffer.metadata?.chatId as string | undefined;

      // If this buffer's chat is the active chat, use the live chatProps
      if (bufferChatId === activeChatId || !bufferChatId) {
        return <Chat {...chatProps} />;
      }

      // Otherwise, look up the cached messages for this specific chat
      const cached = perChatCache?.[bufferChatId];
      if (cached) {
        return (
          <Chat
            {...chatProps}
            messages={cached.messages ?? []}
            toolExecutions={cached.toolExecutions ?? []}
            isProcessing={cached.isProcessing ?? false}
            subagentActivities={cached.subagentActivities ?? []}
            currentTodos={cached.currentTodos ?? []}
            lastError={cached.lastError ?? null}
            queryProgress={cached.queryProgress ?? null}
            worktreePath={cached.worktreePath}
            chatId={chatProps.chatId}
            workspaceRoot={chatProps.workspaceRoot}
            onWorktreeChange={chatProps.onWorktreeChange}
            // Disable input for inactive chat tabs since the backend is focused on active chat
            inputValue=""
            onSendMessage={() => {
              /* noop */
            }}
            onQueueMessage={() => {
              /* noop */
            }}
            onStopProcessing={() => {
              /* noop */
            }}
          />
        );
      }

      // No cache available — show empty state
      return (
        <Chat
          {...chatProps}
          messages={[]}
          toolExecutions={[]}
          isProcessing={false}
          subagentActivities={[]}
          currentTodos={[]}
          lastError={null}
          queryProgress={null}
          inputValue=""
          chatId={chatProps.chatId}
          workspaceRoot={chatProps.workspaceRoot}
          onWorktreeChange={chatProps.onWorktreeChange}
          onSendMessage={() => {
            /* noop */
          }}
          onQueueMessage={() => {
            /* noop */
          }}
          onStopProcessing={() => {
            /* noop */
          }}
        />
      );
    }
    case 'diff': {
      const metadata = buffer.metadata || {};

      // Commit diff opened from CommitDetailPanel — render with DiffSurface
      if (metadata.diffContent) {
        return (
          <div className="workspace-tab workspace-diff-tab">
            <div className="workspace-tab-header">
              <div>
                <div className="workspace-tab-eyebrow">Commit Diff</div>
                <h2>{String(buffer.metadata?.title || buffer.file.name)}</h2>
              </div>
            </div>
            <DiffSurface diffText={String(metadata.diffContent)} />
          </div>
        );
      }

      // Working-tree diff (existing behavior)
      const diffPath = buffer.metadata?.sourcePath as string | undefined;
      const isActiveDiff = diffState.activeDiffPath === diffPath;
      return (
        <DiffWorkspaceTab
          path={diffPath || diffState.activeDiffPath || buffer.file.name}
          diff={(isActiveDiff ? diffState.activeDiff : buffer.metadata?.diff || null) as GitDiffResponse | null}
          diffMode={
            isActiveDiff
              ? diffState.diffMode
              : (((buffer.metadata?.diffMode as string) || 'combined') as 'staged' | 'combined' | 'unstaged')
          }
          isLoading={diffState.isDiffLoading && isActiveDiff}
          error={isActiveDiff ? diffState.diffError : null}
          onDiffModeChange={diffState.onDiffModeChange}
          title={buffer.metadata?.title as string | undefined}
          modeOptions={buffer.metadata?.modeOptions as ('staged' | 'combined' | 'unstaged')[] | undefined}
        />
      );
    }
    case 'review':
      return <ReviewWorkspaceTab {...reviewProps} />;
    case 'welcome':
      return (
        <WelcomeTab
          onDismiss={dismissWelcomeBuffer}
          onOpenCommandPalette={onOpenCommandPalette}
          onOpenTerminal={onOpenTerminal}
          onViewGit={onViewGit}
          onStartChat={onStartChat}
        />
      );
    default:
      return <EditorPane paneId={paneId} onOpenCommandPalette={onOpenCommandPalette} />;
  }
}

export default WorkspacePane;
