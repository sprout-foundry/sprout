import { Pencil, Plus, Trash2, Lock } from 'lucide-react';
import type { SproutSettings } from '../../services/api';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface MCPSettingsTabProps {
  settings: SproutSettings;
  editingServer: { mode: 'add' | 'edit'; originalName?: string } | null;
  serverName: string;
  serverCommand: string;
  serverArgs: string;
  serverEnvVars: Array<{ key: string; value: string }>;
  newEnvKey: string;
  newEnvValue: string;
  credentialServer: string | null;
  credentialEntries: Array<{ key: string; value: string; status: string }>;
  credentialLoading: boolean;
  newCredentialKey: string;
  newCredentialValue: string;
  savingKey: string | null;
  setEditingServer: (v: { mode: 'add' | 'edit'; originalName?: string } | null) => void;
  setServerName: (v: string) => void;
  setServerCommand: (v: string) => void;
  setServerArgs: (v: string) => void;
  setServerEnvVars: (v: Array<{ key: string; value: string }>) => void;
  setNewEnvKey: (v: string) => void;
  setNewEnvValue: (v: string) => void;
  setCredentialEntries: (v: Array<{ key: string; value: string; status: string }>) => void;
  setNewCredentialKey: (v: string) => void;
  setNewCredentialValue: (v: string) => void;
  renderToggle: FieldRenderers['renderToggle'];
  renderTextInput: FieldRenderers['renderTextInput'];
  resetServerForm: () => void;
  handleAddServer: () => Promise<void>;
  handleUpdateServer: () => Promise<void>;
  handleDeleteServer: (name: string) => Promise<void>;
  handleLoadCredentials: (name: string) => Promise<void>;
  handleSaveCredential: () => Promise<void>;
  handleDeleteCredential: (credName: string) => Promise<void>;
  handleAddCredentialEntry: () => void;
  handleCloseCredentials: () => void;
}

