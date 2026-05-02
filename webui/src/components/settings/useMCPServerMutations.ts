import { useCallback } from 'react';
import { debugLog } from '../../utils/log';
import type { MutationContext } from './types';

interface MCPMutationParams {
  // Shared context
  ctx: MutationContext;
  // MCP server state
  editingServer: { mode: 'add' | 'edit'; originalName?: string } | null;
  setEditingServer: (v: { mode: 'add' | 'edit'; originalName?: string } | null) => void;
  serverName: string;
  setServerName: (v: string) => void;
  serverCommand: string;
  setServerCommand: (v: string) => void;
  serverArgs: string;
  setServerArgs: (v: string) => void;
  serverEnvVars: Array<{ key: string; value: string }>;
  setServerEnvVars: (v: Array<{ key: string; value: string }>) => void;
  newEnvKey: string;
  setNewEnvKey: (v: string) => void;
  newEnvValue: string;
  setNewEnvValue: (v: string) => void;
  // Credential management state
  credentialServer: string | null;
  setCredentialServer: (v: string | null) => void;
  credentialEntries: Array<{ key: string; value: string; status: string }>;
  setCredentialEntries: (v: Array<{ key: string; value: string; status: string }> | ((prev: Array<{ key: string; value: string; status: string }>) => Array<{ key: string; value: string; status: string }>)) => void;
  credentialLoading: boolean;
  setCredentialLoading: (v: boolean) => void;
  newCredentialKey: string;
  setNewCredentialKey: (v: string) => void;
  newCredentialValue: string;
  setNewCredentialValue: (v: string) => void;
}

