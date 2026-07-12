/**
 * ApiService — singleton facade that delegates to domain API modules.
 *
 * Each method is a thin wrapper that injects the `clientFetch` transport
 * into the corresponding domain module function.  Consumers access the API
 * exclusively through `ApiService.getInstance()`.
 */

import type { ShellInfo } from '@sprout/ui';
import { clientFetch } from '../clientSession';
import * as changesApi from './changesApi';
import * as chatApi from './chatApi';
import * as credentialsApi from './credentialsApi';
import * as editorApi from './editorApi';
import * as filesApi from './filesApi';
import * as gitApi from './gitApi';
import * as miscApi from './miscApi';
import * as onboardingApi from './onboardingApi';
import * as searchApi from './searchApi';
import * as sessionApi from './sessionApi';
import * as settingsApi from './settingsApi';
import * as sshApi from './sshApi';
import * as terminalApi from './terminalApi';
import type {
  StatsResponse,
  FilesResponse,
  ProviderOption,
  OnboardingStatusResponse,
  SproutInstance,
  SSHHostEntry,
  SSHSessionEntry,
  SSHOpenResponse,
  SSHBrowseEntry,
  SSHLaunchStatus,
  WorkspaceResponse,
  SproutSettings,
  HotkeyConfig,
  MCPServerConfig,
  MCPSettingsResponse,
  CustomProviderConfig,
  CustomProvidersResponse,
  SkillConfig,
  SkillsResponse,
  SubagentTypeInfo,
  ProviderModelsResponse,
  SessionSearchResponse,
  SessionSearchResult,
} from './types';
import * as workspaceApi from './workspaceApi';

class ApiService {
  private static instance: ApiService;

  private constructor() {}

  static getInstance(): ApiService {
    if (!ApiService.instance) {
      ApiService.instance = new ApiService();
    }
    return ApiService.instance;
  }

  // ── Stats/Health/Providers ────────────────────────────────────────

  async getStats(): Promise<StatsResponse> {
    return miscApi.getStats(clientFetch);
  }

  async checkHealth(): Promise<boolean> {
    return miscApi.checkHealth(clientFetch);
  }

  // ── Workspace ──────────────────────────────────────────────────────

  async getWorkspace(): Promise<WorkspaceResponse> {
    return workspaceApi.getWorkspace(clientFetch);
  }

  async setWorkspace(path: string): Promise<WorkspaceResponse & { message: string }> {
    return workspaceApi.setWorkspace(clientFetch, path);
  }

  // ── Terminal ───────────────────────────────────────────────────────

  async getTerminalSessionCount(): Promise<number> {
    return terminalApi.getTerminalSessionCount(clientFetch);
  }

  async getAvailableShells(): Promise<{ shells: ShellInfo[] }> {
    return terminalApi.getAvailableShells(clientFetch);
  }

  // ── Providers ───────────────────────────────────────────────────────

  async getProviders(): Promise<{
    providers: ProviderOption[];
    current_provider?: string;
    current_model?: string;
  }> {
    return miscApi.getProviders(clientFetch);
  }

  async getProviderModels(provider: string): Promise<ProviderModelsResponse> {
    return miscApi.getProviderModels(clientFetch, provider);
  }

  async getProviderCredentials(): Promise<{
    storage_backend: string;
    providers: Array<{
      provider: string;
      display_name: string;
      env_var: string;
      requires_api_key: boolean;
      has_stored_credential: boolean;
      has_env_credential: boolean;
      credential_source: string;
      masked_value: string;
      key_pool_size: number;
    }>;
  }> {
    return credentialsApi.getProviderCredentials(clientFetch);
  }

  async setProviderCredential(provider: string, value: string): Promise<void> {
    return credentialsApi.setProviderCredential(clientFetch, provider, value);
  }

  async deleteProviderCredential(provider: string): Promise<void> {
    return credentialsApi.deleteProviderCredential(clientFetch, provider);
  }

