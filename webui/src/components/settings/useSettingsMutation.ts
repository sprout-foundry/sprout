import { useState, useCallback } from 'react';
import type { SproutSettings } from '../../services/api';
import { debugLog } from '../../utils/log';
import { setNestedValue } from './settingsHelpers';

interface MutationHookParams {
  settings: SproutSettings | null;
  onSettingsChanged: (settings: SproutSettings) => void;
  addNotification: ReturnType<typeof import('../../contexts/NotificationContext').useNotifications>['addNotification'];
  configViewLayer: 'session' | 'workspace' | 'global';
  api: ReturnType<typeof import('../../services/api').ApiService.getInstance>;
  setProvenanceSources: (v: Record<string, string>) => void;
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
  // Credentials
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
  // Provider form
  editingProvider: { mode: 'add' | 'edit'; originalName?: string } | null;
  setEditingProvider: (v: { mode: 'add' | 'edit'; originalName?: string } | null) => void;
  providerName: string;
  setProviderName: (v: string) => void;
  providerApiBase: string;
  setProviderApiBase: (v: string) => void;
  providerModelName: string;
  setProviderModelName: (v: string) => void;
  providerContextSize: number;
  setProviderContextSize: (v: number) => void;
  providerEnvVar: string;
  setProviderEnvVar: (v: string) => void;
  providerSupportsVision: boolean;
  setProviderSupportsVision: (v: boolean) => void;
  providerVisionModel: string;
  setProviderVisionModel: (v: string) => void;
  providerModelContextSizes: string;
  setProviderModelContextSizes: (v: string) => void;
  // Shared settings ref (kept up-to-date by useSettingsState)
  settingsRef: React.MutableRefObject<SproutSettings | null>;
  // Workspace config
  creatingWorkspaceConfig: boolean;
  setCreatingWorkspaceConfig: (v: boolean) => void;
  setLayerData: (v: Record<string, any> | null) => void;
}

