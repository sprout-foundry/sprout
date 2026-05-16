/**
 * SSH/Instances domain API — adapter-aware SSH and instance operations.
 */

import type {
  SSHHostsResponse,
  SSHSessionsResponse,
  SSHOpenResponse,
  SSHLaunchStatus,
  SSHBrowseResponse,
  SSHCloseResponse,
  SelectInstanceResponse,
  InstancesResponse,
} from './types';

// Import SSHWorkspaceOpenError as a value (it's a class)
import { SSHWorkspaceOpenError } from './types';

/** Maximum time (ms) to wait for a ssh-launch-status poll to show completion. */
const SSH_POLL_TIMEOUT_MS = 10 * 60 * 1000; // 10 minutes
const SSH_POLL_INTERVAL_MS = 1_500;

export async function getInstances(fetchFn: typeof fetch): Promise<InstancesResponse> {
  const response = await fetchFn('/api/instances');
  if (!response.ok) throw new Error('Failed to fetch instances');
  return response.json();
}

export async function getSSHHosts(fetchFn: typeof fetch): Promise<SSHHostsResponse> {
  const response = await fetchFn('/api/instances/ssh-hosts');
  if (!response.ok) throw new Error('Failed to fetch SSH hosts');
  return response.json();
}

/**
 * Open SSH workspace with polling for completion.
 * This function polls until the SSH workspace launch completes.
 */
export async function openSSHWorkspace(
  fetchFn: typeof fetch,
  hostAlias: string,
  remoteWorkspacePath?: string,
): Promise<SSHOpenResponse> {
  // Kick off the launch asynchronously — the server returns 202 immediately
  // so the browser never hits a long HTTP timeout.
  const startResponse = await fetchFn('/api/instances/ssh-open', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      host_alias: hostAlias,
      remote_workspace_path: remoteWorkspacePath,
    }),
  });

  if (!startResponse.ok) {
    const errData = (await startResponse.json().catch(() => ({}))) as {
      error?: string;
      message?: string;
      step?: string;
      details?: string;
      log_path?: string;
    };
    throw new SSHWorkspaceOpenError({
      error: errData.error || errData.message || 'Failed to start SSH workspace launch',
      step: errData.step,
      details: errData.details,
      log_path: errData.log_path,
    });
  }

  // Poll ssh-launch-status until the launch completes or times out.
  const deadline = Date.now() + SSH_POLL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    await new Promise<void>((resolve) => window.setTimeout(resolve, SSH_POLL_INTERVAL_MS));

    let status: SSHLaunchStatus;
    try {
      status = await getSSHLaunchStatus(fetchFn, hostAlias, remoteWorkspacePath);
    } catch {
      // Transient network error — keep polling.
      continue;
    }

    if (status.in_progress) {
      continue;
    }

    if (status.last_error) {
      throw new SSHWorkspaceOpenError({
        error: status.last_error,
        step: status.step,
        details: status.details || `SSH launch failed at step: ${status.step}`,
        log_path: status.log_path,
      });
    }

    // Success — proxy_url and proxy_base are populated by the server.
    if (!status.proxy_url) {
      throw new SSHWorkspaceOpenError({
        error: 'SSH workspace launch completed but no proxy URL was returned.',
        step: status.step,
      });
    }

    return {
      message: status.status,
      url: status.proxy_url,
      port: status.local_port,
      proxy_url: status.proxy_url,
      proxy_base: status.proxy_base,
    };
  }

  throw new SSHWorkspaceOpenError({
    error: 'SSH workspace launch timed out. Check SSH connectivity and ~/.config/sprout/workspace.log for details.',
    step: 'launch-timeout',
    details: `Launch did not complete within ${Math.round(SSH_POLL_TIMEOUT_MS / 60_000)} minutes.`,
  });
}

export async function getSSHLaunchStatus(
  fetchFn: typeof fetch,
  hostAlias: string,
  remoteWorkspacePath?: string,
): Promise<SSHLaunchStatus> {
  const params = new URLSearchParams({ host_alias: hostAlias });
  if (remoteWorkspacePath) params.set('remote_workspace_path', remoteWorkspacePath);
  const response = await fetchFn(`/api/instances/ssh-launch-status?${params}`);
  if (!response.ok) throw new Error('Failed to get SSH launch status');
  return response.json();
}

export async function getSSHSessions(fetchFn: typeof fetch): Promise<SSHSessionsResponse> {
  const response = await fetchFn('/api/instances/ssh-sessions');
  if (!response.ok) throw new Error('Failed to fetch SSH sessions');
  return response.json();
}

export async function browseSSHDirectory(
  fetchFn: typeof fetch,
  sessionKey: string,
  path: string,
): Promise<SSHBrowseResponse> {
  const response = await fetchFn(`/api/instances/ssh/${encodeURIComponent(sessionKey)}/browse`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Browse failed' }));
    throw new Error(data.message || data.error || 'Failed to browse SSH directory');
  }
  return response.json();
}

export async function closeSSHSession(fetchFn: typeof fetch, key: string): Promise<SSHCloseResponse> {
  const response = await fetchFn('/api/instances/ssh-close', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Close failed' }));
    throw new Error(data.message || data.error || 'Failed to close SSH session');
  }
  return response.json();
}

/**
 * Browse SSH directory using host_alias (for selecting directory before opening session)
 * This is different from browseSSHDirectory which uses sessionKey.
 */
export async function browseSSHDirectoryByHostAlias(
  fetchFn: typeof fetch,
  hostAlias: string,
  path?: string,
): Promise<SSHBrowseResponse> {
  const response = await fetchFn('/api/instances/ssh-browse', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ host_alias: hostAlias, path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Browse failed' }));
    throw new Error(data.message || data.error || 'Failed to browse SSH directory');
  }
  return response.json();
}

export async function selectInstance(fetchFn: typeof fetch, pid: number): Promise<SelectInstanceResponse> {
  const response = await fetchFn('/api/instances/select', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pid }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Select failed' }));
    throw new Error(data.message || data.error || 'Failed to select instance');
  }
  return response.json();
}
