import { useState } from 'react';
import Dashboard from './pages/Dashboard';
import VMList from './pages/VMList';
import TaskList from './pages/TaskList';
import TaskDetail from './pages/TaskDetail';

type Page = 'dashboard' | 'vms' | 'tasks' | 'task-detail';

function App() {
  const [page, setPage] = useState<Page>('dashboard');
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  const viewTask = (id: string) => {
    setSelectedTaskId(id);
    setPage('task-detail');
  };

  return (
    <div className="min-h-screen bg-gray-950 text-gray-100">
      <nav className="bg-gray-900 border-b border-gray-800 px-6 py-3">
        <div className="max-w-7xl mx-auto flex items-center gap-6">
          <h1
            className="text-lg font-bold text-orange-400 cursor-pointer"
            onClick={() => setPage('dashboard')}
          >
            Orchestrator
          </h1>
          <button
            onClick={() => setPage('dashboard')}
            className={`px-3 py-1 rounded text-sm ${page === 'dashboard' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
          >
            Dashboard
          </button>
          <button
            onClick={() => setPage('vms')}
            className={`px-3 py-1 rounded text-sm ${page === 'vms' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
          >
            VMs
          </button>
          <button
            onClick={() => setPage('tasks')}
            className={`px-3 py-1 rounded text-sm ${page === 'tasks' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
          >
            Tasks
          </button>
        </div>
      </nav>

      <main className="max-w-7xl mx-auto p-6">
        {page === 'dashboard' && <Dashboard onViewTask={viewTask} />}
        {page === 'vms' && <VMList />}
        {page === 'tasks' && <TaskList onViewTask={viewTask} />}
        {page === 'task-detail' && selectedTaskId && (
          <TaskDetail taskId={selectedTaskId} onBack={() => setPage('tasks')} />
        )}
      </main>
    </div>
  );
}

export default App;
