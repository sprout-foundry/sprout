import { useRef, useEffect } from 'react';
import './SettingsPanel.css';
import type { SproutSettings } from '../services/api';
import CredentialsSettingsTab from './CredentialsSettingsTab';

// Import from settings/ subdirectory
import { SUB_TABS, type EditorPreferences, type SettingsPanelProps } from './settings/types';
import { useSettingsState } from './settings/useSettingsState';
import { useSettingsMutation } from './settings/useSettingsMutation';
import { useSettingsFieldRenderers } from './settings/useSettingsFieldRenderers';

// Import tab sub-components
import GeneralSettingsTab from './settings/GeneralSettingsTab';
import SecuritySettingsTab from './settings/SecuritySettingsTab';
import PerformanceSettingsTab from './settings/PerformanceSettingsTab';
import OcrSettingsTab from './settings/OcrSettingsTab';
import SkillsSettingsTab from './settings/SkillsSettingsTab';
import SubagentSettingsTab from './settings/SubagentSettingsTab';
import CommitReviewSettingsTab from './settings/CommitReviewSettingsTab';
import MCPSettingsTab from './settings/MCPSettingsTab';
import ProviderSettingsTab from './settings/ProviderSettingsTab';

/* ─── Component ──────────────────────────────────────────────── */

function SettingsPanel({
  settings,
  onSettingsChanged,
  onRequestProviderSetup,
  editorPreferences,
  onEditorPreferenceChanged,
}: SettingsPanelProps): JSX.Element {
  // Track settings ref for async mutation callbacks
  const settingsRef = useRef<SproutSettings | null>(settings);
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  // Use custom hooks for state and mutations
  const state = useSettingsState(settings, onSettingsChanged, onRequestProviderSetup);
  const mutations = useSettingsMutation({
    settings,
    onSettingsChanged,
    addNotification: state.addNotification,
    configViewLayer: state.configViewLayer,
    api: state.api,
    setProvenanceSources: state.setProvenanceSources,
    // MCP server state
    editingServer: state.editingServer,
    setEditingServer: state.setEditingServer,
    serverName: state.serverName,
    setServerName: state.setServerName,
    serverCommand: state.serverCommand,
    setServerCommand: state.setServerCommand,
    serverArgs: state.serverArgs,
    setServerArgs: state.setServerArgs,
    serverEnvVars: state.serverEnvVars,
    setServerEnvVars: state.setServerEnvVars,
    newEnvKey: state.newEnvKey,
    setNewEnvKey: state.setNewEnvKey,
    newEnvValue: state.newEnvValue,
    setNewEnvValue: state.setNewEnvValue,
    // Credentials
    credentialServer: state.credentialServer,
    setCredentialServer: state.setCredentialServer,
    credentialEntries: state.credentialEntries,
    setCredentialEntries: state.setCredentialEntries,
    credentialLoading: state.credentialLoading,
    setCredentialLoading: state.setCredentialLoading,
    newCredentialKey: state.newCredentialKey,
    setNewCredentialKey: state.setNewCredentialKey,
    newCredentialValue: state.newCredentialValue,
    setNewCredentialValue: state.setNewCredentialValue,
    // Provider form
    editingProvider: state.editingProvider,
    setEditingProvider: state.setEditingProvider,
    providerName: state.providerName,
    setProviderName: state.setProviderName,
    providerApiBase: state.providerApiBase,
    setProviderApiBase: state.setProviderApiBase,
    providerModelName: state.providerModelName,
    setProviderModelName: state.setProviderModelName,
    providerContextSize: state.providerContextSize,
    setProviderContextSize: state.setProviderContextSize,
    providerEnvVar: state.providerEnvVar,
    setProviderEnvVar: state.setProviderEnvVar,
    providerSupportsVision: state.providerSupportsVision,
    setProviderSupportsVision: state.setProviderSupportsVision,
    providerVisionModel: state.providerVisionModel,
    setProviderVisionModel: state.setProviderVisionModel,
    providerModelContextSizes: state.providerModelContextSizes,
    setProviderModelContextSizes: state.setProviderModelContextSizes,
    // Refs and workspace
    settingsRef,
    creatingWorkspaceConfig: state.creatingWorkspaceConfig,
    setCreatingWorkspaceConfig: state.setCreatingWorkspaceConfig,
    setLayerData: state.setLayerData,
  });

  // Use field renderers hook
  const renderers = useSettingsFieldRenderers({
    displaySettingsRef: state.displaySettingsRef,
    settings,
    textDrafts: state.textDrafts,
    setTextDrafts: state.setTextDrafts,
    textSaveTimersRef: state.textSaveTimersRef,
    updateSetting: mutations.updateSetting,
    savingKey: mutations.savingKey,
    provenanceSources: state.provenanceSources,
    configViewLayer: state.configViewLayer,
  });

  // Determine which settings to display based on layer
  const activeSettings: SproutSettings | null =
    state.configViewLayer !== 'session' && state.layerData
      ? (state.layerData as unknown as SproutSettings)
      : settings;

  // Update the display ref with active settings
  state.displaySettingsRef.current = activeSettings;

  /* ─── Render tab content ───────────────────────────── */

  const renderContent = () => {
    if (!settings) {
      return <div className="settings-empty">Loading settings…</div>;
    }

    switch (state.activeSubTab) {
      case 'general':
        return (
          <GeneralSettingsTab
            editorPreferences={editorPreferences}
            onEditorPreferenceChanged={onEditorPreferenceChanged}
            renderToggle={renderers.renderToggle}
            renderSelect={renderers.renderSelect}
            renderTextareaInput={renderers.renderTextareaInput}
          />
        );

      case 'security':
        return (
          <SecuritySettingsTab
            renderToggle={renderers.renderToggle}
            renderNumberInput={renderers.renderNumberInput}
            renderSelect={renderers.renderSelect}
          />
        );

      case 'credentials':
        return <CredentialsSettingsTab />;

      case 'performance':
        return (
          <PerformanceSettingsTab
            renderNumberInput={renderers.renderNumberInput}
          />
        );

      case 'subagents':
        return (
          <SubagentSettingsTab
            settings={settings}
            subagentProviders={state.subagentProviders}
            subagentTypes={state.subagentTypes}
            subagentSavingPersona={state.subagentSavingPersona}
            setSubagentSavingPersona={state.setSubagentSavingPersona}
            setSubagentTypes={state.setSubagentTypes}
            updateSetting={mutations.updateSetting}
            addNotification={state.addNotification}
            renderToggle={renderers.renderToggle}
            renderNumberInput={renderers.renderNumberInput}
            renderSelect={renderers.renderSelect}
            api={state.api}
          />
        );

      case 'commit-review':
        return (
          <CommitReviewSettingsTab
            settings={settings}
            commitReviewProviders={state.commitReviewProviders}
            updateSetting={mutations.updateSetting}
          />
        );

      case 'pdf-ocr':
        return (
          <OcrSettingsTab
            renderToggle={renderers.renderToggle}
            renderTextInput={renderers.renderTextInput}
          />
        );

      case 'mcp':
        return (
          <MCPSettingsTab
            settings={settings}
            editingServer={state.editingServer}
            serverName={state.serverName}
            serverCommand={state.serverCommand}
            serverArgs={state.serverArgs}
            serverEnvVars={state.serverEnvVars}
            newEnvKey={state.newEnvKey}
            newEnvValue={state.newEnvValue}
            credentialServer={state.credentialServer}
            credentialEntries={state.credentialEntries}
            credentialLoading={state.credentialLoading}
            newCredentialKey={state.newCredentialKey}
            newCredentialValue={state.newCredentialValue}
            savingKey={mutations.savingKey}
            setEditingServer={state.setEditingServer}
            setServerName={state.setServerName}
            setServerCommand={state.setServerCommand}
            setServerArgs={state.setServerArgs}
            setServerEnvVars={state.setServerEnvVars}
            setNewEnvKey={state.setNewEnvKey}
            setNewEnvValue={state.setNewEnvValue}
            setCredentialEntries={state.setCredentialEntries}
            setNewCredentialKey={state.setNewCredentialKey}
            setNewCredentialValue={state.setNewCredentialValue}
            renderToggle={renderers.renderToggle}
            renderTextInput={renderers.renderTextInput}
            resetServerForm={mutations.resetServerForm}
            handleAddServer={mutations.handleAddServer}
            handleUpdateServer={mutations.handleUpdateServer}
            handleDeleteServer={mutations.handleDeleteServer}
            handleLoadCredentials={mutations.handleLoadCredentials}
            handleSaveCredential={mutations.handleSaveCredential}
            handleDeleteCredential={mutations.handleDeleteCredential}
            handleAddCredentialEntry={mutations.handleAddCredentialEntry}
            handleCloseCredentials={mutations.handleCloseCredentials}
          />
        );

      case 'providers':
        return (
          <ProviderSettingsTab
            settings={settings}
            onRequestProviderSetup={onRequestProviderSetup}
            editingProvider={state.editingProvider}
            providerName={state.providerName}
            providerApiBase={state.providerApiBase}
            providerModelName={state.providerModelName}
            providerContextSize={state.providerContextSize}
            providerEnvVar={state.providerEnvVar}
            providerSupportsVision={state.providerSupportsVision}
            providerVisionModel={state.providerVisionModel}
            providerModelContextSizes={state.providerModelContextSizes}
            loadingProviderInfo={state.loadingProviderInfo}
            currentProviderInfo={state.currentProviderInfo}
            setEditingProvider={state.setEditingProvider}
            setProviderName={state.setProviderName}
            setProviderApiBase={state.setProviderApiBase}
            setProviderModelName={state.setProviderModelName}
            setProviderContextSize={state.setProviderContextSize}
            setProviderEnvVar={state.setProviderEnvVar}
            setProviderSupportsVision={state.setProviderSupportsVision}
            setProviderVisionModel={state.setProviderVisionModel}
            setProviderModelContextSizes={state.setProviderModelContextSizes}
            resetProviderForm={mutations.resetProviderForm}
            handleAddProvider={mutations.handleAddProvider}
            handleUpdateProvider={mutations.handleUpdateProvider}
            handleDeleteProvider={mutations.handleDeleteProvider}
          />
        );

      case 'skills':
        return (
          <SkillsSettingsTab
            settings={settings}
            toggleSkill={mutations.toggleSkill}
          />
        );

      default:
        return null;
    }
  };

  /* ─── Main render ───────────────────────────────────────── */

  return (
    <div className="settings-panel">
      {/* Sub-tab bar */}
      <div className="settings-subtab-bar">
        {SUB_TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`settings-subtab ${state.activeSubTab === tab.id ? 'active' : ''}`}
            onClick={() => state.setActiveSubTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
        {renderers.renderSaving()}
      </div>

      {/* Config Scope Selector — applies to all tabs */}
      <div className="config-scope-row">
        <div className="config-scope-buttons">
          {(['session', 'workspace', 'global'] as const).map((layer) => (
            <button
              key={layer}
              type="button"
              className={`layerscope-btn ${state.configViewLayer === layer ? 'active' : ''}`}
              onClick={() => state.setConfigViewLayer(layer)}
              disabled={state.layerLoading === layer}
            >
              {layer === 'session' ? 'Session' : layer === 'workspace' ? 'Workspace' : 'Global'}
              {state.layerLoading === layer && <span style={{ marginLeft: 4, opacity: 0.5 }}>…</span>}
            </button>
          ))}
        </div>
        <span className="config-scope-desc">
          {state.configViewLayer === 'session' && 'Session overrides only'}
          {state.configViewLayer === 'workspace' && 'Workspace config (shared across sessions)'}
          {state.configViewLayer === 'global' && 'Global config (~/.config/sprout)'}
        </span>
        {state.layerError && (
          <div className="config-scope-error">{state.layerError}</div>
        )}
        {state.configViewLayer === 'workspace' && state.layerData && Object.keys(state.layerData).length === 0 && (
          <div className="config-scope-create">
            <span>No workspace config found. </span>
            <button
              type="button"
              className="config-scope-create-btn"
              disabled={state.creatingWorkspaceConfig}
              onClick={mutations.handleCreateWorkspaceConfig}
            >
              {state.creatingWorkspaceConfig ? 'Creating…' : 'Create from global'}
            </button>
          </div>
        )}
      </div>

      {/* Content */}
      {renderContent()}
    </div>
  );
}

export default SettingsPanel;
