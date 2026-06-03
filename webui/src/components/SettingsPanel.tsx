import { ChevronRight, Search, X } from 'lucide-react';
import { useRef, useEffect, useMemo, useState } from 'react';
import './SettingsPanel.css';
import type { SproutSettings } from '../services/api';
import type { AgentConfigProps } from './settings/types';
import { Skeleton } from '@sprout/ui';
import CredentialsSettingsTab from './CredentialsSettingsTab';

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
import SubagentSettingsTab from './settings/SubagentSettingsTab';
import {
  SECTION_GROUPS,
  getSectionForSubsection,
  scopeToLayer,
  subsectionToLegacyTab,
  type SectionDef,
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

  /* ─── Collapsible section state (persisted across reopens) ── */

  const STORAGE_KEY = 'sprout.settingsPanel.state.v1';
  const restored = useMemo(() => {
    try {
      const raw = typeof window !== 'undefined' ? window.localStorage.getItem(STORAGE_KEY) : null;
      if (!raw) return null;
      const parsed = JSON.parse(raw) as {
        expanded?: SettingsSection[];
        activeSubsection?: SettingsSubsection | null;
      };
      return parsed;
    } catch {
      return null;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [activeSubsection, setActiveSubsection] = useState<SettingsSubsection | null>(
    restored?.activeSubsection ?? null,
  );
  const [expandedSections, setExpandedSections] = useState<Set<SettingsSection>>(
    new Set(restored?.expanded ?? ['agent']),
  );
  const [showCredentials, setShowCredentials] = useState(false);
  const [filterQuery, setFilterQuery] = useState('');
  const filterInputRef = useRef<HTMLInputElement>(null);

  // Cmd/Ctrl+K focuses the settings filter when the panel is mounted.
  // Escape clears the filter when it's focused. Both are scoped to the panel
  // to avoid stealing global shortcuts.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        // Only intercept if a settings filter input exists in the DOM.
        if (filterInputRef.current && document.body.contains(filterInputRef.current)) {
          e.preventDefault();
          filterInputRef.current.focus();
          filterInputRef.current.select();
        }
      } else if (e.key === 'Escape' && document.activeElement === filterInputRef.current) {
        setFilterQuery('');
        filterInputRef.current?.blur();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  // Persist expanded sections + active subsection on change so reopening the
  // panel or reloading the page restores the user's last position.
  useEffect(() => {
    try {
      window.localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          expanded: Array.from(expandedSections),
          activeSubsection,
        }),
      );
    } catch {
      // ignore quota / privacy-mode errors
    }
  }, [expandedSections, activeSubsection]);

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

  // Filter sections/subsections by user query. A section is visible if its
  // label/description matches, or if any of its subsection labels match.
  // When filtering, matched sections auto-expand so results are visible.
  const normalizedQuery = filterQuery.trim().toLowerCase();
  const filteredSections = useMemo(() => {
    if (!normalizedQuery) return SECTION_GROUPS;
    return SECTION_GROUPS.map((section) => {
      const sectionMatches =
        section.label.toLowerCase().includes(normalizedQuery) ||
        section.description.toLowerCase().includes(normalizedQuery) ||
        section.scope.toLowerCase().includes(normalizedQuery);
      const matchingSubs = section.subsections.filter((sub) =>
        sub.label.toLowerCase().includes(normalizedQuery),
      );
      if (sectionMatches || matchingSubs.length > 0) {
        return {
          ...section,
          subsections: matchingSubs.length > 0 ? matchingSubs : section.subsections,
        };
      }
      return null;
    }).filter((s): s is (typeof SECTION_GROUPS)[number] => s !== null);
  }, [normalizedQuery]);

  // Auto-expand any section that has matches while filtering.
  useEffect(() => {
    if (!normalizedQuery) return;
    setExpandedSections((prev) => {
      const next = new Set(prev);
      for (const section of filteredSections) {
        next.add(section.id);
      }
      return next;
    });
  }, [normalizedQuery, filteredSections]);

  const credentialsMatches = !normalizedQuery || 'credentials'.includes(normalizedQuery);

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
            updateSetting={mutations.updateSetting}
            addNotification={state.addNotification}
            renderToggle={fieldRenderers.renderToggle}
            renderNumberInput={fieldRenderers.renderNumberInput}
            renderSelect={fieldRenderers.renderSelect}
          />
        );

      case 'agent-skills':
        return <SkillsSettingsTab settings={activeSettings ?? settings} toggleSkill={mutations.toggleSkill} />;

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

  /* ─── Main render ─────────────────────────────────────── */

  const scopeTitle: Record<SectionDef['scope'], string> = {
    session: 'Session-scoped — applies to this chat session only.',
    workspace: 'Workspace-scoped — saved in this project directory.',
    global: 'Global — saved in ~/.config/sprout, applies everywhere.',
    runtime: 'Runtime — this browser session only, not persisted to disk.',
  };

  return (
    <div className="settings-panel">
      {/* Filter bar */}
      <div className="settings-filter">
        <Search size={12} className="settings-filter-icon" aria-hidden="true" />
        <input
          ref={filterInputRef}
          type="text"
          className="settings-filter-input"
          value={filterQuery}
          onChange={(e) => setFilterQuery(e.target.value)}
          placeholder="Filter settings… (⌘/Ctrl+K)"
          aria-label="Filter settings"
        />
        {filterQuery && (
          <button
            type="button"
            className="settings-filter-clear"
            onClick={() => setFilterQuery('')}
            title="Clear filter (Esc)"
            aria-label="Clear filter"
          >
            <X size={12} />
          </button>
        )}
      </div>

      {normalizedQuery && filteredSections.length === 0 && !credentialsMatches ? (
        <div className="settings-no-match">
          <Search size={16} />
          <div>
            <div>No settings match “{filterQuery}”</div>
            <button type="button" className="settings-no-match-action" onClick={() => setFilterQuery('')}>
              Clear filter
            </button>
          </div>
        </div>
      ) : null}

      {filteredSections.map((section) => (
        <div key={section.id} className={`settings-section ${expandedSections.has(section.id) ? 'expanded' : ''}`}>
          {/* Section header (clickable to toggle) */}
          <button
            type="button"
            className="settings-section-header"
            onClick={() => toggleSection(section.id)}
            aria-expanded={expandedSections.has(section.id)}
          >
            <span className="settings-section-label">{section.label}</span>
            <span
              className={`settings-scope-badge scope-${section.scope}`}
              title={scopeTitle[section.scope]}
            >
              {section.scope}
            </span>
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
                </div>
              )}

              {/* Subsection buttons */}
              <div className="settings-subsection-list" role="tablist" aria-label={`${section.label} sub-sections`}>
                {section.subsections.map((sub) => {
                  const isActive = activeSubsection === sub.id;
                  return (
                    <button
                      key={sub.id}
                      type="button"
                      role="tab"
                      id={`settings-subtab-${sub.id}`}
                      aria-selected={isActive}
                      aria-controls={`settings-subpanel-${sub.id}`}
                      className={`settings-subsection-btn ${isActive ? 'active' : ''}`}
                      onClick={() => setActiveSubsection(sub.id)}
                    >
                      {sub.label}
                    </button>
                  );
                })}
              </div>

              {/* Subsection content area */}
              <div
                className="settings-subsection-content"
                role="tabpanel"
                id={activeSubsection ? `settings-subpanel-${activeSubsection}` : undefined}
                aria-labelledby={activeSubsection ? `settings-subtab-${activeSubsection}` : undefined}
              >
                {activeSubsection &&
                  section.subsections.some((s) => s.id === activeSubsection) &&
                  renderSubsectionContent(activeSubsection)}
              </div>
            </div>
          )}
        </div>
      ))}
      {/* Credentials — separate panel per spec, not inside any section */}
      {credentialsMatches && (
        <div className="settings-credentials-link">
          <button
            type="button"
            className={`settings-section-header ${showCredentials ? 'expanded' : ''}`}
            onClick={() => setShowCredentials(!showCredentials)}
            aria-expanded={showCredentials}
          >
            <span className="settings-section-label">Credentials</span>
            <span className="settings-scope-badge scope-global" title={scopeTitle.global}>
              global
            </span>
            <ChevronRight className="settings-section-chevron" size={14} />
          </button>
          {showCredentials && (
            <div className="settings-section-body">
              <CredentialsSettingsTab />
            </div>
          )}
        </div>
      )}

      {fieldRenderers.renderSaving()}
    </div>
  );
}

export default SettingsPanel;
