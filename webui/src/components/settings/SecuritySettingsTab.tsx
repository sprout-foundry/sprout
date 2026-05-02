import type { FieldRenderers } from './useSettingsFieldRenderers';

interface SecuritySettingsTabProps {
  renderToggle: FieldRenderers['renderToggle'];
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderSelect: FieldRenderers['renderSelect'];
}

export default function SecuritySettingsTab({
  renderToggle,
  renderNumberInput,
  renderSelect,
}: SecuritySettingsTabProps) {
  return (
    <div className="section">
      <h4>Security</h4>
      {renderNumberInput('security_validation.threshold', 'Validation threshold (0-2)', 0, 2)}
      {renderSelect('self_review_gate_mode', 'Self-review gate', ['off', 'code', 'always'])}
      <div style={{ marginTop: 'var(--space-5)' }}>
        <h4>Git Permissions</h4>
        {renderToggle('allow_orchestrator_git_write', 'Allow orchestrator git write')}
      </div>
    </div>
  );
}
