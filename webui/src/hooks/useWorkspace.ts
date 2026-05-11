/**
 * useWorkspace — lightweight hook for workspace state and switching.
 *
 * Used by WelcomeTab / WorkspacePicker to fetch workspace metadata
 * (is_project, needs_workspace_selection, suggested_projects,
 *  recent_workspaces) and to change the workspace.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import { ApiService } from '../services/api';
import type { WorkspaceResponse } from '../services/api/types';

/* ── Public types ─────────────────────────────────────────────────── */

export interface ProjectSuggestion {
  path: string;
  name: string;
  markers: string[];
}

export interface RecentWorkspace {
  path: string;
  name: string;
  last_used: string;
  markers: string[];
  session_count: number;
}

export interface WorkspaceInfo {
  daemon_root: string;
  workspace_root: string;
  is_project: boolean;
  project_markers: string[];
  needs_workspace_selection: boolean;
  suggested_projects: ProjectSuggestion[];
  recent_workspaces: RecentWorkspace[];
  ssh_context?: WorkspaceResponse['ssh_context'];
}

/* ── Helpers ──────────────────────────────────────────────────────── */

/** Derive the user's home directory from the daemon_root path.
 *  Typical layout: daemon_root = /home/user/.sprout → home = /home/user
 */
function extractHomeDir(daemonRoot: string, workspaceRoot: string): string {
  for (const root of [daemonRoot, workspaceRoot]) {
    if (!root) continue;
    // If the root contains ".sprout" (or other sprout dir), take the parent
    const idx = root.includes('.sprout') ? root.lastIndexOf('/.sprout') : -1;
    if (idx !== -1) return root.slice(0, idx);
    // Fallback: if it looks like a home directory (e.g. /home/user)
    if (/^\/home\/[^/]+\/?$/.test(root) || /^\/root\/?$/.test(root)) {
      return root.replace(/\/+$/, '');
    }
  }
  return '';
}

function mapWorkspaceResponse(data: WorkspaceResponse): WorkspaceInfo {
  return {
    daemon_root: data.daemon_root ?? '',
    workspace_root: data.workspace_root ?? '',
    is_project: data.is_project ?? false,
    project_markers: Array.isArray(data.project_markers) ? data.project_markers : [],
    needs_workspace_selection: data.needs_workspace_selection ?? false,
    suggested_projects: (Array.isArray(data.suggested_projects) ? data.suggested_projects : []).map((p) => ({
      path: p.path ?? '',
      name: p.name ?? '',
      markers: Array.isArray(p.markers) ? p.markers : [],
    })),
    recent_workspaces: (Array.isArray(data.recent_workspaces) ? data.recent_workspaces : []).map((w) => ({
      path: w.path ?? '',
      name: w.name ?? '',
      last_used: w.last_used ?? '',
      markers: Array.isArray(w.markers) ? w.markers : [],
      session_count: typeof w.session_count === 'number' ? w.session_count : 0,
    })),
    ssh_context: data.ssh_context,
  };
}

/* ── Hook ─────────────────────────────────────────────────────────── */

export interface UseWorkspaceResult {
  workspaceInfo: WorkspaceInfo;
  homeDir: string;
  isLoading: boolean;
  setWorkspace: (path: string) => Promise<void>;
  refresh: () => Promise<void>;
}

export function useWorkspace(): UseWorkspaceResult {
  const [workspaceInfo, setWorkspaceInfo] = useState<WorkspaceInfo>({
    daemon_root: '',
    workspace_root: '',
    is_project: false,
    project_markers: [],
    needs_workspace_selection: false,
    suggested_projects: [],
    recent_workspaces: [],
  });
  const [homeDir, setHomeDir] = useState('');
  const [isLoading, setIsLoading] = useState(true);

  const apiService = useRef(ApiService.getInstance());

  const fetchWorkspace = useCallback(async () => {
    try {
      const data = await apiService.current.getWorkspace();
      const info = mapWorkspaceResponse(data);
      setWorkspaceInfo(info);
      setHomeDir(extractHomeDir(info.daemon_root, info.workspace_root));
    } catch {
      // swallow – the caller handles empty state
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchWorkspace();
  }, [fetchWorkspace]);

  const setWorkspace = useCallback(
    async (path: string) => {
      try {
        const data = await apiService.current.setWorkspace(path);
        const info = mapWorkspaceResponse(data);
        setWorkspaceInfo(info);
        setHomeDir(extractHomeDir(info.daemon_root, info.workspace_root));
        // Reload the page so the whole app picks up the new workspace
        window.setTimeout(() => window.location.reload(), 300);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error('[useWorkspace] failed to set workspace:', msg);
        throw err;
      }
    },
    [],
  );

  const refresh = fetchWorkspace;

  return { workspaceInfo, homeDir, isLoading, setWorkspace, refresh };
}
