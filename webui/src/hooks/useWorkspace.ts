/**
 * useWorkspace — lightweight hook for workspace state and switching.
 *
 * Used by WelcomeTab / WorkspacePicker to fetch workspace metadata
 * (is_project, needs_workspace_selection, suggested_projects,
 *  recent_workspaces) and to change the workspace.
 */

import { useCallback, useMemo, useRef } from 'react';
import { ApiService } from '../services/api';
import type { WorkspaceResponse } from '../services/api/types';
import { applyWorkspaceRoot, refreshWorkspace, useWorkspaceState } from '../services/workspaceStore';

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

const EMPTY_WORKSPACE_INFO: WorkspaceInfo = {
  daemon_root: '',
  workspace_root: '',
  is_project: false,
  project_markers: [],
  needs_workspace_selection: false,
  workspace_is_home: false,
  home_dir: '',
  suggested_projects: [],
  recent_workspaces: [],
};

export interface UseWorkspaceResult {
  workspaceInfo: WorkspaceInfo;
  homeDir: string;
  isLoading: boolean;
  setWorkspace: (path: string, consentHome?: boolean) => Promise<void>;
  refresh: () => Promise<void>;
}

export function useWorkspace(): UseWorkspaceResult {
  const { workspace, updatedAt, error } = useWorkspaceState();
  const workspaceInfo = useMemo(
    () => (workspace ? mapWorkspaceResponse(workspace) : EMPTY_WORKSPACE_INFO),
    [workspace],
  );
  // Loading until the first fetch settles, successfully or not.
  const isLoading = updatedAt === 0 && error === null;

  const apiService = useRef(ApiService.getInstance());

  const refresh = useCallback(async () => {
    try {
      await refreshWorkspace({ force: true });
    } catch {
      // best-effort: metadata refresh. An empty picker (no recents, no
      // suggestions) is the visible symptom and the store retries; the
      // switch path is separate and DOES surface its failure to the user.
    }
  }, []);

  const setWorkspace = useCallback(async (path: string, consentHome?: boolean) => {
    try {
      const data = await apiService.current.setWorkspace(path, consentHome);
      void applyWorkspaceRoot(data.workspace_root || path, data.daemon_root);
      // Reload the page so the whole app picks up the new workspace
      window.setTimeout(() => window.location.reload(), 300);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error('[useWorkspace] failed to set workspace:', msg);
      throw err;
    }
  }, []);

  return { workspaceInfo, homeDir: workspaceInfo.home_dir, isLoading, setWorkspace, refresh };
}
