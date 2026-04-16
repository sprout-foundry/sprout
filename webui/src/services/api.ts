import { clientFetch } from './clientSession';

interface StatsResponse {
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

interface QueryRequest {
  query: string;
  chat_id?: string;
}

interface FilesResponse {
  message: string;
  files: Array<{
    path: string;
    modified: boolean;
    content?: string;
  }>;
}

interface SearchMatch {
  line_number: number;
  line: string;
  column_start: number;
  column_end: number;
  context_before: string[];
  context_after: string[];
}

interface SearchResult {
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
}

export interface OnboardingStatusResponse {
  setup_required: boolean;
  reason: string;
  current_provider: string;
  current_model: string;
  providers: OnboardingProviderOption[];
  environment?: OnboardingEnvironment;
}

export interface LeditInstance {
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
  port: number;
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
  updated_at: string;
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

class ApiService {
  private static readonly SSH_OPEN_TIMEOUT_MS = 90_000;

  private static instance: ApiService;

  private constructor() {}

  static getInstance(): ApiService {
    if (!ApiService.instance) {
      ApiService.instance = new ApiService();
    }
    return ApiService.instance;
  }

  private parseWorkspacePayload(text: string): any {
    const trimmed = text.trim();
    if (!trimmed) {
      return {};
    }
    try {
      return JSON.parse(trimmed);
    } catch {
      return { message: trimmed };
    }
  }

  private isHTMLResponseBody(text: string): boolean {
    const trimmed = text.trim().toLowerCase();
    return (
      trimmed.startsWith('<!doctype html') ||
      trimmed.startsWith('<html') ||
      trimmed.startsWith('<head') ||
      trimmed.startsWith('<body')
    );
  }

  async getStats(): Promise<StatsResponse> {
    const response = await clientFetch('/api/stats');
    if (!response.ok) {
      throw new Error('Failed to fetch stats');
    }
    return response.json();
  }

  async getWorkspace(): Promise<WorkspaceResponse> {
    const response = await clientFetch('/api/workspace');
    const text = await response.text();
    const data = this.parseWorkspacePayload(text);

    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to fetch workspace');
    }

    if (this.isHTMLResponseBody(text)) {
      throw new Error('Workspace API returned HTML response');
    }

    if (data && typeof data === 'object' && 'workspace_root' in data && 'daemon_root' in data) {
      return data as WorkspaceResponse;
    }

    throw new Error('Workspace API returned malformed response');
  }

