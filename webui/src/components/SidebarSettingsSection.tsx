import { Keyboard, Upload, Trash2 } from 'lucide-react';
import { useRef, useEffect, useState, useCallback } from 'react';
import type { ChangeEvent } from 'react';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import type { SproutSettings } from '../services/api';
import type { AgentConfigProps } from './settings/types';

// Extended agent config that adds optional role selection (matches SettingsPanel.tsx)
interface ExtendedAgentConfig extends AgentConfigProps {
  selectedRole?: string | null;
  onRoleChange?: (roleId: string | null) => void;
}
import { useLog } from '../utils/log';
import SettingsPanel from './SettingsPanel';

interface SidebarSettingsSectionProps {
  themePack: { id: string };
  availableThemePacks: { id: string; name: string }[];
  setThemePack: (id: string) => void;
  importTheme: (text: string) => { success: boolean; warnings?: string[] };
  removeTheme: (id: string) => void;
  applyPreset: (preset: string) => Promise<void>;
  autoSaveEnabled: boolean;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  formatOnSaveEnabled: boolean;
  setAutoSaveEnabled: (enabled: boolean) => void;
  setWhitespaceRenderingMode: (mode: WhitespaceRenderingMode) => void;
  setFormatOnSaveEnabled: (enabled: boolean) => void;
  settings: SproutSettings | null;
  onSettingsChanged: (settings: SproutSettings | null) => void;
  onRequestProviderSetup?: () => void;
  selectedProvider: string;
  selectedModel: string;
  selectedPersona: string;
  providers: { id: string; name: string }[];
  availableModels: string[];
  personas: { id: string; name: string }[];
  isLoadingProviders: boolean;
  isLoadingPersonas: boolean;
  isConnected: boolean;
  onProviderChange: (provider: string) => void;
  onModelChange: (model: string) => void;
  onPersonaChange: (persona: string) => void;
  selectedRole?: string | null;
  onRoleChange?: (roleId: string | null) => void;
}

