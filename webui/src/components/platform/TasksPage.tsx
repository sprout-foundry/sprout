import React, { useState, useEffect, useCallback } from 'react';
import { ListChecks } from 'lucide-react';
import { getAdapter } from '../../services/apiAdapter';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

interface FoundryTask {
  id: string;
  title: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  created_at: string;
  updated_at?: string;
  description?: string;
}

const TasksPage: React.FC = () => {
  const log = useLog();

  const [tasks, setTasks] = useState<FoundryTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchTasks = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available - running in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const response = await adapter.fetch('/api/foundry/tasks');
      if (!response.ok) {
        throw new Error(`Failed to fetch tasks: ${response.status} ${response.statusText}`);
      }
      const data = await response.json();
      setTasks(Array.isArray(data.tasks) ? data.tasks : []);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load tasks';
      setError(message);
      log.error(message, { title: 'Tasks Page Error' });
    } finally {
      setLoading(false);
    }
  }, [log]);

  useEffect(() => {
    fetchTasks();
  }, [fetchTasks]);

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  };

  const formatTime = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>Tasks</h2>
        <p>View and manage your background tasks and operations.</p>
      </div>

      {loading && <div className="platform-page-loading">Loading tasks...</div>}

      {error && (
        <div className="platform-page-error">
          <h3>Error loading tasks</h3>
          <p>{error}</p>
          <button
            className="platform-button platform-button-secondary platform-button-sm"
            onClick={fetchTasks}
            style={{ marginTop: '16px' }}
          >
            Retry
          </button>
        </div>
      )}

      {!loading && !error && tasks.length === 0 && (
        <div className="platform-page-empty">
          <div className="platform-page-empty-icon">
            <ListChecks size={48} />
          </div>
          <h3>No tasks found</h3>
          <p>Tasks will appear here when you run background operations.</p>
        </div>
      )}

      {!loading && !error && tasks.length > 0 && (
        <div className="platform-list">
          {tasks.map((task) => (
            <div key={task.id} className="platform-list-item">
              <div className="platform-list-item-icon">
                <ListChecks size={20} />
              </div>
              <div className="platform-list-item-content">
                <div className="platform-list-item-title">{task.title}</div>
                <div className="platform-list-item-subtitle">{task.description || 'No description'}</div>
              </div>
              <div className="platform-list-item-meta">
                <span className={`platform-status-badge ${task.status}`}>{task.status}</span>
                <div className="platform-list-item-time">
                  <div>{formatDate(task.created_at)}</div>
                  <div>{formatTime(task.created_at)}</div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default TasksPage;
