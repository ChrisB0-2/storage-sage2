import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';

export function useSchedulerStart() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => api.startScheduler(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
  });
}

export function useSchedulerStop() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => api.stopScheduler(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['status'] });
    },
  });
}