export default function SidebarSettingsSection({
  themePack,
  availableThemePacks,
  setThemePack,
  importTheme,
  removeTheme,
  applyPreset,
  autoSaveEnabled,
  whitespaceRenderingMode,
  formatOnSaveEnabled,
  setAutoSaveEnabled,
  setWhitespaceRenderingMode,
  setFormatOnSaveEnabled,
  settings,
  onSettingsChanged,
  onRequestProviderSetup,
  selectedProvider,
  selectedModel,
  selectedPersona,
  providers,
  availableModels,
  personas,
  isLoadingProviders,
  isLoadingPersonas,
  isConnected,
  onProviderChange,
  onModelChange,
  onPersonaChange,
  selectedRole: initialSelectedRole,
  onRoleChange: externalOnRoleChange,
}: SidebarSettingsSectionProps): JSX.Element {
  const log = useLog();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [importError, setImportError] = useState<string | null>(null);
  const [selectedRole, setSelectedRole] = useState<string | null>(initialSelectedRole ?? null);

  // Fix #4: Sync selectedRole when the external prop changes
  useEffect(() => {
    setSelectedRole(initialSelectedRole ?? null);
  }, [initialSelectedRole]);

  const handleRoleChange = (roleId: string | null) => {
    setSelectedRole(roleId);
    if (externalOnRoleChange) {
      externalOnRoleChange(roleId);
    }
  };

  const handleHotkeyPresetChange = async (e: ChangeEvent<HTMLSelectElement>) => {
    try {
      await applyPreset(e.target.value);
    } catch (err) {
      log.error(`Failed to apply hotkey preset: ${err instanceof Error ? err.message : String(err)}`, {
        title: 'Hotkey Error',
      });
    }
  };

  const handleImportTheme = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (!file) return;
      setImportError(null);
      const reader = new FileReader();
      reader.onload = (ev) => {
        const text = ev.target?.result;
        if (typeof text !== 'string') return;
        const result = importTheme(text);
        if (!result.success) {
          setImportError(result.warnings?.join('; ') || 'Import failed');
        }
      };
      reader.onerror = () => setImportError('Failed to read file');
      reader.readAsText(file);
      // Reset input so same file can be re-imported
      e.target.value = '';
    },
    [importTheme],
  );

  /* ─── Build agent config object with explicit typing (no assertion) ─── */
  const agentConfigObj: ExtendedAgentConfig = {
    selectedProvider,
    selectedModel,
    selectedPersona,
    selectedRole,
    providers,
    availableModels,
    personas,
    isLoadingProviders,
    isLoadingPersonas,
    isConnected,
    onProviderChange,
    onModelChange,
    onPersonaChange,
    onRoleChange: handleRoleChange,
  };

  return (
    <>
      <div className="section">
        <h4>Appearance</h4>
        <div className="config-item">
          <label htmlFor="theme-select">Theme Pack:</label>
          <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
            <select
              id="theme-select"
              value={themePack.id}
              onChange={(e) => setThemePack(e.target.value)}
              className="styled-select"
              style={{ flex: 1 }}
            >
              {availableThemePacks.map((pack) => (
                <option key={pack.id} value={pack.id}>
                  {pack.name}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="config-btn"
              onClick={() => fileInputRef.current?.click()}
              title="Import VSCode theme (.json)"
              style={{
                background: 'var(--bg-tertiary)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-sm)',
                padding: '4px 8px',
                cursor: 'pointer',
                color: 'var(--text-primary)',
                display: 'flex',
                alignItems: 'center',
                flexShrink: 0,
              }}
            >
              <Upload size={14} />
            </button>
            {themePack.id.startsWith('imported-') && (
              <button
                type="button"
                className="config-btn"
                onClick={() => removeTheme(themePack.id)}
                title="Remove this imported theme"
                style={{
                  background: 'var(--color-error-bg)',
                  border: '1px solid var(--accent-error)',
                  borderRadius: 'var(--radius-sm)',
                  padding: '4px 8px',
                  cursor: 'pointer',
                  color: 'var(--accent-error)',
                  display: 'flex',
                  alignItems: 'center',
                  flexShrink: 0,
                }}
              >
                <Trash2 size={14} />
              </button>
            )}
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept=".json"
            style={{ display: 'none' }}
            onChange={handleImportTheme}
          />
          {importError && (
            <div style={{ color: 'var(--accent-error)', fontSize: '12px', marginTop: '2px' }}>{importError}</div>
          )}
        </div>
        <div className="config-item">
          <label htmlFor="hotkey-preset-select">Apply Hotkey Preset:</label>
          <select
            id="hotkey-preset-select"
            defaultValue=""
            onChange={handleHotkeyPresetChange}
            className="styled-select"
          >
            <option value="" disabled>
              Choose a preset…
            </option>
            <option value="vscode">VS Code</option>
            <option value="webstorm">WebStorm</option>
            <option value="sprout">Sprout (Legacy)</option>
          </select>
        </div>
        <div className="config-item" style={{ marginTop: 'var(--space-4, 8px)' }}>
          <button
            type="button"
            className="settings-link-btn"
            onClick={() => {
              // Dispatch event to open hotkeys config
              window.dispatchEvent(new CustomEvent('sprout:open-hotkeys-config'));
            }}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
              padding: '6px 12px',
              background: 'var(--bg-secondary, #2a2a2a)',
              border: '1px solid var(--border-color, #3c3c3c)',
              borderRadius: '4px',
              color: 'var(--text-primary, #fff)',
              cursor: 'pointer',
              fontSize: '13px',
              width: '100%',
            }}
          >
            <Keyboard size={14} />
            Edit Keyboard Shortcuts (JSON)
          </button>
        </div>
      </div>

      {/* Agent Config moved into SettingsPanel (Agent section body) */}
      <SettingsPanel
        settings={settings}
        onSettingsChanged={onSettingsChanged}
        onRequestProviderSetup={onRequestProviderSetup}
        editorPreferences={{ autoSaveEnabled, whitespaceRenderingMode, formatOnSaveEnabled }}
        onEditorPreferenceChanged={(key, value) => {
          if (key === 'autoSaveEnabled') setAutoSaveEnabled(value as boolean);
          if (key === 'whitespaceRenderingMode') setWhitespaceRenderingMode(value as WhitespaceRenderingMode);
          if (key === 'formatOnSaveEnabled') setFormatOnSaveEnabled(value as boolean);
        }}
        agentConfig={agentConfigObj}
      />
    </>
  );
}
