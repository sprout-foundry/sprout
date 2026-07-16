import { Server, Play, Square, Trash2, Plus } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

interface Workspace {
  id: string;
  repo_url: string;
  status: string;
  created_at: string;
  updated_at?: string;
  ports?: { port: number; name?: string }[];
}

const WorkspacesPage: React.FC = () => {
  const log = useLog();
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const fetchWorkspaces = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    try {
      const response = await adapter.fetch('/api/workspace/fly');
      if (!response.ok) throw new Error(`Failed: ${response.status}`);
      const data = await response.json();
      setWorkspaces(data.workspaces ?? []);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load workspaces';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchWorkspaces();
  }, [fetchWorkspaces]);

  const handleAction = async (ws: Workspace, action: 'start' | 'stop' | 'destroy') => {
    const adapter = getAdapter();
    if (!adapter) return;
    setBusyId(ws.id);
    try {
      const response = await adapter.fetch(`/api/workspace/fly/${ws.id}/${action}`, { method: 'POST' });
      if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        throw new Error(data.error || `Action failed: ${response.status}`);
      }
      log.info(`Workspace ${action} succeeded`, { title: 'Workspaces' });
      fetchWorkspaces();
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Action failed';
      log.error(msg, { title: 'Workspaces' });
    } finally {
      setBusyId(null);
    }
  };

  if (loading) {
    return (
      <div className="platform-page">
        <div className="platform-loading">
          <div className="platform-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>
          <Server size={20} /> Workspaces
        </h2>
      </div>

      {error && (
        <div className="platform-card">
          <p className="platform-empty-text">
            {error.includes('not configured') || error.includes('FLY_API_TOKEN')
              ? 'Workspaces require Fly.io configuration. Contact your administrator.'
              : error}
          </p>
        </div>
      )}

      {!error && workspaces.length === 0 && (
        <div className="platform-card">
          <p className="platform-empty-text">No active workspaces. Start one from a repo.</p>
        </div>
      )}

      {workspaces.length > 0 && (
        <div className="platform-list">
          {workspaces.map((ws) => (
            <div key={ws.id} className="platform-card workspace-card">
              <div className="workspace-card-header">
                <div className={`status-dot status-dot-${ws.status}`} />
                <span className="workspace-repo">{ws.repo_url?.split('/').slice(-2).join('/') ?? 'Unknown'}</span>
                <span className="workspace-age">{new Date(ws.updated_at || ws.created_at).toLocaleDateString()}</span>
              </div>
              <div className="workspace-card-actions">
                {ws.status !== 'running' && (
                  <button className="btn btn-sm" disabled={busyId === ws.id} onClick={() => handleAction(ws, 'start')}>
                    <Play size={14} /> Start
                  </button>
                )}
                {ws.status === 'running' && (
                  <button className="btn btn-sm" disabled={busyId === ws.id} onClick={() => handleAction(ws, 'stop')}>
                    <Square size={14} /> Stop
                  </button>
                )}
                <button
                  className="btn btn-sm btn-danger"
                  disabled={busyId === ws.id}
                  onClick={() => handleAction(ws, 'destroy')}
                >
                  <Trash2 size={14} /> Destroy
                </button>
              </div>
              {ws.ports && ws.ports.length > 0 && (
                <div className="workspace-ports">
                  {ws.ports.map((p) => (
                    <span key={p.port} className="workspace-port-chip">
                      {p.name || `:${p.port}`}
                    </span>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default WorkspacesPage;
