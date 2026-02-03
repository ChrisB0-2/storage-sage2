import type { AuditRecord } from '../api/types';

interface HistoryTableProps {
  records: AuditRecord[];
  isLoading: boolean;
}

function formatTimestamp(timestamp: string): string {
  const date = new Date(timestamp);
  return date.toLocaleString();
}

function formatBytes(bytes: number | undefined): string {
  if (!bytes || bytes === 0) return '-';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

function getReasonBadge(reason: string | undefined): { classes: string; label: string; tooltip: string } {
  if (!reason) {
    return { classes: 'bg-gray-100 text-gray-800', label: '-', tooltip: 'No reason provided' };
  }

  // Handle compound reasons (e.g., "policy_deny:age_check")
  const [baseReason, detail] = reason.includes(':') ? reason.split(':', 2) : [reason, ''];

  switch (baseReason) {
    case 'deleted':
      return { classes: 'bg-green-100 text-green-800', label: 'Deleted', tooltip: 'File was permanently deleted' };
    case 'trashed':
      return { classes: 'bg-blue-100 text-blue-800', label: 'Trashed', tooltip: 'File was moved to trash' };
    case 'would_delete':
      return { classes: 'bg-purple-100 text-purple-800', label: 'Dry Run', tooltip: 'Would be deleted (dry-run mode)' };
    case 'already_gone':
      return { classes: 'bg-gray-100 text-gray-800', label: 'Already Gone', tooltip: 'File was already missing' };
    case 'delete_failed':
      return { classes: 'bg-red-100 text-red-800', label: 'Failed', tooltip: 'Deletion failed' };
    case 'ctx_canceled':
      return { classes: 'bg-gray-100 text-gray-800', label: 'Canceled', tooltip: 'Operation was canceled' };
    case 'policy_deny':
      return { classes: 'bg-yellow-100 text-yellow-800', label: 'Policy Denied', tooltip: `Blocked by policy: ${detail}` };
    case 'safety_deny_scan':
      return { classes: 'bg-orange-100 text-orange-800', label: 'Safety Denied', tooltip: `Blocked by safety scan: ${detail}` };
    case 'safety_deny_execute':
      return { classes: 'bg-orange-100 text-orange-800', label: 'Safety Denied', tooltip: `Blocked at execution: ${detail}` };
    default:
      return { classes: 'bg-gray-100 text-gray-800', label: reason, tooltip: reason };
  }
}

function getActionBadge(action: string) {
  switch (action) {
    case 'execute':
      return 'bg-green-100 text-green-800';
    case 'plan':
      return 'bg-purple-100 text-purple-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}

export default function HistoryTable({ records, isLoading }: HistoryTableProps) {
  if (isLoading) {
    return (
      <div className="card">
        <div className="animate-pulse space-y-4">
          {[...Array(5)].map((_, i) => (
            <div key={i} className="flex space-x-4">
              <div className="h-4 bg-gray-200 rounded w-1/6"></div>
              <div className="h-4 bg-gray-200 rounded w-1/6"></div>
              <div className="h-4 bg-gray-200 rounded w-2/6"></div>
              <div className="h-4 bg-gray-200 rounded w-1/6"></div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (!records || records.length === 0) {
    return (
      <div className="card text-center py-12">
        <svg className="mx-auto h-12 w-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
        </svg>
        <h3 className="mt-2 text-sm font-medium text-gray-900">No audit records</h3>
        <p className="mt-1 text-sm text-gray-500">
          Records will appear here after the daemon runs.
        </p>
      </div>
    );
  }

  return (
    <div className="card overflow-hidden p-0">
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th scope="col" className="table-header">Timestamp</th>
              <th scope="col" className="table-header">Action</th>
              <th scope="col" className="table-header">Path</th>
              <th scope="col" className="table-header">Reason</th>
              <th scope="col" className="table-header">Bytes Freed</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {records.map((record) => {
              const reasonInfo = getReasonBadge(record.reason);
              return (
                <tr key={record.id} className="hover:bg-gray-50">
                  <td className="table-cell text-gray-500">
                    {formatTimestamp(record.timestamp)}
                  </td>
                  <td className="table-cell">
                    <span className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getActionBadge(record.action)}`}>
                      {record.action}
                    </span>
                  </td>
                  <td className="table-cell font-mono text-xs max-w-xs truncate" title={record.path}>
                    {record.path || '-'}
                  </td>
                  <td className="table-cell">
                    <span
                      className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${reasonInfo.classes}`}
                      title={reasonInfo.tooltip}
                    >
                      {reasonInfo.label}
                    </span>
                  </td>
                  <td className="table-cell text-right">
                    {formatBytes(record.bytes_freed)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
