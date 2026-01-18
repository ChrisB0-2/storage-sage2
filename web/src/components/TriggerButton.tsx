import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import type { StatusResponse } from '../api/types';

interface TriggerButtonProps {
  status: StatusResponse | undefined;
  disabled?: boolean;
}

export default function TriggerButton({ status, disabled = false }: TriggerButtonProps) {
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: () => api.trigger(),
    onSuccess: () => {
      setError(null);
      // Immediately refetch status to show running state
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
    onError: (err: Error) => {
      setError(err.message);
    },
  });

  const isRunning = status?.running ?? false;
  const isDisabled = disabled || isRunning || mutation.isPending;

  const handleClick = () => {
    setError(null);
    mutation.mutate();
  };

  return (
    <div className="card">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-medium text-gray-900">Manual Trigger</h3>
          <p className="text-sm text-gray-500">
            {isRunning
              ? 'A cleanup run is currently in progress'
              : 'Start a cleanup run immediately'}
          </p>
        </div>

        <button
          onClick={handleClick}
          disabled={isDisabled}
          className={`btn-primary flex items-center space-x-2 ${
            isDisabled ? 'opacity-50 cursor-not-allowed' : ''
          }`}
        >
          {mutation.isPending ? (
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
          ) : isRunning ? (
            <>
              <svg className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              <span>Running...</span>
            </>
          ) : (
            <>
              <svg className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span>Run Now</span>
            </>
          )}
        </button>
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

      {/* Success message */}
      {mutation.isSuccess && !isRunning && (
        <div className="mt-4 p-3 bg-green-50 border border-green-200 rounded-md">
          <div className="flex items-center space-x-2">
            <svg className="h-5 w-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span className="text-sm text-green-600">Cleanup run completed</span>
          </div>
        </div>
      )}
    </div>
  );
}
