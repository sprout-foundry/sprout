import { File } from 'lucide-react';
import type { ComponentProps } from 'react';
import React from 'react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import type { PerChatState } from '../types/app';
import Chat from './ChatView';
import CompareTab from './CompareTab';
import DiffWorkspaceTab from './DiffWorkspaceTab';
import EditorPane from './EditorPane';
import ReviewWorkspaceTab from './ReviewWorkspaceTab';
import './WorkspacePane.css';

// Re-export PerChatState for downstream consumers
export type { PerChatState };

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

interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}

interface WorkspacePaneProps {
  paneId: string;
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;
  onOpenCommandPalette?: () => void;
  onOpenTerminal?: () => void;
  onViewGit?: () => void;
  onStartChat?: () => void;
  chatProps: ComponentProps<typeof Chat>;
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
    activeDiff: GitDiffResponse | null;
    diffMode: 'combined' | 'staged' | 'unstaged';
    isDiffLoading: boolean;
    diffError: string | null;
    onDiffModeChange: (mode: 'combined' | 'staged' | 'unstaged') => void;
  };
}

const WorkspacePane: React.FC<WorkspacePaneProps> = React.memo(
  ({ paneId, chatProps, reviewProps, diffState, perChatCache, activeChatId }) => {
    const { panes, buffers } = useEditorManager();
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
        // When multiple chat buffers exist across panes, each buffer has its own
        // chatId in metadata.  The global chatProps always reflects the *active*
        // chat's state, so for inactive panes we must look up the per-chat cache
        // to render the correct conversation.
        const bufferChatId = buffer.metadata?.chatId as string | undefined;
        const isActiveChat = !bufferChatId || bufferChatId === activeChatId;

        if (isActiveChat || !perChatCache || !bufferChatId) {
          return <Chat {...chatProps} />;
        }

        // Inactive chat pane — use cached state for this specific chat
        const cached = perChatCache[bufferChatId];
        const inactiveChatProps = {
          ...chatProps,
          messages: cached?.messages ?? [],
          toolExecutions: cached?.toolExecutions ?? [],
          subagentActivities: cached?.subagentActivities ?? [],
          currentTodos: cached?.currentTodos ?? [],
          queryProgress: cached?.queryProgress ?? null,
          lastError: cached?.lastError ?? null,
          isProcessing: cached?.isProcessing ?? false,
          inputValue: '',
          queuedMessagesCount: 0,
          queuedMessages: [],
          chatId: bufferChatId,
        };

        return <Chat {...inactiveChatProps} />;
      }
      case 'diff': {
        const diffPath = buffer.metadata?.sourcePath as string | undefined;
        const isActiveDiff = diffState.activeDiffPath === diffPath;
        return (
          <DiffWorkspaceTab
            path={diffPath || diffState.activeDiffPath || buffer.file.name}
            diff={isActiveDiff ? diffState.activeDiff : (buffer.metadata?.diff as GitDiffResponse | null) || null}
            diffMode={
              isActiveDiff
                ? diffState.diffMode
                : (buffer.metadata?.diffMode as 'combined' | 'staged' | 'unstaged' | undefined) || 'combined'
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
      case 'compare': {
        const {
          originalContent = '',
          modifiedContent = '',
          fileName = 'Untitled',
          aLabel,
          bLabel,
          title,
        } = buffer.metadata ?? {};
        return (
          <CompareTab
            fileName={fileName as string}
            originalContent={originalContent as string}
            modifiedContent={modifiedContent as string}
            aLabel={aLabel as string | undefined}
            bLabel={bLabel as string | undefined}
            title={title as string | undefined}
          />
        );
      }
      default:
        return <EditorPane paneId={paneId} />;
    }
  },
);

WorkspacePane.displayName = 'WorkspacePane';

export default WorkspacePane;
