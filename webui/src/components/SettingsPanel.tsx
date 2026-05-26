import { ChevronRight } from 'lucide-react';
import { useRef, useEffect, useState } from 'react';
import './SettingsPanel.css';
import type { SproutSettings } from '../services/api';
import type { AgentConfigProps } from './settings/types';
import { Skeleton } from '@sprout/ui';
import CredentialsSettingsTab from './CredentialsSettingsTab';
import RoleSelector from './RoleSelector';

// Extended agent config that adds optional role selection
type ExtendedAgentConfig = AgentConfigProps & {
  selectedRole?: string | null;
  onRoleChange?: (roleId: string | null) => void;
};

// Import from settings/ subdirectory
import AgentBehaviorSettingsTab from './settings/AgentBehaviorSettingsTab';
import CommitReviewSettingsTab from './settings/CommitReviewSettingsTab';
import EmbeddingSettingsTab from './settings/EmbeddingSettingsTab';
import GeneralSettingsTab from './settings/GeneralSettingsTab';
import LanguageServersSettingsTab from './settings/LanguageServersSettingsTab';
import MCPSettingsTab from './settings/MCPSettingsTab';
import OcrSettingsTab from './settings/OcrSettingsTab';
import PerformanceSettingsTab from './settings/PerformanceSettingsTab';
import PersistentContextSettingsTab from './settings/PersistentContextSettingsTab';
import ProviderSettingsTab from './settings/ProviderSettingsTab';
import SecuritySettingsTab from './settings/SecuritySettingsTab';
import SkillsSettingsTab from './settings/SkillsSettingsTab';
import { RolesSettingsTab } from './settings/RolesSettingsTab';
import SubagentSettingsTab from './settings/SubagentSettingsTab';
import {
  SECTION_GROUPS,
  getSectionForSubsection,
  scopeToLayer,
  subsectionToLegacyTab,
  type SettingsSubsection,
  type SettingsSection,
  type SettingsPanelProps,
} from './settings/types';
import { useSettingsFieldRenderers } from './settings/useSettingsFieldRenderers';
import { useSettingsMutation } from './settings/useSettingsMutation';
import { useSettingsState } from './settings/useSettingsState';

// Import tab sub-components

/* ─── Component ──────────────────────────────────────────────── */