export function useMCPServerMutations(params: MCPMutationParams) {
  const {
    ctx,
    editingServer, setEditingServer,
    serverName, setServerName,
    serverCommand, setServerCommand,
    serverArgs, setServerArgs,
    serverEnvVars, setServerEnvVars,
    newEnvKey: _newEnvKey, setNewEnvKey,
    newEnvValue: _newEnvValue, setNewEnvValue,
    credentialServer, setCredentialServer,
    credentialEntries, setCredentialEntries,
    credentialLoading: _credentialLoading, setCredentialLoading,
    newCredentialKey, setNewCredentialKey,
    newCredentialValue, setNewCredentialValue,
  } = params;

  const { api, onSettingsChanged, addNotification, setSavingKey } = ctx;

  /** Build the server payload object from form state. */
  const buildServerPayload = useCallback((): Record<string, unknown> => {
    const server: Record<string, unknown> = { command: serverCommand };
    if (serverArgs.trim()) {
      server.args = serverArgs.split(/\s+/).filter(Boolean);
    }
    if (serverEnvVars.length > 0) {
      const env: Record<string, string> = {};
      for (const ev of serverEnvVars) {
        if (ev.key.trim() && ev.value !== '{{stored}}') env[ev.key.trim()] = ev.value;
      }
      if (Object.keys(env).length > 0) server.env = env;
    }
    return server;
  }, [serverCommand, serverArgs, serverEnvVars]);

  /* ─── MCP server CRUD ──────────────────────────────────── */

  const resetServerForm = useCallback(() => {
    setEditingServer(null);
    setServerName('');
    setServerCommand('');
    setServerArgs('');
    setServerEnvVars([]);
    setNewEnvKey('');
    setNewEnvValue('');
  }, [setEditingServer, setServerName, setServerCommand, setServerArgs, setServerEnvVars, setNewEnvKey, setNewEnvValue]);

  const handleAddServer = useCallback(async () => {
    if (!serverName.trim()) return;
    const server = buildServerPayload();
    setSavingKey('mcp-server-add');
    try {
      await api.addMCPServer({ name: serverName.trim(), ...server });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Server added', 3000);
      resetServerForm();
    } catch (err) {
      debugLog('[SettingsPanel] failed to add MCP server:', err);
      addNotification('error', 'Settings', 'Failed to add server', 5000);
    } finally {
      setSavingKey(null);
    }
  }, [serverName, buildServerPayload, setSavingKey, api, onSettingsChanged, addNotification, resetServerForm]);

  const handleUpdateServer = useCallback(async () => {
    if (!editingServer?.originalName || !serverName.trim()) return;
    const server = buildServerPayload();
    setSavingKey('mcp-server-update');
    try {
      await api.updateMCPServer(editingServer.originalName, { name: serverName.trim(), ...server });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Server updated', 3000);
      resetServerForm();
    } catch (err) {
      debugLog('[SettingsPanel] failed to update MCP server:', err);
      addNotification('error', 'Settings', 'Failed to update server', 5000);
    } finally {
      setSavingKey(null);
    }
  }, [editingServer, serverName, buildServerPayload, setSavingKey, api, onSettingsChanged, addNotification, resetServerForm]);

  const handleDeleteServer = useCallback(async (name: string) => {
    setSavingKey('mcp-server-delete');
    try {
      await api.deleteMCPServer(name);
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Server deleted', 3000);
      if (editingServer?.originalName === name) resetServerForm();
    } catch (err) {
      debugLog('[SettingsPanel] failed to delete MCP server:', err);
      addNotification('error', 'Settings', 'Failed to delete server', 5000);
    } finally {
      setSavingKey(null);
    }
  }, [setSavingKey, api, onSettingsChanged, addNotification, editingServer, resetServerForm]);

  /* ─── MCP credential management ───────────────────────── */

  const resetCredentialForm = useCallback(() => {
    setCredentialServer(null);
    setCredentialEntries([]);
    setCredentialLoading(false);
    setNewCredentialKey('');
    setNewCredentialValue('');
  }, [setCredentialServer, setCredentialEntries, setCredentialLoading, setNewCredentialKey, setNewCredentialValue]);

  const handleLoadCredentials = useCallback(async (credentialServerName: string) => {
    setCredentialServer(credentialServerName);
    setCredentialLoading(true);
    setCredentialEntries([]);
    setNewCredentialKey('');
    setNewCredentialValue('');
    try {
      const resp = await api.getMCPServerCredentials(credentialServerName);
      const entries = Object.entries(resp.credentials || {}).map(([key, info]: [string, any]) => ({
        key,
        value: '',
        status: info.status === 'set' ? 'set' : 'missing',
      }));
      setCredentialEntries(entries);
    } catch (err) {
      debugLog('[SettingsPanel] failed to load credentials:', err);
      addNotification('error', 'Settings', 'Failed to load credentials', 5000);
      resetCredentialForm();
    } finally {
      setCredentialLoading(false);
    }
  }, [setCredentialServer, setCredentialLoading, setCredentialEntries, setNewCredentialKey, setNewCredentialValue, api, addNotification, resetCredentialForm]);

  const handleSaveCredential = useCallback(async () => {
    if (!credentialServer) return;
    const credentials: Record<string, string> = {};
    for (const entry of credentialEntries) {
      if (entry.value.trim()) {
        credentials[entry.key] = entry.value.trim();
      }
    }
    if (newCredentialKey.trim() && newCredentialValue.trim()) {
      credentials[newCredentialKey.trim()] = newCredentialValue.trim();
    }
    if (Object.keys(credentials).length === 0) {
      addNotification('info', 'Settings', 'No credentials to save', 3000);
      return;
    }
    setSavingKey('mcp-credential-save');
    try {
      await api.updateMCPServerCredentials(credentialServer, credentials);
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Credentials saved', 3000);
      await handleLoadCredentials(credentialServer);
    } catch (err) {
      debugLog('[SettingsPanel] failed to save credentials:', err);
      addNotification('error', 'Settings', 'Failed to save credentials', 5000);
    } finally {
      setSavingKey(null);
    }
  }, [credentialServer, credentialEntries, newCredentialKey, newCredentialValue, setSavingKey, api, onSettingsChanged, addNotification, handleLoadCredentials]);

  const handleDeleteCredential = useCallback(async (credName: string) => {
    if (!credentialServer) return;
    setSavingKey('mcp-credential-delete');
    try {
      await api.deleteMCPServerCredential(credentialServer, credName);
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Credential deleted', 3000);
      await handleLoadCredentials(credentialServer);
    } catch (err) {
      debugLog('[SettingsPanel] failed to delete credential:', err);
      addNotification('error', 'Settings', 'Failed to delete credential', 5000);
    } finally {
      setSavingKey(null);
    }
  }, [credentialServer, setSavingKey, api, onSettingsChanged, addNotification, handleLoadCredentials]);

  const handleAddCredentialEntry = useCallback(() => {
    if (!newCredentialKey.trim() || !newCredentialValue.trim()) return;
    setCredentialEntries((prev: Array<{ key: string; value: string; status: string }>) => [
      ...prev,
      { key: newCredentialKey.trim(), value: newCredentialValue.trim(), status: 'pending' },
    ]);
    setNewCredentialKey('');
    setNewCredentialValue('');
  }, [newCredentialKey, newCredentialValue, setCredentialEntries, setNewCredentialKey, setNewCredentialValue]);

  const handleCloseCredentials = useCallback(() => {
    resetCredentialForm();
  }, [resetCredentialForm]);

  return {
    // MCP server
    resetServerForm,
    handleAddServer,
    handleUpdateServer,
    handleDeleteServer,
    // Credentials
    resetCredentialForm,
    handleLoadCredentials,
    handleSaveCredential,
    handleDeleteCredential,
    handleAddCredentialEntry,
    handleCloseCredentials,
  };
}
