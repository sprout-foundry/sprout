import type { FieldRenderers } from './useSettingsFieldRenderers';

interface AgentBehaviorSettingsTabProps {
  renderSelect: FieldRenderers['renderSelect'];
  renderToggle: FieldRenderers['renderToggle'];
  renderTextareaInput: FieldRenderers['renderTextareaInput'];
}

/**
 * Agent behavior settings — reasoning, thinking, history, system prompt.
 * Session-scoped (Agent section).
 */
export default function AgentBehaviorSettingsTab({
  renderSelect,
  renderToggle,
  renderTextareaInput,
}: AgentBehaviorSettingsTabProps) {
  return (
    <div className="section">
      <h4>Behavior</h4>
      {renderSelect('reasoning_effort', 'Reasoning effort', ['low', 'medium', 'high'])}
      {renderToggle('disable_thinking', 'Disable thinking for thinking models')}
      {renderToggle('skip_prompt', 'Skip confirmation prompt')}
      {renderToggle('enable_pre_write_validation', 'Pre-write validation')}
      {renderSelect('history_scope', 'History scope', ['session', 'project', 'global'])}
      {/*
        SP-058: risk profile selector. Empty string ("") means "use
        the built-in default", which lets the user clear an override
        without having to type a profile name. The list mirrors
        configuration.IsValidRiskProfile in pkg/configuration/config.go;
        keep them in sync if profiles are added. Users defining
        custom profiles via config.risk_profiles can pick those by
        editing config.json directly — they won't appear here.
      */}
      {renderSelect(
        'risk_profile',
        'Risk profile',
        ['', 'readonly', 'cautious', 'default', 'permissive', 'unrestricted'],
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
