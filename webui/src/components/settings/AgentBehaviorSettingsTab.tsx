import type { SproutSettings } from '../../services/api';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface AgentBehaviorSettingsTabProps {
  settings: SproutSettings | null;
  renderSelect: FieldRenderers['renderSelect'];
  renderToggle: FieldRenderers['renderToggle'];
  renderTextareaInput: FieldRenderers['renderTextareaInput'];
}

const BUILT_IN_RISK_PROFILES = ['', 'readonly', 'cautious', 'default', 'permissive', 'unrestricted'];

/**
 * Agent behavior settings — reasoning, thinking, history, system prompt.
 * Session-scoped (Agent section).
 */
export default function AgentBehaviorSettingsTab({
  settings,
  renderSelect,
  renderToggle,
  renderTextareaInput,
}: AgentBehaviorSettingsTabProps) {
  const customProfiles = settings && (settings as unknown as { risk_profiles?: Record<string, unknown> }).risk_profiles
    ? Object.keys((settings as unknown as { risk_profiles?: Record<string, unknown> }).risk_profiles ?? {})
        .filter((name) => !BUILT_IN_RISK_PROFILES.includes(name))
        .sort()
    : [];
  const riskProfileOptions = [...BUILT_IN_RISK_PROFILES, ...customProfiles];

  return (
    <div className="section">
      <h4>Behavior</h4>
      {renderSelect('reasoning_effort', 'Reasoning effort', ['low', 'medium', 'high'])}
      {renderToggle('disable_thinking', 'Disable thinking for thinking models')}
      {renderToggle('skip_prompt', 'Skip confirmation prompt')}
      {renderToggle('enable_pre_write_validation', 'Pre-write validation')}
      {renderSelect('history_scope', 'History scope', ['session', 'project', 'global'])}
      {renderSelect(
        'ea_mode',
        'Executive Assistant mode',
        ['', 'interactive', 'queue'],
        'Controls how the EA persona starts. interactive = wait for prompts (default); queue = autonomous task processing. Empty inherits the default.',
      )}
      {/*
        SP-058: risk profile selector. Empty string ("") means "use
        the built-in default", which lets the user clear an override
        without having to type a profile name. Built-ins mirror
        configuration.IsValidRiskProfile in pkg/configuration/config.go;
        any user-defined names from config.risk_profiles are appended
        below so they appear as selectable options here too.
      */}
      {renderSelect(
        'risk_profile',
        'Risk profile',
        riskProfileOptions,
        'Shell-command gating: readonly (blocks writes) → cautious (prompts) → default → permissive → unrestricted. Persona-defined rules (e.g. EA) still win. See docs/SECURITY.md#risk-profiles.',
      )}
      {renderTextareaInput(
        'system_prompt_text',
        'System prompt',
        'Leave blank to use the embedded default system prompt.',
        12,
        'Applies to the main agent. Leave blank to use the built-in default prompt.',
      )}
    </div>
  );
}