  async getMCPServerCredentials(
    serverName: string,
  ): Promise<{ server: string; credentials: Record<string, { status: string; has_value: boolean }> }> {
    return credentialsApi.getMCPServerCredentials(clientFetch, serverName);
  }

  async updateMCPServerCredentials(
    serverName: string,
    credentials: Record<string, string>,
  ): Promise<{ success: boolean; server: string }> {
    return credentialsApi.updateMCPServerCredentials(clientFetch, serverName, credentials);
  }

  async deleteMCPServerCredential(serverName: string, credentialName: string): Promise<void> {
    return credentialsApi.deleteMCPServerCredential(clientFetch, serverName, credentialName);
  }

  async testProviderConnection(provider: string): Promise<{ success: boolean; error?: string; model_count?: number }> {
    return credentialsApi.testProviderConnection(clientFetch, provider);
  }

  async getKeyPool(provider: string): Promise<{ provider: string; key_count: number; masked_keys: string[] }> {
    return credentialsApi.getKeyPool(clientFetch, provider);
  }

  async addKeyToPool(provider: string, value: string): Promise<void> {
    return credentialsApi.addKeyToPool(clientFetch, provider, value);
  }

  async removeKeyFromPool(provider: string, index: number): Promise<void> {
    return credentialsApi.removeKeyFromPool(clientFetch, provider, index);
  }

  // ── Onboarding ───────────────────────────────────────────────────

  async getOnboardingStatus(): Promise<OnboardingStatusResponse> {
    return onboardingApi.getOnboardingStatus(clientFetch);
  }

  async completeOnboarding(payload: { provider: string; model?: string; api_key?: string }): Promise<{
    success: boolean;
    message: string;
    provider: string;
    model: string;
    validation?: { tested: boolean; model_count?: number };
  }> {
    return onboardingApi.completeOnboarding(clientFetch, payload);
  }

  async skipOnboarding(): Promise<void> {
    return onboardingApi.skipOnboarding(clientFetch);
  }

  // ── Files ───────────────────────────────────────────────────────

  async getFiles(): Promise<FilesResponse> {
    return filesApi.getFiles(clientFetch);
  }

  async createItem(path: string, isDirectory = false): Promise<{ message: string; path: string }> {
    return filesApi.createItem(clientFetch, path, isDirectory);
  }

  async deleteItem(path: string): Promise<{ message: string; path: string }> {
    return filesApi.deleteItem(clientFetch, path);
  }

  async renameItem(oldPath: string, newPath: string): Promise<{ message: string; old_path: string; new_path: string }> {
    return filesApi.renameItem(clientFetch, oldPath, newPath);
  }

  async openInFileBrowser(path: string): Promise<void> {
    return filesApi.openInFileBrowser(clientFetch, path);
  }

  // ── Instances/SSH ────────────────────────────────────────────────

  async getInstances(): Promise<{
    instances: SproutInstance[];
    current_pid: number;
    active_host_pid: number;
    active_host_port: number;
    desired_host_pid: number;
  }> {
    return sshApi.getInstances(clientFetch);
  }

  async getSSHHosts(): Promise<SSHHostEntry[]> {
    const result = await sshApi.getSSHHosts(clientFetch);
    return result.hosts;
  }

  async openSSHWorkspace(hostAlias: string, remoteWorkspacePath?: string): Promise<SSHOpenResponse> {
    return sshApi.openSSHWorkspace(clientFetch, hostAlias, remoteWorkspacePath);
  }

  async getSSHLaunchStatus(hostAlias: string, remoteWorkspacePath?: string): Promise<SSHLaunchStatus> {
    return sshApi.getSSHLaunchStatus(clientFetch, hostAlias, remoteWorkspacePath);
  }

  async getSSHSessions(): Promise<SSHSessionEntry[]> {
    const result = await sshApi.getSSHSessions(clientFetch);
    return result.sessions;
  }

