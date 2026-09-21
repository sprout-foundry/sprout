/**
 * The Design mode's shell.
 *
 * Owns the same `<main>` column the Code shell owns, but composes only what
 * Design needs: its surface (DesignSurface). The surface itself carries the
 * right column (§6f rework: one Details | Agent column instead of a docked
 * detail pane plus a docked chat panel), fed by the same chat payload this
 * shell already receives.
 *
 * No menubar, no status bar, no context panel — those are the Code surface's
 * chrome and belong to CodeShell.
 */

import { Menu } from 'lucide-react';
import React from 'react';
import DesignSurface from '../components/design/DesignSurface';
import ErrorBoundary from '../components/ErrorBoundary';
import type { WorkspaceShellProps } from './shell';

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
    <div className="design-shell-body">
      <ErrorBoundary panelName="Design">
        <DesignSurface
          loading={design.loading}
          present={design.present}
          onRecheck={design.recheck}
          tab={design.tab}
          onTabChange={design.onTabChange}
          onOpenFile={design.onOpenFile}
          chatProps={chat.chatProps}
        />
      </ErrorBoundary>
    </div>
  </main>
);

export default DesignShell;
