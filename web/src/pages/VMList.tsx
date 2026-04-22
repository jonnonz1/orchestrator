import { useState } from 'react';
import { useApi, apiPost, apiDelete } from '../hooks/useApi';
import type { VM } from '../types';

export default function VMList() {
  const { data: vms, refresh } = useApi<VM[]>('/vms', 3000);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [ram, setRam] = useState(1024);
  const [vcpus, setVcpus] = useState(2);

  const createVM = async () => {
    if (!name.trim()) return;
    setCreating(true);
    try {
      await apiPost('/vms', { name, ram_mb: ram, vcpus });
      setName('');
      refresh();
    } catch (e: any) {
      alert(e.message);
    } finally {
      setCreating(false);
    }
  };

  const destroyVM = async (vmName: string) => {
    if (!confirm(`Destroy VM "${vmName}"?`)) return;
    try {
      await apiDelete(`/vms/${vmName}`);
      refresh();
    } catch (e: any) {
      alert(e.message);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Virtual Machines</h2>
      </div>

      <div className="bg-gray-900 rounded-lg p-4 border border-gray-800">
        <h3 className="text-sm font-medium text-gray-400 mb-2">Create VM</h3>
        <div className="flex gap-2">
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name"
            className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm flex-1 focus:outline-none focus:border-orange-500" />
          <select value={ram} onChange={(e) => setRam(Number(e.target.value))}
            className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm">
            <option value={512}>512 MB</option>
            <option value={1024}>1 GB</option>
            <option value={2048}>2 GB</option>
            <option value={4096}>4 GB</option>
          </select>
          <select value={vcpus} onChange={(e) => setVcpus(Number(e.target.value))}
            className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm">
            <option value={1}>1 vCPU</option>
            <option value={2}>2 vCPUs</option>
            <option value={4}>4 vCPUs</option>
          </select>
          <button onClick={createVM} disabled={creating || !name.trim()}
            className="bg-orange-600 hover:bg-orange-500 disabled:opacity-50 px-4 py-1.5 rounded text-sm font-medium">
            {creating ? 'Creating...' : 'Create'}
          </button>
        </div>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-gray-800">
            <tr>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">Name</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">State</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">RAM</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">vCPUs</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">Guest IP</th>
              <th className="text-left px-4 py-2 text-gray-400 font-medium">PID</th>
              <th className="text-right px-4 py-2 text-gray-400 font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {(vms || []).length === 0 ? (
              <tr><td colSpan={7} className="px-4 py-8 text-center text-gray-500">No VMs</td></tr>
            ) : (
              (vms || []).map((v) => (
                <tr key={v.name} className="border-t border-gray-800 hover:bg-gray-800/50">
                  <td className="px-4 py-2 font-mono">{v.name}</td>
                  <td className="px-4 py-2">
                    <span className={`px-2 py-0.5 rounded text-xs ${v.state === 'running' ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-400'}`}>
                      {v.state}
                    </span>
                  </td>
                  <td className="px-4 py-2">{v.ram_mb} MB</td>
                  <td className="px-4 py-2">{v.vcpus}</td>
                  <td className="px-4 py-2 font-mono text-gray-400">{v.guest_ip}</td>
                  <td className="px-4 py-2 text-gray-400">{v.pid}</td>
                  <td className="px-4 py-2 text-right">
                    <button onClick={() => destroyVM(v.name)}
                      className="text-red-400 hover:text-red-300 text-xs px-2 py-1 rounded hover:bg-red-900/30">
                      Destroy
                    </button>
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
