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
import React from 'react';
import ContextSidebar from '../components/ContextSidebar';
import EditorWorkspace from '../components/EditorWorkspace';
import ErrorBoundary from '../components/ErrorBoundary';
import HeaderBar from '../components/HeaderBar';
import StatusBar from '../components/StatusBar';
import Terminal from '../components/Terminal';
import type { WorkspaceShellProps } from './shell';
import Chat from '../components/ChatView';
import { isLayeredLayout } from '../config/layout';
import { isCloud } from '../config/mode';
import { useActiveRepoURL } from '../services/activeRepo';
import { githubRepoSlug } from '../utils/platformUrl';

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

  // Layered layout: while the main view holds other work, the active
  // conversation moves into the contextual sidebar.
  const threadContent = isLayeredLayout && !showContextSidebar ? <Chat {...chatProps} /> : undefined;
  // On phones the project sidebar lives in the drawer, so name the project
  // on the toolbar; tapping it opens the drawer.
  const activeRepoSlug = githubRepoSlug(useActiveRepoURL());
  const projectTitle = isCloud
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
              {showContextSidebar && (
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
        <div className="context-panel-container">
          <ContextSidebar
            isMobile={isMobile}
            isTablet={isTablet}
            showContextSidebar={showContextSidebar || !!threadContent}
            threadContent={threadContent}
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
          <Terminal isExpanded={isTerminalExpanded} onToggleExpand={onTerminalExpandedChange} isConnected={false} />
        </ErrorBoundary>
      )}
    </main>
  );
};

export default CodeShell;
