/**
 * Public API barrel for Sprout WebUI services.
 *
 * Re-exports the ApiService singleton and all shared types so that
 * existing consumers (`import { … } from '../services/api'`) continue
 * to work without changes.
 */

// ── ApiService (singleton facade) ──────────────────────────────────
export { ApiService } from './api/apiService';

// ── Types (re-exported for backward compatibility) ─────────────────
export type {
  StatsResponse,
  QueryRequest,
  FilesResponse,
  SearchMatch,
  SearchResult,
  ShellInfo,
  ProviderOption,
  OnboardingProviderOption,
  OnboardingEnvironment,
  OnboardingStatusResponse,
  SproutInstance,
  SSHHostEntry,
  SSHSessionEntry,
  SSHOpenResponse,
  SSHBrowseEntry,
  SSHOpenErrorPayload,
  SSHLaunchStatus,
  WorkspaceResponse,
  ProvidersResponse,
  SproutSettings,
  HotkeyEntry,
  HotkeyConfig,
  MCPServerConfig,
  MCPSettingsResponse,
  CustomProviderConfig,
  CustomProvidersResponse,
  SkillConfig,
  SkillsResponse,
  SkillInstallResult,
  SkillRegistryEntry,
  SubagentTypeInfo,
  SessionSearchResult,
  SessionSearchResponse,
} from './api/types';

// Re-export SSHWorkspaceOpenError as a value for backward compatibility
export { SSHWorkspaceOpenError } from './api/types';
