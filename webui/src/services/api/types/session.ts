/**
 * Chat session, workspace, and session management API types.
 */

export interface WorkspaceResponse {
  daemon_root: string;
  workspace_root: string;
  is_project: boolean;
  project_markers: string[];
  needs_workspace_selection: boolean;
  suggested_projects: Array<{ path: string; name: string; markers: string[] }>;
  recent_workspaces: Array<{
    path: string;
    name: string;
    last_used: string;
    markers: string[];
    session_count: number;
  }>;
  ssh_context?: {
    host_alias: string;
    session_key?: string;
    is_remote: boolean;
    launch_mode?: string;
    launcher_url?: string;
    home_path?: string;
  };
}

export interface SessionEntry {
  session_id: string;
  name: string;
  working_directory: string;
  last_updated: string;
  message_count: number;
  total_tokens: number;
}

export interface SessionsResponse {
  message: string;
  sessions: SessionEntry[];
  current_session_id: string;
}

export interface SessionRestoreResponse {
  message: string;
  session_id: string;
  message_count: number;
  messages: Array<{ role: string; content: string }>;
  total_tokens: number;
  name?: string;
  working_directory?: string;
}