  async setWorkspace(path: string): Promise<WorkspaceResponse & { message: string }> {
    const response = await clientFetch('/api/workspace', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ path }),
    });

    const text = await response.text();
    const data = this.parseWorkspacePayload(text);

    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to update workspace');
    }

    if (this.isHTMLResponseBody(text)) {
      throw new Error('Workspace API returned HTML response');
    }

    if (data && typeof data === 'object' && 'workspace_root' in data && 'daemon_root' in data) {
      return data as WorkspaceResponse & { message: string };
    }

    // Some remote/proxy setups respond to workspace set with a non-JSON success body.
    // Fall back to querying current workspace so switching can continue.
    const workspace = await this.getWorkspace();
    return {
      ...workspace,
      message: data.message || 'Workspace updated',
    };
  }

  async getTerminalSessionCount(): Promise<number> {
    const response = await clientFetch('/api/terminal/sessions');
    if (!response.ok) throw new Error('Failed to fetch terminal sessions');
    const data = await response.json();
    return data.active_count ?? data.count ?? 0;
  }

  async getAvailableShells(): Promise<{ shells: ShellInfo[] }> {
    const response = await clientFetch('/api/terminal/shells');
    if (!response.ok) throw new Error('Failed to fetch available shells');
    return response.json();
  }

  async getProviders(): Promise<{
    providers: ProviderOption[];
    current_provider?: string;
    current_model?: string;
  }> {
    const response = await clientFetch('/api/providers');
    if (!response.ok) {
      throw new Error('Failed to fetch providers');
    }
    return response.json();
  }

  async getProviderCredentials(): Promise<{ storage_backend: string; providers: Array<{ provider: string; display_name: string; env_var: string; requires_api_key: boolean; has_stored_credential: boolean; has_env_credential: boolean; credential_source: string; masked_value: string; key_pool_size: number }> }> {
    const response = await clientFetch('/api/settings/credentials');
    if (!response.ok) {
      throw new Error(`Failed to fetch provider credentials: HTTP ${response.status}`);
    }
    return response.json();
  }

  async setProviderCredential(provider: string, value: string): Promise<void> {
    const response = await clientFetch(`/api/settings/credentials/${encodeURIComponent(provider)}/`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value }),
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Failed to set credential: HTTP ${response.status}`);
    }
  }

  async deleteProviderCredential(provider: string): Promise<void> {
    const response = await clientFetch(`/api/settings/credentials/${encodeURIComponent(provider)}/`, {
      method: 'DELETE',
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Failed to delete credential: HTTP ${response.status}`);
    }
  }

  async testProviderConnection(provider: string): Promise<{ success: boolean; error?: string; model_count?: number }> {
    const response = await clientFetch(`/api/settings/credentials/${encodeURIComponent(provider)}/test`, {
      method: 'POST',
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Failed to test provider: HTTP ${response.status}`);
    }
    return response.json();
  }

  async getKeyPool(provider: string): Promise<{ provider: string; key_count: number; masked_keys: string[] }> {
    const response = await clientFetch(`/api/settings/credentials/${encodeURIComponent(provider)}/pool`);
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Failed to get key pool: HTTP ${response.status}`);
    }
    return response.json();
  }

  async addKeyToPool(provider: string, value: string): Promise<void> {
    const response = await clientFetch(`/api/settings/credentials/${encodeURIComponent(provider)}/pool`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value }),
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Failed to add key to pool: HTTP ${response.status}`);
    }
  }

  async removeKeyFromPool(provider: string, index: number): Promise<void> {
    const response = await clientFetch(`/api/settings/credentials/${encodeURIComponent(provider)}/pool`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index }),
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Failed to remove key from pool: HTTP ${response.status}`);
    }
  }

  async getOnboardingStatus(): Promise<OnboardingStatusResponse> {
    const response = await clientFetch('/api/onboarding/status', { cache: 'no-store' });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || 'Failed to fetch onboarding status');
    }
    return response.json();
  }

  async completeOnboarding(payload: {
    provider: string;
    model?: string;
    api_key?: string;
  }): Promise<{ success: boolean; message: string; provider: string; model: string; validation?: { tested: boolean; model_count?: number } }> {
    const response = await clientFetch('/api/onboarding/complete', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(payload),
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || 'Failed to complete onboarding');
    }
    return response.json();
  }

  async getFiles(): Promise<FilesResponse> {
    const response = await clientFetch('/api/files');
    if (!response.ok) {
      throw new Error('Failed to fetch files');
    }
    return response.json();
  }

  async createItem(path: string, isDirectory = false): Promise<{ message: string; path: string }> {
    const response = await clientFetch('/api/create', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(isDirectory ? { directory: path, path } : { path }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to create item');
    }
    return data;
  }

  async deleteItem(path: string): Promise<{ message: string; path: string }> {
    const response = await clientFetch('/api/delete', {
      method: 'DELETE',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ path }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to delete item');
    }
    return data;
  }

  async renameItem(oldPath: string, newPath: string): Promise<{ message: string; old_path: string; new_path: string }> {
    const response = await clientFetch('/api/rename', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ old_path: oldPath, new_path: newPath }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to rename item');
    }
    return data;
  }

  async getInstances(): Promise<{
    instances: LeditInstance[];
    current_pid: number;
    active_host_pid: number;
    active_host_port: number;
    desired_host_pid: number;
  }> {
    const response = await clientFetch('/api/instances');
    if (!response.ok) {
      throw new Error('Failed to fetch instances');
    }
    return response.json();
  }

  async getSSHHosts(): Promise<SSHHostEntry[]> {
    const response = await clientFetch('/api/instances/ssh-hosts');
    if (!response.ok) {
      throw new Error('Failed to fetch SSH hosts');
    }
    const data = await response.json();
    return Array.isArray(data.hosts) ? data.hosts : [];
  }

  async openSSHWorkspace(hostAlias: string, remoteWorkspacePath?: string): Promise<SSHOpenResponse> {
    const controller = new AbortController();
    const timeoutId = window.setTimeout(() => controller.abort(), ApiService.SSH_OPEN_TIMEOUT_MS);

    let response: Response;
    try {
      response = await clientFetch('/api/instances/ssh-open', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          host_alias: hostAlias,
          remote_workspace_path: remoteWorkspacePath,
        }),
        signal: controller.signal,
      });
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw new SSHWorkspaceOpenError({
          error: 'SSH workspace launch timed out. Check SSH connectivity and ~/.ledit/workspace.log for details.',
          step: 'launch-timeout',
          details: `No response from /api/instances/ssh-open after ${Math.round(ApiService.SSH_OPEN_TIMEOUT_MS / 1000)} seconds.`,
        });
      }
      throw error;
    } finally {
      window.clearTimeout(timeoutId);
    }

    const text = await response.text();
    let data: any = {};
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = { message: text };
      }
    }

    if (!response.ok) {
      throw new SSHWorkspaceOpenError({
        error: data.error || data.message || 'Failed to open SSH workspace',
        step: data.step,
        details: data.details,
        log_path: data.log_path,
      });
    }

    return data;
  }

  async getSSHLaunchStatus(hostAlias: string, remoteWorkspacePath?: string): Promise<SSHLaunchStatus> {
    const params = new URLSearchParams();
    params.set('host_alias', hostAlias);
    if (remoteWorkspacePath) {
      params.set('remote_workspace_path', remoteWorkspacePath);
    }

    const response = await clientFetch(`/api/instances/ssh-launch-status?${params.toString()}`);
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to fetch SSH launch status');
    }
    return data as SSHLaunchStatus;
  }

  async getSSHSessions(): Promise<SSHSessionEntry[]> {
    const response = await clientFetch('/api/instances/ssh-sessions');
    if (!response.ok) {
      throw new Error('Failed to fetch SSH sessions');
    }
    const data = await response.json();
    return Array.isArray(data.sessions) ? data.sessions : [];
  }

  async browseSSHDirectory(
    hostAlias: string,
    path?: string
  ): Promise<{ path: string; home_path?: string; files: SSHBrowseEntry[] }> {
    const response = await clientFetch('/api/instances/ssh-browse', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        host_alias: hostAlias,
        path,
      }),
    });

    const text = await response.text();
    let data: any = {};
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = { message: text };
      }
    }

    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to browse SSH directory');
    }

    return {
      path: typeof data.path === 'string' ? data.path : '',
      home_path: typeof data.home_path === 'string' ? data.home_path : undefined,
      files: Array.isArray(data.files) ? data.files : [],
    };
  }

  async closeSSHSession(key: string): Promise<{ message: string; key: string }> {
    const response = await clientFetch('/api/instances/ssh-close', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ key }),
    });
    const text = await response.text();
    let data: any = {};
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = { message: text };
      }
    }
    if (!response.ok) {
      throw new Error(data.error || data.message || 'Failed to close SSH session');
    }
    return data;
  }

  async selectInstance(pid: number): Promise<{ message: string; pid: number }> {
    const response = await clientFetch('/api/instances/select', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ pid }),
    });
    if (!response.ok) {
      throw new Error('Failed to select instance');
    }
    return response.json();
  }

  async sendQuery(query: string, chatId?: string): Promise<void> {
    const reqBody: QueryRequest = { query };
    if (chatId) reqBody.chat_id = chatId;
    const response = await clientFetch('/api/query', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(reqBody),
    });

    if (!response.ok) {
      throw new Error('Failed to send query');
    }
  }

  async uploadImage(file: File | Blob): Promise<{ path: string; filename: string }> {
    const formData = new FormData();
    formData.append('image', file);
    const response = await clientFetch('/api/upload/image', {
      method: 'POST',
      body: formData,
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || 'Failed to upload image');
    }
    return response.json();
  }

  async steerQuery(query: string, chatId?: string): Promise<void> {
    const reqBody: QueryRequest = { query };
    if (chatId) reqBody.chat_id = chatId;
    const response = await clientFetch('/api/query/steer', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(reqBody),
    });

    if (!response.ok) {
      const errText = await response.text();
      throw new Error(errText || 'Failed to steer query');
    }
  }

  async stopQuery(): Promise<void> {
    const response = await clientFetch('/api/query/stop', {
      method: 'POST',
    });

    if (!response.ok) {
      const errText = await response.text();
      throw new Error(errText || 'Failed to stop query');
    }
  }

  async checkHealth(): Promise<boolean> {
    try {
      const response = await clientFetch('/');
      return response.ok;
    } catch {
      return false;
    }
  }

  // Get terminal history
  async getTerminalHistory(sessionId?: string): Promise<{ history: string[]; count: number }> {
    try {
      const url = sessionId ? `/api/terminal/history?session_id=${encodeURIComponent(sessionId)}` : '/api/terminal/history';
      const response = await clientFetch(url);
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get terminal history:', error);
      throw error;
    }
  }

  // Add command to terminal history
  async addTerminalHistory(command: string): Promise<{ message: string; command: string }> {
    try {
      const response = await clientFetch('/api/terminal/history', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ command }),
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to add terminal history:', error);
      throw error;
    }
  }

  // Git API methods
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
    try {
      const response = await clientFetch('/api/git/status');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get git status:', error);
      throw error;
    }
  }

  async getGitBranches(): Promise<{ message: string; current: string; branches: string[] }> {
    const response = await clientFetch('/api/git/branches');
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return response.json();
  }

  async checkoutGitBranch(branch: string): Promise<{ message: string; branch: string }> {
    const response = await clientFetch('/api/git/checkout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || `HTTP error! status: ${response.status}`);
    }
    return data;
  }

  async createGitBranch(name: string): Promise<{ message: string; branch: string }> {
    const response = await clientFetch('/api/git/branch/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || `HTTP error! status: ${response.status}`);
    }
    return data;
  }

  async pullGit(): Promise<{ message: string; output?: string }> {
    const response = await clientFetch('/api/git/pull', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || `HTTP error! status: ${response.status}`);
    }
    return data;
  }

  async pushGit(): Promise<{ message: string; output?: string }> {
    const response = await clientFetch('/api/git/push', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || data.message || `HTTP error! status: ${response.status}`);
    }
    return data;
  }

  async stageFile(path: string): Promise<{ message: string; path: string }> {
    try {
      const response = await clientFetch('/api/git/stage', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to stage file:', error);
      throw error;
    }
  }

  async unstageFile(path: string): Promise<{ message: string; path: string }> {
    try {
      const response = await clientFetch('/api/git/unstage', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to unstage file:', error);
      throw error;
    }
  }

  async discardChanges(path: string): Promise<{ message: string; path: string }> {
    try {
      const response = await clientFetch('/api/git/discard', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to discard changes:', error);
      throw error;
    }
  }

  async stageAll(): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/git/stage-all', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to stage all:', error);
      throw error;
    }
  }

  async unstageAll(): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/git/unstage-all', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to unstage all:', error);
      throw error;
    }
  }

  async createCommit(message: string): Promise<{ message: string; commit: string }> {
    try {
      const response = await clientFetch('/api/git/commit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message }),
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to create commit:', error);
      throw error;
    }
  }

  async generateCommitMessage(): Promise<{
    message: string;
    commit_message: string;
    provider?: string;
    model?: string;
    warnings?: string[];
  }> {
    try {
      const response = await clientFetch('/api/git/commit-message', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to generate commit message:', error);
      throw error;
    }
  }

  async getGitLog(limit: number, offset: number, opts?: { signal?: AbortSignal }): Promise<{
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
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    const response = await clientFetch(`/api/git/log?${params.toString()}`, { signal: opts?.signal });
    if (!response.ok) {
      throw new Error(`Failed to get git log: HTTP ${response.status}`);
    }
    return response.json();
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
    const params = new URLSearchParams({ hash });
    const response = await clientFetch(`/api/git/commit/show?${params.toString()}`);
    if (!response.ok) {
      throw new Error(`Failed to get commit detail: HTTP ${response.status}`);
    }
    return response.json();
  }

  async getGitCommitFileDiff(hash: string, path: string): Promise<{ message: string; hash: string; path: string; diff: string }> {
    const params = new URLSearchParams({ hash, path });
    const response = await clientFetch(`/api/git/commit/show/file?${params.toString()}`);
    if (!response.ok) {
      throw new Error(`Failed to get commit file diff: HTTP ${response.status}`);
    }
    return response.json();
  }

  async checkoutGitCommit(commitHash: string): Promise<{ message: string }> {
    const response = await clientFetch('/api/git/checkout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch: commitHash }),
    });
    if (!response.ok) {
      throw new Error(`Failed to checkout: HTTP ${response.status}`);
    }
    return response.json();
  }

  async revertGitCommit(commitHash: string): Promise<{ message: string }> {
    const response = await clientFetch('/api/git/revert', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit: commitHash }),
    });
    if (!response.ok) {
      throw new Error(`Failed to revert commit: HTTP ${response.status}`);
    }
    return response.json();
  }

  async skipOnboarding(): Promise<void> {
    const response = await clientFetch('/api/onboarding/skip', {
      method: 'POST',
    });
    if (!response.ok) {
      throw new Error(`Failed to skip onboarding: HTTP ${response.status}`);
    }
  }

  async getMCPServerCredentials(serverName: string): Promise<{ server: string; credentials: Record<string, { status: string; has_value: boolean }> }> {
    const response = await clientFetch(`/api/settings/mcp/servers/${encodeURIComponent(serverName)}/credentials`);
    if (!response.ok) {
      throw new Error(`Failed to get MCP server credentials: HTTP ${response.status}`);
    }
    return response.json();
  }

  async updateMCPServerCredentials(serverName: string, credentials: Record<string, string>): Promise<{ success: boolean; server: string }> {
    const response = await clientFetch(`/api/settings/mcp/servers/${encodeURIComponent(serverName)}/credentials`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ credentials }),
    });
    if (!response.ok) {
      throw new Error(`Failed to update MCP server credentials: HTTP ${response.status}`);
    }
    return response.json();
  }

  async deleteMCPServerCredential(serverName: string, credentialName: string): Promise<void> {
    const response = await clientFetch(`/api/settings/mcp/servers/${encodeURIComponent(serverName)}/credentials`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ credential_name: credentialName }),
    });
    if (!response.ok) {
      throw new Error(`Failed to delete MCP server credential: HTTP ${response.status}`);
    }
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
    try {
      const response = await clientFetch('/api/git/deep-review', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to generate deep review:', error);
      throw error;
    }
  }

  async fixFromDeepReview(reviewOutput: string): Promise<{
    message: string;
    result: string;
  }> {
    try {
      const response = await clientFetch('/api/git/deep-review/fix', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ review_output: reviewOutput }),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to run deep review fix:', error);
      throw error;
    }
  }

  async startFixFromDeepReview(reviewOutput: string, options?: { fixPrompt?: string; selectedItems?: string[] }): Promise<{
    message: string;
    job_id: string;
    session_id: string;
  }> {
    try {
      const response = await clientFetch('/api/git/deep-review/fix/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ review_output: reviewOutput }),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to start deep review fix:', error);
      throw error;
    }
  }

  async getFixFromDeepReviewStatus(jobId: string, since = 0): Promise<{
    message: string;
    job_id: string;
    session_id: string;
    status: 'running' | 'completed' | 'error';
    logs: string[];
    next_index: number;
    result: string;
    error: string;
  }> {
    try {
      const response = await clientFetch(`/api/git/deep-review/fix/status?job_id=${encodeURIComponent(jobId)}&since=${since}`);
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to fetch deep review fix status:', error);
      throw error;
    }
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
    try {
      const response = await clientFetch(`/api/git/diff?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get git diff:', error);
      throw error;
    }
  }

  async getDiagnostics(path: string, content: string): Promise<{
    message: string;
    path: string;
    diagnostics: Array<{ from: number; to: number; severity: 'error' | 'warning' | 'info'; message: string; source: string }>;
    version: string;
  }> {
    const response = await clientFetch('/api/diagnostics', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, content }),
    });
    if (!response.ok) {
      throw new Error(`Failed to get diagnostics: HTTP ${response.status}`);
    }
    return response.json();
  }

  // History and Rollback API methods
  async getChangelog(): Promise<{
    message: string;
    revisions: Array<{
      revision_id: string;
      timestamp: string;
      files: Array<{
        path: string;
        operation: string;
        lines_added: number;
        lines_deleted: number;
      }>;
      description: string;
    }>;
  }> {
    try {
      const cacheBuster = Date.now();
      const response = await clientFetch(`/api/history/changelog?_=${cacheBuster}`, {
        cache: 'no-store',
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get changelog:', error);
      throw error;
    }
  }

  async getChanges(): Promise<{
    message: string;
    changes: Array<{
      revision_id: string;
      timestamp: string;
      files: Array<{
        path: string;
        operation: string;
        lines_added: number;
        lines_deleted: number;
      }>;
      description: string;
    }>;
  }> {
    try {
      const cacheBuster = Date.now();
      const response = await clientFetch(`/api/history/changes?_=${cacheBuster}`, {
        cache: 'no-store',
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get changes:', error);
      throw error;
    }
  }

  async getRevisionDetails(revisionId: string): Promise<{
    message: string;
    revision: {
      revision_id: string;
      timestamp: string;
      description: string;
      files: Array<{
        file_revision_hash?: string;
        path: string;
        operation: string;
        lines_added: number;
        lines_deleted: number;
        original_code: string;
        new_code: string;
        diff: string;
      }>;
    };
  }> {
    try {
      const cacheBuster = Date.now();
      const response = await clientFetch(
        `/api/history/revision?revision_id=${encodeURIComponent(revisionId)}&_=${cacheBuster}`,
        { cache: 'no-store' },
      );
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get revision details:', error);
      throw error;
    }
  }

  async rollbackToRevision(revisionId: string): Promise<{ message: string; revision_id: string }> {
    try {
      const response = await clientFetch('/api/history/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ revision_id: revisionId }),
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to rollback:', error);
      throw error;
    }
  }

  // ── Session History API ─────────────────────────────────────────

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
    try {
      const params = new URLSearchParams();
      if (scope) params.set('scope', scope);
      const url = `/api/sessions${params.toString() ? '?' + params.toString() : ''}`;
      const response = await clientFetch(url);
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get sessions:', error);
      throw error;
    }
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
    try {
      const response = await clientFetch('/api/sessions/restore', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId }),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to restore session:', error);
      throw error;
    }
  }

  // ── Settings API ─────────────────────────────────────────────────

  async getSettings(): Promise<LeditSettings> {
    try {
      const response = await clientFetch('/api/settings');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get settings:', error);
      throw error;
    }
  }

  async updateSettings(settings: Record<string, any>): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to update settings:', error);
      throw error;
    }
  }

  async getMCPSettings(): Promise<any> {
    try {
      const response = await clientFetch('/api/settings/mcp');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get MCP settings:', error);
      throw error;
    }
  }

  async updateMCPSettings(settings: any): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/settings/mcp', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to update MCP settings:', error);
      throw error;
    }
  }

  async addMCPServer(server: any): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/settings/mcp/servers/', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(server),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to add MCP server:', error);
      throw error;
    }
  }

  async updateMCPServer(name: string, server: any): Promise<{ message: string }> {
    try {
      const response = await clientFetch(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(server),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to update MCP server:', error);
      throw error;
    }
  }

  async deleteMCPServer(name: string): Promise<{ message: string }> {
    try {
      const response = await clientFetch(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to delete MCP server:', error);
      throw error;
    }
  }

  async getCustomProviders(): Promise<any> {
    try {
      const response = await clientFetch('/api/settings/providers');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get custom providers:', error);
      throw error;
    }
  }

  async addCustomProvider(provider: any): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/settings/providers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(provider),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to add custom provider:', error);
      throw error;
    }
  }

  async updateCustomProvider(name: string, provider: any): Promise<{ message: string }> {
    try {
      const response = await clientFetch(`/api/settings/providers/${encodeURIComponent(name)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(provider),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to update custom provider:', error);
      throw error;
    }
  }

  async deleteCustomProvider(name: string): Promise<{ message: string }> {
    try {
      const response = await clientFetch(`/api/settings/providers/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to delete custom provider:', error);
      throw error;
    }
  }

  async getSkills(): Promise<any> {
    try {
      const response = await clientFetch('/api/settings/skills');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get skills:', error);
      throw error;
    }
  }

  async updateSkills(skills: any): Promise<{ message: string }> {
    try {
      const response = await clientFetch('/api/settings/skills', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(skills),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to update skills:', error);
      throw error;
    }
  }

  // ── Subagent Types API ──────────────────────────────────────────

  async getSubagentTypes(): Promise<{
    subagent_types: Record<string, {
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
    }>;
    available_providers: Array<{ id: string; name: string; models: string[] }>;
    current_provider: string;
    current_model: string;
  }> {
    try {
      const response = await clientFetch('/api/settings/subagent-types');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get subagent types:', error);
      throw error;
    }
  }

  async updateSubagentType(
    name: string,
    updates: { provider?: string; model?: string },
  ): Promise<{ success: boolean; type: any }> {
    try {
      const response = await clientFetch(`/api/settings/subagent-types/${encodeURIComponent(name)}/`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updates),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to update subagent type:', error);
      throw error;
    }
  }

  // ── Hotkeys API ──────────────────────────────────────────────────

  async getHotkeys(): Promise<HotkeyConfig> {
    try {
      const response = await clientFetch('/api/hotkeys');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get hotkeys:', error);
      throw error;
    }
  }

  async updateHotkeys(config: HotkeyConfig): Promise<{ success: boolean; config: HotkeyConfig }> {
    try {
      const response = await clientFetch('/api/hotkeys', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to update hotkeys:', error);
      throw error;
    }
  }

  async validateHotkeys(config: HotkeyConfig): Promise<{ valid: boolean; config: HotkeyConfig }> {
    try {
      const response = await clientFetch('/api/hotkeys/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      });
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to validate hotkeys:', error);
      throw error;
    }
  }

  async applyHotkeyPreset(preset: string): Promise<{ success: boolean; preset: string; config: HotkeyConfig }> {
    try {
      const response = await clientFetch('/api/hotkeys/preset', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ preset }),
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to apply hotkey preset:', error);
      throw error;
    }
  }

  // ── Search API ───────────────────────────────────────────────────

  async search(query: string, options?: {
    case_sensitive?: boolean;
    whole_word?: boolean;
    regex?: boolean;
    include?: string;
    exclude?: string;
    max_results?: number;
    context_lines?: number;
  }): Promise<{
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
    try {
      const params = new URLSearchParams({ query });
      if (options?.case_sensitive) params.set('case_sensitive', 'true');
      if (options?.whole_word) params.set('whole_word', 'true');
      if (options?.regex) params.set('regex', 'true');
      if (options?.include) params.set('include', options.include);
      if (options?.exclude) params.set('exclude', options.exclude);
      if (options?.max_results) params.set('max_results', String(options.max_results));
      if (options?.context_lines != null) params.set('context_lines', String(options.context_lines));

      const response = await clientFetch(`/api/search?${params}`);
      if (!response.ok) {
        throw new Error(`Search failed: ${response.statusText}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to search:', error);
      throw error;
    }
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
    try {
      const response = await clientFetch('/api/search/replace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(request),
      });
      if (!response.ok) {
        throw new Error(`Replace failed: ${response.statusText}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to replace:', error);
      throw error;
    }
  }
}

export { ApiService };
export type { StatsResponse, QueryRequest, FilesResponse, SearchMatch, SearchResult };
export interface ProvidersResponse {
  providers: Array<{
    id: string;
    name: string;
    models: string[];
  }>;
}

// ── Settings interfaces ───────────────────────────────────────────

export interface LeditSettings {
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

// ── Hotkeys interfaces ─────────────────────────────────────────────

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
