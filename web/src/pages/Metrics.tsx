import { useAuditStats, useAuditHistory } from '../hooks/useAuditHistory';
import type { AuditRecord } from '../api/types';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

function SimpleBarChart({ data, title }: { data: { label: string; value: number; color: string }[]; title: string }) {
  const maxValue = Math.max(...data.map(d => d.value), 1);

  return (
    <div className="card">
      <h3 className="text-lg font-medium text-gray-900 mb-4">{title}</h3>
      <div className="space-y-3">
        {data.map((item, i) => (
          <div key={i}>
            <div className="flex justify-between text-sm mb-1">
              <span className="text-gray-600">{item.label}</span>
              <span className="font-medium text-gray-900">{item.value.toLocaleString()}</span>
            </div>
            <div className="w-full bg-gray-200 rounded-full h-3">
              <div
                className={`h-3 rounded-full transition-all duration-500 ${item.color}`}
                style={{ width: `${(item.value / maxValue) * 100}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function MetricCard({ title, value, subtitle, color = 'text-gray-900' }: {
  title: string;
  value: string | number;
  subtitle?: string;
  color?: string;
}) {
  return (
    <div className="card">
      <p className="text-sm font-medium text-gray-500 uppercase tracking-wider">{title}</p>
      <p className={`text-3xl font-bold mt-2 ${color}`}>{value}</p>
      {subtitle && <p className="text-sm text-gray-500 mt-1">{subtitle}</p>}
    </div>
  );
}

function calculateRecentMetrics(records: AuditRecord[] | undefined): {
  filesDeleted: number;
  filesTrashed: number;
  filesProcessed: number;
  bytesFreed: number;
  errors: number;
  planEvents: number;
  executeEvents: number;
} {
  if (!records) {
    return { filesDeleted: 0, filesTrashed: 0, filesProcessed: 0, bytesFreed: 0, errors: 0, planEvents: 0, executeEvents: 0 };
  }

  return records.reduce((acc, record) => {
    // Count successful deletions by reason (matches Grafana)
    if (record.action === 'execute' && record.reason === 'deleted') {
      acc.filesDeleted++;
      acc.bytesFreed += record.bytes_freed ?? 0;
    }
    if (record.action === 'execute' && record.reason === 'trashed') {
      acc.filesTrashed++;
      acc.bytesFreed += record.bytes_freed ?? 0;
    }
    if (record.level === 'error') {
      acc.errors++;
    }
    if (record.action === 'plan') {
      acc.planEvents++;
    }
    if (record.action === 'execute') {
      acc.executeEvents++;
    }
    return acc;
  }, { filesDeleted: 0, filesTrashed: 0, filesProcessed: 0, bytesFreed: 0, errors: 0, planEvents: 0, executeEvents: 0 });
}

// Post-process to calculate filesProcessed
function getRecentMetrics(records: AuditRecord[] | undefined) {
  const metrics = calculateRecentMetrics(records);
  metrics.filesProcessed = metrics.filesDeleted + metrics.filesTrashed;
  return metrics;
}

function calculateLevelDistribution(records: AuditRecord[] | undefined) {
  if (!records) return [];

  const counts = records.reduce((acc, record) => {
    acc[record.level] = (acc[record.level] || 0) + 1;
    return acc;
  }, {} as Record<string, number>);

  return [
    { label: 'Info', value: counts['info'] || 0, color: 'bg-blue-500' },
    { label: 'Warning', value: counts['warn'] || 0, color: 'bg-yellow-500' },
    { label: 'Error', value: counts['error'] || 0, color: 'bg-red-500' },
  ];
}

function calculateActionDistribution(records: AuditRecord[] | undefined) {
  if (!records) return [];

  const counts = records.reduce((acc, record) => {
    acc[record.action] = (acc[record.action] || 0) + 1;
    return acc;
  }, {} as Record<string, number>);

  return [
    { label: 'Plan', value: counts['plan'] || 0, color: 'bg-purple-500' },
    { label: 'Execute', value: counts['execute'] || 0, color: 'bg-green-500' },
  ];
}

export default function Metrics() {
  const { data: stats, isLoading: statsLoading } = useAuditStats();
  const { data: recentRecords, isLoading: recordsLoading } = useAuditHistory(
    { limit: 500, since: '7d' },
    true
  );

  const isLoading = statsLoading || recordsLoading;
  const recentMetrics = getRecentMetrics(recentRecords);
  const levelDistribution = calculateLevelDistribution(recentRecords);
  const actionDistribution = calculateActionDistribution(recentRecords);

  const grafanaUrl = (import.meta as any).env?.VITE_GRAFANA_URL || 'http://localhost:3000';
  const dashboardUrl = `${grafanaUrl}/d/storage-sage-ops/storage-sage-operations?orgId=1`;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Metrics</h2>
          <p className="text-gray-600">Key metrics and activity summary</p>
        </div>
        <a
          href={dashboardUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-2 px-4 py-2 bg-orange-500 hover:bg-orange-600 text-white font-medium rounded-lg transition-colors"
        >
          <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
            <path d="M22.7 11.4L20.2 9.3L20.5 6L17.3 5.2L15.5 2.4L12.4 3.6L9.3 2.4L7.5 5.2L4.3 6L4.6 9.3L2.1 11.4L4.2 14.1L3.5 17.3L6.5 18.5L7.9 21.6L11.3 20.7L14.5 22L16.3 19.1L19.5 18.5L18.8 15.2L21.3 12.9L22.7 11.4Z"/>
          </svg>
          Open in Grafana
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
          </svg>
        </a>
      </div>

      {/* Overall Stats */}
      <div>
        <h3 className="text-lg font-medium text-gray-900 mb-4">All-Time Statistics</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <MetricCard
            title="Files Cleaned"
            value={isLoading ? '...' : (stats?.FilesProcessed?.toLocaleString() ?? '0')}
            subtitle={!isLoading && stats ? `${stats.FilesDeleted} deleted, ${stats.FilesTrashed} trashed` : undefined}
            color="text-green-600"
          />
          <MetricCard
            title="Space Freed"
            value={isLoading ? '...' : formatBytes(stats?.TotalBytesFreed ?? 0)}
            color="text-blue-600"
          />
          <MetricCard
            title="Candidates Scanned"
            value={isLoading ? '...' : (stats?.PlanEvents?.toLocaleString() ?? '0')}
            subtitle="Files evaluated"
            color="text-purple-600"
          />
          <MetricCard
            title="Total Errors"
            value={isLoading ? '...' : (stats?.Errors?.toLocaleString() ?? '0')}
            color={(stats?.Errors ?? 0) > 0 ? 'text-red-600' : 'text-gray-900'}
          />
        </div>
      </div>

      {/* Recent Activity (7 days) */}
      <div>
        <h3 className="text-lg font-medium text-gray-900 mb-4">Last 7 Days</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <MetricCard
            title="Files Cleaned"
            value={isLoading ? '...' : recentMetrics.filesProcessed.toLocaleString()}
            subtitle={!isLoading ? `${recentMetrics.filesDeleted} deleted, ${recentMetrics.filesTrashed} trashed` : undefined}
            color="text-green-600"
          />
          <MetricCard
            title="Space Freed"
            value={isLoading ? '...' : formatBytes(recentMetrics.bytesFreed)}
            color="text-blue-600"
          />
          <MetricCard
            title="Candidates Scanned"
            value={isLoading ? '...' : recentMetrics.planEvents.toLocaleString()}
            subtitle="Files evaluated"
            color="text-purple-600"
          />
          <MetricCard
            title="Errors"
            value={isLoading ? '...' : recentMetrics.errors.toLocaleString()}
            color={recentMetrics.errors > 0 ? 'text-red-600' : 'text-gray-900'}
          />
        </div>
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <SimpleBarChart
          title="Event Levels (7 days)"
          data={levelDistribution}
        />
        <SimpleBarChart
          title="Event Actions (7 days)"
          data={actionDistribution}
        />
      </div>

      {/* Activity Summary */}
      <div className="card">
        <h3 className="text-lg font-medium text-gray-900 mb-4">Activity Breakdown (7 days)</h3>
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
          <div className="text-center p-4 bg-purple-50 rounded-lg">
            <p className="text-3xl font-bold text-purple-600">{recentMetrics.planEvents}</p>
            <p className="text-sm text-purple-800 mt-1">Scanned</p>
          </div>
          <div className="text-center p-4 bg-gray-50 rounded-lg">
            <p className="text-3xl font-bold text-gray-600">{recentMetrics.executeEvents}</p>
            <p className="text-sm text-gray-800 mt-1">Attempted</p>
          </div>
          <div className="text-center p-4 bg-green-50 rounded-lg">
            <p className="text-3xl font-bold text-green-600">{recentMetrics.filesDeleted}</p>
            <p className="text-sm text-green-800 mt-1">Deleted</p>
          </div>
          <div className="text-center p-4 bg-blue-50 rounded-lg">
            <p className="text-3xl font-bold text-blue-600">{recentMetrics.filesTrashed}</p>
            <p className="text-sm text-blue-800 mt-1">Trashed</p>
          </div>
          <div className="text-center p-4 bg-red-50 rounded-lg">
            <p className="text-3xl font-bold text-red-600">{recentMetrics.errors}</p>
            <p className="text-sm text-red-800 mt-1">Errors</p>
          </div>
        </div>
      </div>

      {/* Polling indicator */}
      <div className="text-center text-sm text-gray-500">
        <span className="inline-flex items-center">
          <span className="w-2 h-2 bg-green-500 rounded-full mr-2 animate-pulse"></span>
          Auto-refreshing every 10 seconds
        </span>
      </div>
    </div>
  );
}
