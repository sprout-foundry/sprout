import { ListChecks, Plus, X } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import { getEditorSync } from '../../services/crossTabSync';
import { useAppStoreSetState } from '../../contexts/AppStore';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

interface FoundryTask {
  task_id: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  repo_url: string;
  prompt: string;
  provider?: string;
  model?: string;
  created_at: string;
  updated_at?: string;
}

const TasksPage: React.FC = () => {
  const log = useLog();
  const setState = useAppStoreSetState();

  const [tasks, setTasks] = useState<FoundryTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Create task form state
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [createLoading, setCreateLoading] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [createSuccess, setCreateSuccess] = useState<string | null>(null);
  const [repoUrl, setRepoUrl] = useState('');
  const [prompt, setPrompt] = useState('');
  const [provider, setProvider] = useState('');
  const [model, setModel] = useState('');

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
      const response = await adapter.fetch('/api/tasks');
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

  // Cross-tab sync: refresh task list when the dashboard receives a WS task_update
  useEffect(() => {
    const sync = getEditorSync();
    const unsub = sync.subscribe((msg) => {
      if (msg.type === 'task_update') {
        fetchTasks();
      }
    });
    return unsub;
  }, [fetchTasks]);

  const handleCreateTask = async (e: React.FormEvent) => {
    e.preventDefault();
    const adapter = getAdapter();
    if (!adapter) {
      setCreateError('Not available - running in local mode');
      return;
    }

    setCreateLoading(true);
    setCreateError(null);
    setCreateSuccess(null);

    try {
      const body: Record<string, string> = {
        repo_url: repoUrl,
        prompt,
      };
      if (provider) body.provider = provider;
      if (model) body.model = model;

      const response = await adapter.fetch('/api/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.error || `Failed to create task: ${response.status} ${response.statusText}`);
      }

      const data = await response.json();
      setCreateSuccess(`Task created successfully (ID: ${data.task_id})`);
      setRepoUrl('');
      setPrompt('');
      setProvider('');
      setModel('');
      setShowCreateForm(false);

      // Refresh task list
      await fetchTasks();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create task';
      setCreateError(message);
      log.error(message, { title: 'Create Task Error' });
    } finally {
      setCreateLoading(false);
    }
  };

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

  const formatTaskId = (id: string) => {
    return id.length > 12 ? id.slice(0, 8) + '…' + id.slice(-4) : id;
  };

  const handleTaskClick = useCallback(
    (taskId: string) => {
      setState(() => ({ currentView: 'taskdetail', selectedTaskId: taskId }));
    },
    [setState],
  );

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <h2>Tasks</h2>
            <p>View and manage your background tasks and operations.</p>
          </div>
          <button
            className="platform-button platform-button-primary platform-button-sm"
            onClick={() => setShowCreateForm(true)}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Plus size={14} />
            New Task
          </button>
        </div>
      </div>

      {/* Create Task Form */}
      {showCreateForm && (
        <div className="platform-card" style={{ marginBottom: '24px' }}>
          <div className="platform-card-header">
            <h3 className="platform-card-title">Create New Task</h3>
            <button
              onClick={() => {
                setShowCreateForm(false);
                setCreateError(null);
                setCreateSuccess(null);
              }}
              style={{
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                color: 'var(--text-muted)',
                padding: '4px',
                display: 'flex',
              }}
            >
              <X size={16} />
            </button>
          </div>
          <form onSubmit={handleCreateTask}>
            <div className="platform-card-body">
              {createError && (
                <div
                  style={{
                    padding: '12px',
                    background: 'var(--bg-error, rgba(224, 108, 117, 0.12))',
                    border: '1px solid var(--accent-error)',
                    borderRadius: '6px',
                    color: 'var(--accent-error)',
                    fontSize: '13px',
                    marginBottom: '16px',
                  }}
                >
                  {createError}
                </div>
              )}
              {createSuccess && (
                <div
                  style={{
                    padding: '12px',
                    background: 'var(--bg-success, rgba(152, 195, 121, 0.12))',
                    border: '1px solid var(--accent-success)',
                    borderRadius: '6px',
                    color: 'var(--accent-success)',
                    fontSize: '13px',
                    marginBottom: '16px',
                  }}
                >
                  {createSuccess}
                </div>
              )}

              <div style={{ marginBottom: '12px' }}>
                <label
                  style={{
                    display: 'block',
                    fontSize: '13px',
                    fontWeight: 500,
                    color: 'var(--text-secondary)',
                    marginBottom: '4px',
                  }}
                >
                  Repository URL <span style={{ color: 'var(--accent-error)' }}>*</span>
                </label>
                <input
                  type="text"
                  value={repoUrl}
                  onChange={(e) => setRepoUrl(e.target.value)}
                  placeholder="https://github.com/user/repo"
                  required
                  style={{
                    width: '100%',
                    padding: '8px 12px',
                    background: 'var(--bg-tertiary)',
                    border: '1px solid var(--border-color)',
                    borderRadius: '6px',
                    color: 'var(--text-primary)',
                    fontSize: '14px',
                    outline: 'none',
                  }}
                />
              </div>

              <div style={{ marginBottom: '12px' }}>
                <label
                  style={{
                    display: 'block',
                    fontSize: '13px',
                    fontWeight: 500,
                    color: 'var(--text-secondary)',
                    marginBottom: '4px',
                  }}
                >
                  Prompt <span style={{ color: 'var(--accent-error)' }}>*</span>
                </label>
                <textarea
                  value={prompt}
                  onChange={(e) => setPrompt(e.target.value)}
                  placeholder="Describe what you want the agent to do..."
                  required
                  rows={4}
                  style={{
                    width: '100%',
                    padding: '8px 12px',
                    background: 'var(--bg-tertiary)',
                    border: '1px solid var(--border-color)',
                    borderRadius: '6px',
                    color: 'var(--text-primary)',
                    fontSize: '14px',
                    resize: 'vertical',
                    fontFamily: 'inherit',
                    outline: 'none',
                  }}
                />
              </div>

              <div style={{ display: 'flex', gap: '12px', marginBottom: '16px' }}>
                <div style={{ flex: 1 }}>
                  <label
                    style={{
                      display: 'block',
                      fontSize: '13px',
                      fontWeight: 500,
                      color: 'var(--text-secondary)',
                      marginBottom: '4px',
                    }}
                  >
                    Provider (optional)
                  </label>
                  <input
                    type="text"
                    value={provider}
                    onChange={(e) => setProvider(e.target.value)}
                    placeholder="platform"
                    style={{
                      width: '100%',
                      padding: '8px 12px',
                      background: 'var(--bg-tertiary)',
                      border: '1px solid var(--border-color)',
                      borderRadius: '6px',
                      color: 'var(--text-primary)',
                      fontSize: '14px',
                      outline: 'none',
                    }}
                  />
                </div>
                <div style={{ flex: 1 }}>
                  <label
                    style={{
                      display: 'block',
                      fontSize: '13px',
                      fontWeight: 500,
                      color: 'var(--text-secondary)',
                      marginBottom: '4px',
                    }}
                  >
                    Model (optional)
                  </label>
                  <input
                    type="text"
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                    placeholder="deepseek/deepseek-chat"
                    style={{
                      width: '100%',
                      padding: '8px 12px',
                      background: 'var(--bg-tertiary)',
                      border: '1px solid var(--border-color)',
                      borderRadius: '6px',
                      color: 'var(--text-primary)',
                      fontSize: '14px',
                      outline: 'none',
                    }}
                  />
                </div>
              </div>

              <div style={{ display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
                <button
                  type="button"
                  className="platform-button platform-button-secondary platform-button-sm"
                  onClick={() => {
                    setShowCreateForm(false);
                    setCreateError(null);
                    setCreateSuccess(null);
                  }}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="platform-button platform-button-primary platform-button-sm"
                  disabled={createLoading}
                  style={{ opacity: createLoading ? 0.6 : 1 }}
                >
                  {createLoading ? 'Creating...' : 'Create Task'}
                </button>
              </div>
            </div>
          </form>
        </div>
      )}

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
          <p>Tasks will appear here when you create background operations.</p>
        </div>
      )}

      {!loading && !error && tasks.length > 0 && (
        <div className="platform-list">
          {tasks.map((task) => (
            <div
              key={task.task_id}
              className="platform-list-item platform-list-item--clickable"
              onClick={() => handleTaskClick(task.task_id)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  handleTaskClick(task.task_id);
                }
              }}
            >
              <div className="platform-list-item-icon">
                <ListChecks size={20} />
              </div>
              <div className="platform-list-item-content">
                <div className="platform-list-item-title">{task.prompt || formatTaskId(task.task_id)}</div>
                <div className="platform-list-item-subtitle">{task.repo_url}</div>
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
