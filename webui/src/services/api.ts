interface StatsResponse {
  // Basic info
  provider: string;
  model: string;
  session_id: string;
  query_count: number;
  uptime: string;
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

  async getProviders(): Promise<{ providers: Array<{ id: string; name: string; models: string[] }> }> {
    const response = await fetch('/api/providers');
    if (!response.ok) {
      throw new Error('Failed to fetch providers');
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
      const response = await fetch('/api/history/changelog');
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
      const response = await fetch('/api/history/changes');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return await response.json();
    } catch (error) {
      console.error('Failed to get changes:', error);
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
}

export { ApiService };
export type { StatsResponse, QueryRequest, FilesResponse };
export interface ProvidersResponse {
  providers: Array<{
    id: string;
    name: string;
    models: string[];
  }>;
}