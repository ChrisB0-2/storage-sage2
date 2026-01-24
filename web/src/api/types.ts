// Status response from /status endpoint
export interface StatusResponse {
  state: DaemonState;
  running: boolean;
  last_run: string;
  last_error: string;
  run_count: number;
  schedule: string;
}

export type DaemonState = 'starting' | 'ready' | 'running' | 'stopping' | 'stopped';

// Trigger response from POST /trigger
export interface TriggerResponse {
  triggered: boolean;
  error?: string;
}

// Audit record from /api/audit/query
export interface AuditRecord {
  id: number;
  timestamp: string;
  level: 'info' | 'warn' | 'error';
  action: string;
  path?: string;
  mode?: string;
  decision?: string;
  reason?: string;
  score?: number;
  bytes_freed?: number;
  error?: string;
  fields?: string;
  checksum: string;
}

// Audit stats from /api/audit/stats
export interface AuditStats {
  TotalRecords: number;
  FirstRecord: string;
  LastRecord: string;
  TotalBytesFreed: number;
  FilesDeleted: number;
  Errors: number;
}

// Query filter params for /api/audit/query
export interface AuditQueryParams {
  since?: string;
  until?: string;
  action?: string;
  level?: string;
  path?: string;
  limit?: number;
}

// Config types matching Go config.Config
export interface Config {
  version: number;
  scan: ScanConfig;
  policy: PolicyConfig;
  safety: SafetyConfig;
  execution: ExecutionConfig;
  logging: LoggingConfig;
  daemon: DaemonConfig;
  metrics: MetricsConfig;
  notifications: NotificationsConfig;
}

export interface ScanConfig {
  roots: string[];
  recursive: boolean;
  max_depth: number;
  follow_symlinks: boolean;
  include_dirs: boolean;
  include_files: boolean;
}

export interface PolicyConfig {
  min_age_days: number;
  min_size_mb: number;
  extensions: string[];
  exclusions: string[];
  composite_mode: 'and' | 'or';
}

export interface SafetyConfig {
  protected_paths: string[];
  allow_dir_delete: boolean;
  enforce_mount_boundary: boolean;
}

export interface ExecutionConfig {
  mode: 'dry-run' | 'execute';
  timeout: number;
  audit_path: string;
  audit_db_path: string;
  max_items: number;
}

export interface LoggingConfig {
  level: 'debug' | 'info' | 'warn' | 'error';
  format: 'json' | 'text';
  output: string;
  loki?: LokiConfig;
}

export interface LokiConfig {
  enabled: boolean;
  url: string;
  batch_size: number;
  batch_wait: number;
  labels: Record<string, string>;
  tenant_id: string;
}

export interface DaemonConfig {
  enabled: boolean;
  http_addr: string;
  metrics_addr: string;
  schedule: string;
}

export interface MetricsConfig {
  enabled: boolean;
  namespace: string;
}

export interface NotificationsConfig {
  webhooks: WebhookConfig[];
}

export interface WebhookConfig {
  url: string;
  headers?: Record<string, string>;
  events?: string[];
  timeout?: number;
}

// API error response
export interface ApiError {
  error: string;
}

// Trash item from /api/trash
export interface TrashItem {
  name: string;
  original_path: string;
  size: number;
  trashed_at: string;
  is_dir: boolean;
}

// Trash restore request
export interface TrashRestoreRequest {
  name: string;
}

// Trash restore response
export interface TrashRestoreResponse {
  restored: boolean;
  original_path: string;
}

// Trash empty response
export interface TrashEmptyResponse {
  deleted: number;
  bytes_freed: number;
}
