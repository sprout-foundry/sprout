/**
 * The Design mode's shell.
 *
 * Owns the same `<main>` column the Code shell owns, but composes only what
 * Design needs: its surface (DesignSurface) and, on mobile, the sidebar
 * toggle. No menubar, no status bar, no context panel, no mobile
 * chat/terminal controls — those are the Code surface's chrome and belong to
 * CodeShell. Design has its own assets rail inside the surface.
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
  design,
}) => (
  <main
    className={`main-content ${isMobile && isSidebarOpen ? 'sidebar-open' : ''} ${supportsLocalTerminal && isTerminalExpanded ? 'terminal-expanded' : ''}`}
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
    <ErrorBoundary panelName="Design">
      <DesignSurface
        loading={design.loading}
        present={design.present}
        tab={design.tab}
        onTabChange={design.onTabChange}
        onOpenFile={design.onOpenFile}
      />
    </ErrorBoundary>
  </main>
);

export default DesignShell;
