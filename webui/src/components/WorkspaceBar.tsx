import { useState, useEffect, useRef } from 'react';
import type { FC } from 'react';
import { Monitor, Server } from 'lucide-react';
import { ApiService } from '../services/api';
import { getSSHProxyContext } from '../services/clientSession';
import { notificationBus } from '../services/notificationBus';
import { debugLog } from '../utils/log';

interface WorkspaceBarProps {
  isConnected: boolean;
  /** Hide on mobile when sidebar is open */
  isMobileMenuOpen?: boolean;
  isMobile?: boolean;
}

interface BarState {
  workspacePath: string;
  hostAlias: string | null; // null = local
  isRemote: boolean;
}

const WorkspaceBar: FC<WorkspaceBarProps> = ({ isConnected, isMobileMenuOpen = false, isMobile = false }) => {
  const [bar, setBar] = useState<BarState>({ workspacePath: '', hostAlias: null, isRemote: false });
  const apiService = useRef(ApiService.getInstance());

  useEffect(() => {
    if (!isConnected) {
      setBar({ workspacePath: '', hostAlias: null, isRemote: false });
      return;
    }
    let cancelled = false;
    apiService.current
      .getWorkspace()
      .then((ws) => {
        if (cancelled) return;
        const path = ws.workspace_root || '';
        const homePath = ws.ssh_context?.home_path || '';
        const collapsed = homePath && path.startsWith(homePath) ? `~${path.slice(homePath.length)}` : path;
        // Prefer ssh_context from the API; fall back to the proxy base set by the
        // local server when serving the SSH proxy page (LEDIT_PROXY_BASE).
        const proxyCtx = getSSHProxyContext();
        const isRemote = Boolean(ws.ssh_context?.is_remote) || Boolean(proxyCtx);
        const hostAlias =
          (ws.ssh_context?.is_remote ? ws.ssh_context?.host_alias : null) ?? proxyCtx?.hostAlias ?? null;
        setBar({ workspacePath: collapsed, hostAlias, isRemote });
      })
      .catch((err) => {
        debugLog('[WorkspaceBar] Failed to load workspace path (init):', err);
        notificationBus.notify('warning', 'Workspace', 'Failed to load workspace path: ' + String(err), 5000);
      });
    return () => {
      cancelled = true;
    };
  }, [isConnected]);

  // Subscribe to workspace changes from the workspace switcher
  useEffect(() => {
    const onWorkspaceChange = () => {
      if (!isConnected) return;
      apiService.current
        .getWorkspace()
        .then((ws) => {
          const path = ws.workspace_root || '';
          const homePath = ws.ssh_context?.home_path || '';
          const collapsed = homePath && path.startsWith(homePath) ? `~${path.slice(homePath.length)}` : path;
          const proxyCtx = getSSHProxyContext();
          const isRemote = Boolean(ws.ssh_context?.is_remote) || Boolean(proxyCtx);
          const hostAlias =
            (ws.ssh_context?.is_remote ? ws.ssh_context?.host_alias : null) ?? proxyCtx?.hostAlias ?? null;
          setBar({ workspacePath: collapsed, hostAlias, isRemote });
        })
        .catch((err) => {
          debugLog('[WorkspaceBar] Failed to load workspace path (subscribe):', err);
          notificationBus.notify('warning', 'Workspace', 'Failed to load workspace path: ' + String(err), 5000);
        });
    };
    window.addEventListener('ledit:workspace-changed', onWorkspaceChange);
    return () => window.removeEventListener('ledit:workspace-changed', onWorkspaceChange);
  }, [isConnected]);

  // Hide on mobile when the sidebar menu is covering the content
  if (isMobile && isMobileMenuOpen) return null;

  return (
    <div className={`workspace-bar${bar.isRemote ? ' workspace-bar--remote' : ''}`}>
      <span className="workspace-bar-host">
        {bar.isRemote ? (
          <Server size={11} className="workspace-bar-icon workspace-bar-icon--remote" />
        ) : (
          <Monitor size={11} className="workspace-bar-icon" />
        )}
        <span className="workspace-bar-host-name">{bar.hostAlias ?? 'Local'}</span>
      </span>
      <span className="workspace-bar-sep" aria-hidden="true">
        /
      </span>
      <span className="workspace-bar-path" title={bar.workspacePath}>
        {bar.workspacePath || '—'}
      </span>
    </div>
  );
};

export default WorkspaceBar;
