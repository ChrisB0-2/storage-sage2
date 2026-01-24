import { useState } from 'react';
import { useTrash, useTrashRestore, useTrashEmpty } from '../hooks/useTrash';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleString();
}

function formatAge(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
  const diffDays = Math.floor(diffHours / 24);

  if (diffDays > 0) {
    return `${diffDays}d ago`;
  }
  if (diffHours > 0) {
    return `${diffHours}h ago`;
  }
  return 'just now';
}

const EMPTY_OPTIONS = [
  { label: 'Older than 24 hours', value: '24h' },
  { label: 'Older than 7 days', value: '7d' },
  { label: 'Older than 30 days', value: '30d' },
  { label: 'All items', value: '' },
];

export default function Trash() {
  const { data: items, isLoading, error, refetch } = useTrash();
  const restoreMutation = useTrashRestore();
  const emptyMutation = useTrashEmpty();
  const [showEmptyModal, setShowEmptyModal] = useState(false);
  const [emptyOption, setEmptyOption] = useState('7d');
  const [restoringItem, setRestoringItem] = useState<string | null>(null);

  const totalSize = items?.reduce((sum, item) => sum + item.size, 0) ?? 0;

  const handleRestore = async (name: string) => {
    setRestoringItem(name);
    try {
      await restoreMutation.mutateAsync(name);
    } finally {
      setRestoringItem(null);
    }
  };

  const handleEmpty = async () => {
    await emptyMutation.mutateAsync(emptyOption || undefined);
    setShowEmptyModal(false);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Trash</h2>
          <p className="text-gray-600">Manage soft-deleted files</p>
        </div>
        <div className="flex items-center space-x-3">
          <button
            onClick={() => refetch()}
            className="btn-secondary flex items-center space-x-2"
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            <span>Refresh</span>
          </button>
          <button
            onClick={() => setShowEmptyModal(true)}
            disabled={!items || items.length === 0}
            className="btn-danger flex items-center space-x-2 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            <span>Empty Trash</span>
          </button>
        </div>
      </div>

      {/* Summary Card */}
      <div className="card">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-sm text-gray-500">Total Items</p>
            <p className="text-2xl font-semibold">{items?.length ?? 0}</p>
          </div>
          <div>
            <p className="text-sm text-gray-500">Total Size</p>
            <p className="text-2xl font-semibold">{formatBytes(totalSize)}</p>
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
              <h3 className="font-medium text-red-800">Error Loading Trash</h3>
              <p className="text-sm text-red-600">{error.message}</p>
            </div>
          </div>
        </div>
      )}

      {/* Restore success message */}
      {restoreMutation.isSuccess && (
        <div className="card border-l-4 border-green-500 bg-green-50">
          <div className="flex items-center space-x-3">
            <svg className="w-6 h-6 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
            <p className="text-green-800">
              File restored to: <code className="bg-green-100 px-1 rounded">{restoreMutation.data?.original_path}</code>
            </p>
          </div>
        </div>
      )}

      {/* Empty success message */}
      {emptyMutation.isSuccess && (
        <div className="card border-l-4 border-green-500 bg-green-50">
          <div className="flex items-center space-x-3">
            <svg className="w-6 h-6 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
            <p className="text-green-800">
              Deleted {emptyMutation.data?.deleted} items, freed {formatBytes(emptyMutation.data?.bytes_freed ?? 0)}
            </p>
          </div>
        </div>
      )}

      {/* Items Table */}
      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                  Name
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                  Original Path
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                  Size
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                  Trashed
                </th>
                <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="bg-white divide-y divide-gray-200">
              {isLoading ? (
                <tr>
                  <td colSpan={5} className="px-6 py-12 text-center">
                    <div className="flex items-center justify-center space-x-2 text-gray-500">
                      <svg className="animate-spin h-5 w-5" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                      </svg>
                      <span>Loading...</span>
                    </div>
                  </td>
                </tr>
              ) : !items || items.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-6 py-12 text-center text-gray-500">
                    <svg className="mx-auto h-12 w-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                    </svg>
                    <p className="mt-2">Trash is empty</p>
                  </td>
                </tr>
              ) : (
                items.map((item) => (
                  <tr key={item.name} className="hover:bg-gray-50">
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="flex items-center space-x-2">
                        {item.is_dir ? (
                          <svg className="h-5 w-5 text-yellow-500" fill="currentColor" viewBox="0 0 20 20">
                            <path d="M2 6a2 2 0 012-2h5l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" />
                          </svg>
                        ) : (
                          <svg className="h-5 w-5 text-gray-400" fill="currentColor" viewBox="0 0 20 20">
                            <path fillRule="evenodd" d="M4 4a2 2 0 012-2h4.586A2 2 0 0112 2.586L15.414 6A2 2 0 0116 7.414V16a2 2 0 01-2 2H6a2 2 0 01-2-2V4z" clipRule="evenodd" />
                          </svg>
                        )}
                        <span className="text-sm font-medium text-gray-900 truncate max-w-xs" title={item.name}>
                          {item.name}
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-sm text-gray-500 truncate max-w-md block" title={item.original_path}>
                        {item.original_path}
                      </span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                      {formatBytes(item.size)}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500" title={formatDate(item.trashed_at)}>
                      {formatAge(item.trashed_at)}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-right">
                      <button
                        onClick={() => handleRestore(item.name)}
                        disabled={restoringItem === item.name}
                        className="text-blue-600 hover:text-blue-900 text-sm font-medium disabled:opacity-50"
                      >
                        {restoringItem === item.name ? 'Restoring...' : 'Restore'}
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Empty Trash Modal */}
      {showEmptyModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4">
            <div className="p-6">
              <h3 className="text-lg font-semibold text-gray-900 mb-4">Empty Trash</h3>
              <p className="text-sm text-gray-600 mb-4">
                This will permanently delete items from the trash. This action cannot be undone.
              </p>
              <div className="mb-4">
                <label className="label">Delete items:</label>
                <select
                  className="input"
                  value={emptyOption}
                  onChange={(e) => setEmptyOption(e.target.value)}
                >
                  {EMPTY_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex justify-end space-x-3">
                <button
                  onClick={() => setShowEmptyModal(false)}
                  className="btn-secondary"
                >
                  Cancel
                </button>
                <button
                  onClick={handleEmpty}
                  disabled={emptyMutation.isPending}
                  className="btn-danger disabled:opacity-50"
                >
                  {emptyMutation.isPending ? 'Deleting...' : 'Delete'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