  async browseSSHDirectory(
    hostAlias: string,
    path?: string,
  ): Promise<{ path: string; home_path?: string; files: SSHBrowseEntry[] }> {
    return sshApi.browseSSHDirectoryByHostAlias(clientFetch, hostAlias, path);
  }

  async closeSSHSession(key: string): Promise<{ message: string; key: string }> {
    return sshApi.closeSSHSession(clientFetch, key);
  }

  async selectInstance(pid: number): Promise<{ message: string; pid: number }> {
    return sshApi.selectInstance(clientFetch, pid);
  }

  // ── Chat ───────────────────────────────────────────────────────

  async sendQuery(query: string, chatId?: string): Promise<void> {
    return chatApi.sendQuery(clientFetch, query, chatId);
  }

  async uploadImage(file: File | Blob): Promise<{ path: string; filename: string }> {
    return chatApi.uploadImage(clientFetch, file);
  }

  async steerQuery(query: string, chatId?: string): Promise<void> {
    return chatApi.steerQuery(clientFetch, query, chatId);
  }

  async stopQuery(): Promise<void> {
    return chatApi.stopQuery(clientFetch);
  }

  // ── Terminal History ─────────────────────────────────────────────

  async getTerminalHistory(sessionId?: string): Promise<{ history: string[]; count: number }> {
    return terminalApi.getTerminalHistory(clientFetch, sessionId);
  }

  async addTerminalHistory(command: string): Promise<{ message: string; command: string }> {
    return terminalApi.addTerminalHistory(clientFetch, command);
  }

  // ── Git ───────────────────────────────────────────────────────

  async getGitStatus(): Promise<{
    message: string;
    status: {
      branch: string;
      ahead: number;
      behind: number;
      staged: Array<{ path: string; status: string; staged: boolean }>;
      modified: Array<{ path: string; status: string; staged: boolean }>;
      untracked: Array<{ path: string; status: string; staged: boolean }>;
      deleted: Array<{ path: string; status: string; staged: boolean }>;
      renamed: Array<{ path: string; status: string; staged: boolean }>;
      truncated?: boolean;
    };
    files: Array<{ path: string; status: string; staged?: boolean }>;
  }> {
    return gitApi.getGitStatus(clientFetch);
  }

  async getGitBranches(): Promise<{ message: string; current: string; branches: string[] }> {
    return gitApi.getGitBranches(clientFetch);
  }

  async checkoutGitBranch(branch: string): Promise<{ message: string; branch: string }> {
    return gitApi.checkoutGitBranch(clientFetch, branch);
  }

  async createGitBranch(name: string): Promise<{ message: string; branch: string }> {
    return gitApi.createGitBranch(clientFetch, name);
  }

  async pullGit(): Promise<{ message: string; output?: string }> {
    return gitApi.pullGit(clientFetch);
  }

  async pushGit(): Promise<{ message: string; output?: string }> {
    return gitApi.pushGit(clientFetch);
  }

  async stageFile(path: string): Promise<{ message: string; path: string }> {
    return gitApi.stageFile(clientFetch, path);
  }

  async unstageFile(path: string): Promise<{ message: string; path: string }> {
    return gitApi.unstageFile(clientFetch, path);
  }

  async discardChanges(path: string): Promise<{ message: string; path: string }> {
    return gitApi.discardChanges(clientFetch, path);
  }

  async stageAll(): Promise<{ message: string }> {
    return gitApi.stageAll(clientFetch);
  }

  async unstageAll(): Promise<{ message: string }> {
    return gitApi.unstageAll(clientFetch);
  }

  async createCommit(message: string): Promise<{ message: string; commit: string }> {
    return gitApi.createCommit(clientFetch, message);
  }

  async generateCommitMessage(): Promise<{
    message: string;
    commit_message: string;
    provider?: string;
    model?: string;
    warnings?: string[];
  }> {
    return gitApi.generateCommitMessage(clientFetch);
  }

