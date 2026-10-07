/**
 * The Code mode's shell.
 *
 * Owns the `<main>` column and the Code surface's chrome: the app menubar
 * (HeaderBar), the mobile pane controls, the editor, the right-hand context
 * panel (chat context), and the status bar. Everything that reads as "the
 * editor app" lives here; Design mode gets a different shell (DesignShell)
 * so this chrome never wraps a surface that has no buffer in it.
 */

import { Menu, MessageSquare, PanelRightClose, SquareTerminal } from 'lucide-react';
import React, { useState } from 'react';
import ContextSidebar from '../components/ContextSidebar';
import EditorWorkspace from '../components/EditorWorkspace';
import ErrorBoundary from '../components/ErrorBoundary';
import { CreditsChip } from '../components/CreditsChip';
import HeaderBar from '../components/HeaderBar';
import PreviewPanel from '../components/PreviewPanel';
import StatusBar from '../components/StatusBar';
import Terminal from '../components/Terminal';
import type { WorkspaceShellProps } from './shell';
import { isLayeredLayout } from '../config/layout';
import { useHost, useHostCapabilities } from '../host';
import { useActiveRepoURL } from '../services/activeRepo';
import { repoSlug } from '../host/platformUrl';

const CodeShell: React.FC<WorkspaceShellProps> = ({
  isMobile,
  isTablet,
  isSidebarOpen,
  isConnected,
  currentView,
  onViewChange,
  onToggleSidebar,
  onToggleContextPanel,
  supportsLocalTerminal,
  isTerminalExpanded,
  onTerminalExpandedChange,
  showContextSidebar,
  contextPanelRef,
  toolExecutions,
  logs,
  subagentActivities,
  messages,
  isProcessing,
  lastError,
  queryProgress,
  currentBuffer,
  handleOutlineNavigateToSymbol,
  chat,
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

  // On a local daemon the panel holds the agent's change history.
  const hasContextPanel = supportsAgentChanges;
  // SP-155 §155a: the Code-mode preview panel (the running-app dev server),
  // collapsed by default so it never steals editor space or polls.
  const [previewPanelOpen, setPreviewPanelOpen] = useState(false);
  // On phones the project sidebar lives in the drawer, so name the project
  // on the toolbar; tapping it opens the drawer.
  const activeRepoSlug = repoSlug(useActiveRepoURL());
  const projectTitle = hosted
    ? (activeRepoSlug ?? 'No repository open')
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
        onToggleContextPanel={onToggleContextPanel}
        hasContextPanel={hasContextPanel}
        onTogglePreviewPanel={() => setPreviewPanelOpen((open) => !open)}
        previewPanelOpen={previewPanelOpen}
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
              {showContextSidebar && hasContextPanel && (
                <button
                  className="top-mobile-context-btn"
                  onClick={onToggleContextPanel}
                  aria-label="Toggle context panel"
                  title="Toggle context panel"
                >
                  <PanelRightClose size={16} />
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
        <PreviewPanel open={previewPanelOpen} onClose={() => setPreviewPanelOpen(false)} />
        <div className="context-panel-container">
          <ContextSidebar
            isMobile={isMobile}
            isTablet={isTablet}
            showContextSidebar={showContextSidebar}
            contextPanelRef={contextPanelRef}
            toolExecutions={toolExecutions}
            logs={logs}
            subagentActivities={subagentActivities}
            messages={messages}
            isProcessing={isProcessing}
            lastError={lastError}
            queryProgress={queryProgress}
          />
        </div>
      </div>
      <StatusBar
        branch={git.gitBranches.current || git.gitStatus?.branch}
        workspacePath={git.workspaceRoot}
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
      {!supportsLocalTerminal && (
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
