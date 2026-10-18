/**
 * The own-mode space shell of `SproutWorkspace` (no `layout`, no host
 * `shellProps`): the standalone app's code space, assembled from the same
 * components the app root assembles it from, around the composition's chat
 * and editor units.
 *
 * - The sidebar — the app's project sidebar (`ProjectNav`) with Files,
 *   Search and Source control drilled into the workspace's own views
 *   (`WorkspaceSidebar`).
 * - The registered space shell — `CodeShell` (header, editor tabs with the
 *   conversation, the context panel with the agent's changes, the status bar)
 *   or `DesignShell` — fed the props `AppContent` feeds it.
 * - The docked terminal — the app's terminal workbench, docked under the
 *   space in flow (`WorkspaceDockedTerminal`).
 * - The app root's editor, tab and terminal commands (`useHotkeyCommandHandler`)
 *   and its view commands, registered so the scoped hotkeys, the menu and
 *   the palette reach them.
 *
 * Git (the Source control section, the status bar's branch, diff and review
 * tabs) runs only when the host declares `capabilities.git`; the terminal
 * only when the host has one (`resolveWorkspaceTerminalBackend`). What has
 * no backend is not rendered.
 */

import type { ChatProps } from '@sprout/ui';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ComponentType } from 'react';
import { closeBuffersMatching } from '../components/closeBuffers';
import type { ContextPanelHandle } from '../components/contextPanel/types';
import type { DesignTab } from '../components/design/DesignView';
import { DesignWorkspaceProvider } from '../components/design/DesignWorkspaceContext';
import { useDesignPresence } from '../components/design/useDesignPresence';
import ErrorBoundary from '../components/ErrorBoundary';
import type { ProjectNavConversations } from '../components/layered/ProjectNav';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeyCommandHandler } from '../hooks/useHotkeyCommandHandler';
import { useHost, useHostCapabilities } from '../host';
import { ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';
import { useCommandHandler } from '../services/commandHandlers';
import type { ToolExecution, ViewType } from '../types/app';
import type { WorkspaceModeId } from '../workspaces/registry';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { useOpenInEditorRequests } from './SproutWorkspaceUnits';
import { useWorkspaceGitState } from './useWorkspaceGitState';
import type { WorkspaceGitState } from './useWorkspaceGitState';
import { useWorkspaceChat, useWorkspaceChatProps } from './WorkspaceChatContext';
import { WorkspaceDockedTerminal, useToggleTerminalCommand } from './WorkspaceDockedTerminal';
import { useWorkspaceEditor } from './WorkspaceEditorContext';
import { WorkspaceSidebar } from './WorkspaceSidebar';
import { resolveWorkspaceTerminalBackend } from './WorkspaceTerminalView';
import './WorkspaceSpaceShell.css';

const NO_GIT: WorkspaceShellProps['git']['gitBranches'] = { current: '', branches: [] };
const NO_SHIP: WorkspaceShellProps['ship'] = { live: {}, availability: { canDeploy: false }, history: [] };

/** The chat's own tab (the buffer manager opens it with the editor). */
const CHAT_TAB = { kind: 'chat' as const, path: '__workspace/chat', title: 'Chat', ext: '.chat', isClosable: false };

/** The app root's commands this shell handles itself (beyond the editor commands below). */
const VIEW_COMMANDS: readonly string[] = ['switch_to_chat', 'switch_to_editor', 'close_editor'];
const NO_COMMANDS: readonly string[] = [];

/** The commands `useHotkeyCommandHandler` turns into editor and tab actions. */
const EDITOR_COMMANDS: readonly string[] = [
  'new_file',
  'toggle_explorer',
  'focus_next_tab',
  'focus_prev_tab',
  'close_all_editors',
  'close_other_editors',
  'save_all_files',
  'save_file',
  'toggle_pin_tab',
  'format_document',
  'undo',
  'redo',
  'editor_cut',
  'editor_copy',
  'editor_paste',
  'editor_find',
  'editor_replace',
  'editor_select_all',
  'editor_toggle_word_wrap',
  'toggle_linked_scroll',
  'toggle_minimap',
  'editor_cycle_whitespace_rendering',
  'editor_toggle_relative_line_numbers',
  'editor_toggle_inlay_hints',
  'editor_toggle_code_lens',
  'editor_toggle_signature_help',
  'editor_cycle_tab_size',
  'editor_zoom_in',
  'editor_zoom_out',
  'editor_reset_zoom',
  'editor_toggle_format_on_save',
  'editor_reveal_in_explorer',
  'editor_copy_relative_path',
  'editor_copy_absolute_path',
  'editor_open_live_preview',
  'editor_toggle_markdown_preview',
];
const isEditorCommand = (id: string): boolean => EDITOR_COMMANDS.includes(id) || /^focus_tab_[1-9]$/.test(id);

export interface ComposedSpaceShellProps {
  Shell: ComponentType<WorkspaceShellProps>;
  spaceId: WorkspaceModeId;
  chatOverrides?: Partial<ChatProps>;
  hasDesignTree?: boolean;
  /** The project's workspace root (local/daemon hosts). */
  workspaceRoot?: string;
  /** The project's display name, the sidebar's title. */
  projectName: string;
}

/**
 * The resolved space's shell with the app's chrome around it. Git state runs
 * only for a host that declares `capabilities.git`.
 */
export function ComposedSpaceShell(props: ComposedSpaceShellProps): JSX.Element {
  const { git } = useHostCapabilities();
  return git ? <GitBackedSpaceShell {...props} /> : <SpaceShellLayout {...props} git={null} />;
}

function GitBackedSpaceShell(props: ComposedSpaceShellProps): JSX.Element {
  const { openFile } = useWorkspaceEditor();
  const onOpenFile = useCallback((path: string) => void openFile(path), [openFile]);
  const git = useWorkspaceGitState({ onOpenFile });
  return <SpaceShellLayout {...props} git={git} />;
}

function SpaceShellLayout({
  Shell,
  spaceId,
  chatOverrides,
  hasDesignTree,
  workspaceRoot,
  projectName,
  git,
}: ComposedSpaceShellProps & { git: WorkspaceGitState | null }): JSX.Element {
  const { state, chat } = useWorkspaceChat();
  const { buffers, activeBufferId, openWorkspaceBuffer } = useEditorManager();
  const { openFile, activeFile } = useWorkspaceEditor();
  const host = useHost();
  const terminalBackend = resolveWorkspaceTerminalBackend(host);

  const [currentView, setCurrentView] = useState<ViewType>('chat');
  const [isTerminalExpanded, setTerminalExpanded] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [designTab, setDesignTab] = useState<DesignTab>('flows');
  const contextPanelRef = useRef<ContextPanelHandle>(null);

  useOpenInEditorRequests(() => setCurrentView('editor'));
  useToggleTerminalCommand(setTerminalExpanded, terminalBackend);

  const lane: 'code' | 'design' = spaceId === 'design' ? 'design' : 'code';
  const currentBuffer = (activeBufferId ? buffers.get(activeBufferId) : undefined) ?? null;
  const { handleActiveChatChange, handleCreateChat, handleDeleteChat, handleDeleteAllChats, handleRenameChat } = chat;

  const handleOpenFile = useCallback(
    (path: string, lineNumber?: number) => {
      openFile(path, { line: lineNumber });
      setCurrentView('editor');
    },
    [openFile],
  );

  const showConversation = useCallback(() => {
    openWorkspaceBuffer(CHAT_TAB);
    setCurrentView('chat');
  }, [openWorkspaceBuffer]);

  // The tool whose inline detail is open in the chat; a chat switch closes it.
  const [activeToolDetailId, setActiveToolDetailId] = useState<string | null>(null);
  useEffect(() => setActiveToolDetailId(null), [state.activeChatId]);
  const onToolDetailToggle = useCallback(
    (toolId: string | null) => setActiveToolDetailId((open) => (open === toolId ? null : toolId)),
    [],
  );
  const activeToolDetail = useMemo<ToolExecution | null>(
    () => (activeToolDetailId ? (state.toolExecutions.find((t) => t.id === activeToolDetailId) ?? null) : null),
    [activeToolDetailId, state.toolExecutions],
  );

  // A turn's change strip opens the agent's change as a diff tab, as in the app.
  const onReviewChange = useCallback(
    (path: string, diff: { stats?: string; diff?: string }) => {
      openWorkspaceBuffer({
        kind: 'diff',
        path: `__workspace/changes/${path}`,
        title: `Agent change: ${path.split('/').pop() || path}`,
        ext: '.diff',
        metadata: {
          sourcePath: path,
          defaultView: 'text',
          modeOptions: ['combined'],
          title: 'Agent Change',
          diff: {
            message: 'Agent session change',
            path,
            has_staged: false,
            has_unstaged: true,
            staged_diff: '',
            unstaged_diff: '',
            diff: (diff.stats ? `${diff.stats}\n\n` : '') + (diff.diff ?? ''),
          },
        },
      });
      setCurrentView('editor');
    },
    [openWorkspaceBuffer],
  );

  // A new untitled buffer: the Save As prompt on first save gives it a real
  // path. Ctrl+N / File▸New File reach this through the editor commands.
  const newFile = useCallback(() => {
    openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/untitled-${Date.now()}`,
      title: 'Untitled',
      ext: '',
      isClosable: true,
    });
    setCurrentView('editor');
  }, [openWorkspaceBuffer]);

  const chatViewOverrides = useMemo(
    () => ({ activeToolDetail, onToolDetailToggle, onReviewChange, review: git?.review, diff: git?.diff }),
    [activeToolDetail, onToolDetailToggle, onReviewChange, git?.review, git?.diff],
  );
  const assembled = useWorkspaceChatProps(chatViewOverrides);
  const chatProps = useMemo(
    () => (chatOverrides ? { ...assembled.chatProps, ...chatOverrides } : assembled.chatProps),
    [assembled.chatProps, chatOverrides],
  );

  useShellCommands({ lane, showConversation, setCurrentView });

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
      reviewProps: assembled.reviewProps,
      diffState: assembled.diffState,
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
      assembled.reviewProps,
      assembled.diffState,
    ],
  );

  const conversations = useMemo<ProjectNavConversations>(
    () => ({
      sessions: state.chatSessions ?? [],
      activeId: state.activeChatId,
      isWorking: (id) => (id === state.activeChatId ? state.isProcessing : !!state.perChatCache?.[id]?.isProcessing),
      inMain: currentBuffer?.kind === 'chat',
      onSelect: (id) => {
        if (lane === 'code') showConversation();
        void handleActiveChatChange(id, lane);
      },
      onOpen: () => {
        if (lane === 'code') showConversation();
      },
      onCreate: () => {
        void handleCreateChat(lane).then((id) => {
          if (id && lane === 'code') showConversation();
        });
      },
      onRename: (id, name) => void handleRenameChat(id, name),
      onDelete: (id) => void handleDeleteChat(id),
      // The daemon keeps its built-in chat.
      canDelete: (id) => id !== 'default',
    }),
    [
      state.chatSessions,
      state.activeChatId,
      state.isProcessing,
      state.perChatCache,
      currentBuffer?.kind,
      lane,
      showConversation,
      handleActiveChatChange,
      handleCreateChat,
      handleRenameChat,
      handleDeleteChat,
    ],
  );

  const designPresence = useDesignPresenceProbe();
  const activeChatName = state.chatSessions?.find((session) => session.id === state.activeChatId)?.name ?? null;
  const resolvedRoot = git?.workspace.workspaceRoot || workspaceRoot || '';

  return (
    <DesignWorkspaceProvider tab={designTab} active={lane === 'design'}>
      <div className="sprout-workspace-shell" data-testid="workspace-space-shell" data-lane={lane}>
        <ErrorBoundary panelName="Sidebar">
          <WorkspaceSidebar
            lane={lane}
            title={projectName}
            collapsed={sidebarCollapsed}
            onCollapsedChange={setSidebarCollapsed}
            conversations={conversations}
            git={git}
            workspaceRoot={resolvedRoot || undefined}
            selectedFile={activeFile?.workspacePath}
            onOpenFile={handleOpenFile}
            onToggleTerminal={
              terminalBackend === 'wasm' ? () => setTerminalExpanded((expanded) => !expanded) : undefined
            }
            designTab={designTab}
            onDesignTabChange={setDesignTab}
          />
        </ErrorBoundary>
        <div className="sprout-workspace-shell__main">
          <Shell
            isMobile={false}
            isTablet={false}
            isSidebarOpen={!sidebarCollapsed}
            isConnected={state.isConnected}
            currentView={currentView}
            onViewChange={setCurrentView}
            onToggleSidebar={() => setSidebarCollapsed((collapsed) => !collapsed)}
            onToggleContextPanel={() => contextPanelRef.current?.togglePanel()}
            supportsLocalTerminal={terminalBackend === 'pty'}
            // The shell's own in-browser terminal is fixed to the viewport;
            // the docked one below takes its place.
            inBrowserTerminal={false}
            previewAvailable={host.capabilities.preview === true}
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
              loading: designPresence.loading,
              present: designPresence.present || !!hasDesignTree,
              treeState: designPresence.treeState === 'none' && hasDesignTree ? 'recognized' : designPresence.treeState,
              frontendLike: designPresence.frontendLike,
              recheck: designPresence.recheck,
              tab: designTab,
              onTabChange: setDesignTab,
              onOpenFile: handleOpenFile,
              agentChatName: lane === 'design' ? activeChatName : null,
              onAgentCreateChat: () => void handleCreateChat('design'),
            }}
            git={{
              gitBranches: git?.workspace.gitBranches ?? NO_GIT,
              gitStatus: git?.workspace.gitStatus ?? null,
              workspaceRoot: resolvedRoot,
              projectName,
            }}
            ship={NO_SHIP}
          />
          {terminalBackend !== 'none' && (
            <ErrorBoundary panelName="Terminal">
              <WorkspaceDockedTerminal
                backend={terminalBackend}
                expanded={isTerminalExpanded}
                onExpandedChange={setTerminalExpanded}
              />
            </ErrorBoundary>
          )}
        </div>
      </div>
      <EditorCommands onNewFile={newFile} />
    </DesignWorkspaceProvider>
  );
}

/**
 * The view commands the app root handles: back to the conversation, to the
 * editor, and closing the active tab.
 */
function useShellCommands({
  lane,
  showConversation,
  setCurrentView,
}: {
  lane: 'code' | 'design';
  showConversation: () => void;
  setCurrentView: (view: ViewType) => void;
}): void {
  const { activeBufferId, closeBuffer } = useEditorManager();
  const enabled = lane === 'code';
  useCommandHandler(enabled ? VIEW_COMMANDS : NO_COMMANDS);
  useEffect(() => {
    if (!enabled) return;
    const handle = (event: Event) => {
      const commandId = (event as CustomEvent<{ commandId?: string }>).detail?.commandId;
      if (commandId === 'switch_to_chat') showConversation();
      else if (commandId === 'switch_to_editor') setCurrentView('editor');
      else if (commandId === 'close_editor' && activeBufferId) void closeBuffer(activeBufferId);
    };
    window.addEventListener('sprout:hotkey', handle);
    return () => window.removeEventListener('sprout:hotkey', handle);
  }, [enabled, showConversation, setCurrentView, activeBufferId, closeBuffer]);
}

/** Fired by the startup repo import once the repository is in the workspace. */
const REPO_IMPORTED_EVENT = 'sprout:repo-imported';

/**
 * The design-presence probe the app root runs (a listing of the workspace
 * root), re-run when an in-browser host's adapter or repository lands, as the
 * first listing may have run before either.
 */
function useDesignPresenceProbe(): ReturnType<typeof useDesignPresence> {
  const presence = useDesignPresence();
  const { recheck } = presence;
  useEffect(() => {
    window.addEventListener(ADAPTER_INSTALLED_EVENT, recheck);
    window.addEventListener(REPO_IMPORTED_EVENT, recheck);
    return () => {
      window.removeEventListener(ADAPTER_INSTALLED_EVENT, recheck);
      window.removeEventListener(REPO_IMPORTED_EVENT, recheck);
    };
  }, [recheck]);
  return presence;
}

/** The app root's editor and tab commands (`useHotkeyCommandHandler`), registered for the embed. */
function EditorCommands({ onNewFile }: { onNewFile: () => void }): null {
  const {
    buffers,
    buffersRef,
    activeBufferId,
    activePaneId,
    closeBuffer,
    saveAllBuffers,
    toggleBufferPin,
    switchToBuffer,
    switchPane,
  } = useEditorManager();
  useCommandHandler(isEditorCommand);
  const onCloseAllBuffers = useCallback(
    () =>
      void closeBuffersMatching(
        () => buffersRef.current,
        closeBuffer,
        () => true,
      ),
    [buffersRef, closeBuffer],
  );
  const onCloseOtherBuffers = useCallback(() => {
    if (!activeBufferId) return;
    void closeBuffersMatching(
      () => buffersRef.current,
      closeBuffer,
      (buffer) => buffer.id !== activeBufferId,
    );
  }, [activeBufferId, buffersRef, closeBuffer]);
  const onSaveAllBuffers = useCallback(() => void saveAllBuffers(), [saveAllBuffers]);
  const onTogglePinTab = useCallback(() => {
    if (activeBufferId) toggleBufferPin(activeBufferId);
  }, [activeBufferId, toggleBufferPin]);
  useHotkeyCommandHandler({
    onCloseAllBuffers,
    onCloseOtherBuffers,
    onSaveAllBuffers,
    onNewFile,
    onTogglePinTab,
    onSwitchToBuffer: switchToBuffer,
    onSwitchPane: switchPane,
    activeBufferId,
    activePaneId,
    buffers,
  });
  return null;
}
