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
  bytesFreed: number;
  errors: number;
  planEvents: number;
  executeEvents: number;
} {
  if (!records) {
    return { filesDeleted: 0, bytesFreed: 0, errors: 0, planEvents: 0, executeEvents: 0 };
  }

  return records.reduce((acc, record) => {
    if (record.action === 'execute' && record.bytes_freed && record.bytes_freed > 0) {
      acc.filesDeleted++;
      acc.bytesFreed += record.bytes_freed;
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
  }, { filesDeleted: 0, bytesFreed: 0, errors: 0, planEvents: 0, executeEvents: 0 });
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
  const recentMetrics = calculateRecentMetrics(recentRecords);
  const levelDistribution = calculateLevelDistribution(recentRecords);
  const actionDistribution = calculateActionDistribution(recentRecords);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold text-gray-900">Metrics</h2>
        <p className="text-gray-600">Key metrics and activity summary</p>
      </div>

      {/* Overall Stats */}
      <div>
        <h3 className="text-lg font-medium text-gray-900 mb-4">All-Time Statistics</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <MetricCard
            title="Total Records"
            value={isLoading ? '...' : (stats?.TotalRecords?.toLocaleString() ?? '0')}
            subtitle="Audit log entries"
          />
          <MetricCard
            title="Files Deleted"
            value={isLoading ? '...' : (stats?.FilesDeleted?.toLocaleString() ?? '0')}
            color="text-green-600"
          />
          <MetricCard
            title="Space Freed"
            value={isLoading ? '...' : formatBytes(stats?.TotalBytesFreed ?? 0)}
            color="text-blue-600"
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
            title="Recent Events"
            value={isLoading ? '...' : (recentRecords?.length ?? 0).toLocaleString()}
            subtitle="Audit events"
          />
          <MetricCard
            title="Files Deleted"
            value={isLoading ? '...' : recentMetrics.filesDeleted.toLocaleString()}
            color="text-green-600"
          />
          <MetricCard
            title="Space Freed"
            value={isLoading ? '...' : formatBytes(recentMetrics.bytesFreed)}
            color="text-blue-600"
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
        <h3 className="text-lg font-medium text-gray-900 mb-4">Activity Breakdown</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div className="text-center p-4 bg-purple-50 rounded-lg">
            <p className="text-3xl font-bold text-purple-600">{recentMetrics.planEvents}</p>
            <p className="text-sm text-purple-800 mt-1">Plan Events</p>
          </div>
          <div className="text-center p-4 bg-green-50 rounded-lg">
            <p className="text-3xl font-bold text-green-600">{recentMetrics.executeEvents}</p>
            <p className="text-sm text-green-800 mt-1">Execute Events</p>
          </div>
          <div className="text-center p-4 bg-blue-50 rounded-lg">
            <p className="text-3xl font-bold text-blue-600">{recentMetrics.filesDeleted}</p>
            <p className="text-sm text-blue-800 mt-1">Files Deleted</p>
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
