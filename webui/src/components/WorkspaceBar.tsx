import { Monitor, Server } from 'lucide-react';
import React, { useState, useEffect, useRef } from 'react';
import { ApiService } from '../services/api';
import { getSSHProxyContext } from '../services/clientSession';
import { debugLog } from '../utils/log';

interface WorkspaceBarProps {
  isConnected: boolean;
  /** Hide on mobile when sidebar is open */
  isMobileMenuOpen?: boolean;
  isMobile?: boolean;
}

interface BarState {
  hostAlias: string | null; // null = local
  isRemote: boolean;
}

const WorkspaceBar: React.FC<WorkspaceBarProps> = ({ isConnected, isMobileMenuOpen = false, isMobile = false }) => {
  const [bar, setBar] = useState<BarState>({ hostAlias: null, isRemote: false });
  const apiService = useRef(ApiService.getInstance());

  useEffect(() => {
    if (!isConnected) {
      setBar({ hostAlias: null, isRemote: false });
      return;
    }
    let cancelled = false;
    apiService.current
      .getWorkspace()
      .then((ws) => {
        if (cancelled) return;
        const homePath = ws.ssh_context?.home_path || '';
        const collapsed =
          ws.workspace_root && homePath && ws.workspace_root.startsWith(homePath)
            ? `~${ws.workspace_root.slice(homePath.length)}`
            : ws.workspace_root || '';
        // Prefer ssh_context from the API; fall back to the proxy base set by the
        // local server when serving the SSH proxy page (SPROUT_PROXY_BASE).
        const proxyCtx = getSSHProxyContext();
        const isRemote = Boolean(ws.ssh_context?.is_remote) || Boolean(proxyCtx);
        const hostAlias =
          (ws.ssh_context?.is_remote ? ws.ssh_context?.host_alias : null) ?? proxyCtx?.hostAlias ?? null;
        setBar({ hostAlias, isRemote });
      })
      .catch((err) => {
        debugLog('[WorkspaceBar] Failed to fetch workspace:', err);
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
          const homePath = ws.ssh_context?.home_path || '';
          const collapsed =
            ws.workspace_root && homePath && ws.workspace_root.startsWith(homePath)
              ? `~${ws.workspace_root.slice(homePath.length)}`
              : ws.workspace_root || '';
          const proxyCtx = getSSHProxyContext();
          const isRemote = Boolean(ws.ssh_context?.is_remote) || Boolean(proxyCtx);
          const hostAlias =
            (ws.ssh_context?.is_remote ? ws.ssh_context?.host_alias : null) ?? proxyCtx?.hostAlias ?? null;
          setBar({ hostAlias, isRemote });
        })
        .catch((err) => {
          debugLog('[WorkspaceBar] Failed to refresh workspace:', err);
        });
    };
    window.addEventListener('sprout:workspace-changed', onWorkspaceChange);
    return () => window.removeEventListener('sprout:workspace-changed', onWorkspaceChange);
  }, [isConnected]);

  // Hide on mobile when the sidebar menu is covering the content
  if (isMobile && isMobileMenuOpen) return null;

  return (
    <div className={`workspace-bar${bar.isRemote ? ' workspace-bar--remote' : ''}`}>
      <span className="workspace-bar-host" aria-hidden="true">
        {bar.isRemote ? (
          <Server size={11} className="workspace-bar-icon workspace-bar-icon--remote" />
        ) : (
          <Monitor size={11} className="workspace-bar-icon" />
        )}
        <span className="workspace-bar-host-name">{bar.hostAlias ?? 'Local'}</span>
      </span>
    </div>
  );
};

export default WorkspaceBar;
