import { Server, Copy, Check, RefreshCw, Trash2, Plus, AlertTriangle, KeyRound } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

/** A registered CI runner / build agent. */
interface FoundryRunner {
  id: string;
  name: string;
  /** Whether the runner has recently checked in with the platform. */
  status: 'online' | 'offline';
  /** ISO timestamp of the runner's most recent heartbeat. */
  last_seen?: string;
}

/** Response shape from POST /runners/token. */
interface GenerateTokenResponse {
  token: string;
}

const RunnersPage: React.FC = () => {
  const log = useLog();

  const [runners, setRunners] = useState<FoundryRunner[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Token generation form state
  const [tokenName, setTokenName] = useState('');
  const [generatedToken, setGeneratedToken] = useState<string | null>(null);
  const [generateLoading, setGenerateLoading] = useState(false);
  const [generateError, setGenerateError] = useState<string | null>(null);
  const [generateSuccess, setGenerateSuccess] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Per-runner action tracking (which runner is currently being mutated)
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null);

  const fetchRunners = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available - running in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const response = await adapter.fetch('/api/runners');
      if (!response.ok) {
        throw new Error(`Failed to fetch runners: ${response.status} ${response.statusText}`);
      }
      const data = await response.json();
      setRunners(Array.isArray(data.runners) ? data.runners : Array.isArray(data) ? data : []);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load runners';
      setError(message);
      log.error(message, { title: 'Runners Page Error' });
    } finally {
      setLoading(false);
    }
  }, [log]);

  useEffect(() => {
    fetchRunners();
  }, [fetchRunners]);

  const handleGenerateToken = async (e: React.FormEvent) => {
    e.preventDefault();
    const adapter = getAdapter();
    if (!adapter) {
      setGenerateError('Not available - running in local mode');
      return;
    }

    const trimmedName = tokenName.trim();
    if (!trimmedName) {
      setGenerateError('A name is required');
      return;
    }

    setGenerateLoading(true);
    setGenerateError(null);
    setGenerateSuccess(null);
    setGeneratedToken(null);
    setCopied(false);

    try {
      const response = await adapter.fetch('/api/runners/token', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: trimmedName }),
      });

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.error || `Failed to generate token: ${response.status} ${response.statusText}`);
      }

      const data = (await response.json()) as GenerateTokenResponse;
      setGeneratedToken(data.token);
      setGenerateSuccess('Token generated. Copy it now — it will not be shown again.');
      setTokenName('');

      // Refresh runner list (a new runner row may appear)
      await fetchRunners();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to generate token';
      setGenerateError(message);
      log.error(message, { title: 'Generate Token Error' });
    } finally {
      setGenerateLoading(false);
    }
  };

  const handleCopyToken = async () => {
    if (!generatedToken) return;
    try {
      await navigator.clipboard.writeText(generatedToken);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to copy token';
      log.error(message, { title: 'Copy Token Error' });
    }
  };

  const handleRotateKey = async (runnerId: string) => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available - running in local mode');
      return;
    }

    setActionLoadingId(runnerId);
    setError(null);

    try {
      const response = await adapter.fetch(`/api/runners/${encodeURIComponent(runnerId)}/rotate-key`, {
        method: 'POST',
      });

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.error || `Failed to rotate key: ${response.status} ${response.statusText}`);
      }

      // A rotated key may return a new token; surface it like a freshly generated one.
      const data = await response.json().catch(() => null);
      const newToken = data?.token;
      if (typeof newToken === 'string' && newToken.length > 0) {
        setGeneratedToken(newToken);
        setGenerateSuccess('Key rotated. Copy the new token now — it will not be shown again.');
        setGenerateError(null);
        setCopied(false);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to rotate key';
      setError(message);
      log.error(message, { title: 'Rotate Key Error' });
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleDeleteRunner = async (runnerId: string, runnerName: string) => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available - running in local mode');
      return;
    }

    const confirmed = window.confirm(
      `Delete runner "${runnerName}"? This cannot be undone and the runner will no longer be able to connect.`,
    );
    if (!confirmed) return;

    setActionLoadingId(runnerId);
    setError(null);

    try {
      const response = await adapter.fetch(`/api/runners/${encodeURIComponent(runnerId)}`, {
        method: 'DELETE',
      });

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.error || `Failed to delete runner: ${response.status} ${response.statusText}`);
      }

      // Remove the runner from local state immediately
      setRunners((prev) => prev.filter((r) => r.id !== runnerId));
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to delete runner';
      setError(message);
      log.error(message, { title: 'Delete Runner Error' });
      // Refresh in case the server state differs from our optimistic update
      fetchRunners();
    } finally {
      setActionLoadingId(null);
    }
  };

  const formatLastSeen = (dateString?: string) => {
    if (!dateString) return 'Never';
    const date = new Date(dateString);
    if (Number.isNaN(date.getTime())) return 'Unknown';
    return date.toLocaleString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>Runners</h2>
        <p>Manage CI runners and build agents connected to your workspace.</p>
      </div>

      {/* Generate Token */}
      <div className="platform-card" style={{ marginBottom: '24px' }}>
        <div className="platform-card-header">
          <h3 className="platform-card-title">Generate Runner Token</h3>
          <KeyRound size={18} style={{ color: 'var(--text-muted)' }} />
        </div>
        <div className="platform-card-body">
          <p style={{ margin: '0 0 16px 0' }}>
            Create a token to register a new runner. The token authenticates the runner against the platform.
          </p>

          {generateError && (
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
              {generateError}
            </div>
          )}

          {generateSuccess && (
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
              {generateSuccess}
            </div>
          )}

          <form onSubmit={handleGenerateToken} style={{ display: 'flex', gap: '12px', flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: '240px' }}>
              <input
                type="text"
                value={tokenName}
                onChange={(e) => setTokenName(e.target.value)}
                placeholder="Runner name (e.g. ci-agent-1)"
                aria-label="Runner name"
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
            <button
              type="submit"
              className="platform-button platform-button-primary platform-button-sm"
              disabled={generateLoading}
              style={{ opacity: generateLoading ? 0.6 : 1, display: 'flex', alignItems: 'center', gap: '6px' }}
            >
              <Plus size={14} />
              {generateLoading ? 'Generating...' : 'Generate Token'}
            </button>
          </form>

          {/* Generated token display — shown once, with a copy button + warning */}
          {generatedToken && (
            <div
              style={{
                marginTop: '16px',
                padding: '16px',
                background: 'var(--bg-tertiary)',
                border: '1px solid var(--border-color)',
                borderRadius: '6px',
              }}
            >
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  marginBottom: '8px',
                  color: 'var(--accent-warning)',
                  fontSize: '13px',
                  fontWeight: 500,
                }}
              >
                <AlertTriangle size={14} />
                This token is shown only once. Copy it now.
              </div>
              <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                <code
                  style={{
                    flex: 1,
                    padding: '8px 12px',
                    background: 'var(--bg-secondary)',
                    border: '1px solid var(--border-color)',
                    borderRadius: '4px',
                    fontFamily: 'monospace',
                    fontSize: '13px',
                    color: 'var(--text-primary)',
                    wordBreak: 'break-all',
                  }}
                >
                  {generatedToken}
                </code>
                <button
                  type="button"
                  onClick={handleCopyToken}
                  className="platform-button platform-button-secondary platform-button-sm"
                  style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
                  aria-label="Copy token"
                >
                  {copied ? <Check size={14} /> : <Copy size={14} />}
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Runner list */}
      {loading && <div className="platform-page-loading">Loading runners...</div>}

      {error && (
        <div className="platform-page-error">
          <h3>Error loading runners</h3>
          <p>{error}</p>
          <button
            className="platform-button platform-button-secondary platform-button-sm"
            onClick={fetchRunners}
            style={{ marginTop: '16px' }}
          >
            Retry
          </button>
        </div>
      )}

      {!loading && !error && runners.length === 0 && (
        <div className="platform-page-empty">
          <div className="platform-page-empty-icon">
            <Server size={48} />
          </div>
          <h3>No runners registered yet</h3>
          <p>Generate a token above to register your first runner.</p>
        </div>
      )}

      {!loading && !error && runners.length > 0 && (
        <div className="platform-list">
          {runners.map((runner) => {
            const isLoading = actionLoadingId === runner.id;
            return (
              <div key={runner.id} className="platform-list-item">
                <div className="platform-list-item-icon">
                  <Server size={20} />
                </div>
                <div className="platform-list-item-content">
                  <div className="platform-list-item-title">{runner.name}</div>
                  <div className="platform-list-item-subtitle">Last seen: {formatLastSeen(runner.last_seen)}</div>
                </div>
                <div className="platform-list-item-meta">
                  <span className={`platform-status-badge ${runner.status === 'online' ? 'running' : 'cancelled'}`}>
                    {runner.status}
                  </span>
                  <div style={{ display: 'flex', gap: '8px', marginTop: '4px' }}>
                    <button
                      type="button"
                      className="platform-button platform-button-secondary platform-button-sm"
                      onClick={() => handleRotateKey(runner.id)}
                      disabled={isLoading}
                      style={{
                        opacity: isLoading ? 0.6 : 1,
                        display: 'flex',
                        alignItems: 'center',
                        gap: '4px',
                      }}
                      title="Rotate runner key"
                    >
                      <RefreshCw size={13} />
                      Rotate Key
                    </button>
                    <button
                      type="button"
                      className="platform-button platform-button-sm"
                      onClick={() => handleDeleteRunner(runner.id, runner.name)}
                      disabled={isLoading}
                      style={{
                        opacity: isLoading ? 0.6 : 1,
                        display: 'flex',
                        alignItems: 'center',
                        gap: '4px',
                        background: 'var(--bg-error, rgba(224, 108, 117, 0.12))',
                        color: 'var(--accent-error)',
                        border: '1px solid var(--accent-error)',
                      }}
                      title="Delete runner"
                    >
                      <Trash2 size={13} />
                      Delete
                    </button>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default RunnersPage;
