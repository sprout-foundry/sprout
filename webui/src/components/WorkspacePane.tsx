import React from 'react';
import { File } from 'lucide-react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import Chat from './Chat';
import EditorPane from './EditorPane';
import DiffWorkspaceTab from './DiffWorkspaceTab';
import ReviewWorkspaceTab from './ReviewWorkspaceTab';
import './WorkspacePane.css';

interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: any;
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
  perChatCache?: Record<string, {
    messages: Message[];
    toolExecutions?: ToolExecution[];
    fileEdits?: Array<{ path: string; action: string; timestamp: Date; linesAdded?: number; linesDeleted?: number }>;
    subagentActivities?: SubagentActivity[];
    currentTodos?: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
    queryProgress?: any;
    lastError?: string | null;
    isProcessing?: boolean;
  }>;
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
    queryProgress?: any;
    currentTodos?: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
    subagentActivities?: SubagentActivity[];
    onToolPillClick?: (toolId: string) => void;
    onStopProcessing?: () => void;
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
    activeDiff: any;
    diffMode: 'combined' | 'staged' | 'unstaged';
    isDiffLoading: boolean;
    diffError: string | null;
    onDiffModeChange: (mode: 'combined' | 'staged' | 'unstaged') => void;
  };
}

const WorkspacePane: React.FC<WorkspacePaneProps> = ({ paneId, perChatCache, activeChatId, chatProps, reviewProps, diffState }) => {
  const { panes, buffers } = useEditorManager();
  const pane = panes.find((item) => item.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  if (!buffer) {
    return (
      <div className="editor-pane empty">
        <div className="no-file-selected">
          <div className="no-file-icon"><File size={32} /></div>
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
            // Disable input for inactive chat tabs since the backend is focused on active chat
            inputValue=""
            onSendMessage={() => {}}
            onQueueMessage={() => {}}
            onStopProcessing={() => {}}
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
          onSendMessage={() => {}}
          onQueueMessage={() => {}}
          onStopProcessing={() => {}}
        />
      );
    }
    case 'diff': {
      const diffPath = buffer.metadata?.sourcePath;
      const isActiveDiff = diffState.activeDiffPath === diffPath;
      return (
        <DiffWorkspaceTab
          path={diffPath || diffState.activeDiffPath || buffer.file.name}
          diff={isActiveDiff ? diffState.activeDiff : (buffer.metadata?.diff || null)}
          diffMode={isActiveDiff ? diffState.diffMode : (buffer.metadata?.diffMode || 'combined')}
          isLoading={diffState.isDiffLoading && isActiveDiff}
          error={isActiveDiff ? diffState.diffError : null}
          onDiffModeChange={diffState.onDiffModeChange}
          title={buffer.metadata?.title}
          modeOptions={buffer.metadata?.modeOptions}
        />
      );
    }
    case 'review':
      return <ReviewWorkspaceTab {...reviewProps} />;
    default:
      return <EditorPane paneId={paneId} />;
  }
};

export default WorkspacePane;
