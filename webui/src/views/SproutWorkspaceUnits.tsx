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
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { ComponentType } from 'react';
import type { ContextPanelHandle } from '../components/contextPanel/types';
import type { DesignTab } from '../components/design/DesignView';
import type { PreviewPanelProps } from '../components/PreviewPanel';
import { useEditorManager } from '../contexts/EditorManagerContext';
import ErrorBoundary from '../components/ErrorBoundary';
import Terminal from '../components/Terminal';
import { useTogglePreviewCommand } from '../hooks/useTogglePreviewCommand';
import { HostContext, useHost } from '../host';
import { useCommandHandler } from '../services/commandHandlers';
import type { ViewType } from '../types/app';
import type { WorkspaceModeId } from '../workspaces/registry';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { useSproutWorkspaceChatViewProps, useSproutWorkspaceViewProps } from './SproutWorkspaceChat';
import { resolveViewsArrangement, SLOT_ORDER, ViewsLayout } from './ViewsLayout';
import type { ViewComponent, ViewsArrangement, ViewKind } from './ViewsLayout';
import { useWorkspaceChat } from './WorkspaceChatContext';
import { useWorkspaceEditor } from './WorkspaceEditorContext';
import { WorkspaceEditorView } from './WorkspaceEditorView';
import { WorkspaceFileTreeView } from './WorkspaceFileTreeView';
import { resolveWorkspaceTerminalBackend } from './WorkspaceTerminalView';

/**
 * The components the composition renders in place of the primitives: the
 * assembled editor for `editor`, and a tree that reloads when the workspace's
 * contents arrive for `fileTree`.
 */
const ASSEMBLED_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = {
  editor: WorkspaceEditorView,
  fileTree: WorkspaceFileTreeView,
};

/** With a host-supplied `viewProps.editor`, the `editor` kind keeps the bare primitive. */
const HOST_EDITOR_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = { fileTree: WorkspaceFileTreeView };

/** The editor shown in the center slot while files are open in an arrangement without one. */
function OnDemandEditorView(): JSX.Element {
  return <WorkspaceEditorView className="sprout-workspace-editor--on-demand" />;
}

const ON_DEMAND_EDITOR_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = {
  ...ASSEMBLED_VIEW_COMPONENTS,
  editor: OnDemandEditorView,
};

/**
 * The arranged views, rendered inside the chat and editor units so their
 * assembled props are available.
 *
 * The `editor` kind renders the assembled editor view unless the host
 * supplies `viewProps.editor`; then the host gets the bare `Editor` primitive
 * with its own props.
 *
 * An arrangement without an `editor` still opens files: selecting one in the
 * tree (or the changes panel, a chat file link, the palette) opens it in the
 * editor unit, and while any file is open the assembled editor is shown at
 * the start of the `center` slot, splitting it with what the host placed
 * there. Closing the last file removes it again. Without this the selection
 * would open a buffer nothing renders, so nothing would load or show.
 *
 * An arranged `previewPanel` is mounted only when the host declares
 * `capabilities.preview` (its backend serves `/api/preview/*`); otherwise it
 * is dropped from the arrangement, so it renders nothing and makes no
 * request. When mounted it starts closed and is opened by the `toggle_preview`
 * command (the command palette, or a host dispatching it); its close
 * affordance closes it. A host that passes `open` through
 * `viewProps.previewPanel` drives it instead (with its `onClose`), and the
 * command is not registered.
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
  const resolved = usePreviewGatedArrangement(arrangement);
  const hostControlled = (viewProps?.previewPanel as Partial<PreviewPanelProps> | undefined)?.open !== undefined;
  const previewArranged = arrangementHas(resolved, 'previewPanel');
  const [previewOpen, setPreviewOpen] = useState(false);
  const closePreview = useCallback(() => setPreviewOpen(false), []);
  useTogglePreviewCommand(setPreviewOpen, previewArranged && !hostControlled);
  const previewPanel = useMemo<PreviewPanelProps>(
    () => ({ open: previewOpen, onClose: closePreview }),
    [previewOpen, closePreview],
  );
  const props = useSproutWorkspaceViewProps(viewProps, { chatOverrides, previewPanel });
  useOpenInEditorRequests();
  const { openFiles } = useWorkspaceEditor();
  const onDemandEditor = !viewProps?.editor && !arrangementHas(resolved, 'editor') && openFiles.length > 0;
  const shown = useMemo(
    () => (onDemandEditor ? { ...resolved, center: ['editor' as const, ...(resolved.center ?? [])] } : resolved),
    [resolved, onDemandEditor],
  );
  let components = ASSEMBLED_VIEW_COMPONENTS;
  if (viewProps?.editor) components = HOST_EDITOR_VIEW_COMPONENTS;
  else if (onDemandEditor) components = ON_DEMAND_EDITOR_VIEW_COMPONENTS;
  return <ViewsLayout arrangement={shown} props={props} components={components} className="sprout-workspace__layout" />;
}

/** Whether any slot of a resolved arrangement places `kind`. */
function arrangementHas(arrangement: ViewsArrangement, kind: ViewKind): boolean {
  return SLOT_ORDER.some((slot) => arrangement[slot]?.includes(kind) ?? false);
}

