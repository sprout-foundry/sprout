import { PanelRightClose } from 'lucide-react';
import React, { useState, useEffect } from 'react';
import { isCloud } from '../config/mode';
import { notificationBus } from '../services/notificationBus';
import MenuBar from './MenuBar';
import WorkspaceBar from './WorkspaceBar';

export interface HeaderBarProps {
  isMobile: boolean;
  isSidebarOpen: boolean;
  showContextSidebar: boolean;
  isConnected: boolean;
  onToggleSidebar: () => void;
  onToggleContextPanel: () => void;
}

const HeaderBar: React.FC<HeaderBarProps> = ({
  isMobile,
  isSidebarOpen,
  showContextSidebar,
  isConnected,
  onToggleSidebar,
  onToggleContextPanel,
}) => {
  const [busy, setBusy] = useState(false);
  const [repoURL, setRepoURL] = useState<string | null>(null);

  useEffect(() => {
    if (!isCloud) return;
    const params = new URLSearchParams(window.location.search);
    const repo = params.get('repo');
    if (repo) setRepoURL(repo);
  }, []);

  const handleStartBuilding = async () => {
    if (busy) return;
    const url = repoURL;
    if (!url) {
      notificationBus.notify(
        'info',
        'Open a repo first',
        'Import a repository into the browser workspace, then start a full workspace.',
        undefined,
        {
          label: 'Show how',
          onClick: () => {
            // Nothing else to wire — user is already in the workspace
            // picker flow; the toast itself surfaces the next step.
          },
        },
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
        if (response.status === 503) {
          notificationBus.notify(
            'warning',
            'Workspaces coming soon',
            'Full workspaces are not yet configured. Explore in the browser for now.',
            undefined,
            {
              label: 'Open browser workspace',
              onClick: () => {
                // User opted into the browser-only flow — keep them on
                // the current page rather than navigating away.
              },
            },
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
        <a href="/" className="header-back-to-dashboard" title="Back to Dashboard">
          ← Dashboard
        </a>
      )}
      {!isCloud && <MenuBar />}
      <div className="header-bar-actions">
        {isCloud && (
          <button
            className="btn btn-sm btn-accent start-building-btn"
            onClick={handleStartBuilding}
            disabled={busy}
            title="Upgrade to a full workspace with real compute, persistent storage, and git push."
          >
            {busy ? 'Starting…' : 'Start Building'}
          </button>
        )}
        {!isMobile && showContextSidebar && (
          <button
            className="header-context-toggle-btn"
            onClick={onToggleContextPanel}
            aria-label="Toggle context panel"
            title="Toggle context panel"
          >
            <PanelRightClose size={14} />
          </button>
        )}
        <WorkspaceBar isConnected={isConnected} isMobile={isMobile} isMobileMenuOpen={isSidebarOpen} />
      </div>
    </div>
  );
};

export default HeaderBar;
