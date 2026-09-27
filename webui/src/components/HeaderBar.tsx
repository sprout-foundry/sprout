import { PanelRightClose } from 'lucide-react';
import React, { useState } from 'react';
import { isCloud } from '../config/mode';
import { isLayeredLayout, OPEN_COMMAND_PALETTE_EVENT } from '../config/layout';
import { useActiveRepoURL } from '../services/activeRepo';
import { startFullWorkspace, useFullWorkspacesAvailable } from '../services/fullWorkspace';
import { notificationBus } from '../services/notificationBus';
import { githubRepoSlug, platformHref, repoHubPath } from '../utils/platformUrl';
import MenuBar from './MenuBar';
import { CreditsChip } from './CreditsChip';
import { UsageChip } from './UsageChip';
import { UserMenu } from './UserMenu';
import WorkspaceBar from './WorkspaceBar';

export interface HeaderBarProps {
  isMobile: boolean;
  isTablet?: boolean;
  isSidebarOpen: boolean;
  isConnected: boolean;
  onToggleSidebar: () => void;
  onToggleContextPanel: () => void;
}

const HeaderBar: React.FC<HeaderBarProps> = ({
  isMobile,
  isTablet = false,
  isSidebarOpen,
  isConnected,
  onToggleSidebar,
  onToggleContextPanel,
}) => {
  const [busy, setBusy] = useState(false);
  const repoURL = useActiveRepoURL() ?? null;
  // The back-link returns to the hub page of the repo being edited.
  const repoSlug = githubRepoSlug(repoURL);
  // Hidden on deployments without workspace compute rather than offering an
  // action that can only fail.
  const workspacesAvailable = useFullWorkspacesAvailable(isCloud);

  const retry = () => {
    // Defer so the toast's dismiss finishes before the next request starts.
    queueMicrotask(() => {
      void handleStartBuilding();
    });
  };

  const handleStartBuilding = async () => {
    if (busy) return;
    if (!repoURL) {
      notificationBus.notify(
        'info',
        'Add a repository first',
        'A full workspace is created for a repository. Use "Add repository" in the Files panel, then start one.',
      );
      return;
    }

    setBusy(true);
    try {
      const result = await startFullWorkspace(repoURL);
      if (result.kind === 'unavailable') {
        notificationBus.notify(
          'warning',
          'Full workspaces unavailable',
          "This deployment doesn't offer full workspaces. You can keep working in the browser workspace.",
        );
      } else if (result.kind === 'error') {
        notificationBus.notify('error', 'Failed to start workspace', result.message, undefined, {
          label: 'Retry',
          onClick: retry,
        });
      } else if (result.kind === 'status') {
        notificationBus.notify('info', 'Workspace status', result.status);
      }
    } catch (e) {
      notificationBus.notify(
        'error',
        'Error starting workspace',
        e instanceof Error ? e.message : String(e),
        undefined,
        { label: 'Retry', onClick: retry },
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="header-bar">
      {isLayeredLayout && (
        <button
          type="button"
          className="header-search-btn"
          onClick={() => window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT))}
          data-testid="header-search"
        >
          <span>Search files, symbols, commands…</span>
          <kbd>⌘K</kbd>
        </button>
      )}
      {isCloud && !isLayeredLayout && (
        <a
          href={platformHref(repoHubPath(repoURL))}
          className="header-back-to-dashboard"
          title={repoSlug ? `Back to ${repoSlug} on the dashboard` : 'Back to Dashboard'}
        >
          ← <span className="header-back-to-dashboard-label">{repoSlug ?? 'Dashboard'}</span>
        </a>
      )}
      {!isCloud && <MenuBar />}
      <div className="header-bar-actions">
        {isCloud && workspacesAvailable && (
          <button
            className="btn btn-sm btn-accent start-building-btn"
            onClick={handleStartBuilding}
            disabled={busy}
            title="Upgrade to a full workspace with real compute, persistent storage, and git push."
          >
            {busy ? 'Starting…' : 'Start Building'}
          </button>
        )}
        <CreditsChip />
        {/* SP-016 P0.5: avatar menu — cloud mode only, renders nothing in
         * local mode or without a bootstrap identity. */}
        {!isLayeredLayout && <UserMenu />}
        {!isMobile && (
          <button
            className="header-context-toggle-btn"
            onClick={onToggleContextPanel}
            aria-label="Toggle context panel"
            title="Toggle context panel"
          >
            <PanelRightClose size={14} />
          </button>
        )}
        {/* SP-016 P0.6: ambient usage signal — cloud mode only, renders
         * nothing when the bootstrap carried no badge values. */}
        <UsageChip />
        <WorkspaceBar isConnected={isConnected} isMobile={isMobile} isMobileMenuOpen={isSidebarOpen} />
      </div>
    </div>
  );
};

export default HeaderBar;
