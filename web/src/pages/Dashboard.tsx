import { useStatus } from '../hooks/useStatus';
import { useAuditStats } from '../hooks/useAuditHistory';
import StatusCard from '../components/StatusCard';
import TriggerButton from '../components/TriggerButton';
import SchedulerControl from '../components/SchedulerControl';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

function formatDate(dateStr: string): string {
  if (!dateStr || dateStr === '0001-01-01T00:00:00Z') return 'N/A';
  return new Date(dateStr).toLocaleDateString();
}

export default function Dashboard() {
  const { data: status, isLoading: statusLoading, error: statusError } = useStatus();
  const { data: stats, isLoading: statsLoading } = useAuditStats();

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold text-gray-900">Dashboard</h2>
        <p className="text-gray-600">Monitor and control the storage cleanup daemon</p>
      </div>

      {/* Status and Trigger cards */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <StatusCard
          status={status}
          isLoading={statusLoading}
          error={statusError}
        />
        <TriggerButton status={status} />
        <SchedulerControl status={status} isLoading={statusLoading} />
      </div>

      {/* Quick Stats */}
      <div>
        <h3 className="text-lg font-medium text-gray-900 mb-4">Audit Summary</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {/* Total Records */}
          <div className="card">
            <div className="flex flex-col">
              <span className="text-sm font-medium text-gray-500 uppercase">
                Total Records
              </span>
              <span className="text-2xl font-bold text-gray-900">
                {statsLoading ? (
                  <span className="animate-pulse bg-gray-200 rounded h-8 w-20 block"></span>
                ) : (
                  stats?.TotalRecords?.toLocaleString() ?? '0'
                )}
              </span>
            </div>
          </div>

          {/* Files Deleted */}
          <div className="card">
            <div className="flex flex-col">
              <span className="text-sm font-medium text-gray-500 uppercase">
                Files Deleted
              </span>
              <span className="text-2xl font-bold text-green-600">
                {statsLoading ? (
                  <span className="animate-pulse bg-gray-200 rounded h-8 w-20 block"></span>
                ) : (
                  stats?.FilesDeleted?.toLocaleString() ?? '0'
                )}
              </span>
            </div>
          </div>

          {/* Space Freed */}
          <div className="card">
            <div className="flex flex-col">
              <span className="text-sm font-medium text-gray-500 uppercase">
                Space Freed
              </span>
              <span className="text-2xl font-bold text-blue-600">
                {statsLoading ? (
                  <span className="animate-pulse bg-gray-200 rounded h-8 w-20 block"></span>
                ) : (
                  formatBytes(stats?.TotalBytesFreed ?? 0)
                )}
              </span>
            </div>
          </div>

          {/* Errors */}
          <div className="card">
            <div className="flex flex-col">
              <span className="text-sm font-medium text-gray-500 uppercase">
                Errors
              </span>
              <span className={`text-2xl font-bold ${(stats?.Errors ?? 0) > 0 ? 'text-red-600' : 'text-gray-900'}`}>
                {statsLoading ? (
                  <span className="animate-pulse bg-gray-200 rounded h-8 w-20 block"></span>
                ) : (
                  stats?.Errors?.toLocaleString() ?? '0'
                )}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Date Range */}
      {stats && (stats.FirstRecord || stats.LastRecord) && (
        <div className="card">
          <h3 className="text-lg font-medium text-gray-900 mb-2">Audit Date Range</h3>
          <div className="flex items-center space-x-4 text-sm text-gray-600">
            <div>
              <span className="font-medium">First Record:</span>{' '}
              {formatDate(stats.FirstRecord)}
            </div>
            <div className="text-gray-300">|</div>
            <div>
              <span className="font-medium">Last Record:</span>{' '}
              {formatDate(stats.LastRecord)}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
