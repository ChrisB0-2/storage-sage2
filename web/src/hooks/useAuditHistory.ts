import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';
import type { AuditRecord, AuditStats, AuditQueryParams } from '../api/types';

const AUDIT_POLL_INTERVAL = 30000; // 30 seconds

export function useAuditHistory(params?: AuditQueryParams, enabled = true) {
  return useQuery<AuditRecord[], Error>({
    queryKey: ['audit', 'history', params],
    queryFn: () => api.queryAudit(params),
    refetchInterval: AUDIT_POLL_INTERVAL,
    enabled,
  });
}

export function useAuditStats(enabled = true) {
  return useQuery<AuditStats, Error>({
    queryKey: ['audit', 'stats'],
    queryFn: () => api.getAuditStats(),
    refetchInterval: AUDIT_POLL_INTERVAL,
    enabled,
  });
}
