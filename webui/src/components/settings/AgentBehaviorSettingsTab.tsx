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
