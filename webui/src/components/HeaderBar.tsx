import { PanelRightClose } from 'lucide-react';
import React, { useState, useEffect } from 'react';
import { isCloud } from '../config/mode';
import { useActiveRepoURL } from '../services/activeRepo';
import { notificationBus } from '../services/notificationBus';
import { platformHref } from '../utils/platformUrl';
import MenuBar from './MenuBar';
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
  // Deployments without workspace compute answer 503; hide the button there
  // rather than offer an action that can only fail.
  const [workspacesAvailable, setWorkspacesAvailable] = useState(true);

  useEffect(() => {
    if (!isCloud) return;
    let cancelled = false;
    fetch(`${window.location.origin}/workspace/fly`, { credentials: 'include' })
      .then(async (res) => {
        if (res.status !== 503) return;
        // Only the "not configured" 503 is permanent; a transient outage
        // keeps the button so the user can retry.
        const body = await res.text().catch(() => '');
        if (!cancelled && /not available on this deployment/i.test(body)) setWorkspacesAvailable(false);
      })
      .catch(() => {
        // Network failure says nothing about the deployment; keep the button.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleStartBuilding = async () => {
    if (busy) return;
    const url = repoURL;
    if (!url) {
      notificationBus.notify(
        'info',
        'Add a repository first',
        'A full workspace is created for a repository. Use "Add repository" in the Files panel, then start one.',
      );
      return;
    }

    setBusy(true);
    try {
      const response = await fetch(`${window.location.origin}/workspace/fly`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo_url: url }),
        credentials: 'include',
      });

      if (!response.ok) {
        const errData = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
        const msg = errData.error || `HTTP ${response.status}`;
        if (response.status === 503 && /not available on this deployment/i.test(msg)) {
          setWorkspacesAvailable(false);
          notificationBus.notify(
            'warning',
            'Full workspaces unavailable',
            "This deployment doesn't offer full workspaces. You can keep working in the browser workspace.",
          );
        } else {
          notificationBus.notify('error', 'Failed to start workspace', msg, undefined, {
            label: 'Retry',
            onClick: () => {
              // Defer to a microtask so the dismiss animation can
              // finish before the next fetch kicks off; otherwise the
              // busy state would flicker as the second request races.
              queueMicrotask(() => {
                void handleStartBuilding();
              });
            },
          });
        }
        return;
      }

      const data = await response.json();
      if (data.url && data.session_token) {
        // Follow the same auth exchange pattern as the platform webui.
        const wsUrl = new URL(data.url);
        wsUrl.pathname = '/auth/exchange';
        wsUrl.searchParams.set('token', data.session_token);
        window.location.href = wsUrl.toString();
      } else {
        notificationBus.notify('info', 'Workspace status', data.status || 'Unknown');
      }
    } catch (e) {
      notificationBus.notify(
        'error',
        'Error starting workspace',
        e instanceof Error ? e.message : String(e),
        undefined,
        {
          label: 'Retry',
          onClick: () => {
            queueMicrotask(() => {
              void handleStartBuilding();
            });
          },
        },
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="header-bar">
      {isCloud && (
        <a href={platformHref('/?from=editor')} className="header-back-to-dashboard" title="Back to Dashboard">
          ← Dashboard
        </a>
      )}
      {!isCloud && <MenuBar />}
      <div className="header-bar-actions">
        {/* SP-016 P0.5: avatar menu — cloud mode only, renders nothing in
         * local mode or without a bootstrap identity. */}
        <UserMenu />
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
