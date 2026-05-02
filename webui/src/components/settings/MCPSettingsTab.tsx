import { Pencil, Trash2, Lock } from 'lucide-react';
import type { SproutSettings } from '../../services/api';
import type { FieldRenderers } from './useSettingsFieldRenderers';
import MCPServerForm from './MCPServerForm';
import MCPCredentialPanel from './MCPCredentialPanel';

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

  const handleShowAddForm = () => {
    setEditingServer({ mode: 'add' });
    setServerName('');
    setServerCommand('');
    setServerArgs('');
    setServerEnvVars([]);
    setNewEnvKey('');
    setNewEnvValue('');
  };

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
            const server = cfg as unknown as Record<string, unknown>;
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