export default function MCPSettingsTab({
  settings,
  editingServer,
  serverName, serverCommand, serverArgs, serverEnvVars, newEnvKey, newEnvValue,
  credentialServer, credentialEntries, credentialLoading, newCredentialKey, newCredentialValue,
  savingKey,
  setEditingServer, setServerName, setServerCommand, setServerArgs, setServerEnvVars,
  setNewEnvKey, setNewEnvValue, setCredentialEntries, setNewCredentialKey, setNewCredentialValue,
  renderToggle, renderTextInput,
  resetServerForm, handleAddServer, handleUpdateServer, handleDeleteServer,
  handleLoadCredentials, handleSaveCredential, handleDeleteCredential,
  handleAddCredentialEntry, handleCloseCredentials,
}: MCPSettingsTabProps) {
  const mcpSettings = settings.mcp || {};
  const servers = mcpSettings.servers || {};
  const serverEntries = Object.entries(servers);

  return (
    <div className="section">
      <h4>MCP Configuration</h4>

      {renderToggle('mcp.enabled', 'MCP enabled')}
      {renderToggle('mcp.auto_start', 'Auto-start servers')}
      {renderToggle('mcp.auto_discover', 'Auto-discover servers')}
      {renderTextInput('mcp.timeout', 'Timeout (e.g. 30s)', '30s')}

      <div style={{ marginTop: 'var(--space-5)' }}>
        <h4>Servers ({serverEntries.length})</h4>

        {serverEntries.length === 0 && !editingServer && (
          <div className="settings-empty">No MCP servers configured</div>
        )}

        <div className="crud-list">
          {serverEntries.map(([name, cfg]) => {
            const server = cfg as Record<string, unknown>;
            return (
              <div key={name} className="crud-item">
                <span className="crud-item-name">{name}</span>
                <span className="crud-item-detail">{(server.command as string) || ''}</span>
                <button
                  type="button"
                  className="crud-btn"
                  title="Manage credentials"
                  onClick={() => handleLoadCredentials(name)}
                >
                  <Lock size={12} />
                </button>
                <button
                  type="button"
                  className="crud-btn"
                  title="Edit server"
                  onClick={() => {
                    setEditingServer({ mode: 'edit', originalName: name });
                    setServerName(name);
                    setServerCommand((server.command as string) || '');
                    setServerArgs(
                      Array.isArray(server.args) ? (server.args as unknown[]).map(String).join(' ') : '',
                    );
                    const existingEnv = (server.env as Record<string, string>) || {};
                    setServerEnvVars(
                      Object.entries(existingEnv).map(([key, value]) => ({ key, value })),
                    );
                    setNewEnvKey('');
                    setNewEnvValue('');
                  }}
                >
                  <Pencil size={12} />
                </button>
                <button
                  type="button"
                  className="crud-btn danger"
                  title="Delete server"
                  onClick={() => handleDeleteServer(name)}
                >
                  <Trash2 size={12} />
                </button>
              </div>
            );
          })}

          {editingServer && (
            <div className="crud-inline-form">
              <div className="form-row">
                <label>Name</label>
                <input
                  type="text"
                  className="styled-input"
                  value={serverName}
                  onChange={(e) => setServerName(e.target.value)}
                  placeholder="server-name"
                  disabled={editingServer.mode === 'edit'}
                />
              </div>
              <div className="form-row">
                <label>Command</label>
                <input
                  type="text"
                  className="styled-input"
                  value={serverCommand}
                  onChange={(e) => setServerCommand(e.target.value)}
                  placeholder="npx or path/to/binary"
                />
              </div>
              <div className="form-row">
                <label>Args (space-separated)</label>
                <input
                  type="text"
                  className="styled-input"
                  value={serverArgs}
                  onChange={(e) => setServerArgs(e.target.value)}
                  placeholder="--flag value"
                />
              </div>
              <div className="form-row" style={{ flexDirection: 'column' }}>
                <label>Environment Variables</label>
                {serverEnvVars.length > 0 && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', marginBottom: '8px' }}>
                    {serverEnvVars.map((ev, idx) => (
                      <div key={idx} style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
                        <input
                          type="text"
                          className="styled-input"
                          value={ev.key}
                          onChange={(e) => {
                            const updated = [...serverEnvVars];
                            updated[idx] = { ...updated[idx], key: e.target.value };
                            setServerEnvVars(updated);
                          }}
                          placeholder="VAR_NAME"
                          style={{ flex: 1 }}
                        />
                        <span style={{ color: 'var(--text-muted)', fontSize: '12px' }}>=</span>
                        {ev.value === '{{stored}}' ? (
                          <span
                            style={{
                              flex: 1.5,
                              padding: '4px 8px',
                              borderRadius: '4px',
                              background: 'var(--bg-secondary)',
                              color: 'var(--text-muted)',
                              fontSize: '12px',
                              border: '1px solid var(--border)',
                            }}
                          >
                            🔒 Stored in credential manager
                          </span>
                        ) : (
                          <input
                            type="password"
                            className="styled-input"
                            value={ev.value}
                            onChange={(e) => {
                              const updated = [...serverEnvVars];
                              updated[idx] = { ...updated[idx], value: e.target.value };
                              setServerEnvVars(updated);
                            }}
                            placeholder="value"
                            style={{ flex: 1.5 }}
                          />
                        )}
                        <button
                          type="button"
                          className="crud-btn danger"
                          title="Remove"
                          onClick={() => setServerEnvVars(serverEnvVars.filter((_, i) => i !== idx))}
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
                  <input
                    type="text"
                    className="styled-input"
                    value={newEnvKey}
                    onChange={(e) => setNewEnvKey(e.target.value)}
                    placeholder="NEW_VAR"
                    style={{ flex: 1 }}
                  />
                  <span style={{ color: 'var(--text-muted)', fontSize: '12px' }}>=</span>
                  <input
                    type="password"
                    className="styled-input"
                    value={newEnvValue}
                    onChange={(e) => setNewEnvValue(e.target.value)}
                    placeholder="secret value"
                    style={{ flex: 1.5 }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && newEnvKey.trim() && newEnvValue.trim()) {
                        setServerEnvVars([...serverEnvVars, { key: newEnvKey.trim(), value: newEnvValue.trim() }]);
                        setNewEnvKey('');
                        setNewEnvValue('');
                      }
                    }}
                  />
                  <button
                    type="button"
                    className="crud-btn"
                    title="Add env var"
                    onClick={() => {
                      if (newEnvKey.trim() && newEnvValue.trim()) {
                        setServerEnvVars([...serverEnvVars, { key: newEnvKey.trim(), value: newEnvValue.trim() }]);
                        setNewEnvKey('');
                        setNewEnvValue('');
                      }
                    }}
                  >
                    <Plus size={12} />
                  </button>
                </div>
              </div>
              <div className="form-actions">
                <button
                  type="button"
                  className="form-btn primary"
                  onClick={editingServer.mode === 'edit' ? handleUpdateServer : handleAddServer}
                >
                  {editingServer.mode === 'edit' ? 'Update' : 'Add'}
                </button>
                <button type="button" className="form-btn cancel" onClick={resetServerForm}>
                  Cancel
                </button>
              </div>
            </div>
          )}

          {!editingServer && (
            <button
              type="button"
              className="crud-add-btn"
              onClick={() => {
                setEditingServer({ mode: 'add' });
                setServerName('');
                setServerCommand('');
                setServerArgs('');
                setServerEnvVars([]);
                setNewEnvKey('');
                setNewEnvValue('');
              }}
            >
              <Plus size={14} /> Add server
            </button>
          )}
        </div>

        {credentialServer && (
          <div style={{ marginTop: 'var(--space-4)' }}>
            <h4>Credentials — {credentialServer}</h4>

            {credentialLoading ? (
              <div className="settings-empty">Loading credentials…</div>
            ) : (
              <div className="crud-inline-form">
                {credentialEntries.length > 0 && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '12px' }}>
                    {credentialEntries.map((entry, idx) => (
                      <div key={entry.key} style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                        <span
                          title={entry.status === 'set' ? 'Credential is set' : 'Credential is missing'}
                          style={{
                            display: 'inline-block',
                            width: '8px',
                            height: '8px',
                            borderRadius: '50%',
                            backgroundColor: entry.status === 'set' ? 'var(--color-success, #22c55e)' : 'var(--text-muted, #888)',
                            flexShrink: 0,
                          }}
                        />
                        <span
                          style={{
                            flex: 1.2,
                            fontFamily: 'monospace',
                            fontSize: '12px',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                          title={entry.key}
                        >
                          {entry.key}
                        </span>
                        <span
                          style={{
                            fontSize: '11px',
                            color: entry.status === 'set' ? 'var(--color-success, #22c55e)' : 'var(--text-muted, #888)',
                            width: '56px',
                            flexShrink: 0,
                          }}
                        >
                          {entry.status === 'set' ? 'Set' : 'Missing'}
                        </span>
                        <input
                          type="password"
                          className="styled-input"
                          value={entry.value}
                          onChange={(e) => {
                            const updated = [...credentialEntries];
                            updated[idx] = { ...updated[idx], value: e.target.value };
                            setCredentialEntries(updated);
                          }}
                          placeholder="Leave empty to keep current"
                          style={{ flex: 1 }}
                        />
                        <button
                          type="button"
                          className="crud-btn danger"
                          title="Delete credential"
                          onClick={() => handleDeleteCredential(entry.key)}
                          disabled={savingKey === 'mcp-credential-delete'}
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                <div className="form-row" style={{ marginTop: '8px' }}>
                  <label>Add credential</label>
                  <div style={{ display: 'flex', gap: '4px', alignItems: 'center', flex: 1 }}>
                    <input
                      type="text"
                      className="styled-input"
                      value={newCredentialKey}
                      onChange={(e) => setNewCredentialKey(e.target.value)}
                      placeholder="ENV_VAR_NAME"
                      style={{ flex: 1.2 }}
                    />
                    <input
                      type="password"
                      className="styled-input"
                      value={newCredentialValue}
                      onChange={(e) => setNewCredentialValue(e.target.value)}
                      placeholder="secret value"
                      style={{ flex: 1 }}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') handleAddCredentialEntry();
                      }}
                    />
                    <button
                      type="button"
                      className="crud-btn"
                      title="Add credential"
                      onClick={handleAddCredentialEntry}
                      disabled={!newCredentialKey.trim() || !newCredentialValue.trim()}
                    >
                      <Plus size={12} />
                    </button>
                  </div>
                </div>

                <div className="form-actions">
                  <button
                    type="button"
                    className="form-btn primary"
                    onClick={handleSaveCredential}
                    disabled={savingKey === 'mcp-credential-save'}
                  >
                    Save
                  </button>
                  <button type="button" className="form-btn cancel" onClick={handleCloseCredentials}>
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
