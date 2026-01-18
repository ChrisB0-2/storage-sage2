import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';
import ConfigViewer from '../components/ConfigViewer';
import type { Config } from '../api/types';

export default function Config() {
  const { data: config, isLoading, error, refetch } = useQuery<Config, Error>({
    queryKey: ['config'],
    queryFn: () => api.getConfig(),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Configuration</h2>
          <p className="text-gray-600">Current daemon configuration (read-only)</p>
        </div>
        <button
          onClick={() => refetch()}
          className="btn-secondary flex items-center space-x-2"
        >
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>Refresh</span>
        </button>
      </div>

      <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
        <div className="flex items-start space-x-3">
          <svg className="w-5 h-5 text-yellow-600 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <div>
            <p className="text-sm text-yellow-800">
              This is the current running configuration. To modify the configuration,
              edit the config file and restart the daemon.
            </p>
          </div>
        </div>
      </div>

      <ConfigViewer config={config} isLoading={isLoading} error={error} />
    </div>
  );
}
