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
}

interface QueryRequest {
  query: string;
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
}

export interface OnboardingStatusResponse {
  setup_required: boolean;
  reason: string;
  current_provider: string;
  current_model: string;
  providers: OnboardingProviderOption[];
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

class ApiService {
  private static instance: ApiService;

  private constructor() {}

  static getInstance(): ApiService {
    if (!ApiService.instance) {
      ApiService.instance = new ApiService();
    }
    return ApiService.instance;
  }

  async getStats(): Promise<StatsResponse> {
    const response = await fetch('/api/stats');
    if (!response.ok) {
      throw new Error('Failed to fetch stats');
    }
    return response.json();
  }

  async getProviders(): Promise<{
    providers: ProviderOption[];
    current_provider?: string;
    current_model?: string;
  }> {
    const response = await fetch('/api/providers');
    if (!response.ok) {
      throw new Error('Failed to fetch providers');
    }
    return response.json();
  }

  async getOnboardingStatus(): Promise<OnboardingStatusResponse> {
    const response = await fetch('/api/onboarding/status', { cache: 'no-store' });
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
  }): Promise<{ success: boolean; message: string; provider: string; model: string }> {
    const response = await fetch('/api/onboarding/complete', {
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
    const response = await fetch('/api/files');
    if (!response.ok) {
      throw new Error('Failed to fetch files');
    }
    return response.json();
  }

  async createItem(path: string, isDirectory = false): Promise<{ message: string; path: string }> {
    const response = await fetch('/api/create', {
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
    const response = await fetch('/api/delete', {
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
    const response = await fetch('/api/rename', {
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
    const response = await fetch('/api/instances');
    if (!response.ok) {
      throw new Error('Failed to fetch instances');
    }
    return response.json();
  }

  async selectInstance(pid: number): Promise<{ message: string; pid: number }> {
    const response = await fetch('/api/instances/select', {
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

  async sendQuery(query: string): Promise<void> {
    const response = await fetch('/api/query', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ query } as QueryRequest),
    });

    if (!response.ok) {
      throw new Error('Failed to send query');
    }
  }

  async uploadImage(file: File | Blob): Promise<{ path: string; filename: string }> {
    const formData = new FormData();
    formData.append('image', file);
    const response = await fetch('/api/upload/image', {
      method: 'POST',
      body: formData,
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || 'Failed to upload image');
    }
    return response.json();
  }

  async steerQuery(query: string): Promise<void> {
    const response = await fetch('/api/query/steer', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ query } as QueryRequest),
    });

    if (!response.ok) {
      const errText = await response.text();
      throw new Error(errText || 'Failed to steer query');
    }
  }

  async checkHealth(): Promise<boolean> {
    try {
      const response = await fetch('/');
      return response.ok;
    } catch {
      return false;
    }
  }

  // Get terminal history
  async getTerminalHistory(sessionId?: string): Promise<{ history: string[]; count: number }> {
    try {
      const url = sessionId ? `/api/terminal/history?session_id=${encodeURIComponent(sessionId)}` : '/api/terminal/history';
      const response = await fetch(url);
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
      const response = await fetch('/api/terminal/history', {
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
    };
    files: Array<{ path: string; status: string; staged?: boolean }>;
  }> {
    try {
      const response = await fetch('/api/git/status');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get git status:', error);
      throw error;
    }
  }

  async stageFile(path: string): Promise<{ message: string; path: string }> {
    try {
      const response = await fetch('/api/git/stage', {
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
      const response = await fetch('/api/git/unstage', {
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
      const response = await fetch('/api/git/discard', {
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
      const response = await fetch('/api/git/stage-all', {
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
      const response = await fetch('/api/git/unstage-all', {
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
      const response = await fetch('/api/git/commit', {
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
  }> {
    try {
      const response = await fetch('/api/git/commit-message', {
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

  async generateDeepReview(): Promise<{
    message: string;
    status: string;
    feedback: string;
    detailed_guidance?: string;
    suggested_new_prompt?: string;
    review_output: string;
    provider?: string;
    model?: string;
  }> {
    try {
      const response = await fetch('/api/git/deep-review', {
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
      const response = await fetch('/api/git/deep-review/fix', {
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

  async startFixFromDeepReview(reviewOutput: string): Promise<{
    message: string;
    job_id: string;
    session_id: string;
  }> {
    try {
      const response = await fetch('/api/git/deep-review/fix/start', {
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
      const response = await fetch(`/api/git/deep-review/fix/status?job_id=${encodeURIComponent(jobId)}&since=${since}`);
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
      const response = await fetch(`/api/git/diff?path=${encodeURIComponent(path)}`);
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get git diff:', error);
      throw error;
    }
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
      const response = await fetch(`/api/history/changelog?_=${cacheBuster}`, {
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
      const response = await fetch(`/api/history/changes?_=${cacheBuster}`, {
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
      const response = await fetch(
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
      const response = await fetch('/api/history/rollback', {
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
      const response = await fetch(url);
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
      const response = await fetch('/api/sessions/restore', {
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
      const response = await fetch('/api/settings');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get settings:', error);
      throw error;
    }
  }

  async updateSettings(settings: Record<string, any>): Promise<{ message: string }> {
    try {
      const response = await fetch('/api/settings', {
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
      const response = await fetch('/api/settings/mcp');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get MCP settings:', error);
      throw error;
    }
  }

  async updateMCPSettings(settings: any): Promise<{ message: string }> {
    try {
      const response = await fetch('/api/settings/mcp', {
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
      const response = await fetch('/api/settings/mcp/servers/', {
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
      const response = await fetch(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, {
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
      const response = await fetch(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, {
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
      const response = await fetch('/api/settings/providers');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get custom providers:', error);
      throw error;
    }
  }

  async addCustomProvider(provider: any): Promise<{ message: string }> {
    try {
      const response = await fetch('/api/settings/providers', {
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
      const response = await fetch(`/api/settings/providers/${encodeURIComponent(name)}`, {
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
      const response = await fetch(`/api/settings/providers/${encodeURIComponent(name)}`, {
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
      const response = await fetch('/api/settings/skills');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get skills:', error);
      throw error;
    }
  }

  async updateSkills(skills: any): Promise<{ message: string }> {
    try {
      const response = await fetch('/api/settings/skills', {
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

  // ── Hotkeys API ──────────────────────────────────────────────────

  async getHotkeys(): Promise<HotkeyConfig> {
    try {
      const response = await fetch('/api/hotkeys');
      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
      return await response.json();
    } catch (error) {
      console.error('Failed to get hotkeys:', error);
      throw error;
    }
  }

  async updateHotkeys(config: HotkeyConfig): Promise<{ success: boolean; config: HotkeyConfig }> {
    try {
      const response = await fetch('/api/hotkeys', {
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
      const response = await fetch('/api/hotkeys/validate', {
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
      const response = await fetch('/api/hotkeys/preset', {
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

      const response = await fetch(`/api/search?${params}`);
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
      const response = await fetch('/api/search/replace', {
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
  skip_prompt: boolean;
  enable_pre_write_validation: boolean;
  enable_zsh_command_detection: boolean;
  auto_execute_detected_commands: boolean;
  history_scope: string;
  self_review_gate_mode: string;
  subagent_provider: string;
  subagent_model: string;
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
