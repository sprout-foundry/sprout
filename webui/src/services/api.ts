interface StatsResponse {
  provider: string;
  model: string;
  query_count: number;
  uptime: string;
  total_tokens: number;
  total_cost: number;
  connections: number;
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
}

export { ApiService };
export type { StatsResponse, QueryRequest, FilesResponse };