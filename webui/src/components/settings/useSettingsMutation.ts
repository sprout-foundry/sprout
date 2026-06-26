import { useState, useCallback } from 'react';
import type { SproutSettings, ApiService } from '../../services/api';
import { debugLog } from '../../utils/log';
import { setNestedValue } from './settingsHelpers';
import type { MutationContext } from './types';
import { useMCPServerMutations } from './useMCPServerMutations';
import { useProviderMutations } from './useProviderMutations';

type AddNotificationFn = (
  type: 'success' | 'error' | 'info',
  title: string,
  message: string,
  duration?: number,
) => string;

interface MutationHookParams {
  settings: SproutSettings | null;
  onSettingsChanged: (settings: SproutSettings) => void;
  addNotification: AddNotificationFn;
  configViewLayer: 'session' | 'workspace' | 'global';
  api: ReturnType<typeof ApiService.getInstance>;
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
  setCredentialEntries: (
    v:
      | Array<{ key: string; value: string; status: string }>
      | ((
          prev: Array<{ key: string; value: string; status: string }>,
        ) => Array<{ key: string; value: string; status: string }>),
  ) => void;
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
  providerApiKey: string;
  setProviderApiKey: (v: string) => void;
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
  setLayerData: (v: Record<string, unknown> | null) => void;
  // Provider catalog refresh (used after custom provider CRUD)
  refreshProviderCatalog?: () => void;
}

export function useSettingsMutation(params: MutationHookParams) {
  const {
    settings,
    onSettingsChanged,
    addNotification,
    configViewLayer,
    api,
    setProvenanceSources,
    settingsRef,
    editingServer,
    setEditingServer,
    serverName,
    setServerName,
    serverCommand,
    setServerCommand,
    serverArgs,
    setServerArgs,
    serverEnvVars,
    setServerEnvVars,
    newEnvKey,
    setNewEnvKey,
    newEnvValue,
    setNewEnvValue,
    credentialServer,
    setCredentialServer,
    credentialEntries,
    setCredentialEntries,
    credentialLoading,
    setCredentialLoading,
    newCredentialKey,
    setNewCredentialKey,
    newCredentialValue,
    setNewCredentialValue,
    editingProvider,
    setEditingProvider,
    providerName,
    setProviderName,
    providerApiBase,
    setProviderApiBase,
    providerModelName,
    setProviderModelName,
    providerContextSize,
    setProviderContextSize,
    providerEnvVar,
    setProviderEnvVar,
    providerApiKey,
    setProviderApiKey,
    providerSupportsVision,
    setProviderSupportsVision,
    providerVisionModel,
    setProviderVisionModel,
    providerModelContextSizes,
    setProviderModelContextSizes,
    creatingWorkspaceConfig,
    setCreatingWorkspaceConfig,
    setLayerData,
    refreshProviderCatalog,
  } = params;

  const [savingKey, setSavingKey] = useState<string | null>(null);

  // Create shared mutation context for sub-hooks
  const mutationContext: MutationContext = {
    api,
    settingsRef,
    onSettingsChanged,
    addNotification,
    configViewLayer,
    setProvenanceSources,
    setSavingKey,
    refreshProviderCatalog,
  };

  /* ─── Domain-specific mutation hooks ───────────────────── */

  const mcpMutations = useMCPServerMutations({
    ctx: mutationContext,
    editingServer,
    setEditingServer,
    serverName,
    setServerName,
    serverCommand,
    setServerCommand,
    serverArgs,
    setServerArgs,
    serverEnvVars,
    setServerEnvVars,
    newEnvKey,
    setNewEnvKey,
    newEnvValue,
    setNewEnvValue,
    credentialServer,
    setCredentialServer,
    credentialEntries,
    setCredentialEntries,
    credentialLoading,
    setCredentialLoading,
    newCredentialKey,
    setNewCredentialKey,
    newCredentialValue,
    setNewCredentialValue,
  });

  const providerMutations = useProviderMutations({
    ctx: mutationContext,
    editingProvider,
    setEditingProvider,
    providerName,
    setProviderName,
    providerApiBase,
    setProviderApiBase,
    providerModelName,
    setProviderModelName,
    providerContextSize,
    setProviderContextSize,
    providerEnvVar,
    setProviderEnvVar,
    providerApiKey,
    setProviderApiKey,
    providerSupportsVision,
    setProviderSupportsVision,
    providerVisionModel,
    setProviderVisionModel,
    providerModelContextSizes,
    setProviderModelContextSizes,
  });

  /* ─── Core settings mutation ───────────────────────────── */

  const updateSetting = useCallback(
    async (keyOrPath: string, value: unknown) => {
      const current = settingsRef.current;
      if (!current) return;

      const prev = { ...current };
      setSavingKey(keyOrPath);

      try {
        const updated = setNestedValue(current, keyOrPath, value) as SproutSettings;
        onSettingsChanged(updated);

        let layer: 'session' | 'workspace' | 'global' | undefined;
        if (configViewLayer !== 'session') {
          layer = configViewLayer;
        }
        await api.updateSettings({ [keyOrPath]: value }, layer);
        addNotification('success', 'Settings', 'Saved', 3000);
        if (configViewLayer === 'session') {
          api
            .getSettingsProvenance()
            .then((data) => setProvenanceSources(data.sources || {}))
            .catch((err) => {
              debugLog('[SettingsPanel] Failed to refresh provenance after save:', err);
            });
        }
      } catch (err) {
        debugLog('[SettingsPanel] failed to save setting:', err);
        onSettingsChanged(prev);
        addNotification('error', 'Settings', 'Save failed', 5000);
      } finally {
        setSavingKey(null);
      }
    },
    [onSettingsChanged, api, addNotification, configViewLayer, setProvenanceSources, settingsRef],
  );

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
      await api.updateSkills({ skills: updatedSkills });
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
    ...mcpMutations,
    // Provider
    ...providerMutations,
    // Skills
    toggleSkill,
    // Workspace
    handleCreateWorkspaceConfig,
  };
}
