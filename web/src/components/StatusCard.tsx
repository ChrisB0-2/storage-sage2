import type { StatusResponse, DaemonState } from '../api/types';

interface StatusCardProps {
  status: StatusResponse | undefined;
  isLoading: boolean;
  error: Error | null;
}

function getStateConfig(state: DaemonState) {
  switch (state) {
    case 'starting':
      return {
        color: 'bg-yellow-100 border-yellow-400 text-yellow-800',
        icon: 'text-yellow-500',
        label: 'Initializing...',
      };
    case 'ready':
      return {
        color: 'bg-green-100 border-green-400 text-green-800',
        icon: 'text-green-500',
        label: 'Idle',
      };
    case 'running':
      return {
        color: 'bg-blue-100 border-blue-400 text-blue-800',
        icon: 'text-blue-500 status-pulse',
        label: 'Cleanup in progress...',
      };
    case 'stopping':
      return {
        color: 'bg-orange-100 border-orange-400 text-orange-800',
        icon: 'text-orange-500',
        label: 'Shutting down...',
      };
    case 'stopped':
      return {
        color: 'bg-gray-100 border-gray-400 text-gray-800',
        icon: 'text-gray-500',
        label: 'Stopped',
      };
    default:
      return {
        color: 'bg-gray-100 border-gray-400 text-gray-800',
        icon: 'text-gray-500',
        label: 'Unknown',
      };
  }
}

function formatLastRun(timestamp: string): string {
  if (!timestamp) return 'Never';

  const date = new Date(timestamp);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins} minute${diffMins === 1 ? '' : 's'} ago`;
  if (diffHours < 24) return `${diffHours} hour${diffHours === 1 ? '' : 's'} ago`;
  if (diffDays < 7) return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`;

  return date.toLocaleDateString();
}

export default function StatusCard({ status, isLoading, error }: StatusCardProps) {
  if (error) {
    return (
      <div className="card border-l-4 border-red-500">
        <div className="flex items-center space-x-3">
          <svg className="w-6 h-6 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <div>
            <h3 className="font-medium text-red-800">Connection Error</h3>
            <p className="text-sm text-red-600">{error.message}</p>
          </div>
        </div>
      </div>
    );
  }

  if (isLoading || !status) {
    return (
      <div className="card">
        <div className="animate-pulse flex space-x-4">
          <div className="rounded-full bg-gray-200 h-12 w-12"></div>
          <div className="flex-1 space-y-3 py-1">
            <div className="h-4 bg-gray-200 rounded w-3/4"></div>
            <div className="h-3 bg-gray-200 rounded w-1/2"></div>
          </div>
        </div>
      </div>
    );
  }

  const config = getStateConfig(status.state);

  return (
    <div className={`card border-l-4 ${config.color}`}>
      <div className="flex items-start justify-between">
        <div className="flex items-center space-x-4">
          {/* State icon */}
          <div className={`w-12 h-12 rounded-full flex items-center justify-center ${config.color}`}>
            <svg className={`w-6 h-6 ${config.icon}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              {status.running ? (
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              ) : (
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
              )}
            </svg>
          </div>

          {/* State info */}
          <div>
            <h3 className="text-lg font-semibold">{config.label}</h3>
            <p className="text-sm text-gray-600">
              Last run: {formatLastRun(status.last_run)}
            </p>
            {status.schedule && (
              <p className="text-sm text-gray-500">
                Schedule: {status.schedule}
              </p>
            )}
          </div>
        </div>

        {/* Run count */}
        <div className="text-right">
          <p className="text-2xl font-bold text-gray-900">{status.run_count}</p>
          <p className="text-xs text-gray-500 uppercase">Total Runs</p>
        </div>
      </div>

      {/* Last error */}
      {status.last_error && (
        <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded-md">
          <div className="flex items-start space-x-2">
            <svg className="w-5 h-5 text-red-500 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <div>
              <p className="text-sm font-medium text-red-800">Last Error</p>
              <p className="text-sm text-red-600">{status.last_error}</p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
