/**
 * The Code mode's shell.
 *
 * Owns the `<main>` column and the Code surface's chrome: the app menubar
 * (HeaderBar), the mobile pane controls, the editor, and the status bar.
 * Everything that reads as "the editor app" lives here.
 *
 * The preview is an editor buffer (`kind: 'preview'`), opened in the
 * rightmost pane so it sits beside the code. A workspace with a mockup shows
 * the mockup (`draw_mockup`'s `design/mockup.html`); otherwise the dev
 * server's output.
 */

import { Menu, MessageSquare, SquareTerminal } from 'lucide-react';
import React, { useCallback, useEffect, useMemo, useRef } from 'react';
import EditorWorkspace from '../components/EditorWorkspace';
import ErrorBoundary from '../components/ErrorBoundary';
import { CreditsChip } from '../components/CreditsChip';
import HeaderBar from '../components/HeaderBar';
import StatusBar from '../components/StatusBar';
import Terminal from '../components/Terminal';
import type { WorkspaceShellProps } from './shell';
import { isLayeredLayout } from '../config/layout';
import { copy } from '../config/copy';
import { TOGGLE_PREVIEW_COMMAND, useTogglePreviewCommand } from '../hooks/useTogglePreviewCommand';
import { useOptionalEditorManager } from '../contexts/EditorManagerContext';
import { useHost, useHostCapabilities } from '../host';
import { useActiveRepoURL } from '../services/activeRepo';
import { useHasCommandHandler } from '../services/commandHandlers';
import { repoSlug } from '../host/repoName';

const PREVIEW_TOGGLE_COMMANDS: readonly string[] = [TOGGLE_PREVIEW_COMMAND];

/** The preview buffer's key — one preview per workspace, reopened not duplicated. */
const PREVIEW_BUFFER_PATH = '__workspace/preview';

