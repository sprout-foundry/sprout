import { useState, useCallback, useEffect, useRef } from 'react';
import { ApiService } from '../services/api';
import { useNotifications } from '../contexts/NotificationContext';
import { Pencil, Plus, Trash2, Lock } from 'lucide-react';
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
}

interface CredentialsResponse {
  storage_backend: string;
  providers: CredentialProvider[];
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

  const getStorageBackendLabel = (): string => {
    switch (storageBackend) {
      case 'keyring':
        return 'OS keyring';
      case 'stored':
        return 'encrypted file';
      case 'file':
        return 'encrypted file';
      default:
        return storageBackend || 'storage';
    }
  };

  const renderSourceBadge = (source: string) => {
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

    switch (source) {
      case 'environment':
        return <span style={{ ...baseStyle, background: '#e8f5e9', color: '#2e7d32' }}>env</span>;
      case 'stored':
        return <span style={{ ...baseStyle, background: '#e3f2fd', color: '#1565c0' }}>stored</span>;
      default:
        return <span style={{ ...baseStyle, background: '#f5f5f5', color: '#757575' }}>none</span>;
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

          return (
            <div key={provider.provider} className="crud-item">
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                  <span className="crud-item-name">{provider.display_name}</span>
                  {renderSourceBadge(provider.credential_source)}
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
              </div>

              {/* Actions */}
              <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
                {!isEditing && (
                  <>
                    {provider.requires_api_key && (
                      <button
                        type="button"
                        className="crud-btn"
                        title={isEnvOnly ? 'Add stored key' : 'Edit API key'}
                        onClick={() => handleEditStart(provider)}
                      >
                        {isEnvOnly ? <Plus size={12} /> : <Pencil size={12} />}
                      </button>
                    )}
                    {provider.has_stored_credential && (
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
                            ? { animation: 'pulse 1s infinite' }
                            : undefined
                        }
                      >
                        <Trash2 size={12} />
                      </button>
                    )}
                  </>
                )}
              </div>
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
