/**
 * Settings, MCP, custom providers, skills, hotkeys, and subagent API types.
 */

export interface SproutSettings {
  reasoning_effort: string;
  output_verbosity: string;
  disable_thinking?: boolean;
  risk_profile?: string;
  system_prompt_text: string;
  skip_prompt: boolean;
  enable_pre_write_validation: boolean;
  enable_zsh_command_detection: boolean;
  auto_execute_detected_commands: boolean;
  history_scope: string;
  subagent_provider: string;
  subagent_model: string;
  subagent_max_depth?: number;
  subagent_max_parallel?: number;
  subagent_parallel_enabled?: boolean;
  default_subagent_persona: string;
  pdf_ocr_enabled: boolean;
  pdf_ocr_provider: string;
  pdf_ocr_model: string;
  api_timeouts: {
    connection_timeout_sec: number;
    first_chunk_timeout_sec: number;
    chunk_timeout_sec: number;
    overall_timeout_sec: number;
    commit_message_timeout_sec?: number;
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
  /** Cap the effective context window (tokens). Limits how large a request's input can be,
   *  reducing cost on models with very large native context windows. 0 or absent = no limit. */
  max_context_tokens?: number | null;
  /** Computer Use configuration (SP-063) — gates the computer_user persona's desktop-control tools. */
  computer_use?: {
    enabled: boolean;
    max_actions_per_minute: number;
    audit_log_dir: string;
    workspace_allowlist: string[];
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
  path?: string;
}

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

// SP-086-4: Install/manage skills via the HTTP API.
export interface SkillInstallResult {
  skill_id: string;
  install_dir: string;
  origin: {
    type: string;
    url?: string;
    path?: string;
    registry_id?: string;
    ref?: string;
    commit_sha?: string;
    installed_at: string;
  };
}

export interface SkillRegistryEntry {
  id: string;
  name: string;
  description: string;
  git_url: string;
  git_ref: string;
  path_in_repo: string;
}

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