const CodeShell: React.FC<WorkspaceShellProps> = ({
  isMobile,
  isTablet,
  isSidebarOpen,
  isConnected,
  currentView,
  onViewChange,
  onToggleSidebar,
  supportsLocalTerminal,
  inBrowserTerminal = true,
  previewAvailable = true,
  isTerminalExpanded,
  onTerminalExpandedChange,
  currentBuffer,
  handleOutlineNavigateToSymbol,
  chat,
  mockup,
  git,
}) => {
  const { agentChanges: supportsAgentChanges } = useHostCapabilities();
  // host.8: the hosted build names the project by its repo slug (the browser
  // IDE has no local workspace root); the hosted transport authenticates
  // against a platform (authMode 'bearer') — the former isCloud branch.
  const hosted = useHost().transport.authMode === 'bearer';
  const {
    perChatCache,
    activeChatId,
    chatSessions,
    onActiveChatChange,
    onCreateChat,
    onCreateChatInWorktree,
    onDeleteChat,
    onDeleteAllChats,
    onRenameChat,
    chatProps,
    reviewProps,
    diffState,
  } = chat;

  // The preview panel: the workspace's mockup when it has one, otherwise the
  // The preview is an editor buffer, not a docked panel: it opens in the
  // rightmost pane so it sits beside the code, and the split-screen model
  // gives it resize, tabs and close for free. A shell mounted outside an
  // editor manager (an embed rendering only the chrome) has no buffers, so
  // the toggle stays inert rather than throwing.
  const editor = useOptionalEditorManager();
  const buffers = editor?.buffers;
  const openWorkspaceBuffer = editor?.openWorkspaceBuffer;
  const closeBuffer = editor?.closeBuffer;
  const splitPane = editor?.splitPane;
  const closePane = editor?.closePane;
  const panes = editor?.panes;
  const activePaneId = editor?.activePaneId;
  const previewBufferId = useMemo(() => {
    if (!buffers) return null;
    for (const [id, b] of buffers) if (b.kind === 'preview') return id;
    return null;
  }, [buffers]);
  // The toggle and the command can both fire before the buffer store
  // re-renders this shell, so the open/close decision reads a ref rather than
  // the rendered value — otherwise a second click in the same batch sees the
  // stale "closed" and opens a duplicate.
  const previewBufferIdRef = useRef<string | null>(previewBufferId);
  previewBufferIdRef.current = previewBufferId;
  // The pane the preview created for itself, if any — closing the preview
  // collapses that split again rather than leaving an empty pane behind.
  const previewPaneIdRef = useRef<string | null>(null);
  const setPreviewOpen = useCallback(
    (update: (open: boolean) => boolean, activate = true) => {
      if (!openWorkspaceBuffer || !closeBuffer) return;
      const current = previewBufferIdRef.current;
      const next = update(current !== null);
      if (next && current === null) {
        // The preview belongs beside the code. With a single pane there is no
        // "beside", so split first and open into the new pane on the next
        // tick: the buffer manager resolves the target pane from refs that
        // only sync on re-render, so opening in the same tick would land the
        // buffer back in the original pane.
        //
        // Phones render peer surfaces (chat / editor), not a split, so a pane
        // created there would never be shown — the buffer renders in the
        // editor surface instead.
        if (!isMobile && splitPane && panes && panes.length < 2 && activePaneId) {
          const newPaneId = splitPane(activePaneId, 'vertical');
          if (newPaneId) {
            previewPaneIdRef.current = newPaneId;
            setTimeout(() => {
              previewBufferIdRef.current = openWorkspaceBuffer({
                kind: 'preview',
                path: PREVIEW_BUFFER_PATH,
                title: 'Preview',
                isPinned: false,
                isClosable: true,
                activate,
                paneId: newPaneId,
              });
            }, 0);
            return;
          }
        }
        previewBufferIdRef.current = openWorkspaceBuffer({
          kind: 'preview',
          path: PREVIEW_BUFFER_PATH,
          title: 'Preview',
          isPinned: false,
          isClosable: true,
          activate,
        });
      } else if (!next && current !== null) {
        previewBufferIdRef.current = null;
        void closeBuffer(current);
        // Collapse the split the preview opened for itself; a split the user
        // already had stays as it is.
        const createdPane = previewPaneIdRef.current;
        previewPaneIdRef.current = null;
        if (createdPane && closePane) closePane(createdPane);
      }
    },
    [openWorkspaceBuffer, closeBuffer, splitPane, closePane, panes, activePaneId, isMobile],
  );
  useTogglePreviewCommand(setPreviewOpen, previewAvailable);
  // Inside an embed the toggle shows only while the command is handled; the
  // standalone app shell handles every command, so its toggle always shows.
  const previewToggleHandled = useHasCommandHandler(PREVIEW_TOGGLE_COMMANDS);
  const showPreview = previewAvailable && previewToggleHandled;
  const showMockup = mockup.present;
  // Open the preview the first time a mockup appears (a draw just landed, or
  // the workspace was opened with one): that is the thing the person is
  // reacting to, so it is placed beside the code rather than summoned. It
  // opens without activating — a draw landing must not pull focus off the
  // code the person is writing from it.
  const openedOnMockup = useRef(false);
  useEffect(() => {
    if (showMockup && showPreview && !openedOnMockup.current) {
      openedOnMockup.current = true;
      // A phone has no split to protect focus in: the preview renders as the
      // editor surface, so it must be the active buffer to be visible.
      setPreviewOpen(() => true, isMobile);
    }
  }, [showMockup, showPreview, setPreviewOpen, isMobile]);
  // On phones the project sidebar lives in the drawer, so name the project
  // on the toolbar; tapping it opens the drawer.
  const activeRepoSlug = repoSlug(useActiveRepoURL());
  const projectTitle = hosted
    ? (activeRepoSlug ?? copy('workspace.none'))
    : (git.workspaceRoot?.split('/').filter(Boolean).pop() ?? '');

  return (
    <main
      className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${supportsLocalTerminal && isTerminalExpanded ? 'terminal-expanded' : ''}`}
    >
      <HeaderBar
        isMobile={isMobile}
        isTablet={isTablet}
        isSidebarOpen={isSidebarOpen}
        isConnected={isConnected}
        onToggleSidebar={onToggleSidebar}
        onTogglePreviewPanel={showPreview ? () => setPreviewOpen((open) => !open) : undefined}
        previewPanelOpen={previewBufferId !== null}
      />
      <div className="main-view-content">
        <div className="editor-view">
          {isMobile && (
            <div className="pane-controls pane-controls-mobile">
              <button
                className="top-mobile-menu-btn"
                onClick={onToggleSidebar}
                aria-label={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
                title={isSidebarOpen ? 'Close sidebar' : 'Open sidebar'}
              >
                <Menu size={16} />
              </button>
              {isLayeredLayout && (
                <button className="top-mobile-project" onClick={onToggleSidebar} title={projectTitle}>
                  {projectTitle}
                </button>
              )}
              {/* The phone header row gives way to the tab bar; the balance moves here. */}
              {isLayeredLayout && hosted && <CreditsChip />}
              {currentView !== 'chat' && (
                <button
                  className="top-mobile-chat-btn"
                  onClick={() => onViewChange('chat')}
                  aria-label="Back to chat"
                  title="Back to chat"
                >
                  <MessageSquare size={16} />
                </button>
              )}
              {supportsLocalTerminal && (
                <button
                  className="top-mobile-terminal-btn"
                  onClick={() => onTerminalExpandedChange(!isTerminalExpanded)}
                  aria-label={isTerminalExpanded ? 'Hide terminal' : 'Show terminal'}
                  title={isTerminalExpanded ? 'Hide terminal' : 'Show terminal'}
                >
                  <SquareTerminal size={16} />
                </button>
              )}
            </div>
          )}
          <ErrorBoundary panelName="Editor">
            <EditorWorkspace
              currentView={currentView}
              perChatCache={perChatCache}
              activeChatId={activeChatId}
              chatSessions={chatSessions}
              onActiveChatChange={onActiveChatChange}
              onCreateChat={onCreateChat}
              onCreateChatInWorktree={onCreateChatInWorktree}
              onDeleteChat={onDeleteChat}
              onDeleteAllChats={onDeleteAllChats}
              onRenameChat={onRenameChat}
              chatProps={chatProps}
              reviewProps={reviewProps}
              diffState={diffState}
              handleOutlineNavigateToSymbol={handleOutlineNavigateToSymbol}
              onViewChange={onViewChange}
            />
          </ErrorBoundary>
        </div>
      </div>
      <StatusBar
        branch={git.gitBranches.current || git.gitStatus?.branch}
        workspacePath={git.workspaceRoot}
        projectName={git.projectName}
        onWorkspaceClick={() => onToggleSidebar()}
        buffer={
          currentBuffer
            ? {
                kind: currentBuffer.kind,
                file: currentBuffer.file,
                content: currentBuffer.content,
                cursorPosition: currentBuffer.cursorPosition,
                languageOverride: currentBuffer.languageOverride,
              }
            : null
        }
      />
      {!supportsLocalTerminal && inBrowserTerminal && (
        <ErrorBoundary panelName="Terminal">
          <Terminal
            isExpanded={isTerminalExpanded}
            onToggleExpand={onTerminalExpandedChange}
            isConnected={false}
            // The in-browser shell is occasional: out of the way until opened
            // (the Terminal sidebar entry, the palette, or Ctrl+`).
            hideWhenCollapsed
          />
        </ErrorBoundary>
      )}
    </main>
  );
};

export default CodeShell;
