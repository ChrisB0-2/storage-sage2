import { useState } from 'react';
import { useSchedulerStart, useSchedulerStop } from '../hooks/useScheduler';
import type { StatusResponse } from '../api/types';

interface SchedulerControlProps {
  status: StatusResponse | undefined;
  isLoading?: boolean;
}

export default function SchedulerControl({ status, isLoading = false }: SchedulerControlProps) {
  const [error, setError] = useState<string | null>(null);

  const startMutation = useSchedulerStart();
  const stopMutation = useSchedulerStop();

  const isEnabled = status?.scheduler_enabled ?? true;
  const isPending = startMutation.isPending || stopMutation.isPending;

  const handleStart = () => {
    setError(null);
    startMutation.mutate(undefined, {
      onError: (err: Error) => setError(err.message),
    });
  };

  const handleStop = () => {
    setError(null);
    stopMutation.mutate(undefined, {
      onError: (err: Error) => setError(err.message),
    });
  };

  return (
    <div className="card">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-medium text-gray-900">Scheduler</h3>
          <p className="text-sm text-gray-500">
            {isEnabled
              ? 'Automated cleanup runs are active'
              : 'Automated cleanup runs are paused'}
          </p>
        </div>

        <div className="flex items-center space-x-3">
          {/* Status badge */}
          {isLoading ? (
            <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-800">
              Loading...
            </span>
          ) : isEnabled ? (
            <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">
              Active
            </span>
          ) : (
            <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-yellow-100 text-yellow-800">
              Paused
            </span>
          )}

          {/* Control button */}
          {isEnabled ? (
            <button
              onClick={handleStop}
              disabled={isPending || isLoading}
              className={`btn-secondary flex items-center space-x-2 bg-yellow-50 hover:bg-yellow-100 text-yellow-700 border-yellow-200 ${
                isPending || isLoading ? 'opacity-50 cursor-not-allowed' : ''
              }`}
            >
              {isPending ? (
                <>
                  <svg className="animate-spin h-5 w-5" viewBox="0 0 24 24">
                    <circle
                      className="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      strokeWidth="4"
                      fill="none"
                    />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                  <span>Pausing...</span>
                </>
              ) : (
                <>
                  <svg className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span>Pause</span>
                </>
              )}
            </button>
          ) : (
            <button
              onClick={handleStart}
              disabled={isPending || isLoading}
              className={`btn-primary flex items-center space-x-2 bg-green-600 hover:bg-green-700 ${
                isPending || isLoading ? 'opacity-50 cursor-not-allowed' : ''
              }`}
            >
              {isPending ? (
                <>
                  <svg className="animate-spin h-5 w-5" viewBox="0 0 24 24">
                    <circle
                      className="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      strokeWidth="4"
                      fill="none"
                    />
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    />
                  </svg>
                  <span>Starting...</span>
                </>
              ) : (
                <>
                  <svg className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span>Start</span>
                </>
              )}
            </button>
          )}
        </div>
      </div>

      {/* Error message */}
      {error && (
        <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded-md">
          <div className="flex items-center space-x-2">
            <svg className="h-5 w-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span className="text-sm text-red-600">{error}</span>
          </div>
        </div>
      )}
    </div>
  );
}
