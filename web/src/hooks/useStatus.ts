import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';
import type { StatusResponse } from '../api/types';

const STATUS_POLL_INTERVAL = 5000; // 5 seconds

export function useStatus() {
  return useQuery<StatusResponse, Error>({
    queryKey: ['status'],
    queryFn: () => api.getStatus(),
    refetchInterval: STATUS_POLL_INTERVAL,
    refetchIntervalInBackground: true,
  });
}

export function useStatusOnce() {
  return useQuery<StatusResponse, Error>({
    queryKey: ['status'],
    queryFn: () => api.getStatus(),
  });
}
