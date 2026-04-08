import { useState, useCallback, useEffect, useRef } from 'react';
import { ApiService } from '../services/api';
import { useNotifications } from '../contexts/NotificationContext';
import { Pencil, Plus, Trash2, Lock, RefreshCw, ChevronDown, ChevronRight, Key } from 'lucide-react';
import { debugLog } from '../utils/log';
import './SettingsPanel.css';

interface CredentialProvider {
  provider: string;
  display_name: string;
  env_var: string;
  requires_api_key: boolean;
  has_stored_credential: boolean;
  has_env_credential: boolean;
  credential_source: string;
  masked_value: string;
  key_pool_size: number;
}

interface CredentialsResponse {
  storage_backend: string;
  providers: CredentialProvider[];
}

/** Truncate long error messages for display */
function truncateError(error: string, maxLength: number = 100): string {
  if (error.length <= maxLength) {
    return error;
  }
  return error.slice(0, maxLength) + '…';
}

function CredentialsSettingsTab(): JSX.Element {
  const [providers, setProviders] = useState<CredentialProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [storageBackend, setStorageBackend] = useState<string>('');
  const [editingProvider, setEditingProvider] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [pendingDeleteProvider, setPendingDeleteProvider] = useState<string | null>(null);
  const deleteTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [testingProvider, setTestingProvider] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, { success: boolean; error?: string; model_count?: number }>>({});
  const [expandedPoolProvider, setExpandedPoolProvider] = useState<string | null>(null);
  const [poolKeys, setPoolKeys] = useState<Record<string, string[]>>({});
  const [addPoolValue, setAddPoolValue] = useState('');
  const [poolActionLoading, setPoolActionLoading] = useState(false);

  const { addNotification } = useNotifications();
  const api = ApiService.getInstance();

  const fetchCredentials = useCallback(async () => {
    try {
      setLoading(true);
      const data: CredentialsResponse = await api.getProviderCredentials();
      setProviders(data.providers || []);
      setStorageBackend(data.storage_backend || 'unknown');
    } catch (err) {
      debugLog('[CredentialsSettingsTab] failed to fetch credentials:', err);
      addNotification('error', 'Credentials', 'Failed to load credentials', 5000);
    } finally {
      setLoading(false);
    }
  }, [api, addNotification]);

  useEffect(() => {
    fetchCredentials();
    return () => {
      if (deleteTimerRef.current) {
        clearTimeout(deleteTimerRef.current);
        deleteTimerRef.current = null;
      }
    };
  }, [fetchCredentials]);

  const handleEditStart = (provider: CredentialProvider) => {
    setEditingProvider(provider.provider);
    setEditValue('');
  };

  const handleEditCancel = () => {
    setEditingProvider(null);
    setEditValue('');
  };

  const handleEditSave = async () => {
    if (!editingProvider || !editValue.trim()) {
      addNotification('info', 'Credentials', 'Please enter an API key', 3000);
      return;
    }

    setSaving(true);
    try {
      await api.setProviderCredential(editingProvider, editValue.trim());
      addNotification('success', 'Credentials', 'API key saved', 3000);
      setTestResults({});
      handleEditCancel();
      await fetchCredentials();
    } catch (err) {
      debugLog('[CredentialsSettingsTab] failed to save credential:', err);
      addNotification('error', 'Credentials', 'Failed to save API key', 5000);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = (provider: CredentialProvider) => {
    if (!provider.has_stored_credential) {
      addNotification('info', 'Credentials', 'No stored credential to delete', 3000);
      return;
    }

    if (pendingDeleteProvider === provider.provider) {
      // Second click — confirm delete
      if (deleteTimerRef.current) {
        clearTimeout(deleteTimerRef.current);
        deleteTimerRef.current = null;
      }
      setPendingDeleteProvider(null);

      api.deleteProviderCredential(provider.provider)
        .then(() => {
          addNotification('success', 'Credentials', `${provider.display_name} credential deleted`, 3000);
          setTestResults(prev => {
            const next = { ...prev };
            delete next[provider.provider];
            return next;
          });
          fetchCredentials();
        })
        .catch((err) => {
          debugLog('[CredentialsSettingsTab] failed to delete credential:', err);
          addNotification('error', 'Credentials', 'Failed to delete credential', 5000);
        });
    } else {
      // First click — enter confirmation state
      setPendingDeleteProvider(provider.provider);
      deleteTimerRef.current = setTimeout(() => {
        setPendingDeleteProvider(null);
        deleteTimerRef.current = null;
      }, 3000);
    }
  };

  const handleTestConnection = async (provider: CredentialProvider) => {
    setTestingProvider(provider.provider);
    setTestResults(prev => {
      const next = { ...prev };
      delete next[provider.provider];
      return next;
    });

    try {
      const result = await api.testProviderConnection(provider.provider);
      setTestResults(prev => ({
        ...prev,
        [provider.provider]: {
          success: result.success,
          error: result.error,
          model_count: result.model_count,
        },
      }));
    } catch (err) {
      setTestResults(prev => ({
        ...prev,
        [provider.provider]: {
          success: false,
          error: err instanceof Error ? err.message : String(err),
        },
      }));
    } finally {
      setTestingProvider(null);
    }
  };

  const handlePoolToggle = async (provider: CredentialProvider) => {
    if (expandedPoolProvider === provider.provider) {
      setExpandedPoolProvider(null);
      return;
    }

    try {
      setPoolActionLoading(true);
      const poolData = await api.getKeyPool(provider.provider);
      setPoolKeys(prev => ({
        ...prev,
        [provider.provider]: poolData.masked_keys || [],
      }));
      setExpandedPoolProvider(provider.provider);
      setAddPoolValue('');
    } catch (err) {
      debugLog('[CredentialsSettingsTab] failed to fetch pool keys:', err);
      addNotification('error', 'Credentials', 'Failed to load key pool', 5000);
    } finally {
      setPoolActionLoading(false);
    }
  };

  const handleAddToPool = async (provider: CredentialProvider) => {
    if (!addPoolValue.trim()) {
      addNotification('info', 'Credentials', 'Please enter a key value', 3000);
      return;
    }

    setPoolActionLoading(true);
    try {
      await api.addKeyToPool(provider.provider, addPoolValue.trim());
      addNotification('success', 'Credentials', 'Key added to pool', 3000);
      setAddPoolValue('');
      await fetchCredentials();
      // Refresh pool keys if expanded
      if (expandedPoolProvider === provider.provider) {
        const poolData = await api.getKeyPool(provider.provider);
        setPoolKeys(prev => ({
          ...prev,
          [provider.provider]: poolData.masked_keys || [],
        }));
      }
    } catch (err) {
      debugLog('[CredentialsSettingsTab] failed to add key to pool:', err);
      addNotification('error', 'Credentials', 'Failed to add key to pool', 5000);
    } finally {
      setPoolActionLoading(false);
    }
  };

  const handleRemoveFromPool = async (provider: CredentialProvider, index: number) => {
    setPoolActionLoading(true);
    try {
      await api.removeKeyFromPool(provider.provider, index);
      addNotification('success', 'Credentials', 'Key removed from pool', 3000);
      // Refresh pool keys and provider list
      const [poolData] = await Promise.all([
        api.getKeyPool(provider.provider).catch(() => null),
        fetchCredentials(),
      ]);
      if (poolData) {
        setPoolKeys(prev => ({
          ...prev,
          [provider.provider]: poolData.masked_keys || [],
        }));
      }
      // Collapse pool view if only 1 key remains (back to single-key edit mode)
      if (expandedPoolProvider === provider.provider && poolData && poolData.key_count <= 1) {
        setExpandedPoolProvider(null);
        setPoolKeys((prev) => ({ ...prev, [provider.provider]: [] }));
      }
    } catch (err) {
      debugLog('[CredentialsSettingsTab] failed to remove key from pool:', err);
      addNotification('error', 'Credentials', 'Failed to remove key from pool', 5000);
    } finally {
      setPoolActionLoading(false);
    }
  };

  const getStorageBackendLabel = (): string => {
    switch (storageBackend) {
      case 'keyring':
        return 'OS keyring';
      case 'stored':
        return 'encrypted file';
      default:
        return storageBackend || 'storage';
    }
  };

  const renderSourceBadge = (source: string, keyPoolSize: number = 0) => {
    const baseStyle = {
      display: 'inline-flex',
      alignItems: 'center',
      padding: '2px 8px',
      borderRadius: '12px',
      fontSize: '10px',
      fontWeight: 600,
      textTransform: 'uppercase',
      marginLeft: '8px',
      flexShrink: 0,
    };

    if (keyPoolSize > 1) {
      return <span style={{ ...baseStyle, background: 'color-mix(in srgb, var(--color-warning, #f59e0b) 15%, var(--bg-elevated, #fff))', color: 'var(--color-warning, #f59e0b)' }}>pool</span>;
    }

    switch (source) {
      case 'environment':
        return <span style={{ ...baseStyle, background: 'color-mix(in srgb, var(--color-success, #22c55e) 15%, var(--bg-elevated, #fff))', color: 'var(--color-success, #22c55e)' }}>env</span>;
      case 'stored':
        return <span style={{ ...baseStyle, background: 'color-mix(in srgb, var(--color-info, #3b82f6) 15%, var(--bg-elevated, #fff))', color: 'var(--color-info, #3b82f6)' }}>stored</span>;
      default:
        return <span style={{ ...baseStyle, background: 'color-mix(in srgb, var(--text-muted, #888) 10%, var(--bg-elevated, #fff))', color: 'var(--text-muted, #888)' }}>none</span>;
    }
  };

  const renderStatusIndicator = (provider: CredentialProvider) => {
    if (!provider.requires_api_key) {
      return <span style={{ fontSize: '11px', color: 'var(--text-muted)', marginLeft: '8px' }}>(no key required)</span>;
    }

    const isConfigured = provider.has_stored_credential || provider.has_env_credential;
    return (
      <span
        title={isConfigured ? 'Credential is configured' : 'Credential is not configured'}
        style={{
          display: 'inline-block',
          width: '8px',
          height: '8px',
          borderRadius: '50%',
          backgroundColor: isConfigured ? 'var(--color-success, #22c55e)' : 'var(--text-muted, #888)',
          marginLeft: '8px',
          flexShrink: 0,
        }}
      />
    );
  };

  if (loading) {
    return (
      <div className="section">
        <div className="settings-empty">Loading credentials…</div>
      </div>
    );
  }

  return (
    <div className="section">
      <style>{`
        @keyframes spin {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }
      `}</style>
      {/* Header with storage info */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: 'var(--space-4)' }}>
        <Lock size={16} color="var(--text-muted)" />
        <span style={{ fontSize: '14px', color: 'var(--text-muted)' }}>
          Credentials are stored in: {getStorageBackendLabel()}
        </span>
      </div>

      {/* Provider list */}
      <div className="crud-list">
        {providers.length === 0 && (
          <div className="settings-empty">No providers with credentials configured</div>
        )}

        {providers.map((provider) => {
          const isEditing = editingProvider === provider.provider;
          const isEnvOnly = provider.has_env_credential && !provider.has_stored_credential;
          const isPoolExpanded = expandedPoolProvider === provider.provider;
          const providerPoolKeys = poolKeys[provider.provider] || [];
          const hasPool = provider.key_pool_size > 1;

          return (
            <div key={provider.provider} className="crud-item">
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                  <span className="crud-item-name">{provider.display_name}</span>
                  {renderSourceBadge(provider.credential_source, provider.key_pool_size)}
                  {hasPool && (
                    <span
                      style={{
                        fontSize: '10px',
                        color: 'var(--text-tertiary)',
                        marginLeft: '4px',
                      }}
                    >
                      ({provider.key_pool_size} keys)
                    </span>
                  )}
                  {renderStatusIndicator(provider)}
                </div>
                {provider.masked_value && (
                  <span
                    style={{
                      fontSize: '12px',
                      color: 'var(--text-muted)',
                      fontFamily: 'monospace',
                      display: 'block',
                      marginTop: '4px',
                    }}
                  >
                    {provider.masked_value}
                  </span>
                )}
                {provider.env_var && (
                  <span
                    style={{
                      opacity: 0.6,
                      fontSize: '11px',
                      fontFamily: 'monospace',
                      display: 'block',
                      marginTop: '2px',
                    }}
                  >
                    {provider.env_var}
                  </span>
                )}
                {testResults[provider.provider] && (
                  <span
                    style={{
                      fontSize: '11px',
                      marginTop: '4px',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '4px',
                      color: testResults[provider.provider].success
                        ? 'var(--color-success, #22c55e)'
                        : 'var(--color-error, #ef4444)',
                      maxWidth: '420px',
                      wordBreak: 'break-word',
                    }}
                  >
                    {testResults[provider.provider].success ? '✓' : '✗'}
                    {testResults[provider.provider]?.success
                      ? `Connected — ${testResults[provider.provider].model_count ?? 0} models available`
                      : truncateError(testResults[provider.provider]?.error || 'Unknown error')}
                  </span>
                )}
              </div>

              {/* Actions */}
              <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
                {!isEditing && (
                  <>
                    {hasPool ? (
                      <button
                        type="button"
                        className="crud-btn"
                        title={isPoolExpanded ? 'Collapse pool' : 'Expand pool'}
                        onClick={() => handlePoolToggle(provider)}
                        disabled={poolActionLoading}
                      >
                        {isPoolExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                      </button>
                    ) : (
                      provider.requires_api_key && (
                        <button
                          type="button"
                          className="crud-btn"
                          title={isEnvOnly ? 'Add stored key' : 'Edit API key'}
                          onClick={() => handleEditStart(provider)}
                        >
                          {isEnvOnly ? <Plus size={12} /> : <Pencil size={12} />}
                        </button>
                      )
                    )}
                    {provider.has_stored_credential && !hasPool && (
                      <button
                        type="button"
                        className="crud-btn danger"
                        title={
                          pendingDeleteProvider === provider.provider
                            ? 'Click again to confirm deletion'
                            : 'Delete stored credential'
                        }
                        onClick={() => handleDelete(provider)}
                        style={
                          pendingDeleteProvider === provider.provider
                            ? { animation: 'settings-pulse 1s ease-in-out infinite' }
                            : undefined
                        }
                      >
                        <Trash2 size={12} />
                      </button>
                    )}
                    <button
                      type="button"
                      className="crud-btn"
                      title={testingProvider === provider.provider
                        ? 'Testing connection…'
                        : !provider.requires_api_key
                          ? 'Test if local provider service is reachable'
                          : provider.has_stored_credential || provider.has_env_credential
                            ? 'Test connection'
                            : 'No credential configured - save a key first'}
                      onClick={() => handleTestConnection(provider)}
                      disabled={testingProvider === provider.provider ||
                        (provider.requires_api_key && !provider.has_stored_credential && !provider.has_env_credential)}
                    >
                      <RefreshCw
                        size={12}
                        style={testingProvider === provider.provider ? { animation: 'spin 1s linear infinite' } : undefined}
                      />
                    </button>
                  </>
                )}
              </div>

              {/* Pool section (when expanded) */}
              {hasPool && isPoolExpanded && (
                <div className="crud-inline-form" style={{ marginTop: 'var(--space-3)' }}>
                  <div style={{ marginBottom: 'var(--space-3)' }}>
                    <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-tertiary)', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '4px' }}>
                      <Key size={12} />
                      Configured Keys ({providerPoolKeys.length})
                    </div>
                    {providerPoolKeys.length === 0 ? (
                      <div style={{ fontSize: '11px', color: 'var(--text-muted)', fontStyle: 'italic' }}>No keys in pool</div>
                    ) : (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                        {providerPoolKeys.map((maskedKey, idx) => (
                          <div
                            key={idx}
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: '8px',
                              padding: '6px 8px',
                              background: 'var(--bg-surface)',
                              borderRadius: '6px',
                              fontSize: '11px',
                              fontFamily: 'monospace',
                            }}
                          >
                            <span style={{ color: 'var(--text-secondary)', flex: 1 }}>{maskedKey}</span>
                            <button
                              type="button"
                              className="crud-btn danger"
                              title="Remove key"
                              onClick={() => handleRemoveFromPool(provider, idx)}
                              disabled={poolActionLoading}
                            >
                              <Trash2 size={10} />
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  {/* Add key form */}
                  <div className="form-row">
                    <label>Add New Key</label>
                    <input
                      type="password"
                      className="styled-input"
                      value={addPoolValue}
                      onChange={(e) => setAddPoolValue(e.target.value)}
                      placeholder="Enter new API key"
                      disabled={poolActionLoading}
                    />
                  </div>
                  <div className="form-actions">
                    <button
                      type="button"
                      className="form-btn primary"
                      onClick={() => handleAddToPool(provider)}
                      disabled={poolActionLoading || !addPoolValue.trim()}
                    >
                      {poolActionLoading ? 'Adding…' : 'Add to Pool'}
                    </button>
                  </div>
                </div>
              )}
            </div>
          );
        })}

        {/* Inline edit form */}
        {editingProvider && (
          <div className="crud-inline-form">
            <div className="form-row">
              <label>API Key for {providers.find((p) => p.provider === editingProvider)?.display_name}</label>
              <input
                type="password"
                className="styled-input"
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                placeholder="Enter API key"
                autoFocus
              />
            </div>
            <div className="form-actions">
              <button
                type="button"
                className="form-btn primary"
                onClick={handleEditSave}
                disabled={saving || !editValue.trim()}
              >
                {saving ? 'Saving…' : 'Save'}
              </button>
              <button type="button" className="form-btn cancel" onClick={handleEditCancel}>
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default CredentialsSettingsTab;
