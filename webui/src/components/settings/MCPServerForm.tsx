import { Lock, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

interface MCPServerFormProps {
  editingServer: { mode: 'add' | 'edit'; originalName?: string } | null;
  serverName: string;
  serverType: 'stdio' | 'http';
  serverURL: string;
  serverHeaders: Array<{ key: string; value: string }>;
  serverCommand: string;
  serverArgs: string;
  serverEnvVars: Array<{ key: string; value: string }>;
  newEnvKey: string;
  newEnvValue: string;
  setServerName: (v: string) => void;
  setServerType: (v: 'stdio' | 'http') => void;
  setServerURL: (v: string) => void;
  setServerHeaders: (v: Array<{ key: string; value: string }>) => void;
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
  serverType,
  serverURL,
  serverHeaders,
  serverCommand,
  serverArgs,
  serverEnvVars,
  newEnvKey,
  newEnvValue,
  setServerName,
  setServerType,
  setServerURL,
  setServerHeaders,
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
  // The header being typed is local to the form: it is not part of the saved
  // server, only a staging slot for the next entry.
  const [newHeaderKey, setNewHeaderKey] = useState('');
  const [newHeaderValue, setNewHeaderValue] = useState('');
  // A stored credential is shown as a badge; typing in its slot replaces it.
  // The buffer is keyed by row so two stored headers can be replaced in one
  // pass without one row's input blanking the other.
  const [replacements, setReplacements] = useState<Record<number, string>>({});

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
          <label>Type</label>
          <select
            className="styled-input"
            value={serverType}
            onChange={(e) => setServerType(e.target.value === 'http' ? 'http' : 'stdio')}
            data-testid="mcp-server-type-select"
          >
            <option value="stdio">stdio — local command</option>
            <option value="http">http — remote URL</option>
          </select>
        </div>
        {serverType === 'http' ? (
          <>
            <div className="form-row">
              <label>URL</label>
              <input
                type="text"
                className="styled-input"
                value={serverURL}
                onChange={(e) => setServerURL(e.target.value)}
                placeholder="https://example.com/mcp"
                data-testid="mcp-server-url-input"
              />
            </div>
            <div className="form-row mcp-env-form-row">
              <label>Headers</label>
              {serverHeaders.length > 0 && (
                <div className="mcp-env-list">
                  {serverHeaders.map((h, idx) => (
                    <div key={idx} className="mcp-env-row">
                      <input
                        type="text"
                        className="styled-input mcp-env-key"
                        value={h.key}
                        onChange={(e) => {
                          const updated = [...serverHeaders];
                          updated[idx] = { ...updated[idx], key: e.target.value };
                          setServerHeaders(updated);
                        }}
                        placeholder="Authorization"
                      />
                      <span className="mcp-env-equals">:</span>
                      {h.value === '{{stored}}' ? (
                        <span className="mcp-env-row" style={{ flex: 1 }}>
                          <span className="mcp-env-stored-badge">
                            <Lock size={11} aria-hidden="true" />
                            <span>Stored in credential manager</span>
                          </span>
                          <input
                            type="password"
                            className="styled-input mcp-env-value"
                            value={replacements[idx] ?? ''}
                            onChange={(e) => {
                              setReplacements({ ...replacements, [idx]: e.target.value });
                              const updated = [...serverHeaders];
                              updated[idx] = { ...updated[idx], value: e.target.value };
                              setServerHeaders(updated);
                            }}
                            placeholder="Type to replace"
                            aria-label={`Replace stored value for ${h.key}`}
                          />
                        </span>
                      ) : (
                        <input
                          type="password"
                          className="styled-input mcp-env-value"
                          value={h.value}
                          onChange={(e) => {
                            const updated = [...serverHeaders];
                            updated[idx] = { ...updated[idx], value: e.target.value };
                            setServerHeaders(updated);
                          }}
                          placeholder="Bearer ..."
                        />
                      )}
                      <button
                        type="button"
                        className="crud-btn danger"
                        title="Remove header"
                        onClick={() => setServerHeaders(serverHeaders.filter((_, i) => i !== idx))}
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
                  value={newHeaderKey}
                  onChange={(e) => setNewHeaderKey(e.target.value)}
                  placeholder="Header-Name"
                />
                <span className="mcp-env-equals">:</span>
                <input
                  type="password"
                  className="styled-input mcp-env-value"
                  value={newHeaderValue}
                  onChange={(e) => setNewHeaderValue(e.target.value)}
                  placeholder="value"
                />
                <button
                  type="button"
                  className="crud-btn"
                  title="Add header"
                  onClick={() => {
                    if (newHeaderKey.trim() && newHeaderValue.trim()) {
                      setServerHeaders([...serverHeaders, { key: newHeaderKey.trim(), value: newHeaderValue.trim() }]);
                      setNewHeaderKey('');
                      setNewHeaderValue('');
                    }
                  }}
                >
                  <Plus size={12} />
                </button>
              </div>
            </div>
          </>
        ) : (
          <>
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
          </>
        )}
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
