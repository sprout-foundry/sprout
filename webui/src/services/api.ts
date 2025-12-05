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
  async getTerminalHistory(): Promise<{ history: string[]; count: number }> {
    try {
      const response = await fetch('/api/terminal/history');
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
}

export { ApiService };
export type { StatsResponse, QueryRequest, FilesResponse };