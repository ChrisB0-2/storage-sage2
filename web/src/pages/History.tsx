import { useState } from 'react';
import { useAuditHistory } from '../hooks/useAuditHistory';
import HistoryTable from '../components/HistoryTable';
import type { AuditQueryParams } from '../api/types';

const LIMIT_OPTIONS = [25, 50, 100, 250, 500];
const TIME_RANGE_OPTIONS = [
  { label: 'Last Hour', value: '1h' },
  { label: 'Last 24 Hours', value: '24h' },
  { label: 'Last 7 Days', value: '7d' },
  { label: 'Last 30 Days', value: '30d' },
  { label: 'All Time', value: '' },
];

export default function History() {
  const [filters, setFilters] = useState<AuditQueryParams>({
    limit: 100,
    since: '24h',
  });

  const { data: records, isLoading, error, refetch } = useAuditHistory(filters);

  const handleFilterChange = (key: keyof AuditQueryParams, value: string | number) => {
    setFilters(prev => ({
      ...prev,
      [key]: value || undefined,
    }));
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Audit History</h2>
          <p className="text-gray-600">Browse and filter audit records</p>
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

      {/* Filters */}
      <div className="card">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
          {/* Time Range */}
          <div>
            <label className="label">Time Range</label>
            <select
              className="input"
              value={filters.since || ''}
              onChange={(e) => handleFilterChange('since', e.target.value)}
            >
              {TIME_RANGE_OPTIONS.map(opt => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>

          {/* Level */}
          <div>
            <label className="label">Level</label>
            <select
              className="input"
              value={filters.level || ''}
              onChange={(e) => handleFilterChange('level', e.target.value)}
            >
              <option value="">All Levels</option>
              <option value="info">Info</option>
              <option value="warn">Warning</option>
              <option value="error">Error</option>
            </select>
          </div>

          {/* Action */}
          <div>
            <label className="label">Action</label>
            <select
              className="input"
              value={filters.action || ''}
              onChange={(e) => handleFilterChange('action', e.target.value)}
            >
              <option value="">All Actions</option>
              <option value="plan">Plan</option>
              <option value="execute">Execute</option>
            </select>
          </div>

          {/* Path Filter */}
          <div>
            <label className="label">Path Contains</label>
            <input
              type="text"
              className="input"
              placeholder="e.g., /tmp"
              value={filters.path || ''}
              onChange={(e) => handleFilterChange('path', e.target.value)}
            />
          </div>

          {/* Limit */}
          <div>
            <label className="label">Max Records</label>
            <select
              className="input"
              value={filters.limit || 100}
              onChange={(e) => handleFilterChange('limit', parseInt(e.target.value))}
            >
              {LIMIT_OPTIONS.map(limit => (
                <option key={limit} value={limit}>
                  {limit}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Error display */}
      {error && (
        <div className="card border-l-4 border-red-500 bg-red-50">
          <div className="flex items-center space-x-3">
            <svg className="w-6 h-6 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <div>
              <h3 className="font-medium text-red-800">Error Loading Records</h3>
              <p className="text-sm text-red-600">{error.message}</p>
            </div>
          </div>
        </div>
      )}

      {/* Results count */}
      {records && (
        <div className="text-sm text-gray-500">
          Showing {records.length} record{records.length !== 1 ? 's' : ''}
        </div>
      )}

      {/* Table */}
      <HistoryTable records={records || []} isLoading={isLoading} />
    </div>
  );
}
