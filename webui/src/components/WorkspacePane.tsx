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

interface WorkspacePaneProps {
  paneId: string;
  chatProps: {
    messages: Message[];
    onSendMessage: (message: string) => void;
    onQueueMessage: (message: string) => void;
    queuedMessages: string[];
    onRemoveQueuedMessage: (index: number) => void;
    onEditQueuedMessage: (index: number, newText: string) => void;
    onReorderQueuedMessage: (fromIndex: number, toIndex: number) => void;
    onClearQueuedMessages: () => void;
    inputValue: string;
    onInputChange: (value: string) => void;
    isProcessing?: boolean;
    lastError?: string | null;
    toolExecutions?: ToolExecution[];
    queryProgress?: any;
    currentTodos?: Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }>;
    onToolPillClick?: (toolId: string) => void;
    onStopProcessing?: () => void;
    subagentActivities?: Array<{
      id: string;
      toolCallId: string;
      toolName: string;
      phase: 'spawn' | 'output' | 'complete';
      message: string;
      timestamp: Date;
      taskId?: string;
      persona?: string;
      isParallel?: boolean;
      provider?: string;
      model?: string;
      taskCount?: number;
      failures?: number;
    }>;
  };
  reviewProps: {
    review: DeepReviewResult | null;
    reviewError: string | null;
    reviewFixResult: string | null;
    reviewFixLogs: string[];
    reviewFixSessionID: string | null;
    isReviewLoading: boolean;
    isReviewFixing: boolean;
    onFixFromReview: (options?: { fixPrompt?: string; selectedItems?: string[] }) => void;
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

const WorkspacePane: React.FC<WorkspacePaneProps> = ({ paneId, chatProps, reviewProps, diffState }) => {
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
    case 'chat':
      return <Chat {...chatProps} />;
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
