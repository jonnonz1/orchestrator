import { useApi } from '../hooks/useApi';
import type { Task } from '../types';

export default function TaskList({ onViewTask }: { onViewTask: (id: string) => void }) {
  const { data: tasks } = useApi<Task[]>('/tasks', 3000);
  const sorted = [...(tasks || [])].reverse();

  return (
    <div className="space-y-4">
      <h2 className="text-xl font-semibold">Tasks</h2>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-gray-800">
            <tr>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">ID</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">Prompt</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">VM</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">Status</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">Cost</th>
            </tr>
          </thead>
          <tbody>
            {sorted.length === 0 ? (
              <tr><td colSpan={5} className="px-4 py-8 text-center text-gray-500">No tasks</td></tr>
            ) : (
              sorted.map((t) => (
                <tr key={t.id} onClick={() => onViewTask(t.id)}
                  className="border-t border-gray-800 hover:bg-gray-800/50 cursor-pointer">
                  <td className="px-4 py-2 font-mono text-orange-400">{t.id}</td>
                  <td className="px-4 py-2 max-w-md truncate">{t.prompt}</td>
                  <td className="px-4 py-2 font-mono text-gray-400">{t.vm_name}</td>
                  <td className="px-4 py-2">
                    <StatusBadge status={t.status} />
                  </td>
                  <td className="px-4 py-2 text-gray-400">
                    {t.cost_usd ? `$${t.cost_usd.toFixed(4)}` : '-'}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    running: 'bg-green-900 text-green-300',
    completed: 'bg-blue-900 text-blue-300',
    failed: 'bg-red-900 text-red-300',
    pending: 'bg-yellow-900 text-yellow-300',
  };
  return (
    <span className={`px-2 py-0.5 rounded text-xs font-medium ${colors[status] || 'bg-gray-700 text-gray-400'}`}>
      {status}
    </span>
  );
}
