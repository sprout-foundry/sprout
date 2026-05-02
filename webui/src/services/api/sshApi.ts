/**
 * SSH/Instances domain API — adapter-aware SSH and instance operations.
 */

import {
  SproutInstance,
  SSHHostEntry,
  SSHHostsResponse,
  SSHSessionEntry,
  SSHSessionsResponse,
  SSHOpenResponse,
  SSHLaunchStatus,
  SSHBrowseEntry,
  SSHBrowseResponse,
  SSHCloseResponse,
  SelectInstanceResponse,
  InstancesResponse,
} from './types';

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

export async function openSSHWorkspace(fetchFn: typeof fetch, hostAlias: string, remoteWorkspacePath?: string): Promise<SSHOpenResponse> {
  const response = await fetchFn('/api/instances/ssh/open', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ host_alias: hostAlias, remote_workspace_path: remoteWorkspacePath }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(data.error || data.message || 'Failed to open SSH workspace');
  }
  return response.json();
}

export async function getSSHLaunchStatus(fetchFn: typeof fetch, hostAlias: string, remoteWorkspacePath?: string): Promise<SSHLaunchStatus> {
  const params = new URLSearchParams({ host_alias: hostAlias });
  if (remoteWorkspacePath) params.set('remote_workspace_path', remoteWorkspacePath);
  const response = await fetchFn(`/api/instances/ssh/launch-status?${params}`);
  if (!response.ok) throw new Error('Failed to get SSH launch status');
  return response.json();
}

export async function getSSHSessions(fetchFn: typeof fetch): Promise<SSHSessionsResponse> {
  const response = await fetchFn('/api/instances/ssh/sessions');
  if (!response.ok) throw new Error('Failed to fetch SSH sessions');
  return response.json();
}

export async function browseSSHDirectory(fetchFn: typeof fetch, sessionKey: string, path: string): Promise<SSHBrowseResponse> {
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
  const response = await fetchFn(`/api/instances/ssh/sessions/${encodeURIComponent(key)}`, { method: 'DELETE' });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Close failed' }));
    throw new Error(data.message || data.error || 'Failed to close SSH session');
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
