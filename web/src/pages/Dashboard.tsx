import { useApi, apiPost } from '../hooks/useApi';
import type { Stats, Task, VM } from '../types';
import { useState } from 'react';

export default function Dashboard({ onViewTask }: { onViewTask: (id: string) => void }) {
  const { data: stats } = useApi<Stats>('/stats', 3000);
  const { data: tasks } = useApi<Task[]>('/tasks', 3000);
  const { data: vms } = useApi<VM[]>('/vms', 3000);
  const [prompt, setPrompt] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const submitTask = async () => {
    if (!prompt.trim()) return;
    setSubmitting(true);
    try {
      const t = await apiPost<Task>('/tasks', { prompt, ram_mb: 2048, vcpus: 2 });
      onViewTask(t.id);
    } catch (e: any) {
      alert(e.message);
    } finally {
      setSubmitting(false);
    }
  };

  const recentTasks = (tasks || []).slice(-5).reverse();

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-3 gap-4">
        <StatCard label="Running VMs" value={stats?.running_vms ?? 0} color="text-green-400" />
        <StatCard label="Total VMs" value={stats?.total_vms ?? 0} color="text-blue-400" />
        <StatCard label="Total Tasks" value={stats?.total_tasks ?? 0} color="text-purple-400" />
      </div>

      <div className="bg-gray-900 rounded-lg p-4 border border-gray-800">
        <h2 className="text-lg font-semibold mb-3">Quick Task</h2>
        <div className="flex gap-2">
          <input
            type="text"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submitTask()}
            placeholder="Enter a prompt for Claude Code..."
            className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-orange-500"
          />
          <button
            onClick={submitTask}
            disabled={submitting || !prompt.trim()}
            className="bg-orange-600 hover:bg-orange-500 disabled:opacity-50 px-4 py-2 rounded text-sm font-medium"
          >
            {submitting ? 'Starting...' : 'Run Task'}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div className="bg-gray-900 rounded-lg p-4 border border-gray-800">
          <h2 className="text-lg font-semibold mb-3">Recent Tasks</h2>
          {recentTasks.length === 0 ? (
            <p className="text-gray-500 text-sm">No tasks yet</p>
          ) : (
            <div className="space-y-2">
              {recentTasks.map((t) => (
                <div
                  key={t.id}
                  onClick={() => onViewTask(t.id)}
                  className="flex items-center justify-between bg-gray-800 rounded p-2 cursor-pointer hover:bg-gray-750"
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">{t.prompt}</p>
                    <p className="text-xs text-gray-500">{t.id}</p>
                  </div>
                  <StatusBadge status={t.status} />
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="bg-gray-900 rounded-lg p-4 border border-gray-800">
          <h2 className="text-lg font-semibold mb-3">Running VMs</h2>
          {(vms || []).length === 0 ? (
            <p className="text-gray-500 text-sm">No VMs running</p>
          ) : (
            <div className="space-y-2">
              {(vms || []).map((v) => (
                <div key={v.name} className="flex items-center justify-between bg-gray-800 rounded p-2">
                  <div>
                    <p className="text-sm font-mono">{v.name}</p>
                    <p className="text-xs text-gray-500">{v.guest_ip} | {v.ram_mb}MB | {v.vcpus} vCPU</p>
                  </div>
                  <StatusBadge status={v.state} />
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="bg-gray-900 rounded-lg p-4 border border-gray-800">
      <p className="text-gray-400 text-sm">{label}</p>
      <p className={`text-3xl font-bold ${color}`}>{value}</p>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    running: 'bg-green-900 text-green-300',
    completed: 'bg-blue-900 text-blue-300',
    failed: 'bg-red-900 text-red-300',
    pending: 'bg-yellow-900 text-yellow-300',
    cancelled: 'bg-gray-700 text-gray-400',
    creating: 'bg-yellow-900 text-yellow-300',
    stopped: 'bg-gray-700 text-gray-400',
  };
  return (
    <span className={`px-2 py-0.5 rounded text-xs font-medium ${colors[status] || 'bg-gray-700 text-gray-400'}`}>
      {status}
    </span>
  );
}