function SettingsPanel({
  settings,
  onSettingsChanged,
  onRequestProviderSetup,
  editorPreferences,
  onEditorPreferenceChanged,
  agentConfig,
}: SettingsPanelProps): JSX.Element {
  // Track settings ref for async mutation callbacks
  const settingsRef = useRef<SproutSettings | null>(settings);
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  /* ─── Collapsible section state ──────────────────────── */

  const [activeSubsection, setActiveSubsection] = useState<SettingsSubsection | null>(null);
  const [expandedSections, setExpandedSections] = useState<Set<SettingsSection>>(new Set(['agent']));
  const [showCredentials, setShowCredentials] = useState(false);

  const toggleSection = (sectionId: SettingsSection) => {
    setExpandedSections((prev) => {
      const next = new Set(prev);
      if (next.has(sectionId)) {
        next.delete(sectionId);
      } else {
        next.add(sectionId);
      }
      return next;
    });
  };

  /* ─── Custom hooks (must be called before any useEffects) ── */

  const state = useSettingsState(settings, onSettingsChanged, onRequestProviderSetup);

  /* ─── Derive effective layer from active subsection ──── */

  const activeSection = activeSubsection ? getSectionForSubsection(activeSubsection) : undefined;
  const effectiveLayer = activeSection ? scopeToLayer(activeSection.scope) : 'session';

  /* ─── Sync legacy activeSubTab to trigger fetch effects ── */

  useEffect(() => {
    if (activeSubsection) {
      const legacyTab = subsectionToLegacyTab(activeSubsection);
      state.setActiveSubTab(legacyTab);
    }
  }, [activeSubsection]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    state.setConfigViewLayer(effectiveLayer);
  }, [effectiveLayer]); // eslint-disable-line react-hooks/exhaustive-deps

  const mutations = useSettingsMutation({
    settings,
    onSettingsChanged,
    addNotification: state.addNotification,
    configViewLayer: effectiveLayer,
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
    refreshProviderCatalog: state.refreshSubagentProviders,
  });

  // Use field renderers hook — pass effectiveLayer for provenance/scoping
  // eslint-disable-next-line testing-library/render-result-naming-convention
  const fieldRenderers = useSettingsFieldRenderers({
    displaySettingsRef: state.displaySettingsRef,
    settings,
    textDrafts: state.textDrafts,
    setTextDrafts: state.setTextDrafts,
    textSaveTimersRef: state.textSaveTimersRef,
    updateSetting: mutations.updateSetting,
    savingKey: mutations.savingKey,
    provenanceSources: state.provenanceSources,
    configViewLayer: effectiveLayer,
  });

  // Determine which settings to display based on effective layer.
  // Merge with session settings as fallback for missing fields.
  const activeSettings: SproutSettings | null =
    effectiveLayer !== 'session' && state.layerData
      ? ({ ...settings, ...state.layerData } as SproutSettings)
      : settings;

  // Sync display ref with active settings (in useEffect to avoid render-time mutation)
  useEffect(() => {
    state.displaySettingsRef.current = activeSettings;
  }, [activeSettings]); // eslint-disable-line react-hooks/exhaustive-deps

  /* ─── Render subsection content ──────────────────────── */

  const renderSubsectionContent = (subsectionId: SettingsSubsection) => {
    if (!settings) {
      return (
        <div className="settings-skeleton" role="status" aria-label="Loading settings">
          <div className="settings-skeleton-rows">
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="settings-skeleton-row">
                <div className="settings-skeleton-label">
                  <Skeleton width={`${30 + Math.floor((i * 53) % 40)}%`} height="12px" />
                </div>
                <div className="settings-skeleton-input">
                  <Skeleton width="100%" height="30px" radius="6px" />
                </div>
              </div>
            ))}
          </div>
          <span className="sr-only">Loading settings...</span>
        </div>
      );
    }

    switch (subsectionId) {
      /* ── Agent section ─────────────────────────────── */
      case 'agent-general':
        return (
          <AgentBehaviorSettingsTab
            settings={activeSettings ?? settings}
            renderToggle={fieldRenderers.renderToggle}
            renderSelect={fieldRenderers.renderSelect}
            renderTextareaInput={fieldRenderers.renderTextareaInput}
          />
        );

      case 'agent-behavior':
        return (
          <SecuritySettingsTab
            settings={activeSettings ?? settings}
            renderToggle={fieldRenderers.renderToggle}
            renderNumberInput={fieldRenderers.renderNumberInput}
            renderSelect={fieldRenderers.renderSelect}
            updateSetting={mutations.updateSetting}
          />
        );

      case 'agent-subagents':
        return (
          <SubagentSettingsTab
            settings={activeSettings ?? settings}
            subagentProviders={state.subagentProviders}
            subagentTypes={state.subagentTypes}
            subagentSavingPersona={state.subagentSavingPersona}
            setSubagentSavingPersona={state.setSubagentSavingPersona}
            setSubagentTypes={state.setSubagentTypes}
            updateSetting={mutations.updateSetting}
            addNotification={state.addNotification}
            renderToggle={fieldRenderers.renderToggle}
            renderNumberInput={fieldRenderers.renderNumberInput}
            renderSelect={fieldRenderers.renderSelect}
            api={state.api}
          />
        );

      case 'agent-skills':
        return <SkillsSettingsTab settings={activeSettings ?? settings} toggleSkill={mutations.toggleSkill} />;

      case 'agent-roles':
        return <RolesSettingsTab addNotification={state.addNotification} />;

      case 'agent-memory':
        return (
          <PersistentContextSettingsTab
            settings={activeSettings ?? settings}
            updateSetting={mutations.updateSetting}
          />
        );

      /* ── Workspace section ─────────────────────────── */
      case 'workspace-embeddings':
        return (
          <EmbeddingSettingsTab
            settings={activeSettings ?? settings}
            renderToggle={fieldRenderers.renderToggle}
            renderTextInput={fieldRenderers.renderTextInput}
            updateSetting={mutations.updateSetting}
          />
        );

      case 'workspace-lsp':
        return (
          <LanguageServersSettingsTab
            settings={activeSettings ?? settings}
            updateSetting={mutations.updateSetting}
          />
        );

      case 'workspace-mcp':
        return (
          <MCPSettingsTab
            settings={activeSettings ?? settings}
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
            renderToggle={fieldRenderers.renderToggle}
            renderTextInput={fieldRenderers.renderTextInput}
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

      /* ── Environment section ───────────────────────── */
      case 'env-providers':
        return (
          <ProviderSettingsTab
            settings={activeSettings ?? settings}
            onRequestProviderSetup={onRequestProviderSetup}
            availableProviders={state.subagentProviders}
            onPrimaryProviderChanged={state.refreshCurrentProviderInfo}
            updateSetting={mutations.updateSetting}
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

      case 'env-performance':
        return (
          <PerformanceSettingsTab
            renderNumberInput={fieldRenderers.renderNumberInput}
            renderTextInput={fieldRenderers.renderTextInput}
          />
        );

      case 'env-commit-review':
        return (
          <CommitReviewSettingsTab
            settings={activeSettings ?? settings}
            commitReviewProviders={state.commitReviewProviders}
            updateSetting={mutations.updateSetting}
          />
        );

      case 'env-ocr':
        return (
          <OcrSettingsTab renderToggle={fieldRenderers.renderToggle} renderTextInput={fieldRenderers.renderTextInput} />
        );

      /* ── Editor section ────────────────────────────── */
      case 'editor-preferences':
        return (
          <GeneralSettingsTab
            editorPreferences={editorPreferences}
            onEditorPreferenceChanged={onEditorPreferenceChanged}
          />
        );

      default:
        return null;
    }
  };

  /* ─── Extract typed role config to avoid repeated assertions ─── */
  const roleConfig = (
    agentConfig && 'selectedRole' in agentConfig && 'onRoleChange' in agentConfig
  )
    ? agentConfig as unknown as ExtendedAgentConfig
    : null;

  /* ─── Main render ─────────────────────────────────────── */

  return (
    <div className="settings-panel">
      {SECTION_GROUPS.map((section) => (
        <div key={section.id} className={`settings-section ${expandedSections.has(section.id) ? 'expanded' : ''}`}>
          {/* Section header (clickable to toggle) */}
          <button
            type="button"
            className="settings-section-header"
            onClick={() => toggleSection(section.id)}
            aria-expanded={expandedSections.has(section.id)}
          >
            <span className="settings-section-label">{section.label}</span>
            <span className={`settings-scope-badge scope-${section.scope}`}>{section.scope}</span>
            <ChevronRight className="settings-section-chevron" size={14} />
          </button>

          {/* Expanded body */}
          {expandedSections.has(section.id) && (
            <div className="settings-section-body">
              <p className="settings-section-desc">{section.description}</p>

              {/* Agent config selectors (Provider, Model, Persona) */}
              {section.id === 'agent' && agentConfig && (
                <div className="agent-config-body">
                  <div className="config-item">
                    <label htmlFor="provider-select">Provider:</label>
                    <select
                      id="provider-select"
                      value={agentConfig.selectedProvider}
                      onChange={(e) => agentConfig.onProviderChange(e.target.value)}
                      disabled={!agentConfig.isConnected || agentConfig.isLoadingProviders}
                      className="styled-select"
                    >
                      {agentConfig.providers.length === 0 && (
                        <option value="">
                          {agentConfig.isLoadingProviders ? 'Loading providers...' : 'No providers available'}
                        </option>
                      )}
                      {agentConfig.providers.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="config-item">
                    <label htmlFor="model-select">Model:</label>
                    <select
                      id="model-select"
                      value={agentConfig.selectedModel}
                      onChange={(e) => agentConfig.onModelChange(e.target.value)}
                      disabled={!agentConfig.isConnected || agentConfig.availableModels.length === 0}
                      className="styled-select"
                    >
                      {agentConfig.availableModels.map((m) => (
                        <option key={m} value={m}>
                          {m}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="config-item">
                    <label htmlFor="persona-select">Persona:</label>
                    <select
                      id="persona-select"
                      value={agentConfig.selectedPersona}
                      onChange={(e) => agentConfig.onPersonaChange(e.target.value)}
                      disabled={!agentConfig.isConnected || agentConfig.isLoadingPersonas}
                      className="styled-select"
                    >
                      {agentConfig.isLoadingPersonas ? (
                        <option value="">Loading personas...</option>
                      ) : (
                        agentConfig.personas.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))
                      )}
                    </select>
                  </div>
                  {roleConfig && (
                    <RoleSelector
                      selectedRole={roleConfig.selectedRole ?? null}
                      onRoleChange={roleConfig.onRoleChange ?? (() => {})}
                    />
                  )}
                </div>
              )}

              {/* Subsection buttons */}
              <div className="settings-subsection-list">
                {section.subsections.map((sub) => (
                  <button
                    key={sub.id}
                    type="button"
                    className={`settings-subsection-btn ${activeSubsection === sub.id ? 'active' : ''}`}
                    onClick={() => setActiveSubsection(sub.id)}
                  >
                    {sub.label}
                  </button>
                ))}
              </div>

              {/* Subsection content area */}
              <div className="settings-subsection-content">
                {activeSubsection &&
                  section.subsections.some((s) => s.id === activeSubsection) &&
                  renderSubsectionContent(activeSubsection)}
              </div>
            </div>
          )}
        </div>
      ))}
      {/* Credentials — separate panel per spec, not inside any section */}
      <div className="settings-credentials-link">
        <button
          type="button"
          className={`settings-section-header ${showCredentials ? 'expanded' : ''}`}
          onClick={() => setShowCredentials(!showCredentials)}
          aria-expanded={showCredentials}
        >
          <span className="settings-section-label">Credentials</span>
          <span className="settings-scope-badge scope-global">global</span>
          <ChevronRight className="settings-section-chevron" size={14} />
        </button>
        {showCredentials && (
          <div className="settings-section-body">
            <CredentialsSettingsTab />
          </div>
        )}
      </div>

      {fieldRenderers.renderSaving()}
    </div>
  );
}

export default SettingsPanel;
