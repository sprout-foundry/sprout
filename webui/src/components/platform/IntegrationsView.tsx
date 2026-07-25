/**
 * IntegrationsView — Manage connected git provider accounts.
 *
 * Shows provider cards for GitHub, GitLab, Bitbucket with
 * connect/disconnect status and account info.
 *
 * This is rendered as a view from the EditorWorkspace when the
 * user navigates to the Integrations page.
 */

import React, { useState, useEffect, useCallback } from 'react';
import { GitBranch, Check, X, Loader2, ExternalLink, AlertCircle, KeyRound } from 'lucide-react';
import { getAdapter } from '../../services/apiAdapter';
import './PlatformPages.css';

interface ProviderStatus {
  provider: string;
  connected: boolean;
  accountName?: string;
  accountAvatar?: string;
}

const PROVIDER_META: Record<string, { label: string; color: string; icon: string; docUrl: string }> = {
  github: {
    label: 'GitHub',
    color: '#24292e',
    icon: '🐙',
    docUrl:
      'https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens',
  },
  gitlab: {
    label: 'GitLab',
    color: '#e2432a',
    icon: '🦊',
    docUrl: 'https://docs.gitlab.com/ee/user/profile/personal_access_tokens.html',
  },
  bitbucket: {
    label: 'Bitbucket',
    color: '#0052cc',
    icon: '🔵',
    docUrl: 'https://support.atlassian.com/bitbucket-cloud/docs/create-an-app-password/',
  },
};

const IntegrationsView: React.FC = () => {
  const [statuses, setStatuses] = useState<Record<string, ProviderStatus>>({});
  const [loading, setLoading] = useState(true);
  const [connecting, setConnecting] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [patInputs, setPatInputs] = useState<Record<string, string>>({});

  const fetchStatus = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available in local mode');
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      // Fetch provider connection status from the dedicated endpoint
      const response = await adapter.fetch('/user/me/provider-status');
      if (response.ok) {
        const data = await response.json();
        setStatuses({
          github: {
            provider: 'github',
            connected: data.github?.connected ?? false,
            accountName: data.github?.account_name || undefined,
          },
          gitlab: {
            provider: 'gitlab',
            connected: data.gitlab?.connected ?? false,
          },
          bitbucket: {
            provider: 'bitbucket',
            connected: data.bitbucket?.connected ?? false,
          },
        });
      }
    } catch {
      // fallback: show disconnected for all
      setStatuses({
        github: { provider: 'github', connected: false },
        gitlab: { provider: 'gitlab', connected: false },
        bitbucket: { provider: 'bitbucket', connected: false },
      });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  const handleConnect = async (provider: string) => {
    setConnecting(provider);
    try {
      // Initiate OAuth flow: redirect to the backend
      const adapter = getAdapter();
      const baseUrl = (adapter as { config?: { apiBase?: string } })?.config?.apiBase || '';
      window.location.href = `${baseUrl}/auth/${provider}/login`;
    } catch {
      setConnecting(null);
    }
  };

  const handleDisconnect = async (provider: string) => {
    if (
      !window.confirm(
        `Disconnect ${PROVIDER_META[provider]?.label ?? provider}? This will remove access to repos from this provider.`,
      )
    ) {
      return;
    }

    const adapter = getAdapter();
    if (!adapter) return;

    setConnecting(provider);
    try {
      const response = await adapter.fetch(`/auth/${provider}`, { method: 'DELETE' });
      if (response.ok) {
        setStatuses((prev) => ({ ...prev, [provider]: { provider, connected: false } }));
      }
    } catch {
      // failed
    } finally {
      setConnecting(null);
    }
  };

  const handleSavePat = async (provider: string) => {
    const token = patInputs[provider]?.trim();
    if (!token) return;

    const adapter = getAdapter();
    if (!adapter) return;

    setConnecting(provider);
    try {
      const response = await adapter.fetch(`/auth/${provider}/token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      });
      if (response.ok) {
        setPatInputs((prev) => ({ ...prev, [provider]: '' }));
        setStatuses((prev) => ({
          ...prev,
          [provider]: { provider, connected: true, accountName: 'PAT' },
        }));
      }
    } catch {
      // failed
    } finally {
      setConnecting(null);
    }
  };

  if (loading) {
    return (
      <div className="integrations-view">
        <div className="platform-loading">
          <div className="platform-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="integrations-view">
      <div className="platform-page-header">
        <h2>Connected Accounts</h2>
        <p>
          Link your git provider accounts to access your repositories. Repos from all connected providers appear in your
          unified dashboard.
        </p>
      </div>

      <div className="integrations-grid">
        {Object.entries(PROVIDER_META).map(([key, meta]) => {
          const status = statuses[key];
          const isConnected = status?.connected;
          const isConnecting = connecting === key;

          return (
            <div key={key} className="platform-card integrations-card">
              <div className="integrations-card-header">
                <span className="integrations-icon">{meta.icon}</span>
                <div className="integrations-card-info">
                  <h3>{meta.label}</h3>
                  <span className={`integrations-status ${isConnected ? 'connected' : 'disconnected'}`}>
                    {isConnected ? (
                      <>
                        <Check size={12} /> Connected
                      </>
                    ) : (
                      'Disconnected'
                    )}
                  </span>
                </div>
                {isConnected && status?.accountName && (
                  <span className="integrations-account">{status.accountName}</span>
                )}
              </div>

              <div className="integrations-card-body">
                {isConnected ? (
                  <button
                    className="btn btn-sm integrations-btn-disconnect"
                    onClick={() => handleDisconnect(key)}
                    disabled={isConnecting}
                  >
                    {isConnecting ? <Loader2 size={14} className="spinner" /> : <X size={14} />}
                    Disconnect
                  </button>
                ) : (
                  <div className="integrations-connect-options">
                    {key === 'github' ? (
                      <button
                        className="btn btn-sm btn-primary"
                        onClick={() => handleConnect(key)}
                        disabled={isConnecting}
                      >
                        {isConnecting ? <Loader2 size={14} className="spinner" /> : <ExternalLink size={14} />}
                        Connect {meta.label}
                      </button>
                    ) : (
                      <>
                        <div className="integrations-coming-soon">
                          <AlertCircle size={14} />
                          <span>
                            {meta.label} OAuth coming soon.
                            <br />
                            Use a Personal Access Token in the meantime:
                          </span>
                        </div>
                        <div className="integrations-pat-row">
                          <input
                            type="password"
                            className="input"
                            placeholder="ghp_xxx or gitlab_pat_xxx"
                            value={patInputs[key] ?? ''}
                            onChange={(e) => setPatInputs((prev) => ({ ...prev, [key]: e.target.value }))}
                          />
                          <button
                            className="btn btn-sm"
                            onClick={() => handleSavePat(key)}
                            disabled={isConnecting || !patInputs[key]?.trim()}
                          >
                            <KeyRound size={14} /> Save
                          </button>
                        </div>
                        <a
                          href={meta.docUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="integrations-doc-link"
                        >
                          How to create a {meta.label} token ↗
                        </a>
                      </>
                    )}
                  </div>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {error && <div className="platform-error-banner">{error}</div>}
    </div>
  );
};

export default IntegrationsView;
