import { useState } from 'react';
import type { Config } from '../api/types';

interface ConfigViewerProps {
  config: Config | undefined;
  isLoading: boolean;
  error: Error | null;
}

interface CollapsibleSectionProps {
  title: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
}

function CollapsibleSection({ title, children, defaultOpen = true }: CollapsibleSectionProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen);

  return (
    <div className="border border-gray-200 rounded-lg overflow-hidden">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full px-4 py-3 bg-gray-50 flex items-center justify-between text-left hover:bg-gray-100 transition-colors"
      >
        <span className="font-medium text-gray-900">{title}</span>
        <svg
          className={`w-5 h-5 text-gray-500 transform transition-transform ${isOpen ? 'rotate-180' : ''}`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {isOpen && (
        <div className="px-4 py-3 bg-white">
          {children}
        </div>
      )}
    </div>
  );
}

function ConfigRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between py-2 border-b border-gray-100 last:border-0">
      <span className="text-sm text-gray-600">{label}</span>
      <span className="text-sm font-medium text-gray-900">{value}</span>
    </div>
  );
}

function ConfigList({ items, emptyText = 'None' }: { items: string[] | undefined; emptyText?: string }) {
  if (!items || items.length === 0) {
    return <span className="text-gray-400 italic">{emptyText}</span>;
  }
  return (
    <ul className="text-sm font-mono space-y-1">
      {items.map((item, i) => (
        <li key={i} className="text-gray-900 bg-gray-100 px-2 py-1 rounded">
          {item}
        </li>
      ))}
    </ul>
  );
}

function formatDuration(ns: number): string {
  if (ns === 0) return '0';
  const seconds = ns / 1e9;
  if (seconds < 60) return `${seconds}s`;
  const minutes = seconds / 60;
  if (minutes < 60) return `${minutes}m`;
  const hours = minutes / 60;
  return `${hours}h`;
}

export default function ConfigViewer({ config, isLoading, error }: ConfigViewerProps) {
  if (error) {
    return (
      <div className="card border-l-4 border-red-500">
        <div className="flex items-center space-x-3">
          <svg className="w-6 h-6 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <div>
            <h3 className="font-medium text-red-800">Error Loading Config</h3>
            <p className="text-sm text-red-600">{error.message}</p>
          </div>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-4">
        {[...Array(4)].map((_, i) => (
          <div key={i} className="card animate-pulse">
            <div className="h-4 bg-gray-200 rounded w-1/4 mb-4"></div>
            <div className="space-y-3">
              <div className="h-3 bg-gray-200 rounded w-full"></div>
              <div className="h-3 bg-gray-200 rounded w-3/4"></div>
            </div>
          </div>
        ))}
      </div>
    );
  }

  if (!config) {
    return (
      <div className="card text-center py-12">
        <svg className="mx-auto h-12 w-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
        <h3 className="mt-2 text-sm font-medium text-gray-900">No configuration available</h3>
        <p className="mt-1 text-sm text-gray-500">
          Configuration will be available when the daemon is running.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Scan Config */}
      <CollapsibleSection title="Scan Configuration">
        <div className="mb-3">
          <span className="text-sm text-gray-600">Roots:</span>
          <ConfigList items={config.scan.roots} emptyText="No roots configured" />
        </div>
        <ConfigRow label="Recursive" value={config.scan.recursive ? 'Yes' : 'No'} />
        <ConfigRow label="Max Depth" value={config.scan.max_depth || 'Unlimited'} />
        <ConfigRow label="Follow Symlinks" value={config.scan.follow_symlinks ? 'Yes' : 'No'} />
        <ConfigRow label="Include Files" value={config.scan.include_files ? 'Yes' : 'No'} />
        <ConfigRow label="Include Directories" value={config.scan.include_dirs ? 'Yes' : 'No'} />
      </CollapsibleSection>

      {/* Policy Config */}
      <CollapsibleSection title="Policy Configuration">
        <ConfigRow label="Minimum Age" value={`${config.policy.min_age_days} days`} />
        <ConfigRow label="Minimum Size" value={config.policy.min_size_mb > 0 ? `${config.policy.min_size_mb} MB` : 'Any'} />
        <ConfigRow label="Composite Mode" value={config.policy.composite_mode.toUpperCase()} />
        <div className="mt-3">
          <span className="text-sm text-gray-600">Extensions:</span>
          <ConfigList items={config.policy.extensions} emptyText="All extensions" />
        </div>
        <div className="mt-3">
          <span className="text-sm text-gray-600">Exclusions:</span>
          <ConfigList items={config.policy.exclusions} emptyText="None" />
        </div>
      </CollapsibleSection>

      {/* Safety Config */}
      <CollapsibleSection title="Safety Configuration">
        <ConfigRow label="Allow Directory Delete" value={config.safety.allow_dir_delete ? 'Yes' : 'No'} />
        <ConfigRow label="Enforce Mount Boundary" value={config.safety.enforce_mount_boundary ? 'Yes' : 'No'} />
        <div className="mt-3">
          <span className="text-sm text-gray-600">Protected Paths:</span>
          <ConfigList items={config.safety.protected_paths} />
        </div>
      </CollapsibleSection>

      {/* Execution Config */}
      <CollapsibleSection title="Execution Configuration">
        <ConfigRow
          label="Mode"
          value={
            <span className={`px-2 py-1 rounded text-xs font-semibold ${
              config.execution.mode === 'execute'
                ? 'bg-red-100 text-red-800'
                : 'bg-yellow-100 text-yellow-800'
            }`}>
              {config.execution.mode}
            </span>
          }
        />
        <ConfigRow label="Timeout" value={formatDuration(config.execution.timeout)} />
        <ConfigRow label="Audit Path" value={config.execution.audit_path || 'Not set'} />
        <ConfigRow label="Audit DB Path" value={config.execution.audit_db_path || 'Not set'} />
        <ConfigRow label="Max Items" value={config.execution.max_items} />
      </CollapsibleSection>

      {/* Daemon Config */}
      <CollapsibleSection title="Daemon Configuration" defaultOpen={false}>
        <ConfigRow label="Enabled" value={config.daemon.enabled ? 'Yes' : 'No'} />
        <ConfigRow label="HTTP Address" value={config.daemon.http_addr} />
        <ConfigRow label="Metrics Address" value={config.daemon.metrics_addr} />
        <ConfigRow label="Schedule" value={config.daemon.schedule || 'Not set'} />
      </CollapsibleSection>

      {/* Metrics Config */}
      <CollapsibleSection title="Metrics Configuration" defaultOpen={false}>
        <ConfigRow label="Enabled" value={config.metrics.enabled ? 'Yes' : 'No'} />
        <ConfigRow label="Namespace" value={config.metrics.namespace} />
      </CollapsibleSection>
    </div>
  );
}
