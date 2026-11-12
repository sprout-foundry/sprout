/**
 * The Design mode's shell.
 *
 * Owns the same `<main>` column the Code shell owns, but composes only what
 * Design needs: its surface (DesignSurface) — the mockup and the chat.
 *
 * No menubar, no status bar, no context panel — those are the Code surface's
 * chrome and belong to CodeShell.
 */

import { Menu } from 'lucide-react';
import React from 'react';
import DesignSurface from '../components/design/DesignSurface';
import ErrorBoundary from '../components/ErrorBoundary';
import type { WorkspaceShellProps } from './shell';
import LayeredTopBar from '../components/layered/LayeredTopBar';
import { isLayeredLayout } from '../config/layout';

const DesignShell: React.FC<WorkspaceShellProps> = ({
  isMobile,
  isSidebarOpen,
  supportsLocalTerminal,
  isTerminalExpanded,
  onToggleSidebar,
  chat,
  design,
}) => (
  <main
    className={`main-content design-shell ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${supportsLocalTerminal && isTerminalExpanded ? 'terminal-expanded' : ''}`}
  >
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
      </div>
    )}
    {isLayeredLayout && !isMobile && <LayeredTopBar />}
    <div className="design-shell-body">
      <ErrorBoundary panelName="Design">
        <DesignSurface
          loading={design.loading}
          present={design.present}
          agentChatName={design.agentChatName}
          onAgentCreateChat={design.onAgentCreateChat}
          chatProps={chat.chatProps}
        />
      </ErrorBoundary>
    </div>
  </main>
);

export default DesignShell;
