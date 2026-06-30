import { Pencil, Trash2, Lock } from 'lucide-react';
import { useState } from 'react';
import type { SproutSettings } from '../../services/api';
import { showThemedConfirm } from '../ThemedDialog';
import ListFilter from './ListFilter';
import MCPCredentialPanel from './MCPCredentialPanel';
import MCPServerForm from './MCPServerForm';
import type { FieldRenderers } from './useSettingsFieldRenderers';

const FILTER_THRESHOLD = 4;

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
              <div key={name} className="crud-item" data-testid="mcp-server-row">
                <span className="crud-item-name">{name}</span>
                <span className="crud-item-detail">{cfg.command || ''}</span>
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
