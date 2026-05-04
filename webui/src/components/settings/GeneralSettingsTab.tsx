import type { EditorPreferences } from './types';

interface GeneralSettingsTabProps {
  editorPreferences: EditorPreferences | null | undefined;
  onEditorPreferenceChanged?: (key: string, value: unknown) => void;
}

/**
 * Editor display preferences — auto-save, format-on-save, whitespace rendering.
 * Runtime-scoped (Editor section).
 */
export default function GeneralSettingsTab({
  editorPreferences,
  onEditorPreferenceChanged,
}: GeneralSettingsTabProps) {
  if (!editorPreferences || !onEditorPreferenceChanged) {
    return <div className="settings-empty">Editor preferences not available</div>;
  }

  return (
    <div className="section">
      <h4>Display</h4>
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
      <span style={{ fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)', lineHeight: 1.3, marginTop: 2 }}>
        Format files with Prettier before saving
      </span>
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
