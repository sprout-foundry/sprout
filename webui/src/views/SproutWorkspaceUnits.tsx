/**
 * The own-mode content of `SproutWorkspace`: what renders inside the chat and
 * editor units the composition mounts. Split from `SproutWorkspace` because
 * both paths read the units' state, which only exists below their providers.
 *
 * - `ArrangedViews` — the `layout` path: the arranged views with their
 *   assembled props.
 * - `ComposedSpaceShell` — the space-shell path without host `shellProps`:
 *   the resolved space's registered shell, fed from the chat and editor
 *   units, so Code and Design have a live chat and editor in own mode.
 *
 * Internal to the composition; not re-exported from the views entry.
 */

import type { ChatProps } from '@sprout/ui';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ComponentType } from 'react';
import type { ContextPanelHandle } from '../components/contextPanel/types';
import type { DesignTab } from '../components/design/DesignView';
import type { PreviewPanelProps } from '../components/PreviewPanel';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHostCapabilities } from '../host';
import type { ViewType } from '../types/app';
import type { WorkspaceModeId } from '../workspaces/registry';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { useSproutWorkspaceChatViewProps, useSproutWorkspaceViewProps } from './SproutWorkspaceChat';
import { ViewsLayout } from './ViewsLayout';
import type { ViewComponent, ViewsArrangement, ViewKind } from './ViewsLayout';
import { useWorkspaceChat } from './WorkspaceChatContext';
import { useWorkspaceEditor } from './WorkspaceEditorContext';
import { WorkspaceEditorView } from './WorkspaceEditorView';

/** The composition's assembled editor, rendered for the `editor` kind. */
const ASSEMBLED_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = { editor: WorkspaceEditorView };

/**
 * The arranged views, rendered inside the chat and editor units so their
 * assembled props are available.
 *
 * The `editor` kind renders the assembled editor view unless the host
 * supplies `viewProps.editor`; then the host gets the bare `Editor` primitive
 * with its own props.
 *
 * An arranged `previewPanel` starts open — the host placed it, and the
 * arrangement has no toggle of its own to open it — and its close affordance
 * closes it. A host that wants to drive it passes `open` (and `onClose`)
 * through `viewProps.previewPanel`, which is merged over these defaults.
 */
export function ArrangedViews({
  arrangement,
  viewProps,
  chatOverrides,
}: {
  arrangement: ViewsArrangement;
  viewProps?: Partial<Record<ViewKind, object>>;
  chatOverrides?: Partial<ChatProps>;
}): JSX.Element {
  const [previewOpen, setPreviewOpen] = useState(true);
  const closePreview = useCallback(() => setPreviewOpen(false), []);
  const previewPanel = useMemo<PreviewPanelProps>(
    () => ({ open: previewOpen, onClose: closePreview }),
    [previewOpen, closePreview],
  );
  const props = useSproutWorkspaceViewProps(viewProps, { chatOverrides, previewPanel });
  useOpenInEditorRequests();
  const components = viewProps?.editor ? undefined : ASSEMBLED_VIEW_COMPONENTS;
  return (
    <ViewsLayout arrangement={arrangement} props={props} components={components} className="sprout-workspace__layout" />
  );
}

/**
 * Open the files the rest of the workspace asks for — file links in the chat
 * and tool output dispatch `sprout:open-in-editor` — in the editor unit. The
 * standalone app handles the same event at its root (where it also switches
 * the view), so only the composed workspace listens here.
 */
export function useOpenInEditorRequests(onOpen?: () => void): void {
  const { openFile } = useWorkspaceEditor();
  const onOpenRef = useRef(onOpen);
  onOpenRef.current = onOpen;
  useEffect(() => {
    const handle = (event: Event) => {
      const detail = (event as CustomEvent<{ path?: string; lineNumber?: number }>).detail;
      if (!detail?.path) return;
      openFile(detail.path, { line: detail.lineNumber });
      onOpenRef.current?.();
    };
    window.addEventListener('sprout:open-in-editor', handle);
    return () => window.removeEventListener('sprout:open-in-editor', handle);
  }, [openFile]);
}

const NO_GIT: WorkspaceShellProps['git']['gitBranches'] = { current: '', branches: [] };
const NO_SHIP: WorkspaceShellProps['ship'] = { live: {}, availability: { canDeploy: false }, history: [] };
const noop = (): void => undefined;

export interface ComposedSpaceShellProps {
  Shell: ComponentType<WorkspaceShellProps>;
  spaceId: WorkspaceModeId;
  chatOverrides?: Partial<ChatProps>;
  hasDesignTree?: boolean;
  /** The project's workspace root, shown by the Code shell's status bar. */
  workspaceRoot?: string;
}