  async getGitLog(
    limit: number,
    offset: number,
    opts?: { signal?: AbortSignal },
  ): Promise<{
    message: string;
    commits: Array<{
      hash: string;
      short_hash: string;
      author: string;
      date: string;
      message: string;
      ref_names?: string;
    }>;
    offset: number;
    limit: number;
    total: number;
  }> {
    return gitApi.getGitLog(clientFetch, limit, offset, opts);
  }

  async getGitCommitDetail(hash: string): Promise<{
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
  }> {
    return gitApi.getGitCommitDetail(clientFetch, hash);
  }

  async getGitCommitFileDiff(
    hash: string,
    path: string,
  ): Promise<{ message: string; hash: string; path: string; diff: string }> {
    return gitApi.getGitCommitFileDiff(clientFetch, hash, path);
  }

  async checkoutGitCommit(commitHash: string): Promise<{ message: string }> {
    return gitApi.checkoutGitCommit(clientFetch, commitHash);
  }

  async revertGitCommit(commitHash: string): Promise<{ message: string }> {
    return gitApi.revertGitCommit(clientFetch, commitHash);
  }

  async generateDeepReview(): Promise<{
    message: string;
    status: string;
    feedback: string;
    detailed_guidance?: string;
    suggested_new_prompt?: string;
    review_output: string;
    provider?: string;
    model?: string;
    warnings?: string[];
  }> {
    return miscApi.generateDeepReview(clientFetch);
  }

  async fixFromDeepReview(reviewOutput: string): Promise<{
    message: string;
    result: string;
  }> {
    return miscApi.fixFromDeepReview(clientFetch, reviewOutput);
  }

  async startFixFromDeepReview(
    reviewOutput: string,
    options?: { fixPrompt?: string; selectedItems?: string[] },
  ): Promise<{
    message: string;
    job_id: string;
    session_id: string;
  }> {
    return miscApi.startFixFromDeepReview(clientFetch, reviewOutput, options);
  }

  async getFixFromDeepReviewStatus(
    jobId: string,
    since = 0,
  ): Promise<{
    message: string;
    job_id: string;
    session_id: string;
    status: 'running' | 'completed' | 'error';
    logs: string[];
    next_index: number;
    result: string;
    error: string;
  }> {
    return miscApi.getFixFromDeepReviewStatus(clientFetch, jobId, since);
  }

  async getGitDiff(path: string): Promise<{
    message: string;
    path: string;
    has_staged: boolean;
    has_unstaged: boolean;
    staged_diff: string;
    unstaged_diff: string;
    diff: string;
  }> {
    return gitApi.getGitDiff(clientFetch, path);
  }

  // ── Editor/Semantic ─────────────────────────────────────────────

  async getPrettierConfig(filePath: string): Promise<Record<string, unknown>> {
    return editorApi.getPrettierConfig(clientFetch, filePath);
  }

  async getDiagnostics(
    path: string,
    content: string,
  ): Promise<{
    message: string;
    path: string;
    diagnostics: Array<{
      from: number;
      to: number;
      severity: 'error' | 'warning' | 'info';
      message: string;
      source: string;
    }>;
    version: string;
  }> {
    return editorApi.getDiagnostics(clientFetch, path, content);
  }

