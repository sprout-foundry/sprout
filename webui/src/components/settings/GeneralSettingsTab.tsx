import type { EditorPreferences } from './types';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface GeneralSettingsTabProps {
  editorPreferences: EditorPreferences | null | undefined;
  onEditorPreferenceChanged?: (key: string, value: unknown) => void;
  renderToggle: FieldRenderers['renderToggle'];
  renderSelect: FieldRenderers['renderSelect'];
  renderTextareaInput: FieldRenderers['renderTextareaInput'];
}

export default function GeneralSettingsTab({
  editorPreferences,
  onEditorPreferenceChanged,
  renderToggle,
  renderSelect,
  renderTextareaInput,
}: GeneralSettingsTabProps) {
  return (
    <>
      {editorPreferences && onEditorPreferenceChanged && (
        <div className="section">
          <h4>Editor</h4>
          <label className="styled-toggle">
            <input
              type="checkbox"
              checked={!!editorPreferences.autoSaveEnabled}
              onChange={() => onEditorPreferenceChanged('autoSaveEnabled', !editorPreferences.autoSaveEnabled)}
            />
            <span className="toggle-track" />
            <span className="toggle-label">Auto-save files (every 30s)</span>
          </label>
          <label className="styled-toggle">
            <input
              type="checkbox"
              checked={!!editorPreferences.formatOnSaveEnabled}
              onChange={() => onEditorPreferenceChanged('formatOnSaveEnabled', !editorPreferences.formatOnSaveEnabled)}
            />
            <span className="toggle-track" />
            <span className="toggle-label">Format on Save</span>
          </label>
          <span style={{ fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)', lineHeight: 1.3, marginTop: 2 }}>Format files with Prettier before saving</span>
          <div className="config-item">
            <label htmlFor="whitespace-rendering-select">Render whitespace</label>
            <select
              id="whitespace-rendering-select"
              className="styled-select"
              value={editorPreferences.whitespaceRenderingMode}
              onChange={(e) => onEditorPreferenceChanged('whitespaceRenderingMode', e.target.value)}
            >
              <option value="none">None</option>
              <option value="boundary">Boundary (trailing only)</option>
              <option value="all">All</option>
            </select>
          </div>
        </div>
      )}
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
    </>
  );
}
