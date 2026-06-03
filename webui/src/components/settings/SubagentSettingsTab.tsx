import type { SproutSettings, ProviderOption } from '../../services/api';
import type { SubagentTypeInfo } from '../../services/api/types';
import { getNestedValue } from './settingsHelpers';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface SubagentSettingsTabProps {
  settings: SproutSettings;
  subagentProviders: ProviderOption[];
  subagentTypes: Record<string, SubagentTypeInfo>;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
  addNotification: (type: 'success' | 'error' | 'info', title: string, message: string, duration?: number) => string;
  renderToggle: FieldRenderers['renderToggle'];
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderSelect: FieldRenderers['renderSelect'];
}

export default function SubagentSettingsTab({
  settings,
  subagentProviders,
  subagentTypes,
  updateSetting,
  renderToggle,
  renderNumberInput,
  renderSelect,
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
      ])}

      <div className="settings-section-spaced">
        <h4>Personas</h4>
        <div className="config-help settings-help-spaced">
          Personas are catalog-fixed. To customize behavior, create a skill in <code>~/.config/sprout/skills/</code>.
        </div>

        {personaEntries.length === 0 && <div className="settings-empty">No personas available</div>}

        <div className="persona-mapping-list">
          {personaEntries.map(([personaId, persona]) => {
            const personaProvider = persona.provider || '';
            const personaModel = persona.model || '';
            return (
              <div key={personaId} className="persona-mapping-row">
                <span className="persona-mapping-name" title={persona.description}>
                  {persona.name}
                </span>
                <span className="persona-mapping-value">
                  {personaProvider ? personaProvider : <em>default provider</em>}
                </span>
                <span className="persona-mapping-value">
                  {personaModel ? personaModel : <em>default model</em>}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
