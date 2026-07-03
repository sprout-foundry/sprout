import { Plus, Trash2 } from 'lucide-react';

interface MCPCredentialPanelProps {
  credentialServer: string | null;
  credentialEntries: Array<{ key: string; value: string; status: string }>;
  credentialLoading: boolean;
  newCredentialKey: string;
  newCredentialValue: string;
  savingKey: string | null;
  setCredentialEntries: (v: Array<{ key: string; value: string; status: string }>) => void;
  setNewCredentialKey: (v: string) => void;
  setNewCredentialValue: (v: string) => void;
  handleAddCredentialEntry: () => void;
  handleSaveCredential: () => Promise<void>;
  handleDeleteCredential: (credName: string) => Promise<void>;
  handleCloseCredentials: () => void;
}

export default function MCPCredentialPanel({
  credentialServer,
  credentialEntries,
  credentialLoading,
  newCredentialKey,
  newCredentialValue,
  savingKey,
  setCredentialEntries,
  setNewCredentialKey,
  setNewCredentialValue,
  handleAddCredentialEntry,
  handleSaveCredential,
  handleDeleteCredential,
  handleCloseCredentials,
}: MCPCredentialPanelProps) {
  if (!credentialServer) {
    return null;
  }

  return (
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
                      backgroundColor:
                        entry.status === 'set' ? 'var(--accent-success)' : 'var(--text-muted, #888)',
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
                      color: entry.status === 'set' ? 'var(--accent-success)' : 'var(--text-muted, #888)',
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
  );
}
