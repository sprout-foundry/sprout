import { SkeletonText } from '@sprout/ui';
import { Keyboard, Upload, Trash2 } from 'lucide-react';
import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
import type { ChangeEvent } from 'react';
import { isCloud } from '../config/mode';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { ApiService } from '../services/api';
import type { SproutSettings } from '../services/api';
import { useLog } from '../utils/log';
import CredentialsSettingsTab from './CredentialsSettingsTab';
import type { AgentConfigProps } from './settings/types';

// SettingsPanel pulls in CredentialsSettingsTab, ProviderSettingsTab,
// onnxEmbeddingProvider, and a few other heavy dependencies. It only
// renders when the sidebar settings section is open, so split it into
// its own chunk; the bundle no longer pays for it on initial load.
const SettingsPanel = lazy(() => import('./SettingsPanel'));

/**
 * Cloud-mode provider/model selection (BYOK). In cloud builds the full
 * SettingsPanel (local mode) is tree-shaken away, so the status-bar
 * chip and this section are the only provider/model surfaces. Lives
 * directly above the API Key section — key entry and model selection
 * are one workflow. The `provider-select` id is the focus target for
 * the status-bar's "provider unknown" fallback routing.
 *
 * NOTE (platform): a second, keyless "included provider" path — where
 * the Sprout cloud serves inference — is planned but depends on the
 * cloud platform; only BYOK is wired here today.
 */
function CloudProviderModelSection({
  selectedProvider,
  selectedModel,
  providers,
  availableModels,
  isLoadingProviders,
  isConnected,
  onProviderChange,
  onModelChange,
}: {
  selectedProvider: string;
  selectedModel: string;
  providers: { id: string; name: string }[];
  availableModels: string[];
  isLoadingProviders: boolean;
  isConnected: boolean;
  onProviderChange: (provider: string) => void;
  onModelChange: (model: string) => void;
}): JSX.Element {
  const [models, setModels] = useState<string[]>(availableModels);
  const [isLoadingModels, setIsLoadingModels] = useState(false);
  const [emptyReason, setEmptyReason] = useState<string | null>(null);
  const [fetchFailed, setFetchFailed] = useState(false);

  // Human copy for each machine-readable empty-reason returned by the
  // environment shell (Studio bridge). Unrecognized values fall through
  // to a generic line rather than silence.
  const emptyReasonCopy: Record<string, string> = {
    'no-key': 'No API key stored for this provider yet. Add it in the API Key section below, then tap Retry.',
    'no-endpoint': 'This provider needs a server address that is only known while it is active.',
    'unknown-provider': 'This provider is not available in this environment.',
    error: 'The provider request failed. Check the API key and network, then tap Retry.',
    unavailable: 'The provider could not be reached. Check your connection, then tap Retry.',
  };

  // Available models for the active provider. The bootstrap-loaded
  // catalog may already carry them; otherwise fetch from the provider
  // endpoint (natively served by the shell bridge in Studio builds).
  const fetchModelList = useCallback(
    (provider: string) => {
      if (!isConnected) return;
      let cancelled = false;
      setIsLoadingModels(true);
      setEmptyReason(null);
      setFetchFailed(false);
      ApiService.getInstance()
        .getProviderModels(provider)
        .then((response) => {
          if (cancelled) return;
          // getProviderModels returns either rich ProviderModel objects
          // or legacy plain strings depending on the backend; accept both.
          const list = (response.models as unknown[])
            .map((m) =>
              typeof m === 'string'
                ? m
                : ((m as { id?: string; name?: string }).id ?? (m as { name?: string }).name ?? ''),
            )
            .filter((m): m is string => !!m);
          setModels(list);
          // Explain empty results instead of rendering a bare empty state.
          if (list.length === 0) setEmptyReason(response.reason ?? null);
        })
        .catch(() => {
          // HTTP-level failure — surface it rather than swallow it.
          if (!cancelled) setFetchFailed(true);
        })
        .finally(() => {
          if (!cancelled) setIsLoadingModels(false);
        });
      return () => {
        cancelled = true;
      };
    },
    [isConnected],
  );

  // Refetch when the provider changes; also adopts the catalog when
  // availableModels populates later (or clears fetch state).
  useEffect(() => {
    if (availableModels.length > 0) {
      setModels(availableModels);
      setEmptyReason(null);
      setFetchFailed(false);
      return;
    }
    if (!selectedProvider) {
      setModels([]);
      setEmptyReason(null);
      setFetchFailed(false);
      return;
    }
    return fetchModelList(selectedProvider);
  }, [selectedProvider, availableModels, fetchModelList]);

  const handleProviderChange = useCallback(
    (provider: string) => {
      setModels([]);
      onProviderChange(provider);
    },
    [onProviderChange],
  );

  const modelValue = models.includes(selectedModel) ? selectedModel : '';

  return (
    <>
      <div className="config-item">
        <label htmlFor="provider-select">Provider:</label>
        <div className="theme-picker-row">
          <select
            id="provider-select"
            value={selectedProvider || ''}
            onChange={(e) => handleProviderChange(e.target.value)}
            className="styled-select theme-picker-select"
          >
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
        {!selectedProvider && !isLoadingProviders && (
          <div className="theme-picker-error">
            No provider selected. Add an API key below, then pick a provider here.
          </div>
        )}
      </div>
      <div className="config-item">
        <label htmlFor="model-select">Model:</label>
        <div className="theme-picker-row">
          <select
            id="model-select"
            value={modelValue}
            onChange={(e) => onModelChange(e.target.value)}
            className="styled-select theme-picker-select"
            disabled={!selectedProvider || models.length === 0}
          >
            {models.length === 0 ? (
              <option value="">{isLoadingModels ? 'Loading models…' : 'No models loaded'}</option>
            ) : (
              models.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))
            )}
          </select>
        </div>
        {!isLoadingModels && models.length === 0 && selectedProvider && (fetchFailed || emptyReason) && (
          <div className="theme-picker-error">
            <span>
              {fetchFailed
                ? emptyReasonCopy.error
                : (emptyReason && emptyReasonCopy[emptyReason]) || 'No models available for this provider.'}
            </span>
            <button
              type="button"
              className="settings-link-btn"
              onClick={() => fetchModelList(selectedProvider)}
            >
              Retry
            </button>
          </div>
        )}
      </div>
    </>
  );
}

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
        <>
          <div className="section">
            <h4>Provider &amp; Model</h4>
            <CloudProviderModelSection
              selectedProvider={selectedProvider}
              selectedModel={selectedModel}
              providers={providers}
              availableModels={availableModels}
              isLoadingProviders={isLoadingProviders}
              isConnected={isConnected}
              onProviderChange={onProviderChange}
              onModelChange={onModelChange}
            />
          </div>
          <div className="section">
            <h4>API Key</h4>
            <p className="settings-section-desc">
              Add your LLM provider API key to enable AI chat in the browser. Your key is encrypted and stored securely on
              the server.
            </p>
            <CredentialsSettingsTab />
          </div>
        </>
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
