import { Lock, Plus, Trash2 } from 'lucide-react';

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
      <div className="crud-inline-form" data-testid="mcp-server-form">
        <div className="form-row">
          <label>Name</label>
          <input
            type="text"
            className="styled-input"
            value={serverName}
            onChange={(e) => setServerName(e.target.value)}
            placeholder="server-name"
            data-testid="mcp-server-name-input"
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
            data-testid="mcp-server-command-input"
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
        <div className="form-row mcp-env-form-row">
          <label>Environment Variables</label>
          {serverEnvVars.length > 0 && (
            <div className="mcp-env-list">
              {serverEnvVars.map((ev, idx) => (
                <div key={idx} className="mcp-env-row">
                  <input
                    type="text"
                    className="styled-input mcp-env-key"
                    value={ev.key}
                    onChange={(e) => {
                      const updated = [...serverEnvVars];
                      updated[idx] = { ...updated[idx], key: e.target.value };
                      setServerEnvVars(updated);
                    }}
                    placeholder="VAR_NAME"
                  />
                  <span className="mcp-env-equals">=</span>
                  {ev.value === '{{stored}}' ? (
                    <span className="mcp-env-stored-badge">
                      <Lock size={11} aria-hidden="true" />
                      <span>Stored in credential manager</span>
                    </span>
                  ) : (
                    <input
                      type="password"
                      className="styled-input mcp-env-value"
                      value={ev.value}
                      onChange={(e) => {
                        const updated = [...serverEnvVars];
                        updated[idx] = { ...updated[idx], value: e.target.value };
                        setServerEnvVars(updated);
                      }}
                      placeholder="value"
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
          <div className="mcp-env-row">
            <input
              type="text"
              className="styled-input mcp-env-key"
              value={newEnvKey}
              onChange={(e) => setNewEnvKey(e.target.value)}
              placeholder="NEW_VAR"
            />
            <span className="mcp-env-equals">=</span>
            <input
              type="password"
              className="styled-input mcp-env-value"
              value={newEnvValue}
              onChange={(e) => setNewEnvValue(e.target.value)}
              placeholder="secret value"
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
            data-testid="mcp-server-add-button"
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
    <button type="button" className="crud-add-btn" onClick={handleShowAddForm}>
      <Plus size={14} /> Add server
    </button>
  );
}
