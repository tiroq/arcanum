import { useMetricsSummary } from '../api/hooks';
import { LoadingSpinner } from '../components/LoadingSpinner';
import { AlertTriangle, CheckCircle, Clock, Database, RefreshCw, XCircle } from 'lucide-react';

interface StatCardProps {
  label: string;
  value: number;
  icon: React.ReactNode;
  color?: string;
}

function StatCard({ label, value, icon, color = 'text-gray-700' }: StatCardProps) {
  return (
    <div className="bg-white rounded-lg border border-gray-200 p-5">
      <div className="flex items-center justify-between">
        <p className="text-sm text-gray-500">{label}</p>
        <span className={color}>{icon}</span>
      </div>
      <p className="mt-2 text-3xl font-semibold text-gray-900">{value}</p>
    </div>
  );
}

export function Dashboard() {
  const { data, isLoading, error } = useMetricsSummary();

  if (isLoading) return <LoadingSpinner />;
  if (error || !data)
    return (
      <div className="p-8 text-red-600">
        Failed to load metrics. Make sure the API gateway is running.
      </div>
    );

  return (
    <div className="p-8">
      <h2 className="text-2xl font-bold text-gray-900 mb-6">Dashboard</h2>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard label="Total Tasks" value={data.total_tasks} icon={<Database size={20} />} />
        <StatCard label="Changed Today" value={data.changed_tasks_today} icon={<RefreshCw size={20} />} color="text-blue-600" />
        <StatCard label="Queued Jobs" value={data.queued_jobs} icon={<Clock size={20} />} color="text-yellow-600" />
        <StatCard label="Running Jobs" value={data.running_jobs} icon={<RefreshCw size={20} />} color="text-blue-600" />
        <StatCard label="Failed Jobs" value={data.failed_jobs} icon={<XCircle size={20} />} color="text-red-600" />
        <StatCard label="Pending Approvals" value={data.pending_approvals} icon={<AlertTriangle size={20} />} color="text-orange-600" />
        <StatCard label="Writeback Success" value={data.writeback_success} icon={<CheckCircle size={20} />} color="text-green-600" />
        <StatCard label="Writeback Failures" value={data.writeback_failure} icon={<XCircle size={20} />} color="text-red-600" />
      </div>
    </div>
  );
}
