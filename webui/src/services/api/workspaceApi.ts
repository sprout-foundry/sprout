/**
 * Workspace domain API — adapter-aware workspace operations.
 */

import type { WorkspaceResponse } from './types';

function toWorkspaceResponse(data: Record<string, unknown>): WorkspaceResponse {
  return {
    daemon_root: String(data.daemon_root ?? ''),
    workspace_root: String(data.workspace_root ?? ''),
    ...(data.ssh_context != null && typeof data.ssh_context === 'object'
      ? { ssh_context: data.ssh_context as WorkspaceResponse['ssh_context'] }
      : {}),
  };
}

export async function getWorkspace(fetchFn: typeof fetch): Promise<WorkspaceResponse> {
  const response = await fetchFn('/api/workspace');
  const text = await response.text();

  const parseWorkspacePayload = (t: string): Record<string, unknown> => {
    const trimmed = t.trim();
    if (!trimmed) return {} as Record<string, unknown>;
    try {
      return JSON.parse(trimmed);
    } catch {
      return { message: trimmed };
    }
  };

  const isHTMLResponseBody = (t: string): boolean => {
    const trimmed = t.trim().toLowerCase();
    return (
      trimmed.startsWith('<!doctype html') ||
      trimmed.startsWith('<html') ||
      trimmed.startsWith('<head') ||
      trimmed.startsWith('<body')
    );
  };

  const data = parseWorkspacePayload(text);

  if (!response.ok) {
    const errMsg = String(data.error ?? data.message ?? 'Failed to fetch workspace');
    throw new Error(errMsg);
  }

  if (isHTMLResponseBody(text)) {
    throw new Error('Workspace API returned HTML response');
  }

  if (data && typeof data === 'object' && 'workspace_root' in data && 'daemon_root' in data) {
    return toWorkspaceResponse(data);
  }

  throw new Error('Workspace API returned malformed response');
}

export async function setWorkspace(
  fetchFn: typeof fetch,
  path: string,
): Promise<WorkspaceResponse & { message: string }> {
  const response = await fetchFn('/api/workspace', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });

  const text = await response.text();
  const data = (() => {
    const trimmed = text.trim();
    if (!trimmed) return {} as Record<string, unknown>;
    try {
      return JSON.parse(trimmed);
    } catch {
      return { message: trimmed };
    }
  })();

  if (!response.ok) {
    throw new Error(String(data.error ?? data.message ?? 'Failed to update workspace'));
  }

  const isHTML =
    text.trim().toLowerCase().startsWith('<!doctype html') || text.trim().toLowerCase().startsWith('<html');
  if (isHTML) {
    throw new Error('Workspace API returned HTML response');
  }

  if (data && typeof data === 'object' && 'workspace_root' in data && 'daemon_root' in data) {
    return { ...toWorkspaceResponse(data), message: String(data.message ?? '') };
  }

  // Remote/proxy setups may respond with non-JSON success body
  const workspace = await getWorkspace(fetchFn);
  return { ...workspace, message: String(data.message ?? 'Workspace updated') };
}
