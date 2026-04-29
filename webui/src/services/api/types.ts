/**
 * Shared API types for Sprout WebUI
 *
 * This file contains all interfaces, types, and error classes used by the ApiService.
 * These types are shared across the application and can be imported from this module.
 */

export interface StatsResponse {
  // Basic info
  provider: string;
  model: string;
  session_id: string;
  query_count: number;
  queries?: number;
  uptime: string;
  uptime_formatted?: string;
  connections: number;

  // Token usage
  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  cached_tokens: number;
  cache_efficiency: number;

  // Context usage
  current_context_tokens: number;
  max_context_tokens: number;
  context_usage_percent: number;
  context_warning_issued: boolean;

  // Cost tracking
  total_cost: number;
  cached_cost_savings: number;

  // Performance metrics
  last_tps: number;

  // Iteration tracking
  current_iteration: number;
  max_iterations: number;

  // Configuration
  streaming_enabled: boolean;
  debug_mode: boolean;

  // Processing state
  is_processing?: boolean;
}

export interface QueryRequest {
  query: string;
  chat_id?: string;
}

export interface FilesResponse {
  message: string;
  files: Array<{
    path: string;
    modified: boolean;
    content?: string;
  }>;
}

export interface SearchMatch {
  line_number: number;
  line: string;
  column_start: number;
  column_end: number;
  context_before: string[];
  context_after: string[];
}

export interface SearchResult {
  file: string;
  matches: SearchMatch[];
  match_count: number;
}

export interface ProviderOption {
  id: string;
  name: string;
  models: string[];
}

export interface OnboardingProviderOption {
  id: string;
  name: string;
  models: string[];
  requires_api_key: boolean;
  has_credential: boolean;
  recommended: boolean;
  description: string;
  setup_hint: string;
  docs_url: string;
  signup_url: string;
  api_key_label: string;
  api_key_help: string;
  recommended_model: string;
  recommended_model_why: string;
}

export interface OnboardingEnvironment {
  runtime_platform: string;
  host_platform: string;
  backend_mode: string;
  has_wsl: boolean;
  has_git_bash: boolean;
  recommended_terminal: string;
  active_distro: string;
  wsl_distros: string[];
}

export interface OnboardingStatusResponse {
  setup_required: boolean;
  reason: string;
  current_provider: string;
  current_model: string;
  providers: OnboardingProviderOption[];
  environment?: OnboardingEnvironment;
}

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
  /** Same-origin proxy URL served by the local ledit server (e.g. http://127.0.0.1:54421/ssh/{key}/).
   *  Prefer this over `url` to keep the browser on the same origin for PWA compatibility. */
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
  /** Non-empty when in_progress=false and last_error is absent. */
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

export interface ShellInfo {
  name: string;
  path: string;
  default: boolean;
}

export interface WorkspaceResponse {
  daemon_root: string;
  workspace_root: string;
  ssh_context?: {
    host_alias: string;
    session_key?: string;
    is_remote: boolean;
    launch_mode?: string;
    launcher_url?: string;
    home_path?: string;
  };
}

export interface ProvidersResponse {
  providers: Array<{
    id: string;
    name: string;
    models: string[];
  }>;
}

export interface SproutSettings {
  reasoning_effort: string;
  system_prompt_text: string;
  skip_prompt: boolean;
  enable_pre_write_validation: boolean;
  enable_zsh_command_detection: boolean;
  auto_execute_detected_commands: boolean;
  history_scope: string;
  self_review_gate_mode: string;
  subagent_provider: string;
  subagent_model: string;
  default_subagent_persona: string;
  pdf_ocr_enabled: boolean;
  pdf_ocr_provider: string;
  pdf_ocr_model: string;
  api_timeouts: {
    connection_timeout_sec: number;
    first_chunk_timeout_sec: number;
    chunk_timeout_sec: number;
    overall_timeout_sec: number;
  } | null;
  mcp: {
    enabled: boolean;
    auto_start: boolean;
    auto_discover: boolean;
    timeout: string;
    servers: Record<string, any>;
  };
  custom_providers: Record<string, any>;
  skills: Record<string, any>;
}

export interface HotkeyEntry {
  key: string;
  command_id: string;
  description?: string;
  global?: boolean;
}

export interface HotkeyConfig {
  version: string;
  hotkeys: HotkeyEntry[];
  path?: string;  // Filesystem path to the hotkeys config file
}