export function useSettingsMutation(params: MutationHookParams) {
  const {
    settings, onSettingsChanged, addNotification, configViewLayer, api, setProvenanceSources,
    settingsRef,
    editingServer, setEditingServer,
    serverName, setServerName,
    serverCommand, setServerCommand,
    serverArgs, setServerArgs,
    serverEnvVars, setServerEnvVars,
    newEnvKey, setNewEnvKey,
    newEnvValue, setNewEnvValue,
    credentialServer, setCredentialServer,
    credentialEntries, setCredentialEntries,
    credentialLoading, setCredentialLoading,
    newCredentialKey, setNewCredentialKey,
    newCredentialValue, setNewCredentialValue,
    editingProvider, setEditingProvider,
    providerName, setProviderName,
    providerApiBase, setProviderApiBase,
    providerModelName, setProviderModelName,
    providerContextSize, setProviderContextSize,
    providerEnvVar, setProviderEnvVar,
    providerSupportsVision, setProviderSupportsVision,
    providerVisionModel, setProviderVisionModel,
    providerModelContextSizes, setProviderModelContextSizes,
    creatingWorkspaceConfig, setCreatingWorkspaceConfig,
    setLayerData,
  } = params;

  const [savingKey, setSavingKey] = useState<string | null>(null);

  const updateSetting = useCallback(
    async (keyOrPath: string, value: unknown) => {
      const current = settingsRef.current;
      if (!current) return;

      const prev = { ...current };
      setSavingKey(keyOrPath);

      try {
        const updated = setNestedValue(
          current as unknown as Record<string, unknown>,
          keyOrPath,
          value,
        ) as unknown as SproutSettings;
        onSettingsChanged(updated);

        let layer: 'session' | 'workspace' | 'global' | undefined;
        if (configViewLayer !== 'session') {
          layer = configViewLayer;
        }
        await api.updateSettings({ [keyOrPath]: value }, layer);
        addNotification('success', 'Settings', 'Saved', 3000);
        if (configViewLayer === 'session') {
          api.getSettingsProvenance()
            .then((data) => setProvenanceSources(data.sources || {}))
            .catch(() => { /* keep current badges */ });
        }
      } catch (err) {
        debugLog('[SettingsPanel] failed to save setting:', err);
        onSettingsChanged(prev);
        addNotification('error', 'Settings', 'Save failed', 5000);
      } finally {
        setSavingKey(null);
      }
    },
    [onSettingsChanged, api, addNotification, configViewLayer, setProvenanceSources],
  );

  /* ─── MCP server CRUD ──────────────────────────────────── */

  const resetServerForm = () => {
    setEditingServer(null);
    setServerName('');
    setServerCommand('');
    setServerArgs('');
    setServerEnvVars([]);
    setNewEnvKey('');
    setNewEnvValue('');
  };

  const handleAddServer = async () => {
    if (!serverName.trim()) return;
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
  };

  const handleUpdateServer = async () => {
    if (!editingServer?.originalName || !serverName.trim()) return;
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
  };

  const handleDeleteServer = async (name: string) => {
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
  };

  /* ─── MCP credential management ───────────────────────── */

  const resetCredentialForm = () => {
    setCredentialServer(null);
    setCredentialEntries([]);
    setCredentialLoading(false);
    setNewCredentialKey('');
    setNewCredentialValue('');
  };

  const handleLoadCredentials = async (credentialServerName: string) => {
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
  };

  const handleSaveCredential = async () => {
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
  };

  const handleDeleteCredential = async (credName: string) => {
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
  };

  const handleAddCredentialEntry = () => {
    if (!newCredentialKey.trim() || !newCredentialValue.trim()) return;
    setCredentialEntries((prev: Array<{ key: string; value: string; status: string }>) => [
      ...prev,
      { key: newCredentialKey.trim(), value: newCredentialValue.trim(), status: 'pending' },
    ]);
    setNewCredentialKey('');
    setNewCredentialValue('');
  };

  const handleCloseCredentials = () => {
    resetCredentialForm();
  };

  /* ─── Custom Provider CRUD ─────────────────────────────── */

  const resetProviderForm = () => {
    setEditingProvider(null);
    setProviderName('');
    setProviderApiBase('');
    setProviderModelName('');
    setProviderContextSize(32768);
    setProviderEnvVar('');
    setProviderSupportsVision(false);
    setProviderVisionModel('');
    setProviderModelContextSizes('');
  };

  const handleAddProvider = async () => {
    if (!providerName.trim()) return;
    const modelName = providerModelName.trim();
    const supportsVision = providerSupportsVision;
    const visionModel = providerVisionModel.trim() || modelName;
    const envVar = providerEnvVar.trim();

    const modelContextSizes: Record<string, number> = {};
    if (providerModelContextSizes.trim()) {
      const pairs = providerModelContextSizes
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      for (const pair of pairs) {
        const [model, size] = pair.split(':');
        if (model && size) {
          const sizeNum = parseInt(size, 10);
          if (!isNaN(sizeNum) && sizeNum > 0) {
            modelContextSizes[model.trim()] = sizeNum;
          }
        }
      }
    }

    const provider: Record<string, unknown> = {
      endpoint: providerApiBase.trim(),
      model_name: modelName,
      context_size: providerContextSize,
      model_context_sizes: Object.keys(modelContextSizes).length > 0 ? modelContextSizes : undefined,
      env_var: envVar,
      requires_api_key: envVar.length > 0,
      supports_vision: supportsVision,
      vision_model: supportsVision ? visionModel : '',
    };
    setSavingKey('provider-add');
    try {
      await api.addCustomProvider({ name: providerName.trim(), ...provider });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Provider added', 3000);
      resetProviderForm();
    } catch (err) {
      debugLog('[SettingsPanel] failed to add custom provider:', err);
      addNotification('error', 'Settings', 'Failed to add provider', 5000);
    } finally {
      setSavingKey(null);
    }
  };

  const handleUpdateProvider = async () => {
    if (!editingProvider?.originalName || !providerName.trim()) return;
    const modelName = providerModelName.trim();
    const supportsVision = providerSupportsVision;
    const visionModel = providerVisionModel.trim() || modelName;
    const envVar = providerEnvVar.trim();

    const modelContextSizes: Record<string, number> = {};
    if (providerModelContextSizes.trim()) {
      const pairs = providerModelContextSizes
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      for (const pair of pairs) {
        const [model, size] = pair.split(':');
        if (model && size) {
          const sizeNum = parseInt(size, 10);
          if (!isNaN(sizeNum) && sizeNum > 0) {
            modelContextSizes[model.trim()] = sizeNum;
          }
        }
      }
    }

    const provider: Record<string, unknown> = {
      endpoint: providerApiBase.trim(),
      model_name: modelName,
      context_size: providerContextSize,
      model_context_sizes: Object.keys(modelContextSizes).length > 0 ? modelContextSizes : undefined,
      env_var: envVar,
      requires_api_key: envVar.length > 0,
      supports_vision: supportsVision,
      vision_model: supportsVision ? visionModel : '',
    };
    setSavingKey('provider-update');
    try {
      await api.updateCustomProvider(editingProvider.originalName, {
        name: providerName.trim(),
        ...provider,
      });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Provider updated', 3000);
      resetProviderForm();
    } catch (err) {
      debugLog('[SettingsPanel] failed to update custom provider:', err);
      addNotification('error', 'Settings', 'Failed to update provider', 5000);
    } finally {
      setSavingKey(null);
    }
  };

  const handleDeleteProvider = async (name: string) => {
    setSavingKey('provider-delete');
    try {
      await api.deleteCustomProvider(name);
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      addNotification('success', 'Settings', 'Provider deleted', 3000);
      if (editingProvider?.originalName === name) resetProviderForm();
    } catch (err) {
      debugLog('[SettingsPanel] failed to delete custom provider:', err);
      addNotification('error', 'Settings', 'Failed to delete provider', 5000);
    } finally {
      setSavingKey(null);
    }
  };

  /* ─── Skills toggle ────────────────────────────────────── */

  const toggleSkill = async (skillName: string, enabled: boolean) => {
    if (!settings) return;
    setSavingKey(`skill-${skillName}`);
    try {
      const updatedSkills = {
        ...settings.skills,
        [skillName]: {
          ...(settings.skills?.[skillName] || {}),
          enabled,
        },
      };
      await api.updateSkills(updatedSkills);
      onSettingsChanged({ ...settings, skills: updatedSkills });
      addNotification('success', 'Settings', `${skillName} ${enabled ? 'enabled' : 'disabled'}`, 3000);
    } catch (err) {
      debugLog('[SettingsPanel] failed to update skill:', err);
      addNotification('error', 'Settings', 'Failed to update skill', 5000);
    } finally {
      setSavingKey(null);
    }
  };

  /* ─── Workspace config creation ────────────────────────── */

  const handleCreateWorkspaceConfig = async () => {
    if (creatingWorkspaceConfig) return;
    setCreatingWorkspaceConfig(true);
    try {
      const globalData = await api.getSettingsLayer('global');
      const { mcp: _mcpRedacted, ...workspaceData } = globalData;
      await api.updateSettings(workspaceData, 'workspace');
      const data = await api.getSettingsLayer('workspace');
      setLayerData(data);
      addNotification('success', 'Settings', 'Workspace config created from global settings', 3000);
    } catch (err) {
      addNotification('error', 'Settings', 'Failed to create workspace config', 5000);
    } finally {
      setCreatingWorkspaceConfig(false);
    }
  };

  return {
    savingKey,
    updateSetting,
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
    // Provider
    resetProviderForm,
    handleAddProvider,
    handleUpdateProvider,
    handleDeleteProvider,
    // Skills
    toggleSkill,
    // Workspace
    handleCreateWorkspaceConfig,
  };
}
