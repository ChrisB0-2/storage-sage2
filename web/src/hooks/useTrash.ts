import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import type { TrashItem } from '../api/types';

const TRASH_POLL_INTERVAL = 30000; // 30 seconds

export function useTrash() {
  return useQuery<TrashItem[], Error>({
    queryKey: ['trash'],
    queryFn: () => api.listTrash(),
    refetchInterval: TRASH_POLL_INTERVAL,
  });
}

export function useTrashRestore() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (name: string) => api.restoreTrash(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['trash'] });
    },
  });
}

export function useTrashEmpty() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (olderThan?: string) => api.emptyTrash(olderThan),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['trash'] });
    },
  });
}
