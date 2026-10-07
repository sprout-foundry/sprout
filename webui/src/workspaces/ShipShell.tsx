/**
 * The Ship mode's shell.
 *
 * Owns the same `<main>` column the Code and Design shells own, but composes
 * only what Ship needs: its surface. No menubar, no status bar, no context
 * panel — those are other modes' chrome. The shell carries the mobile sidebar
 * toggle so a phone user can still reach the rail, and nothing else.
 *
 * The shell reads exactly one slice of the shell props: `ship`. That keeps the
 * mode additive — adding Ship does not touch how Code or Design render.
 */

import { Menu } from 'lucide-react';
import React from 'react';
import ErrorBoundary from '../components/ErrorBoundary';
import ShipSurface from '../components/ship/ShipSurface';
import type { WorkspaceShellProps } from './shell';

const ShipShell: React.FC<WorkspaceShellProps> = ({ isMobile, isSidebarOpen, onToggleSidebar, ship }) => (
  <main className={`main-content ship-shell ${isMobile && isSidebarOpen ? 'sidebar-open' : ''}`}>
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
    <div className="ship-shell-body">
      <ErrorBoundary panelName="Ship">
        <ShipSurface payload={ship} />
      </ErrorBoundary>
    </div>
  </main>
);

export default ShipShell;
