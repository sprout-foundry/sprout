/**
 * Shared API types for Sprout WebUI
 *
 * This file contains all interfaces, types, and error classes used by the ApiService.
 * These types are shared across the application and can be imported from this module.
 */

import type { ShellInfo } from '@sprout/ui';
export type { ShellInfo };
export type { ChangelogResponse, ChangesResponse, RevisionDetailResponse, RollbackResponse } from '@sprout/ui';

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

export interface ProviderModelsResponse {
  provider: string;
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
  /** Same-origin proxy URL served by the local sprout server (e.g. http://127.0.0.1:54421/ssh/{key}/).
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
    servers: Record<string, MCPServerConfig>;
  };
  custom_providers: Record<string, CustomProviderConfig>;
  skills: Record<string, SkillConfig>;
  embedding_index?: {
    enabled: boolean;
    provider: string;
    ort_library_path: string;
    model_dir: string;
    auto_index: boolean;
    similarity_threshold: number;
    max_results: number;
    exclude_paths: string[];
  };
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

// ── MCP Settings interfaces ─────────────────────────────────────────

export interface MCPServerConfig {
  name: string;
  type?: string;
  command?: string;
  args?: string[];
  url?: string;
  env?: Record<string, string>;
  credentials?: Record<string, string>;
  working_dir?: string;
  timeout?: string;
  auto_start?: boolean;
  max_restarts?: number;
}

export interface MCPSettingsResponse {
  mcp: {
    enabled: boolean;
    auto_start: boolean;
    auto_discover: boolean;
    timeout: string;
    servers: Record<string, MCPServerConfig>;
  };
}

// ── Custom Provider interfaces ───────────────────────────────────────

export interface CustomProviderConfig {
  name: string;
  endpoint: string;
  model_name: string;
  context_size: number;
  model_context_sizes?: Record<string, number>;
  reasoning_effort?: string;
  temperature?: number;
  top_p?: number;
  parameters?: Record<string, unknown>;
  requires_api_key: boolean;
  tool_calls?: string[];
  env_var?: string;
  chunk_timeout_ms?: number;
  supports_vision?: boolean;
  vision_model?: string;
  vision_fallback_provider?: string;
  vision_fallback_model?: string;
}

export interface CustomProvidersResponse {
  custom_providers: Record<string, CustomProviderConfig>;
}

// ── Skills interfaces ───────────────────────────────────────────────

export interface SkillConfig {
  id: string;
  name: string;
  description: string;
  path: string;
  enabled: boolean;
  metadata?: Record<string, string>;
  allowed_tools?: string;
}

export interface SkillsResponse {
  skills: Record<string, SkillConfig>;
}

// ── Subagent Type interfaces ─────────────────────────────────────────

export interface SubagentTypeInfo {
  id: string;
  name: string;
  description: string;
  provider: string;
  model: string;
  system_prompt: string;
  system_prompt_text?: string;
  allowed_tools: string[];
  aliases: string[];
  enabled: boolean;
}

export interface SubagentTypesResponse {
  subagent_types: Record<string, SubagentTypeInfo>;
  available_providers: Array<{ id: string; name: string; models: string[] }>;
  current_provider: string;
  current_model: string;
}

export interface UpdateSubagentTypeResponse {
  success: boolean;
  type: SubagentTypeInfo;
}

// ── Git types ───────────────────────────────────────────────────────

export interface GitStatusEntry {
  path: string;
  status: string;
  staged: boolean;
}

export interface GitStatusResponse {
  message: string;
  status: {
    branch: string;
    ahead: number;
    behind: number;
    staged: GitStatusEntry[];
    modified: GitStatusEntry[];
    untracked: GitStatusEntry[];
    deleted: GitStatusEntry[];
    renamed: GitStatusEntry[];
    truncated?: boolean;
  };
  files: Array<{ path: string; status: string; staged?: boolean }>;
}

export interface GitBranchesResponse {
  message: string;
  current: string;
  branches: string[];
}

export interface GitBranchResponse {
  message: string;
  branch: string;
}

export interface GitPushPullResponse {
  message: string;
  output?: string;
}

export interface GitStageResponse {
  message: string;
  path: string;
}

export interface GitStageAllResponse {
  message: string;
}

export interface GitCommitResponse {
  message: string;
  commit: string;
}

export interface GitCommitMessageResponse {
  message: string;
  commit_message: string;
  provider?: string;
  model?: string;
  warnings?: string[];
}

export interface GitLogEntry {
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  message: string;
  ref_names?: string;
}

export interface GitLogResponse {
  message: string;
  commits: GitLogEntry[];
  offset: number;
  limit: number;
  total: number;
}

export interface GitCommitDetailResponse {
  message: string;
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  ref_names?: string;
  subject: string;
  files: Array<{ path: string; status: string }>;
  diff: string;
  stats: string;
}

export interface GitCommitFileDiffResponse {
  message: string;
  hash: string;
  path: string;
  diff: string;
}

export interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}

// ── Credentials types ───────────────────────────────────────────────

export interface ProviderCredentialEntry {
  provider: string;
  display_name: string;
  env_var: string;
  requires_api_key: boolean;
  has_stored_credential: boolean;
  has_env_credential: boolean;
  credential_source: string;
  masked_value: string;
  key_pool_size: number;
}

export interface ProviderCredentialsResponse {
  storage_backend: string;
  providers: ProviderCredentialEntry[];
}

export interface TestProviderConnectionResponse {
  success: boolean;
  error?: string;
  model_count?: number;
}

export interface KeyPoolResponse {
  provider: string;
  key_count: number;
  masked_keys: string[];
}

export interface MCPServerCredentialsResponse {
  server: string;
  credentials: Record<string, { status: string; has_value: boolean }>;
}

export interface UpdateMCPServerCredentialsResponse {
  success: boolean;
  server: string;
}

// ── Onboarding types ────────────────────────────────────────────────

export interface CompleteOnboardingRequest {
  provider: string;
  model?: string;
  api_key?: string;
}

export interface CompleteOnboardingResponse {
  success: boolean;
  message: string;
  provider: string;
  model: string;
  validation?: { tested: boolean; model_count?: number };
}

// ── Search types ────────────────────────────────────────────────────

export interface SearchOptions {
  case_sensitive?: boolean;
  whole_word?: boolean;
  regex?: boolean;
  include?: string;
  exclude?: string;
  max_results?: number;
  context_lines?: number;
}

export interface SearchResponse {
  results: SearchResult[];
  total_matches: number;
  total_files: number;
  truncated: boolean;
  query: string;
}

export interface SearchReplaceMatch {
  line_number: number;
  old_line: string;
  new_line: string;
  column_start: number;
  column_end: number;
}

export interface SearchReplaceChange {
  file: string;
  matches: SearchReplaceMatch[];
  changed_lines: number;
}

export interface SearchReplaceRequest {
  search: string;
  replace: string;
  files: string[];
  case_sensitive?: boolean;
  whole_word?: boolean;
  regex?: boolean;
  preview: boolean;
}

export interface SearchReplaceResponse {
  changes: SearchReplaceChange[];
  total_changes: number;
  preview: boolean;
}

export interface SemanticSearchOptions {
  top_k?: number;
  threshold?: number;
}

export interface SemanticSearchResult {
  file: string;
  name: string;
  signature: string;
  start_line: number;
  end_line: number;
  language: string;
  similarity: number;
  type: string;  // "code_unit" or "file"
}

export interface SemanticSearchDuplicateCluster {
  files: string[];
  similarity: number;
}

export interface SemanticSearchResponse {
  results: SemanticSearchResult[];
  duplicate_clusters: SemanticSearchDuplicateCluster[];
  query: string;
  total: number;
  duration: string;
}

export interface SemanticSearchStatusResponse {
  available: boolean;
  initialized: boolean;
  building: boolean;
  record_count: number;
  workspace: string;
  init_error?: string;
}

export interface SemanticSearchPreviewResponse {
  file: string;
  start_line: number;
  snippet: Array<{
    line_number: number;
    content: string;
    is_context: boolean;
  }>;
  total_lines: number;
}

// ── Editor types ────────────────────────────────────────────────────

export interface DiagnosticEntry {
  from: number;
  to: number;
  severity: 'error' | 'warning' | 'info';
  message: string;
  source: string;
}

export interface DiagnosticsResponse {
  message: string;
  path: string;
  diagnostics: DiagnosticEntry[];
  version: string;
}

export interface SemanticCapabilities {
  diagnostics: boolean;
  definition: boolean;
}

export interface SemanticDiagnosticsResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities;
  diagnostics: DiagnosticEntry[];
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticDefinitionResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities;
  definition?: { path: string; line: number; column: number } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticHoverResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean };
  hover?: { contents: string } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticRenameResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; rename: boolean };
  rename?: { locations: Array<{ filePath: string; from: number; to: number }> } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticReferencesResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; rename: boolean; references: boolean };
  references?: { locations: Array<{ filePath: string; line: number; startCol: number; endCol: number; lineText: string }>; symbolName: string } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticCodeActionsResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; rename: boolean; references: boolean; code_actions: boolean };
  code_actions?: Array<{ title: string; kind: string; edits: Array<{ filePath: string; from: number; to: number; newText: string }> }> | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticInlayHintsResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { inlay_hints: boolean };
  inlay_hints?: Array<{ from: number; to: number; label: string; kind: 'type' | 'parameter' | 'none' }> | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticSignatureHelpResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; signature_help: boolean };
  signature_help?: {
    signatures: Array<{
      label: string;
      documentation?: string;
      parameters: Array<{
        label: string;
        documentation?: string;
      }>;
    }>;
    activeSignature: number;
    activeParameter: number;
  } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface WorkspaceSymbolEntry {
  name: string;
  kind: string;
  line?: number;
}

export interface WorkspaceSymbolFile {
  file: string;
  symbols: WorkspaceSymbolEntry[];
}

export interface WorkspaceSymbolsResponse {
  message: string;
  files: WorkspaceSymbolFile[];
  total: number;
}

// ── Session types ───────────────────────────────────────────────────

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

// ── Misc / Changelog types ──────────────────────────────────────────

// ChangelogResponse, ChangesResponse, RevisionDetailResponse, and RollbackResponse
// are now imported from @sprout/ui

// ── Review types ────────────────────────────────────────────────────

export interface DeepReviewResponse {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
  warnings?: string[];
}

export interface DeepReviewFixResponse {
  message: string;
  result: string;
}

export interface DeepReviewFixStartResponse {
  message: string;
  job_id: string;
  session_id: string;
}

export interface DeepReviewFixStatusResponse {
  message: string;
  job_id: string;
  session_id: string;
  status: 'running' | 'completed' | 'error';
  logs: string[];
  next_index: number;
  result: string;
  error: string;
}

// ── Instances / SSH types ───────────────────────────────────────────

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

// ── Terminal types ──────────────────────────────────────────────────

export interface TerminalHistoryResponse {
  history: string[];
  count: number;
}

export interface AddTerminalHistoryResponse {
  message: string;
  command: string;
}

// ── Chat types ──────────────────────────────────────────────────────

export interface UploadImageResponse {
  path: string;
  filename: string;
}

// ── File operations types ───────────────────────────────────────────

export interface CreateItemResponse {
  message: string;
  path: string;
}

export interface DeleteItemResponse {
  message: string;
  path: string;
}

export interface RenameItemResponse {
  message: string;
  old_path: string;
  new_path: string;
}
