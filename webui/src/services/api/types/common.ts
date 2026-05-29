/**
 * Common/general API types shared across domains.
 */

import type { ShellInfo } from '@sprout/ui';

export type { ShellInfo };

export interface StatsResponse {
  provider: string;
  model: string;
  session_id: string;
  query_count: number;
  queries?: number;
  uptime: string;
  uptime_formatted?: string;
  connections: number;

  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  cached_tokens: number;
  cache_efficiency: number;

  current_context_tokens: number;
  max_context_tokens: number;
  context_usage_percent: number;
  context_warning_issued: boolean;

  total_cost: number;
  cached_cost_savings: number;

  last_tps: number;

  current_iteration: number;
  max_iterations: number;

  streaming_enabled: boolean;
  debug_mode: boolean;

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

export interface ProviderOption {
  id: string;
  name: string;
  models: string[];
}

export interface ProviderModel {
  id: string;
  name?: string;
  context_length?: number;
  /** Roles the model meets the deterministic minimum bar for ("primary", "subagent"). */
  eligible_roles?: string[];
  /** Roles backed by the capability probe (⊆ eligible_roles). */
  recommended_roles?: string[];
  /** Non-blocking caveats to surface (e.g. a small context window). */
  warnings?: string[];
}

export interface ProviderModelsResponse {
  provider: string;
  models: ProviderModel[];
}

export interface ProvidersResponse {
  providers: Array<{
    id: string;
    name: string;
    models: string[];
  }>;
}
