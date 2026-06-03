import type { SproutSettings, ProviderOption } from '../../services/api';
import type { SubagentTypeInfo } from '../../services/api/types';
import { getNestedValue } from './settingsHelpers';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface SubagentSettingsTabProps {
  settings: SproutSettings;
  subagentProviders: ProviderOption[];
  subagentTypes: Record<string, SubagentTypeInfo>;
  subagentSavingPersona: string | null;
  setSubagentSavingPersona: (v: string | null) => void;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
  addNotification: (type: 'success' | 'error' | 'info', title: string, message: string, duration?: number) => string;
  renderToggle: FieldRenderers['renderToggle'];
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderSelect: FieldRenderers['renderSelect'];
  api: { updateSubagentType: (id: string, data: Record<string, unknown>) => Promise<unknown> };
  setSubagentTypes: (
    v:
      | Record<string, SubagentTypeInfo>
      | ((prev: Record<string, SubagentTypeInfo>) => Record<string, SubagentTypeInfo>),
  ) => void;
}

export default function SubagentSettingsTab({
  settings,
  subagentProviders,
  subagentTypes,
  subagentSavingPersona,
  setSubagentSavingPersona,
  setSubagentTypes,
  updateSetting,
  addNotification,
  renderToggle,
  renderNumberInput,
  renderSelect,
  api,
}: SubagentSettingsTabProps) {
  const currentSubProvider = String(getNestedValue(settings, 'subagent_provider') || '');
  const currentSubModel = String(getNestedValue(settings, 'subagent_model') || '');

  const selectedProvider = subagentProviders.find((p) => p.id === currentSubProvider);
  const availableModels = selectedProvider?.models || [];

  const personaEntries = Object.entries(subagentTypes)
    .filter(([, v]) => v.enabled)
    .sort(([a], [b]) => a.localeCompare(b));

  return (
    <div className="section">
      <h4>Default Subagent</h4>

      <div className="config-item">
        <label htmlFor="subagent-provider-select">Provider</label>
        <select
          id="subagent-provider-select"
          className="styled-select"
          value={currentSubProvider}
          onChange={(e) => updateSetting('subagent_provider', e.target.value)}
        >
          <option value="">Default (inherit from main agent)</option>
          {subagentProviders.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </div>

      <div className="config-item">
        <label htmlFor="subagent-model-select">Model</label>
        <select
          id="subagent-model-select"
          className="styled-select"
          value={currentSubModel}
          onChange={(e) => updateSetting('subagent_model', e.target.value)}
        >
          <option value="">Default (use provider&apos;s default model)</option>
          {availableModels.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </div>

      <div className="settings-section-spaced-bordered">
        <h4>Parallel Subagents</h4>
        {renderToggle('subagent_parallel_enabled', 'Enable parallel subagent execution')}

        {Boolean(getNestedValue(settings, 'subagent_parallel_enabled')) && (
          <div className="settings-help-spaced-top">
            {renderNumberInput('subagent_max_parallel', 'Maximum parallel subagents', 1, 10)}
            <div className="config-help">
              Controls how many subagents can run simultaneously. Set to 0 or disable above to run all subagents
              serially.
            </div>
          </div>
        )}
        <div className="settings-help-spaced-top">
          {renderNumberInput(
            'subagent_max_depth',
            'Maximum subagent nesting depth',
            1,
            5,
            1,
            'How many levels deep subagents may delegate to further subagents. Higher values risk runaway recursion; default is 2.',
          )}
        </div>
      </div>

      {renderSelect('default_subagent_persona', 'Default Persona', [
        'general',
        'coder',
        'refactor',
        'debugger',
        'tester',
        'reviewer',
        'researcher',
        'web_scraper',
        'orchestrator',
        'computer_user',
      ])}

      <div className="settings-section-spaced">
        <h4>Per-Persona Overrides</h4>
        <div className="config-help settings-help-spaced">
          Set a specific provider and/or model for individual personas. Empty values inherit from the default subagent
          settings above.
        </div>

        {personaEntries.length === 0 && <div className="settings-empty">No personas available</div>}

        <div className="persona-mapping-list">
          {personaEntries.map(([personaId, persona]) => {
            const isSaving = subagentSavingPersona === personaId;
            const personaProvider = persona.provider || '';
            const personaModelsForProvider = subagentProviders.find((p) => p.id === personaProvider)?.models || [];

            return (
              <div key={personaId} className="persona-mapping-row">
                <span className="persona-mapping-name" title={persona.description}>
                  {persona.name}
                </span>
                <select
                  className="styled-select persona-mapping-select"
                  value={personaProvider}
                  onChange={async (e) => {
                    setSubagentSavingPersona(personaId);
                    try {
                      await api.updateSubagentType(personaId, {
                        provider: e.target.value,
                        model: '',
                      });
                      setSubagentTypes((prev: Record<string, SubagentTypeInfo>) => ({
                        ...prev,
                        [personaId]: {
                          ...prev[personaId],
                          provider: e.target.value,
                          model: '',
                        },
                      }));
                      addNotification('success', 'Settings', `${persona.name}: provider updated`, 3000);
                    } catch (err) {
                      addNotification('error', 'Settings', `Failed to update ${persona.name}`, 5000);
                    } finally {
                      setSubagentSavingPersona(null);
                    }
                  }}
                  disabled={isSaving}
                >
                  <option value="">Default</option>
                  {subagentProviders.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
                <select
                  className="styled-select persona-mapping-select"
                  value={persona.model || ''}
                  onChange={async (e) => {
                    setSubagentSavingPersona(personaId);
                    try {
                      await api.updateSubagentType(personaId, {
                        model: e.target.value,
                      });
                      setSubagentTypes((prev: Record<string, SubagentTypeInfo>) => ({
                        ...prev,
                        [personaId]: { ...prev[personaId], model: e.target.value },
                      }));
                      addNotification('success', 'Settings', `${persona.name}: model updated`, 3000);
                    } catch (err) {
                      addNotification('error', 'Settings', `Failed to update ${persona.name}`, 5000);
                    } finally {
                      setSubagentSavingPersona(null);
                    }
                  }}
                  disabled={isSaving || personaProvider === ''}
                >
                  <option value="">Default</option>
                  {personaModelsForProvider.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
