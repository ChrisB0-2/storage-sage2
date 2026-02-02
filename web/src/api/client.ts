import type {
  StatusResponse,
  TriggerResponse,
  AuditRecord,
  AuditStats,
  AuditQueryParams,
  Config,
  ApiError,
  TrashItem,
  TrashRestoreResponse,
  TrashEmptyResponse,
  SchedulerControlResponse,
} from './types';

class ApiClient {
  private baseUrl: string;

  constructor(baseUrl: string = '') {
    this.baseUrl = baseUrl;
  }

  private async fetch<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    });

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({
        error: `HTTP ${response.status}: ${response.statusText}`,
      })) as ApiError;
      throw new Error(errorData.error);
    }

    return response.json();
  }

  // Status endpoint - GET /status
  async getStatus(): Promise<StatusResponse> {
    return this.fetch<StatusResponse>('/status');
  }

  // Trigger endpoint - POST /trigger
  async trigger(): Promise<TriggerResponse> {
    return this.fetch<TriggerResponse>('/trigger', {
      method: 'POST',
    });
  }

  // Config endpoint - GET /api/config
  async getConfig(): Promise<Config> {
    return this.fetch<Config>('/api/config');
  }

  // Audit query endpoint - GET /api/audit/query
  async queryAudit(params?: AuditQueryParams): Promise<AuditRecord[]> {
    const searchParams = new URLSearchParams();
    if (params) {
      if (params.since) searchParams.set('since', params.since);
      if (params.until) searchParams.set('until', params.until);
      if (params.action) searchParams.set('action', params.action);
      if (params.level) searchParams.set('level', params.level);
      if (params.path) searchParams.set('path', params.path);
      if (params.limit !== undefined) searchParams.set('limit', String(params.limit));
    }
    const query = searchParams.toString();
    const path = query ? `/api/audit/query?${query}` : '/api/audit/query';
    return this.fetch<AuditRecord[]>(path);
  }

  // Audit stats endpoint - GET /api/audit/stats
  async getAuditStats(): Promise<AuditStats> {
    return this.fetch<AuditStats>('/api/audit/stats');
  }

  // Health check - GET /health
  async healthCheck(): Promise<{ status: string; state: string }> {
    return this.fetch('/health');
  }

  // Trash list endpoint - GET /api/trash
  async listTrash(): Promise<TrashItem[]> {
    return this.fetch<TrashItem[]>('/api/trash');
  }

  // Trash restore endpoint - POST /api/trash/restore
  async restoreTrash(name: string): Promise<TrashRestoreResponse> {
    return this.fetch<TrashRestoreResponse>('/api/trash/restore', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  }

  // Trash empty endpoint - DELETE /api/trash
  async emptyTrash(olderThan?: string): Promise<TrashEmptyResponse> {
    const params = new URLSearchParams();
    if (olderThan) {
      params.set('older_than', olderThan);
    } else {
      params.set('all', 'true');
    }
    return this.fetch<TrashEmptyResponse>(`/api/trash?${params.toString()}`, {
      method: 'DELETE',
    });
  }

  // Scheduler start endpoint - POST /api/scheduler/start
  async startScheduler(): Promise<SchedulerControlResponse> {
    return this.fetch<SchedulerControlResponse>('/api/scheduler/start', {
      method: 'POST',
    });
  }

  // Scheduler stop endpoint - POST /api/scheduler/stop
  async stopScheduler(): Promise<SchedulerControlResponse> {
    return this.fetch<SchedulerControlResponse>('/api/scheduler/stop', {
      method: 'POST',
    });
  }
}

// Export singleton instance
export const api = new ApiClient();

// Also export class for testing or custom instances
export { ApiClient };
