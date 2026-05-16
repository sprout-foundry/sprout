/**
 * Credential and key pool API types.
 */

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
