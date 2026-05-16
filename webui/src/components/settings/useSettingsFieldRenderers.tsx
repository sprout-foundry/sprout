import { getNestedValue } from './settingsHelpers';
import type { SproutSettings } from '../../services/api';

interface FieldRenderersParams {
  displaySettingsRef: React.MutableRefObject<SproutSettings | null>;
  settings: SproutSettings | null;
  textDrafts: Record<string, string>;
  setTextDrafts: (v: Record<string, string> | ((prev: Record<string, string>) => Record<string, string>)) => void;
  textSaveTimersRef: React.MutableRefObject<Record<string, ReturnType<typeof setTimeout>>>;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
  savingKey: string | null;
  provenanceSources: Record<string, string>;
  configViewLayer: 'session' | 'workspace' | 'global';
}

export interface FieldRenderers {
  renderProvenanceBadge: (settingKey: string) => JSX.Element | null;
  renderToggle: (settingKey: string, label: string) => JSX.Element | null;
  renderSelect: (settingKey: string, label: string, options: string[]) => JSX.Element | null;
  renderNumberInput: (
    settingKey: string,
    label: string,
    min?: number,
    max?: number,
    step?: number,
  ) => JSX.Element | null;
  renderTextInput: (settingKey: string, label: string, placeholder?: string) => JSX.Element | null;
  renderTextareaInput: (
    settingKey: string,
    label: string,
    placeholder?: string,
    rows?: number,
    helpText?: string,
  ) => JSX.Element | null;
  renderSaving: () => JSX.Element | null;
}

export function useSettingsFieldRenderers(params: FieldRenderersParams): FieldRenderers {
  const {
    displaySettingsRef,
    settings,
    textDrafts,
    setTextDrafts,
    textSaveTimersRef,
    updateSetting,
    savingKey,
    provenanceSources,
    configViewLayer,
  } = params;

  const renderProvenanceBadge = (settingKey: string) => {
    const source = provenanceSources[settingKey];
    if (!source || configViewLayer !== 'session') return null;
    const colors: Record<string, string> = {
      session: 'var(--accent-primary, #4a9eff)',
      workspace: 'var(--accent-warning, #f0ad4e)',
      global: 'var(--text-tertiary, #888)',
    };
    return (
      <span
        title={`This value comes from your ${source} configuration`}
        style={{
          fontSize: 9,
          padding: '1px 4px',
          borderRadius: 3,
          marginLeft: 6,
          backgroundColor: `color-mix(in srgb, ${colors[source] || colors.global} 15%, transparent)`,
          color: colors[source] || colors.global,
          fontWeight: 600,
          textTransform: 'uppercase',
          letterSpacing: 0.5,
          verticalAlign: 'middle',
        }}
      >
        {source}
      </span>
    );
  };

  const renderToggle = (settingKey: string, label: string) => {
    const current = displaySettingsRef.current ?? settings;
    if (!current) return null;
    const checked = !!getNestedValue(current, settingKey);
    return (
      <label className="styled-toggle">
        <input type="checkbox" checked={checked} onChange={() => updateSetting(settingKey, !checked)} />
        <span className="toggle-track" />
        <span className="toggle-label">
          {label}
          {renderProvenanceBadge(settingKey)}
        </span>
      </label>
    );
  };

  const renderSelect = (settingKey: string, label: string, options: string[]) => {
    const current = displaySettingsRef.current ?? settings;
    if (!current) return null;
    const value = String(getNestedValue(current, settingKey) || '');
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>
          {label}
          {renderProvenanceBadge(settingKey)}
        </label>
        <select
          id={`setting-${settingKey}`}
          value={value}
          onChange={(e) => updateSetting(settingKey, e.target.value)}
          className="styled-select"
        >
          {options.map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      </div>
    );
  };

  const renderNumberInput = (settingKey: string, label: string, min?: number, max?: number, step = 1) => {
    const current = displaySettingsRef.current ?? settings;
    if (!current) return null;
    const value = getNestedValue(current, settingKey);
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>
          {label}
          {renderProvenanceBadge(settingKey)}
        </label>
        <input
          id={`setting-${settingKey}`}
          type="number"
          className="styled-input config-row-input"
          value={String(value ?? '')}
          min={min}
          max={max}
          step={step}
          onChange={(e) => {
            const v = e.target.value === '' ? 0 : Number(e.target.value);
            updateSetting(settingKey, v);
          }}
        />
      </div>
    );
  };

  const renderTextInput = (settingKey: string, label: string, placeholder?: string) => {
    const current = displaySettingsRef.current ?? settings;
    if (!current) return null;
    const persistedValue = String(getNestedValue(current, settingKey) || '');
    const value = textDrafts[settingKey] ?? persistedValue;
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>
          {label}
          {renderProvenanceBadge(settingKey)}
        </label>
        <input
          id={`setting-${settingKey}`}
          type="text"
          className="styled-input"
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            const nextValue = e.target.value;
            setTextDrafts((prev) => ({ ...prev, [settingKey]: nextValue }));

            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
            }

            textSaveTimersRef.current[settingKey] = setTimeout(() => {
              delete textSaveTimersRef.current[settingKey];
              void updateSetting(settingKey, nextValue);
            }, 250);
          }}
          onBlur={() => {
            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
              delete textSaveTimersRef.current[settingKey];
            }

            const draftValue = textDrafts[settingKey];
            if (draftValue !== undefined && draftValue !== persistedValue) {
              void updateSetting(settingKey, draftValue);
            }
          }}
        />
      </div>
    );
  };

  const renderTextareaInput = (
    settingKey: string,
    label: string,
    placeholder?: string,
    rows = 10,
    helpText?: string,
  ) => {
    const current = displaySettingsRef.current ?? settings;
    if (!current) return null;
    const persistedValue = String(getNestedValue(current, settingKey) || '');
    const value = textDrafts[settingKey] ?? persistedValue;
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>
          {label}
          {renderProvenanceBadge(settingKey)}
        </label>
        <textarea
          id={`setting-${settingKey}`}
          className="styled-input styled-textarea"
          value={value}
          rows={rows}
          placeholder={placeholder}
          onChange={(e) => {
            const nextValue = e.target.value;
            setTextDrafts((prev) => ({ ...prev, [settingKey]: nextValue }));

            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
            }

            textSaveTimersRef.current[settingKey] = setTimeout(() => {
              delete textSaveTimersRef.current[settingKey];
              void updateSetting(settingKey, nextValue);
            }, 400);
          }}
          onBlur={() => {
            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
              delete textSaveTimersRef.current[settingKey];
            }

            const draftValue = textDrafts[settingKey];
            if (draftValue !== undefined && draftValue !== persistedValue) {
              void updateSetting(settingKey, draftValue);
            }
          }}
        />
        {helpText && <div className="config-help">{helpText}</div>}
      </div>
    );
  };

  const renderSaving = () => {
    if (!savingKey) return null;
    return (
      <span className="settings-saving">
        <span className="saving-dot" />
        Saving…
      </span>
    );
  };

  return {
    renderProvenanceBadge,
    renderToggle,
    renderSelect,
    renderNumberInput,
    renderTextInput,
    renderTextareaInput,
    renderSaving,
  };
}
