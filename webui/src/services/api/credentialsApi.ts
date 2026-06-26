/**
 * Credentials domain API — adapter-aware credential operations.
 */

import type {
  ProviderCredentialsResponse,
  TestProviderConnectionResponse,
  KeyPoolResponse,
  MCPServerCredentialsResponse,
  UpdateMCPServerCredentialsResponse,
} from './types';

export async function getProviderCredentials(fetchFn: typeof fetch): Promise<ProviderCredentialsResponse> {
  const response = await fetchFn('/api/settings/credentials');
  if (!response.ok) throw new Error(`Failed to fetch provider credentials: HTTP ${response.status}`);
  return response.json();
}

export async function setProviderCredential(fetchFn: typeof fetch, provider: string, value: string): Promise<void> {
  const response = await fetchFn(`/api/settings/credentials/${encodeURIComponent(provider)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ value }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to set credential' }));
    throw new Error(data.message || data.error || 'Failed to set credential');
  }
}

export async function deleteProviderCredential(fetchFn: typeof fetch, provider: string): Promise<void> {
  const response = await fetchFn(`/api/settings/credentials/${encodeURIComponent(provider)}`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to delete credential' }));
    throw new Error(data.message || data.error || 'Failed to delete credential');
  }
}

export async function testProviderConnection(
  fetchFn: typeof fetch,
  provider: string,
): Promise<TestProviderConnectionResponse> {
  const response = await fetchFn(`/api/settings/credentials/${encodeURIComponent(provider)}/test`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Test failed' }));
    throw new Error(data.message || data.error || 'Failed to test provider connection');
  }
  return response.json();
}

export async function getKeyPool(fetchFn: typeof fetch, provider: string): Promise<KeyPoolResponse> {
  const response = await fetchFn(`/api/settings/credentials/pool/${encodeURIComponent(provider)}`);
  if (!response.ok) throw new Error('Failed to fetch key pool');
  return response.json();
}

export async function addKeyToPool(fetchFn: typeof fetch, provider: string, value: string): Promise<void> {
  const response = await fetchFn(`/api/settings/credentials/pool/${encodeURIComponent(provider)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ value }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to add key' }));
    throw new Error(data.message || data.error || 'Failed to add key to pool');
  }
}

export async function removeKeyFromPool(fetchFn: typeof fetch, provider: string, index: number): Promise<void> {
  const response = await fetchFn(`/api/settings/credentials/pool/${encodeURIComponent(provider)}`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ index }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to remove key' }));
    throw new Error(data.message || data.error || 'Failed to remove key from pool');
  }
}

export async function getMCPServerCredentials(
  fetchFn: typeof fetch,
  serverName: string,
): Promise<MCPServerCredentialsResponse> {
  const response = await fetchFn(`/api/settings/mcp/servers/${encodeURIComponent(serverName)}/credentials`);
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateMCPServerCredentials(
  fetchFn: typeof fetch,
  serverName: string,
  credentials: Record<string, string>,
): Promise<UpdateMCPServerCredentialsResponse> {
  const response = await fetchFn(`/api/settings/mcp/servers/${encodeURIComponent(serverName)}/credentials`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(credentials),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function deleteMCPServerCredential(
  fetchFn: typeof fetch,
  serverName: string,
  credentialName: string,
): Promise<void> {
  const response = await fetchFn(
    `/api/settings/mcp/servers/${encodeURIComponent(serverName)}/credentials/${encodeURIComponent(credentialName)}`,
    { method: 'DELETE' },
  );
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
}
