import { SkeletonText } from '@sprout/ui';
import { Keyboard, Upload, Trash2 } from 'lucide-react';
import { Suspense, lazy, useRef, useState, useCallback } from 'react';
import type { ChangeEvent } from 'react';
import { isCloud } from '../config/mode';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import type { SproutSettings } from '../services/api';
import { useLog } from '../utils/log';
import CredentialsSettingsTab from './CredentialsSettingsTab';
import type { AgentConfigProps } from './settings/types';

// SettingsPanel pulls in CredentialsSettingsTab, ProviderSettingsTab,
// onnxEmbeddingProvider, and a few other heavy dependencies. It only
// renders when the sidebar settings section is open, so split it into
// its own chunk; the bundle no longer pays for it on initial load.
const SettingsPanel = lazy(() => import('./SettingsPanel'));

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
}: SidebarSettingsSectionProps): JSX.Element {
  const log = useLog();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [importError, setImportError] = useState<string | null>(null);

  const handleHotkeyPresetChange = async (e: ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value;
    if (!value) return;
    const labels: Record<string, string> = {
      vscode: 'VS Code',
      webstorm: 'WebStorm',
      sprout: 'Sprout (Legacy)',
    };
    try {
      await applyPreset(value);
      log.success(`Hotkey preset applied: ${labels[value] ?? value}`, {
        title: 'Hotkeys updated',
        duration: 3000,
      });
      // Reset the select back to the placeholder so the user can re-apply.
      e.target.value = '';
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
  const agentConfigObj: AgentConfigProps = {
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
  };

  return (
    <>
      <div className="section">
        <h4>Appearance</h4>
        <div className="config-item">
          <label htmlFor="theme-select">Theme Pack:</label>
          <div className="theme-picker-row">
            <select
              id="theme-select"
              value={themePack.id}
              onChange={(e) => setThemePack(e.target.value)}
              className="styled-select theme-picker-select"
              data-testid="theme-toggle"
            >
              {availableThemePacks.map((pack) => (
                <option key={pack.id} value={pack.id}>
                  {pack.name}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="theme-picker-btn"
              onClick={() => fileInputRef.current?.click()}
              title="Import VSCode theme (.json)"
              aria-label="Import VSCode theme"
            >
              <Upload size={14} />
            </button>
            {themePack.id.startsWith('imported-') && (
              <button
                type="button"
                className="theme-picker-btn theme-picker-btn--danger"
                onClick={() => removeTheme(themePack.id)}
                title="Remove this imported theme"
                aria-label="Remove imported theme"
              >
                <Trash2 size={14} />
              </button>
            )}
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept=".json"
            className="theme-picker-file-input"
            onChange={handleImportTheme}
          />
          {importError && <div className="theme-picker-error">{importError}</div>}
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
        <div className="config-item settings-help-spaced-top">
          <button
            type="button"
            className="settings-link-btn settings-link-btn--hotkeys"
            onClick={() => {
              // Dispatch a dedicated event so it doesn't trigger the keyboard-shortcuts modal.
              window.dispatchEvent(new CustomEvent('sprout:open-hotkeys-json'));
            }}
          >
            <Keyboard size={14} />
            Edit Keyboard Shortcuts (JSON)
          </button>
        </div>
      </div>

      {/* ─── Cloud mode: simplified settings ──────────────────── */}
      {isCloud ? (
        <div className="section">
          <h4>API Key</h4>
          <p className="settings-section-desc">
            Add your LLM provider API key to enable AI chat in the browser. Your key is encrypted and stored securely on
            the server.
          </p>
          <CredentialsSettingsTab />
        </div>
      ) : (
        <>
          {/* Agent Config moved into SettingsPanel (Agent section body) */}
          <Suspense fallback={<SkeletonText lines={6} />}>
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
          </Suspense>
        </>
      )}
    </>
  );
}
