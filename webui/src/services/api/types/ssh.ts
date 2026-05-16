/**
 * SSH and instance management API types.
 */

export interface SproutInstance {
  id: string;
  pid: number;
  port: number;
  working_dir: string;
  start_time: string;
  last_ping: string;
  session_id?: string;
  is_host: boolean;
  is_current: boolean;
}

export interface SSHHostEntry {
  alias: string;
  hostname?: string;
  user?: string;
  port?: string;
}

export interface SSHSessionEntry {
  key: string;
  host_alias: string;
  remote_workspace_path: string;
  local_port?: number;
  remote_port: number;
  remote_pid?: number;
  url?: string;
  started_at: string;
  active: boolean;
}

export interface SSHOpenResponse {
  message: string;
  url: string;
  port?: number;
  proxy_url?: string;
  proxy_base?: string;
}

export interface SSHBrowseEntry {
  name: string;
  path: string;
  type: string;
}

export interface SSHOpenErrorPayload {
  error: string;
  step?: string;
  details?: string;
  log_path?: string;
}

export interface SSHLaunchStatus {
  key: string;
  step: string;
  status: string;
  in_progress: boolean;
  last_error?: string;
  details?: string;
  log_path?: string;
  updated_at: string;
  proxy_base?: string;
  proxy_url?: string;
  local_port?: number;
}

export class SSHWorkspaceOpenError extends Error {
  step?: string;
  details?: string;
  logPath?: string;

  constructor(payload: SSHOpenErrorPayload) {
    const baseMessage = payload.error || 'Failed to open SSH workspace';
    super(payload.step ? `${baseMessage} (${payload.step})` : baseMessage);
    this.name = 'SSHWorkspaceOpenError';
    this.step = payload.step;
    this.details = payload.details;
    this.logPath = payload.log_path;
  }
}

export interface InstancesResponse {
  instances: SproutInstance[];
  current_pid: number;
  active_host_pid: number;
  active_host_port: number;
  desired_host_pid: number;
}

export interface SSHHostsResponse {
  hosts: SSHHostEntry[];
}

export interface SSHSessionsResponse {
  sessions: SSHSessionEntry[];
}

export interface SSHBrowseResponse {
  path: string;
  home_path?: string;
  files: SSHBrowseEntry[];
}

export interface SSHCloseResponse {
  message: string;
  key: string;
}

export interface SelectInstanceResponse {
  message: string;
  pid: number;
}
