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
  /** True when the workspace root resolves to the user's home dir (SP-130). */
  workspace_is_home: boolean;
  /** The user's home directory, as resolved by the backend (SP-130). */
  home_dir: string;
  suggested_projects: ProjectSuggestion[];
  recent_workspaces: RecentWorkspace[];
  ssh_context?: WorkspaceResponse['ssh_context'];
}

/* ── Helpers ──────────────────────────────────────────────────────── */

function mapWorkspaceResponse(data: WorkspaceResponse): WorkspaceInfo {
  return {
    daemon_root: data.daemon_root ?? '',
    workspace_root: data.workspace_root ?? '',
    is_project: data.is_project ?? false,
    project_markers: Array.isArray(data.project_markers) ? data.project_markers : [],
    needs_workspace_selection: data.needs_workspace_selection ?? false,
    workspace_is_home: data.workspace_is_home ?? false,
    home_dir: data.home_dir ?? '',
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
  setWorkspace: (path: string, consentHome?: boolean) => Promise<void>;
  refresh: () => Promise<void>;
}

export function useWorkspace(): UseWorkspaceResult {
  const [workspaceInfo, setWorkspaceInfo] = useState<WorkspaceInfo>({
    daemon_root: '',
    workspace_root: '',
    is_project: false,
    project_markers: [],
    needs_workspace_selection: false,
    workspace_is_home: false,
    home_dir: '',
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
      setHomeDir(info.home_dir);
    } catch {
      // swallow – the caller handles empty state
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchWorkspace();
  }, [fetchWorkspace]);

  const setWorkspace = useCallback(async (path: string, consentHome?: boolean) => {
    try {
      const data = await apiService.current.setWorkspace(path, consentHome);
      const info = mapWorkspaceResponse(data);
      setWorkspaceInfo(info);
      setHomeDir(info.home_dir);
      // Reload the page so the whole app picks up the new workspace
      window.setTimeout(() => window.location.reload(), 300);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error('[useWorkspace] failed to set workspace:', msg);
      throw err;
    }
  }, []);

  const refresh = fetchWorkspace;

  return { workspaceInfo, homeDir, isLoading, setWorkspace, refresh };
}