/**
 * The resolved space's shell, fed from the chat and editor units.
 *
 * It assembles the subset of `WorkspaceShellProps` the units own — the chat
 * payload (transcript, sessions, the chat view props), the context panel's
 * activity, the active buffer and the view route — and supplies inert values
 * for the app-level chrome the composition does not own (the project sidebar,
 * the viewport class, git status, deploys). A host that owns that chrome
 * supplies `shellProps` instead and this component is not rendered.
 */
export function ComposedSpaceShell({
  Shell,
  spaceId,
  chatOverrides,
  hasDesignTree,
  workspaceRoot,
}: ComposedSpaceShellProps): JSX.Element {
  const { state, chat } = useWorkspaceChat();
  const { chatProps, reviewProps, diffState } = useSproutWorkspaceChatViewProps(chatOverrides);
  const { buffers, activeBufferId } = useEditorManager();
  const { openFile } = useWorkspaceEditor();
  const { localTerminal } = useHostCapabilities();

  const [currentView, setCurrentView] = useState<ViewType>('chat');
  const [isTerminalExpanded, setTerminalExpanded] = useState(false);
  const [designTab, setDesignTab] = useState<DesignTab>('flows');
  const contextPanelRef = useRef<ContextPanelHandle>(null);

  useOpenInEditorRequests(() => setCurrentView('editor'));

  const lane: 'code' | 'design' = spaceId === 'design' ? 'design' : 'code';
  const currentBuffer = (activeBufferId ? buffers.get(activeBufferId) : undefined) ?? null;
  const { handleActiveChatChange, handleCreateChat, handleDeleteChat, handleDeleteAllChats, handleRenameChat } = chat;

  const handleOpenFile = useCallback(
    (path: string, lineNumber?: number) => {
      openFile(path, { line: lineNumber });
    },
    [openFile],
  );

  const shellChat = useMemo<WorkspaceShellProps['chat']>(
    () => ({
      perChatCache: state.perChatCache,
      activeChatId: state.activeChatId,
      chatSessions: state.chatSessions,
      onActiveChatChange: (id: string) => void handleActiveChatChange(id, lane),
      onCreateChat: () => handleCreateChat(lane),
      onDeleteChat: handleDeleteChat,
      onDeleteAllChats: () => void handleDeleteAllChats(),
      onRenameChat: (id: string, name: string) => void handleRenameChat(id, name),
      chatProps,
      reviewProps,
      diffState,
    }),
    [
      state.perChatCache,
      state.activeChatId,
      state.chatSessions,
      handleActiveChatChange,
      handleCreateChat,
      handleDeleteChat,
      handleDeleteAllChats,
      handleRenameChat,
      lane,
      chatProps,
      reviewProps,
      diffState,
    ],
  );

  const activeChatName = state.chatSessions?.find((session) => session.id === state.activeChatId)?.name ?? null;

  return (
    <Shell
      isMobile={false}
      isTablet={false}
      isSidebarOpen={false}
      isConnected={state.isConnected}
      currentView={currentView}
      onViewChange={setCurrentView}
      onToggleSidebar={noop}
      onToggleContextPanel={() => contextPanelRef.current?.togglePanel()}
      supportsLocalTerminal={localTerminal}
      isTerminalExpanded={isTerminalExpanded}
      onTerminalExpandedChange={setTerminalExpanded}
      showContextSidebar={currentBuffer?.kind === 'chat'}
      contextPanelRef={contextPanelRef}
      toolExecutions={state.toolExecutions}
      logs={state.logs}
      subagentActivities={state.subagentActivities}
      messages={state.messages}
      isProcessing={state.isProcessing}
      lastError={state.lastError}
      queryProgress={state.queryProgress}
      currentBuffer={currentBuffer}
      handleOutlineNavigateToSymbol={(line) =>
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }))
      }
      chat={shellChat}
      design={{
        loading: false,
        present: !!hasDesignTree,
        treeState: hasDesignTree ? 'recognized' : 'none',
        frontendLike: false,
        recheck: noop,
        tab: designTab,
        onTabChange: setDesignTab,
        onOpenFile: handleOpenFile,
        agentChatName: lane === 'design' ? activeChatName : null,
        onAgentCreateChat: () => void handleCreateChat('design'),
      }}
      git={{ gitBranches: NO_GIT, gitStatus: null, workspaceRoot: workspaceRoot ?? '' }}
      ship={NO_SHIP}
    />
  );
}
