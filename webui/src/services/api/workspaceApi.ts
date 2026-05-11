/**
 * Workspace domain API — adapter-aware workspace operations.
 */

import type { WorkspaceResponse } from './types';

function toWorkspaceResponse(data: Record<string, unknown>): WorkspaceResponse {
  const parseBool = (v: unknown): boolean => v === true || v === 'true';
  const parseStrArray = (v: unknown): string[] => {
    if (Array.isArray(v)) return v.filter((x) => typeof x === 'string');
    return [];
  };

  const suggested_projects = ((): Array<{ path: string; name: string; markers: string[] }> => {
    if (!Array.isArray(data.suggested_projects)) return [];
    return data.suggested_projects.filter(
      (p) => typeof p === 'object' && p != null && 'path' in p,
    ).map((p) => ({
      path: String((p as Record<string, unknown>).path ?? ''),
      name: String((p as Record<string, unknown>).name ?? ''),
      markers: parseStrArray((p as Record<string, unknown>).markers),
    }));
  })();

  const recent_workspaces = ((): Array<{
    path: string;
    name: string;
    last_used: string;
    markers: string[];
    session_count: number;
  }> => {
    if (!Array.isArray(data.recent_workspaces)) return [];
    return data.recent_workspaces.filter(
      (w) => typeof w === 'object' && w != null && 'path' in w,
    ).map((w) => ({
      path: String((w as Record<string, unknown>).path ?? ''),
      name: String((w as Record<string, unknown>).name ?? ''),
      last_used: String((w as Record<string, unknown>).last_used ?? ''),
      markers: parseStrArray((w as Record<string, unknown>).markers),
      session_count: Number((w as Record<string, unknown>).session_count ?? 0),
    }));
  })();

  return {
    daemon_root: String(data.daemon_root ?? ''),
    workspace_root: String(data.workspace_root ?? ''),
    is_project: parseBool(data.is_project),
    project_markers: parseStrArray(data.project_markers),
    needs_workspace_selection: parseBool(data.needs_workspace_selection),
    suggested_projects,
    recent_workspaces,
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
