import type { EditorPreferences } from './types';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface GeneralSettingsTabProps {
  editorPreferences: EditorPreferences | null | undefined;
  onEditorPreferenceChanged?: (key: string, value: unknown) => void;
  /** Shared toggle renderer for local component state (editor prefs).
   *  Optional for backwards compatibility — falls back to inline markup. */
  renderLocalToggle?: FieldRenderers['renderLocalToggle'];
}

/**
 * Editor display preferences — auto-save, format-on-save, whitespace rendering.
 * Runtime-scoped (Editor section).
 */
export default function GeneralSettingsTab({
  editorPreferences,
  onEditorPreferenceChanged,
  renderLocalToggle,
}: GeneralSettingsTabProps) {
  if (!editorPreferences || !onEditorPreferenceChanged) {
    return <div className="settings-empty">Editor preferences not available</div>;
  }

  // Fallback for callers that haven't wired renderLocalToggle yet —
  // identical markup so the visual treatment stays consistent across
  // both paths.
  const toggle =
    renderLocalToggle ??
    ((checked: boolean, label: string, onChange: (next: boolean) => void) => (
      <label className="styled-toggle">
        <input type="checkbox" checked={checked} onChange={() => onChange(!checked)} />
        <span className="toggle-track" />
        <span className="toggle-label">{label}</span>
      </label>
    ));

  return (
    <div className="section">
      <h4>Display</h4>
      {toggle(!!editorPreferences.autoSaveEnabled, 'Auto-save files (every 30s)', (v) =>
        onEditorPreferenceChanged('autoSaveEnabled', v),
      )}
      {toggle(
        !!editorPreferences.formatOnSaveEnabled,
        'Format on Save',
        (v) => onEditorPreferenceChanged('formatOnSaveEnabled', v),
        'Format files with Prettier before saving',
      )}
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
  );
}
