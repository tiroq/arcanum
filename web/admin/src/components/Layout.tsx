import { NavLink, Outlet } from 'react-router-dom';
import {
  LayoutDashboard,
  Database,
  Briefcase,
  CheckCircle,
  Cpu,
  Settings,
} from 'lucide-react';
import { clsx } from 'clsx';

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
  { to: '/tasks', icon: Database, label: 'Source Tasks' },
  { to: '/jobs', icon: Briefcase, label: 'Jobs' },
  { to: '/proposals', icon: CheckCircle, label: 'Proposals' },
  { to: '/processor-runs', icon: Cpu, label: 'Processor Runs' },
  { to: '/settings', icon: Settings, label: 'Settings' },
];

export function Layout() {
  return (
    <div className="flex h-screen bg-gray-50">
      <aside className="w-56 bg-white border-r border-gray-200 flex flex-col">
        <div className="px-5 py-4 border-b border-gray-200">
          <h1 className="text-lg font-bold text-gray-900">⚡ Runeforge</h1>
          <p className="text-xs text-gray-500 mt-0.5">Agent Platform</p>
        </div>
        <nav className="flex-1 px-3 py-4 space-y-1">
          {navItems.map(({ to, icon: Icon, label, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                clsx(
                  'flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-blue-50 text-blue-700'
                    : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900'
                )
              }
            >
              <Icon size={16} />
              {label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