/**
 * Resolve an arrangement against the default and drop `previewPanel` when the
 * host's backend does not serve the preview lifecycle, so the panel is never
 * mounted there (and never polls `/api/preview/status`). Reads the host
 * through the context directly: an ambient caller may render the layout
 * without a `HostProvider`, which counts as no capability.
 */
export function usePreviewGatedArrangement(arrangement?: ViewsArrangement): ViewsArrangement {
  const previewCapable = useContext(HostContext)?.capabilities.preview === true;
  return useMemo(() => {
    const resolved = resolveViewsArrangement(arrangement);
    if (previewCapable) return resolved;
    const gated: ViewsArrangement = {};
    for (const slot of SLOT_ORDER) {
      gated[slot] = (resolved[slot] ?? []).filter((kind) => kind !== 'previewPanel');
    }
    return gated;
  }, [arrangement, previewCapable]);
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
const TERMINAL_COMMANDS: readonly string[] = ['toggle_terminal'];
const NO_COMMANDS: readonly string[] = [];

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
  const host = useHost();
  const terminalBackend = resolveWorkspaceTerminalBackend(host);

  const [currentView, setCurrentView] = useState<ViewType>('chat');
  const [isTerminalExpanded, setTerminalExpanded] = useState(false);
  const [designTab, setDesignTab] = useState<DesignTab>('flows');
  const contextPanelRef = useRef<ContextPanelHandle>(null);

  useOpenInEditorRequests(() => setCurrentView('editor'));
  useToggleTerminalCommand(setTerminalExpanded, terminalBackend !== 'none');

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
    <>
      <Shell
        isMobile={false}
        isTablet={false}
        isSidebarOpen={false}
        isConnected={state.isConnected}
        currentView={currentView}
        onViewChange={setCurrentView}
        onToggleSidebar={noop}
        onToggleContextPanel={() => contextPanelRef.current?.togglePanel()}
        supportsLocalTerminal={terminalBackend === 'pty'}
        inBrowserTerminal={terminalBackend === 'wasm'}
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
      {/* The PTY terminal for a host whose backend serves one: the docked
          terminal the standalone app renders beside its shell (the shell
          itself renders only the in-browser one). Hidden while collapsed, so
          an embed shows no strip across the host page until it is opened. */}
      {terminalBackend === 'pty' && (
        <ErrorBoundary panelName="Terminal">
          <Terminal isExpanded={isTerminalExpanded} onToggleExpand={setTerminalExpanded} hideWhenCollapsed />
        </ErrorBoundary>
      )}
    </>
  );
}

/**
 * Handle `toggle_terminal` (Ctrl+` and the palette's Toggle Terminal) for the
 * composed shell's terminal, registered so the scoped hotkeys dispatch it
 * from inside the workspace. Nothing registers when the host has no terminal.
 */
function useToggleTerminalCommand(
  setExpanded: (update: (expanded: boolean) => boolean) => void,
  enabled: boolean,
): void {
  useCommandHandler(enabled ? TERMINAL_COMMANDS : NO_COMMANDS);
  useEffect(() => {
    if (!enabled) return;
    const handle = (event: Event) => {
      if ((event as CustomEvent<{ commandId?: string }>).detail?.commandId !== 'toggle_terminal') return;
      setExpanded((expanded) => !expanded);
    };
    window.addEventListener('sprout:hotkey', handle);
    return () => window.removeEventListener('sprout:hotkey', handle);
  }, [enabled, setExpanded]);
}
