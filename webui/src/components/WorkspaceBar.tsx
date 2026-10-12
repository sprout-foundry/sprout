import { Monitor, Server } from 'lucide-react';
import React, { useEffect, useMemo } from 'react';
import { useHostCapabilities } from '../host';
import { getSSHProxyContext } from '../services/clientSession';
import { refreshWorkspace, useWorkspaceState } from '../services/workspaceStore';
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
  const { workspaceSwitching } = useHostCapabilities();
  const { workspace } = useWorkspaceState();

  useEffect(() => {
    if (!isConnected) return;
    refreshWorkspace().catch((err) => {
      debugLog('[WorkspaceBar] Failed to fetch workspace:', err);
    });
  }, [isConnected]);

  // The last known workspace stays on screen while disconnected; the proxy
  // base set by the local server when serving the SSH proxy page
  // (SPROUT_PROXY_BASE) is the fallback when the API has no ssh_context.
  const bar = useMemo<BarState>(() => {
    const proxyCtx = getSSHProxyContext();
    const isRemote = Boolean(workspace?.ssh_context?.is_remote) || Boolean(proxyCtx);
    const hostAlias =
      (workspace?.ssh_context?.is_remote ? workspace.ssh_context.host_alias : null) ?? proxyCtx?.hostAlias ?? null;
    return { hostAlias, isRemote };
  }, [workspace]);

  // Hide on mobile when the sidebar menu is covering the content
  if (isMobile && isMobileMenuOpen) return null;
  // The hosted editor has no host to name unless it is attached to a remote one.
  if (!workspaceSwitching && !bar.isRemote) return null;

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
