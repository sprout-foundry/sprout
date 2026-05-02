import { Plus, Trash2 } from 'lucide-react';

interface MCPServerFormProps {
  editingServer: { mode: 'add' | 'edit'; originalName?: string } | null;
  serverName: string;
  serverCommand: string;
  serverArgs: string;
  serverEnvVars: Array<{ key: string; value: string }>;
  newEnvKey: string;
  newEnvValue: string;
  setServerName: (v: string) => void;
  setServerCommand: (v: string) => void;
  setServerArgs: (v: string) => void;
  setServerEnvVars: (v: Array<{ key: string; value: string }>) => void;
  setNewEnvKey: (v: string) => void;
  setNewEnvValue: (v: string) => void;
  resetServerForm: () => void;
  handleAddServer: () => Promise<void>;
  handleUpdateServer: () => Promise<void>;
  handleShowAddForm: () => void;
}

export default function MCPServerForm({
  editingServer,
  serverName,
  serverCommand,
  serverArgs,
  serverEnvVars,
  newEnvKey,
  newEnvValue,
  setServerName,
  setServerCommand,
  setServerArgs,
  setServerEnvVars,
  setNewEnvKey,
  setNewEnvValue,
  resetServerForm,
  handleAddServer,
  handleUpdateServer,
  handleShowAddForm,
}: MCPServerFormProps) {
  if (editingServer) {
    return (
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
    );
  }

  return (
    <button
      type="button"
      className="crud-add-btn"
      onClick={handleShowAddForm}
    >
      <Plus size={14} /> Add server
    </button>
  );
}
