import { NavLink, Outlet } from 'react-router-dom';
import { useStatus } from '../hooks/useStatus';
import type { DaemonState } from '../api/types';

const navItems = [
  { to: '/', label: 'Dashboard', exact: true },
  { to: '/history', label: 'History' },
  { to: '/trash', label: 'Trash' },
  { to: '/config', label: 'Config' },
  { to: '/metrics', label: 'Metrics' },
];

function getStateColor(state: DaemonState): string {
  switch (state) {
    case 'starting':
      return 'bg-yellow-400';
    case 'ready':
      return 'bg-green-500';
    case 'running':
      return 'bg-blue-500';
    case 'stopping':
      return 'bg-orange-500';
    case 'stopped':
      return 'bg-gray-500';
    default:
      return 'bg-gray-400';
  }
}

export default function Layout() {
  const { data: status } = useStatus();

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <header className="bg-white shadow-sm border-b border-gray-200">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center justify-between h-16">
            {/* Logo and title */}
            <div className="flex items-center space-x-3">
              <div className="w-8 h-8 bg-blue-600 rounded-lg flex items-center justify-center">
                <svg className="w-5 h-5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
                </svg>
              </div>
              <h1 className="text-xl font-semibold text-gray-900">Storage Sage</h1>
            </div>

            {/* Navigation */}
            <nav className="flex space-x-1">
              {navItems.map(({ to, label, exact }) => (
                <NavLink
                  key={to}
                  to={to}
                  end={exact}
                  className={({ isActive }) =>
                    `px-4 py-2 rounded-md text-sm font-medium transition-colors ${
                      isActive
                        ? 'bg-blue-100 text-blue-700'
                        : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900'
                    }`
                  }
                >
                  {label}
                </NavLink>
              ))}
            </nav>

            {/* Status indicator */}
            <div className="flex items-center space-x-2">
              <div
                className={`w-3 h-3 rounded-full ${
                  status ? getStateColor(status.state) : 'bg-gray-300'
                } ${status?.state === 'running' ? 'status-pulse' : ''}`}
              />
              <span className="text-sm text-gray-600 capitalize">
                {status?.state || 'connecting...'}
              </span>
            </div>
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <Outlet />
      </main>

      {/* Footer */}
      <footer className="bg-white border-t border-gray-200 mt-auto">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
          <p className="text-sm text-gray-500 text-center">
            Storage Sage - Automated File Cleanup Daemon
          </p>
        </div>
      </footer>
    </div>
  );
}
