import { MonitorPlay } from 'lucide-react';
import React, { useState } from 'react';
import { isLayeredLayout } from '../config/layout';
import { LayeredSearchButton } from './layered/LayeredTopBar';
import { useHost } from '../host';
import { useActiveRepoURL } from '../services/activeRepo';
import { startDevSpace, useDevSpacesAvailable } from '../services/cloudDevSpace';
import { notificationBus } from '../services/notificationBus';
import HostNotificationCount from '../host/HostNotificationCount';
import { githubRepoSlug } from '../host/repoName';
import MenuBar from './MenuBar';
import { CreditsChip } from './CreditsChip';
import { UsageChip } from './UsageChip';
import { UserMenu } from './UserMenu';
import DevSpaceBar from './ProjectFolderBar';

export interface HeaderBarProps {
  isMobile: boolean;
  isTablet?: boolean;
  isSidebarOpen: boolean;
  isConnected: boolean;
  onToggleSidebar: () => void;
  /** Toggle the preview panel. Rendered when provided. */
  onTogglePreviewPanel?: () => void;
  /** Whether the preview panel is currently open (drives the toggle's state). */
  previewPanelOpen?: boolean;
}

const HeaderBar: React.FC<HeaderBarProps> = ({
  isMobile,
  isTablet = false,
  isSidebarOpen,
  isConnected,
  onToggleSidebar,
  onTogglePreviewPanel,
  previewPanelOpen = false,
}) => {
  const [busy, setBusy] = useState(false);
  // host.8: the hosted build (authMode 'bearer') shows the platform's header
  // affordances (back-to-dashboard, Start Building, credits); the local build
  // shows its own MenuBar. This replaces the former isCloud branch.
  const host = useHost();
  const hosted = host.transport.authMode === 'bearer';
  const repoURL = useActiveRepoURL() ?? null;
  // The back-link returns to the hub page of the repo being edited. The route
  // is host data: Sprout asks for the repo's hub destination by (generic) nav
  // intent and the host resolves it to its own page URL. With no repo open it
  // falls back to the host's account/dashboard page.
  const repoSlug = githubRepoSlug(repoURL);
  const backPath =
    (repoSlug ? host.navigation.intentPath?.({ type: 'nav', id: 'repos', detail: repoSlug }) : null) ??
    host.navigation.intentPath?.({ type: 'account' }) ??
    null;
  const backHref = backPath ? (host.navigation.platformPagePath?.(backPath) ?? backPath) : undefined;
  // Hidden on deployments without dev space compute rather than offering an
  // action that can only fail, when the agent already runs on a dev space's
  // daemon (that dev space is the dev space the button would start, and
  // the host owns its lifecycle), and when the host offers the move itself
  // (capabilities.containerUpgrade off).
  const onDaemon = host.transport.agent?.kind === 'daemon';
  const offersUpgrade = hosted && !onDaemon && host.capabilities.containerUpgrade === true;
  const devSpacesAvailable = useDevSpacesAvailable(offersUpgrade);

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
        'A dev space is created for a repository. Use "Add repository" in the Files panel, then start one.',
      );
      return;
    }

    setBusy(true);
    try {
      const result = await startDevSpace(repoURL);
      if (result.kind === 'unavailable') {
        notificationBus.notify(
          'warning',
          'Dev spaces unavailable',
          "This deployment doesn't offer dev spaces. You can keep working in the browser.",
        );
      } else if (result.kind === 'error') {
        notificationBus.notify('error', 'Failed to start the dev space', result.message, undefined, {
          label: 'Retry',
          onClick: retry,
        });
      } else if (result.kind === 'status') {
        notificationBus.notify('info', 'Dev space status', result.status);
      }
    } catch (e) {
      notificationBus.notify(
        'error',
        'Error starting the dev space',
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
      {/* Phones in the hosted editor search from the tab bar. */}
      {isLayeredLayout && !(hosted && isMobile) && <LayeredSearchButton />}
      {hosted && !isLayeredLayout && (
        <a
          href={backHref}
          className="header-back-to-dashboard"
          title={repoSlug ? `Back to ${repoSlug} on the dashboard` : 'Back to Dashboard'}
        >
          ← <span className="header-back-to-dashboard-label">{repoSlug ?? 'Dashboard'}</span>
        </a>
      )}
      {!hosted && <MenuBar />}
      <div className="header-bar-actions">
        {offersUpgrade && devSpacesAvailable && (
          <button
            className="btn btn-sm btn-accent start-building-btn"
            onClick={handleStartBuilding}
            disabled={busy}
            title="Upgrade to a dev space with real compute, persistent storage, and git push."
          >
            {busy ? 'Starting…' : 'Start Building'}
          </button>
        )}
        {!(isLayeredLayout && hosted && isMobile) && <CreditsChip />}
        {/* host.7: the host's unread-notification count, when it supplies one. */}
        {!isLayeredLayout && <HostNotificationCount />}
        {/* SP-016 P0.5: avatar menu — cloud mode only, renders nothing in
         * local mode or without a bootstrap identity. */}
        {!isLayeredLayout && <UserMenu />}
        {/* Toggle the Code-mode preview panel (the running-app
         * dev server). Rendered only when the host wires it in (Code mode). */}
        {!isMobile && onTogglePreviewPanel && (
          <button
            className="header-context-toggle-btn"
            data-testid="preview-panel-toggle"
            onClick={onTogglePreviewPanel}
            aria-label="Toggle preview panel"
            aria-pressed={previewPanelOpen}
            title="Toggle preview panel"
          >
            <MonitorPlay size={14} />
          </button>
        )}
        {/* SP-016 P0.6: ambient usage signal — cloud mode only, renders
         * nothing when the bootstrap carried no badge values. */}
        <UsageChip />
        <DevSpaceBar isConnected={isConnected} isMobile={isMobile} isMobileMenuOpen={isSidebarOpen} />
      </div>
    </div>
  );
};

export default HeaderBar;
