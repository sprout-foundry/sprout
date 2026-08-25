import { Loader2, Pencil, Trash2, Lock, Zap, Check, X } from 'lucide-react';
import { Fragment, useState } from 'react';
import type { SproutSettings } from '../../services/api';
import { clientFetch } from '../../services/clientSession';
import { showThemedConfirm } from '../ThemedDialog';
import ListFilter from './ListFilter';
import MCPCredentialPanel from './MCPCredentialPanel';
import MCPServerForm from './MCPServerForm';
import type { FieldRenderers } from './useSettingsFieldRenderers';

const FILTER_THRESHOLD = 4;

type TestPhase = 'idle' | 'testing' | 'ok' | 'error';

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
  serverName,
  serverCommand,
  serverArgs,
  serverEnvVars,
  newEnvKey,
  newEnvValue,
  credentialServer,
  credentialEntries,
  credentialLoading,
  newCredentialKey,
  newCredentialValue,
  savingKey,
  setEditingServer,
  setServerName,
  setServerCommand,
  setServerArgs,
  setServerEnvVars,
  setNewEnvKey,
  setNewEnvValue,
  setCredentialEntries,
  setNewCredentialKey,
  setNewCredentialValue,
  renderToggle,
  renderTextInput,
  resetServerForm,
  handleAddServer,
  handleUpdateServer,
  handleDeleteServer,
  handleLoadCredentials,
  handleSaveCredential,
  handleDeleteCredential,
  handleAddCredentialEntry,
  handleCloseCredentials,
}: MCPSettingsTabProps) {
  const mcpSettings = settings.mcp || {};
  const servers = mcpSettings.servers || {};
  const serverEntries = Object.entries(servers);
  const [serverFilter, setServerFilter] = useState('');
  const normalizedServerFilter = serverFilter.trim().toLowerCase();
  const filteredServerEntries = normalizedServerFilter
    ? serverEntries.filter(
        ([name, cfg]) =>
          name.toLowerCase().includes(normalizedServerFilter) ||
          (cfg.command || '').toLowerCase().includes(normalizedServerFilter),
      )
    : serverEntries;

  const handleShowAddForm = () => {
    setEditingServer({ mode: 'add' });
    setServerName('');
    setServerCommand('');
    setServerArgs('');
    setServerEnvVars([]);
    setNewEnvKey('');
    setNewEnvValue('');
  };

  // ── Test connection ─────────────────────────────────────────
  const [testState, setTestState] = useState<Record<string, { phase: TestPhase; message: string }>>({});

  const handleTestServer = async (name: string) => {
    setTestState((prev) => ({ ...prev, [name]: { phase: 'testing', message: '' } }));
    try {
      const res = await clientFetch(`/api/settings/mcp/servers/${encodeURIComponent(name)}/test`, {
        method: 'POST',
      });
      const data = (await res.json().catch(() => ({}))) as { status?: string; message?: string };
      if (res.ok && data.status === 'ok') {
        setTestState((prev) => ({
          ...prev,
          [name]: { phase: 'ok', message: data.message || 'Connected successfully' },
        }));
      } else {
        setTestState((prev) => ({
          ...prev,
          [name]: { phase: 'error', message: data.message || `HTTP ${res.status}` },
        }));
      }
    } catch (err) {
      setTestState((prev) => ({
        ...prev,
        [name]: { phase: 'error', message: err instanceof Error ? err.message : 'Request failed' },
      }));
    }
  };

  return (
    <div className="section" data-testid="settings-mcp-tab">
      <h4>MCP Configuration</h4>

      {renderToggle('mcp.enabled', 'MCP enabled')}
      {renderToggle('mcp.auto_start', 'Auto-start servers')}
      {renderToggle('mcp.auto_discover', 'Auto-discover servers')}
      {renderTextInput('mcp.timeout', 'Timeout (e.g. 30s)', '30s')}

      <div className="settings-section-spaced">
        <h4>Servers ({serverEntries.length})</h4>

        {serverEntries.length >= FILTER_THRESHOLD && (
          <ListFilter
            value={serverFilter}
            onChange={setServerFilter}
            placeholder={`Filter ${serverEntries.length} servers…`}
            ariaLabel="Filter MCP servers"
          />
        )}

        {serverEntries.length === 0 && !editingServer && (
          <div className="settings-empty">No MCP servers configured</div>
        )}

        {normalizedServerFilter && filteredServerEntries.length === 0 && (
          <div className="settings-empty">No servers match “{serverFilter}”</div>
        )}

        <div className="crud-list">
          {filteredServerEntries.map(([name, cfg]) => {
            return (
              <Fragment key={name}>
                <div className="crud-item" data-testid="mcp-server-row">
                  <span className="crud-item-name">{name}</span>
                  <span className="crud-item-detail">{cfg.command || ''}</span>
                  <button
                    type="button"
                    className="crud-btn"
                    data-testid="mcp-server-test-button"
                    title="Test connection"
                    disabled={testState[name]?.phase === 'testing'}
                    onClick={() => handleTestServer(name)}
                  >
                    {testState[name]?.phase === 'testing' ? (
                      <Loader2 size={12} className="spinning" />
                    ) : testState[name]?.phase === 'ok' ? (
                      <Check size={12} />
                    ) : testState[name]?.phase === 'error' ? (
                      <X size={12} />
                    ) : (
                      <Zap size={12} />
                    )}
                  </button>
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
                      setServerCommand(cfg.command || '');
                      setServerArgs(cfg.args ? cfg.args.join(' ') : '');
                      const existingEnv = cfg.env || {};
                      setServerEnvVars(Object.entries(existingEnv).map(([key, value]) => ({ key, value })));
                      setNewEnvKey('');
                      setNewEnvValue('');
                    }}
                  >
                    <Pencil size={12} />
                  </button>
                  <button
                    type="button"
                    className="crud-btn danger"
                    data-testid="mcp-server-delete-button"
                    title="Delete server"
                    onClick={async () => {
                      const confirmed = await showThemedConfirm(
                        `Delete MCP server "${name}"? This removes its config and disconnects it.`,
                        { title: 'Delete MCP server', type: 'danger', confirmLabel: 'Delete' },
                      );
                      if (!confirmed) return;
                      void handleDeleteServer(name);
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
                {testState[name]?.message && (
                  <div
                    data-testid={`mcp-server-test-message-${name}`}
                    style={{
                      fontSize: 11,
                      marginTop: 4,
                      color:
                        testState[name]?.phase === 'ok'
                          ? 'var(--accent-success)'
                          : testState[name]?.phase === 'error'
                            ? 'var(--accent-error)'
                            : 'var(--text-tertiary)',
                    }}
                  >
                    {testState[name].message}
                  </div>
                )}
              </Fragment>
            );
          })}

          <MCPServerForm
            editingServer={editingServer}
            serverName={serverName}
            serverCommand={serverCommand}
            serverArgs={serverArgs}
            serverEnvVars={serverEnvVars}
            newEnvKey={newEnvKey}
            newEnvValue={newEnvValue}
            setServerName={setServerName}
            setServerCommand={setServerCommand}
            setServerArgs={setServerArgs}
            setServerEnvVars={setServerEnvVars}
            setNewEnvKey={setNewEnvKey}
            setNewEnvValue={setNewEnvValue}
            resetServerForm={resetServerForm}
            handleAddServer={handleAddServer}
            handleUpdateServer={handleUpdateServer}
            handleShowAddForm={handleShowAddForm}
          />
        </div>

        <MCPCredentialPanel
          credentialServer={credentialServer}
          credentialEntries={credentialEntries}
          credentialLoading={credentialLoading}
          newCredentialKey={newCredentialKey}
          newCredentialValue={newCredentialValue}
          savingKey={savingKey}
          setCredentialEntries={setCredentialEntries}
          setNewCredentialKey={setNewCredentialKey}
          setNewCredentialValue={setNewCredentialValue}
          handleAddCredentialEntry={handleAddCredentialEntry}
          handleSaveCredential={handleSaveCredential}
          handleDeleteCredential={handleDeleteCredential}
          handleCloseCredentials={handleCloseCredentials}
        />
      </div>
    </div>
  );
}