  async getSemanticDiagnostics(
    path: string,
    content: string,
    languageId: string,
    trigger: 'edit' | 'save' = 'edit',
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean };
    diagnostics: Array<{
      from: number;
      to: number;
      severity: 'error' | 'warning' | 'info';
      message: string;
      source: string;
    }>;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticDiagnostics(clientFetch, path, content, languageId, trigger);
  }

  async getSemanticDefinition(
    path: string,
    content: string,
    languageId: string,
    line: number,
    column: number,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean };
    definition?: { path: string; line: number; column: number } | null;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticDefinition(clientFetch, path, content, languageId, line, column);
  }

  async getSemanticHover(
    path: string,
    content: string,
    languageId: string,
    line: number,
    column: number,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean; hover: boolean };
    hover?: { contents: string } | null;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticHover(clientFetch, path, content, languageId, line, column);
  }

  async getSemanticRename(
    path: string,
    content: string,
    languageId: string,
    line: number,
    column: number,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean; hover: boolean; rename: boolean };
    rename?: { locations: Array<{ filePath: string; from: number; to: number }> } | null;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticRename(clientFetch, path, content, languageId, line, column);
  }

  async getSemanticReferences(
    path: string,
    content: string,
    languageId: string,
    line: number,
    column: number,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean; hover: boolean; rename: boolean; references: boolean };
    references?: {
      locations: Array<{ filePath: string; line: number; startCol: number; endCol: number; lineText: string }>;
      symbolName: string;
    } | null;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticReferences(clientFetch, path, content, languageId, line, column);
  }

  async getSemanticCodeActions(
    path: string,
    content: string,
    languageId: string,
    line: number,
    column: number,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: {
      diagnostics: boolean;
      definition: boolean;
      hover: boolean;
      rename: boolean;
      references: boolean;
      code_actions: boolean;
    };
    code_actions?: Array<{
      title: string;
      kind: string;
      edits: Array<{ filePath: string; from: number; to: number; newText: string }>;
    }> | null;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticCodeActions(clientFetch, path, content, languageId, line, column);
  }

  async getSemanticInlayHints(
    path: string,
    content: string,
    languageId: string,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean; inlay_hints: boolean };
    inlay_hints?: Array<{ from: number; to: number; label: string; kind: 'type' | 'parameter' | 'none' }> | null;
    duration_ms?: number;
    error?: string;
    version: string;
  }> {
    return editorApi.getSemanticInlayHints(clientFetch, path, content, languageId);
  }

  async getSemanticSignatureHelp(
    path: string,
    content: string,
    languageId: string,
    line: number,
    column: number,
  ): Promise<{
    message: string;
    path: string;
    language_id: string;
    method: string;
    capabilities: { diagnostics: boolean; definition: boolean; hover: boolean; signature_help: boolean };
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
  }> {
    return editorApi.getSemanticSignatureHelp(clientFetch, path, content, languageId, line, column);
  }

  async getWorkspaceSymbols(query: string): Promise<{
    message: string;
    files: Array<{
      file: string;
      symbols: Array<{
        name: string;
        kind: string;
        line?: number;
      }>;
    }>;
    total: number;
  }> {
    return editorApi.getWorkspaceSymbols(clientFetch, query);
  }

  // ── Agent Changes (ChangeTracker session buffer) ─────────────────

  async getAgentSessionChanges(
    filter: changesApi.SessionChangesFilter = {},
  ): Promise<changesApi.SessionChangesResponse> {
    return changesApi.getSessionChanges(clientFetch, filter);
  }

  async getAgentChangeDiff(path: string): Promise<changesApi.ChangeDiffResponse> {
    return changesApi.getChangeDiff(clientFetch, path);
  }

  async getAgentSessionSummary(): Promise<changesApi.SessionSummaryResponse> {
    return changesApi.getSessionSummary(clientFetch);
  }

  async getAgentChangesTimeline(since?: string): Promise<changesApi.TimelineResponse> {
    return changesApi.getChangesTimeline(clientFetch, since);
  }

  async revertAgentChanges(req: changesApi.RevertRequest): Promise<changesApi.RevertResponse> {
    return changesApi.revertChanges(clientFetch, req);
  }

  // ── Sessions ───────────────────────────────────────────────────

  async getSessions(scope?: string): Promise<{
    message: string;
    sessions: Array<{
      session_id: string;
      name: string;
      working_directory: string;
      last_updated: string;
      message_count: number;
      total_tokens: number;
    }>;
    current_session_id: string;
  }> {
    return sessionApi.getSessions(clientFetch, scope);
  }

  async restoreSession(sessionId: string): Promise<{
    message: string;
    session_id: string;
    message_count: number;
    messages: Array<{ role: string; content: string }>;
    total_tokens: number;
    name?: string;
    working_directory?: string;
  }> {
    return sessionApi.restoreSession(clientFetch, sessionId);
  }

  async searchSessions(
    query: string,
    options?: {
      cwd?: string;
      since?: string;
      until?: string;
      limit?: number;
    },
  ): Promise<SessionSearchResponse> {
    return sessionApi.searchSessions(clientFetch, query, options);
  }

  // ── Settings ───────────────────────────────────────────────────

  async getSettings(): Promise<SproutSettings> {
    return settingsApi.getSettings(clientFetch);
  }

  async getSettingsLayer(layer: 'global' | 'workspace' | 'session'): Promise<Record<string, unknown>> {
    return settingsApi.getSettingsLayer(clientFetch, layer);
  }

  async getSettingsProvenance(): Promise<{ sources: Record<string, string> }> {
    return settingsApi.getSettingsProvenance(clientFetch);
  }

  async updateSettings(
    settings: Record<string, unknown>,
    layer?: 'session' | 'workspace' | 'global',
  ): Promise<{ message: string }> {
    return settingsApi.updateSettings(clientFetch, settings, layer);
  }

  async getMCPSettings(): Promise<MCPSettingsResponse> {
    return settingsApi.getMCPSettings(clientFetch);
  }

  async updateMCPSettings(settings: MCPSettingsResponse): Promise<{ message: string }> {
    return settingsApi.updateMCPSettings(clientFetch, settings);
  }

  async addMCPServer(server: MCPServerConfig): Promise<{ message: string }> {
    return settingsApi.addMCPServer(clientFetch, server);
  }

  async updateMCPServer(name: string, server: MCPServerConfig): Promise<{ message: string }> {
    return settingsApi.updateMCPServer(clientFetch, name, server);
  }

  async deleteMCPServer(name: string): Promise<{ message: string }> {
    return settingsApi.deleteMCPServer(clientFetch, name);
  }

  async getCustomProviders(): Promise<CustomProvidersResponse> {
    return settingsApi.getCustomProviders(clientFetch);
  }

  async addCustomProvider(provider: CustomProviderConfig): Promise<{ message: string }> {
    return settingsApi.addCustomProvider(clientFetch, provider);
  }

  async updateCustomProvider(name: string, provider: CustomProviderConfig): Promise<{ message: string }> {
    return settingsApi.updateCustomProvider(clientFetch, name, provider);
  }

  async deleteCustomProvider(name: string): Promise<{ message: string }> {
    return settingsApi.deleteCustomProvider(clientFetch, name);
  }

  async getSkills(): Promise<SkillsResponse> {
    return settingsApi.getSkills(clientFetch);
  }

  async updateSkills(skills: SkillsResponse): Promise<{ message: string }> {
    return settingsApi.updateSkills(clientFetch, skills);
  }

  // ── SP-086-4: Skill install / manage ────────────────────────

  async listInstalledSkills(): Promise<
    Array<{
      id: string;
      origin: { type: string; installed_at?: string };
      installed_at?: string;
      updated_at?: string;
    }>
  > {
    return settingsApi.listInstalledSkills(clientFetch);
  }

  async listSkillRegistry(): Promise<import('./types/settings').SkillRegistryEntry[]> {
    return settingsApi.listSkillRegistry(clientFetch);
  }

  async installSkill(
    source: string,
    opts?: { ref?: string; force?: boolean },
  ): Promise<import('./types/settings').SkillInstallResult[]> {
    return settingsApi.installSkill(clientFetch, source, opts);
  }

  async updateSkill(id: string): Promise<import('./types/settings').SkillInstallResult[]> {
    return settingsApi.updateSkill(clientFetch, id);
  }

  async removeSkill(id: string): Promise<{ status: string; id: string }> {
    return settingsApi.removeSkill(clientFetch, id);
  }

  async getSubagentTypes(): Promise<{
    subagent_types: Record<string, SubagentTypeInfo>;
    available_providers: Array<{ id: string; name: string; models: string[] }>;
    current_provider: string;
    current_model: string;
  }> {
    return settingsApi.getSubagentTypes(clientFetch);
  }

  async getHotkeys(): Promise<HotkeyConfig> {
    return settingsApi.getHotkeys(clientFetch);
  }

  async updateHotkeys(config: HotkeyConfig): Promise<{ success: boolean; config: HotkeyConfig }> {
    return settingsApi.updateHotkeys(clientFetch, config);
  }

  async validateHotkeys(config: HotkeyConfig): Promise<{ valid: boolean; config: HotkeyConfig }> {
    return settingsApi.validateHotkeys(clientFetch, config);
  }

  async applyHotkeyPreset(preset: string): Promise<{ success: boolean; preset: string; config: HotkeyConfig }> {
    return settingsApi.applyHotkeyPreset(clientFetch, preset);
  }

  // ── Search API ───────────────────────────────────────────────────

  async search(
    query: string,
    options?: {
      case_sensitive?: boolean;
      whole_word?: boolean;
      regex?: boolean;
      include?: string;
      exclude?: string;
      max_results?: number;
      context_lines?: number;
    },
  ): Promise<{
    results: Array<{
      file: string;
      matches: Array<{
        line_number: number;
        line: string;
        column_start: number;
        column_end: number;
        context_before: string[];
        context_after: string[];
      }>;
      match_count: number;
    }>;
    total_matches: number;
    total_files: number;
    truncated: boolean;
    query: string;
  }> {
    return searchApi.search(clientFetch, query, options);
  }

  // ── Semantic Search API ──────────────────────────────────────────

  async searchSemantic(
    query: string,
    options?: {
      top_k?: number;
      threshold?: number;
    },
  ): Promise<{
    results: Array<{
      file: string;
      name: string;
      signature: string;
      start_line: number;
      end_line: number;
      language: string;
      similarity: number;
      type: string;
    }>;
    duplicate_clusters: Array<{
      files: string[];
      similarity: number;
    }>;
    query: string;
    total: number;
    duration: string;
    note?: string;
  }> {
    return searchApi.searchSemantic(clientFetch, query, options);
  }

  async searchSemanticStatus(): Promise<{
    available: boolean;
    initialized: boolean;
    building: boolean;
    record_count: number;
    workspace: string;
    init_error?: string;
  }> {
    return searchApi.searchSemanticStatus(clientFetch);
  }

  async searchSemanticBuild(): Promise<{ status: string }> {
    return searchApi.searchSemanticBuild(clientFetch);
  }

  async searchSemanticPreview(
    file: string,
    startLine: number,
    context?: number,
  ): Promise<{
    file: string;
    start_line: number;
    snippet: Array<{
      line_number: number;
      content: string;
      is_context: boolean;
    }>;
    total_lines: number;
  }> {
    return searchApi.searchSemanticPreview(clientFetch, file, startLine, context);
  }

  async searchReplace(request: {
    search: string;
    replace: string;
    files: string[];
    case_sensitive?: boolean;
    whole_word?: boolean;
    regex?: boolean;
    preview: boolean;
  }): Promise<{
    changes: Array<{
      file: string;
      matches: Array<{
        line_number: number;
        old_line: string;
        new_line: string;
        column_start: number;
        column_end: number;
      }>;
      changed_lines: number;
    }>;
    total_changes: number;
    preview: boolean;
  }> {
    return searchApi.searchReplace(clientFetch, request);
  }

  async exportSupportBundle(): Promise<void> {
    return miscApi.exportSupportBundle(clientFetch);
  }
}

export { ApiService };
