/**
 * Adapter-aware domain API modules.
 *
 * Each module exports plain async functions that accept a fetch function
 * as their first parameter. This decouples API calls from the transport
 * layer — use clientFetch for local mode, useSproutFetch() for React
 * components, or adapter.fetch() directly.
 *
 * The ApiService class remains as a backward-compatible singleton facade
 * that delegates to these modules.
 */

// Types — re-exported for convenience
export type {
  StatsResponse,
  QueryRequest,
  FilesResponse,
  SearchMatch,
  SearchResult,
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
  ShellInfo,
  WorkspaceResponse,
  SproutSettings,
  HotkeyEntry,
  HotkeyConfig,
  ProvidersResponse,
  SessionSearchResult,
  SessionSearchResponse,
} from './types';

export { SSHWorkspaceOpenError } from './types';

// ApiService — singleton facade
export { ApiService } from './apiService';

// Domain modules
export * as filesApi from './filesApi';
export * as gitApi from './gitApi';
export * as chatApi from './chatApi';
export * as terminalApi from './terminalApi';
export * as settingsApi from './settingsApi';
export * as credentialsApi from './credentialsApi';
export * as workspaceApi from './workspaceApi';
export * as sshApi from './sshApi';
export * as searchApi from './searchApi';
export * as editorApi from './editorApi';
export * as onboardingApi from './onboardingApi';
export * as sessionApi from './sessionApi';
export * as miscApi from './miscApi';
