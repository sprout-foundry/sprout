import { useState, useRef, useEffect, useCallback, type ReactElement } from 'react';
import { X } from 'lucide-react';
import type { OnboardingState } from '../types/app';
import type { OnboardingProviderOption } from '../services/api';
import type { WindowsOnboardingGuidance } from '../hooks/useOnboarding';

export interface OnboardingDialogProps {
  onboarding: OnboardingState;
  selectedProvider: OnboardingProviderOption | null;
  recommendedProviders: OnboardingProviderOption[];
  advancedProviders: OnboardingProviderOption[];
  windowsGuidance: WindowsOnboardingGuidance | null;
  onProviderChange: (providerID: string) => void;
  /** Callback: complete onboarding (parent should bake in any state updater). */
  onComplete: () => Promise<void>;
  /** Callback: skip onboarding and use as editor-only mode */
  onSkip: () => Promise<void>;
  onRefresh: () => Promise<void>;
  onInstallWsl: () => Promise<void>;
  onInstallGitBash: () => Promise<void>;
  /** Update arbitrary onboarding state fields (partial merge). */
  updateOnboarding: (patch: Partial<OnboardingState> | ((prev: OnboardingState) => OnboardingState)) => void;
}

function OnboardingDialog({
  onboarding,
  selectedProvider,
  recommendedProviders,
  advancedProviders,
  windowsGuidance,
  onProviderChange,
  onComplete,
  onSkip,
  onRefresh,
  onInstallWsl,
  onInstallGitBash,
  updateOnboarding,
}: OnboardingDialogProps): ReactElement | null {
  // Model combobox state
  const [modelListOpen, setModelListOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const comboboxRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (comboboxRef.current && !comboboxRef.current.contains(event.target as Node)) {
        setModelListOpen(false);
        setHighlightedIndex(-1);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Close dropdown on Escape key
  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLInputElement>) => {
    const models = selectedProvider?.models || [];
    const recommendedModel = selectedProvider?.recommended_model;

    // Sort models so recommended model comes first
    const sortedModels = [...models].sort((a, b) => {
      if (a === recommendedModel) return -1;
      if (b === recommendedModel) return 1;
      return a.localeCompare(b);
    });

    // Filter models based on input value
    const filterText = e.currentTarget.value.toLowerCase();
    const filteredModels = sortedModels.filter((model) =>
      model.toLowerCase().includes(filterText)
    );

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setModelListOpen(true);
        setHighlightedIndex((prev) =>
          prev < filteredModels.length - 1 ? prev + 1 : prev
        );
        break;
      case 'ArrowUp':
        e.preventDefault();
        setModelListOpen(true);
        setHighlightedIndex((prev) => (prev > 0 ? prev - 1 : -1));
        break;
      case 'Enter':
        e.preventDefault();
        if (highlightedIndex >= 0 && filteredModels[highlightedIndex]) {
          updateOnboarding((prev) => ({
            ...prev,
            model: filteredModels[highlightedIndex],
            error: null,
          }));
          setModelListOpen(false);
          setHighlightedIndex(-1);
        }
        break;
      case 'Escape':
        setModelListOpen(false);
        setHighlightedIndex(-1);
        break;
    }
  }, [selectedProvider, highlightedIndex, updateOnboarding]);

  // Select a model from the dropdown
  const selectModel = useCallback(
    (modelName: string) => {
      updateOnboarding((prev) => ({ ...prev, model: modelName, error: null }));
      setModelListOpen(false);
      setHighlightedIndex(-1);
      inputRef.current?.blur();
    },
    [updateOnboarding]
  );

  // Get filtered and sorted models for display
  const getDisplayModels = useCallback(() => {
    const models = selectedProvider?.models || [];
    const recommendedModel = selectedProvider?.recommended_model;

    // Sort models so recommended model comes first
    const sortedModels = [...models].sort((a, b) => {
      if (a === recommendedModel) return -1;
      if (b === recommendedModel) return 1;
      return a.localeCompare(b);
    });

    // Filter based on current input value
    const filterText = onboarding.model.toLowerCase();
    return sortedModels.filter((model) =>
      model.toLowerCase().includes(filterText)
    );
  }, [selectedProvider, onboarding.model]);

  if (!onboarding.open) {
    return null;
  }

  // Compute display models for the combobox
  const displayModels = (selectedProvider?.models || []).length > 0
    ? getDisplayModels()
    : null;

  return (
    <div className="onboarding-overlay" role="dialog" aria-modal="true" aria-label={onboarding.isReonboarding ? 'Change provider' : 'Set up ledit'}>
      <div className="onboarding-card">
        {onboarding.isReonboarding && (
          <button
            type="button"
            className="onboarding-card-close"
            onClick={() => updateOnboarding((prev) => ({ ...prev, open: false }))}
            disabled={onboarding.submitting || onboarding.checking || onboarding.validationSuccess}
            aria-label="Close"
          >
            <X size={18} />
          </button>
        )}
        <h2>{onboarding.isReonboarding ? 'Change Provider' : 'Set Up Ledit'}</h2>
        <p>
          {onboarding.isReonboarding
            ? 'Choose a new provider and model, or update your API key.'
            : (onboarding.reason === 'missing_provider_credential'
              ? 'The selected provider is missing credentials.'
              : 'Choose a provider and model to get started.')}
        </p>

        {windowsGuidance && (
          <div className={`onboarding-platform-panel ${windowsGuidance.tone}`}>
            <div className="onboarding-platform-title">{windowsGuidance.title}</div>
            <div className="onboarding-platform-body">{windowsGuidance.body}</div>
            {onboarding.environment?.backend_mode === 'wsl' && onboarding.environment.active_distro && (
              <div className="onboarding-wsl-distro">
                Running in WSL distro: <strong>{onboarding.environment.active_distro}</strong>
                {onboarding.environment.wsl_distros?.length > 1 && (
                  <span className="onboarding-wsl-distro-hint">
                    {' '}(other available: {onboarding.environment.wsl_distros.filter((d) => d !== onboarding.environment!.active_distro).join(', ')})
                  </span>
                )}
              </div>
            )}
            <ul className="onboarding-platform-list">
              {windowsGuidance.checklist.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
            <div className="onboarding-platform-actions">
              {windowsGuidance.canInstallWsl && (
                <button
                  type="button"
                  className="onboarding-platform-btn"
                  onClick={onInstallWsl}
                  disabled={onboarding.submitting || onboarding.checking}
                >
                  Install WSL
                </button>
              )}
              {windowsGuidance.canInstallGitBash && (
                <button
                  type="button"
                  className="onboarding-platform-btn"
                  onClick={onInstallGitBash}
                  disabled={onboarding.submitting || onboarding.checking}
                >
                  Install Git Bash
                </button>
              )}
            </div>
            <div className="onboarding-provider-links onboarding-platform-links">
              <a href="https://learn.microsoft.com/windows/wsl/install" target="_blank" rel="noreferrer">
                Install WSL
              </a>
              <a href="https://gitforwindows.org/" target="_blank" rel="noreferrer">
                Install Git Bash
              </a>
            </div>
          </div>
        )}

        <div className="onboarding-step-title">1. Choose an inference provider</div>
        <div className="onboarding-provider-grid">
          {recommendedProviders.map((providerOption) => (
            <button
              key={providerOption.id}
              type="button"
              className={`onboarding-provider-card ${onboarding.provider === providerOption.id ? 'selected' : ''} ${providerOption.has_credential ? 'configured' : ''}`}
              onClick={() => onProviderChange(providerOption.id)}
              disabled={onboarding.submitting || onboarding.checking}
            >
              <span className="onboarding-provider-name">{providerOption.name}</span>
              {providerOption.has_credential && (
                <span className="onboarding-configured-badge" title="Credentials already configured" aria-label="Credentials already configured">
                  ✓ Configured
                </span>
              )}
            </button>
          ))}
        </div>

        {advancedProviders.length > 0 && (
          <>
            <button
              type="button"
              className="onboarding-toggle-btn"
              onClick={() => updateOnboarding((prev) => ({ ...prev, showAllProviders: !prev.showAllProviders }))}
              disabled={onboarding.submitting || onboarding.checking}
            >
              {onboarding.showAllProviders ? 'Hide other providers' : 'Show other providers'}
            </button>

            {onboarding.showAllProviders && (
              <>
                <label htmlFor="onboarding-provider">Other Providers</label>
                <select
                  id="onboarding-provider"
                  value={onboarding.provider}
                  onChange={(e) => onProviderChange(e.target.value)}
                  disabled={onboarding.submitting || onboarding.checking}
                >
                  {onboarding.providers.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                      {p.has_credential ? ' (configured)' : ''}
                      {!p.has_credential && p.requires_api_key ? ' (API key required)' : ''}
                    </option>
                  ))}
                </select>
              </>
            )}
          </>
        )}

        {selectedProvider && (
          <div className="onboarding-provider-summary">
            <div className="onboarding-provider-summary-title">{selectedProvider.name}</div>
            <div className="onboarding-provider-summary-body">
              {selectedProvider.setup_hint || selectedProvider.description}
            </div>
            <div className="onboarding-provider-links">
              {selectedProvider.docs_url && (
                <a href={selectedProvider.docs_url} target="_blank" rel="noreferrer">
                  Docs
                </a>
              )}
              {selectedProvider.signup_url && (
                <a href={selectedProvider.signup_url} target="_blank" rel="noreferrer">
                  Get API access
                </a>
              )}
            </div>
          </div>
        )}

        {onboarding.initialModelSet && selectedProvider?.recommended_model && onboarding.model !== selectedProvider.recommended_model && (
          <div className="onboarding-note">
            Recommended model: <strong>{selectedProvider.recommended_model}</strong>
            {selectedProvider.recommended_model_why ? ` — ${selectedProvider.recommended_model_why}` : ''}
            {' '}
            <button
              type="button"
              className="onboarding-inline-action"
              onClick={() =>
                updateOnboarding((prev) => ({
                  ...prev,
                  model: selectedProvider.recommended_model,
                  error: null,
                }))
              }
              disabled={onboarding.submitting || onboarding.checking}
            >
              Use recommended model
            </button>
          </div>
        )}

        <div className="onboarding-step-title">2. Choose a model</div>
        <label htmlFor="onboarding-model">Model</label>
        <div className="onboarding-model-combobox" ref={comboboxRef}>
          <input
            ref={inputRef}
            id="onboarding-model"
            value={onboarding.model}
            onChange={(e) => {
              updateOnboarding((prev) => ({ ...prev, model: e.target.value }));
              setHighlightedIndex(-1);
            }}
            onFocus={() => setModelListOpen(true)}
            onKeyDown={handleKeyDown}
            placeholder="Enter model name"
            disabled={onboarding.submitting || onboarding.checking}
            autoComplete="off"
          />
          {modelListOpen && displayModels && (
            <ul className="onboarding-model-list">
              {displayModels.map((modelName, index) => {
                const isRecommended = selectedProvider?.recommended_model === modelName;
                return (
                  <li
                    key={modelName}
                    className={`onboarding-model-list-item ${
                      isRecommended ? 'recommended' : ''
                    } ${index === highlightedIndex ? 'highlighted' : ''}`}
                    onClick={() => selectModel(modelName)}
                  >
                    <span className="model-name">{modelName}</span>
                    {isRecommended && (
                      <span className="recommended-badge">★ Recommended</span>
                    )}
                  </li>
                );
              })}
              {displayModels.length === 0 && (
                <li className="onboarding-model-list-item no-results">
                  No matching models
                </li>
              )}
            </ul>
          )}
        </div>

        {selectedProvider?.requires_api_key && !selectedProvider?.has_credential && (
          <>
            <div className="onboarding-step-title">3. Add your API key</div>
            <label htmlFor="onboarding-api-key">{selectedProvider.api_key_label || 'API Key'}</label>
            <input
              id="onboarding-api-key"
              type="password"
              value={onboarding.apiKey}
              className={onboarding.keyError ? 'onboarding-key-error' : ''}
              onChange={(e) => updateOnboarding((prev) => ({ ...prev, apiKey: e.target.value, error: null, keyError: false }))}
              placeholder="Paste API key"
              disabled={onboarding.submitting || onboarding.checking}
            />
            {selectedProvider.api_key_help && <div className="onboarding-help">{selectedProvider.api_key_help}</div>}
          </>
        )}

        {selectedProvider?.requires_api_key && selectedProvider?.has_credential && (
          <div className="onboarding-note">Credential already configured for this provider.</div>
        )}

        {onboarding.error && <div className="onboarding-error">{onboarding.error}</div>}

        {onboarding.validationSuccess && (
          <div className="onboarding-success">
            ✓ API key validated — {onboarding.validationModelCount > 0 ? `${onboarding.validationModelCount} models available` : 'connection successful'}
          </div>
        )}

        {onboarding.platformActionMessage && <div className="onboarding-help">{onboarding.platformActionMessage}</div>}

        {!onboarding.isReonboarding && (
          <div className="onboarding-editor-only-note">Want to explore first? You can set up AI later from Settings.</div>
        )}

        <div className="onboarding-actions">
          {!onboarding.isReonboarding && (
            <button type="button" className="onboarding-skip-btn" onClick={onSkip} disabled={onboarding.submitting || onboarding.checking || onboarding.validationSuccess}>
              Skip — use as editor
            </button>
          )}
          <button type="button" onClick={onRefresh} disabled={onboarding.submitting || onboarding.validationSuccess}>
            Refresh
          </button>
          <button
            type="button"
            className={onboarding.validationSuccess ? 'primary success' : 'primary'}
            onClick={onComplete}
            disabled={onboarding.submitting || onboarding.checking || onboarding.validationSuccess}
          >
            {onboarding.validationSuccess
              ? 'Done ✓'
              : onboarding.submitting
                ? 'Validating…'
                : (onboarding.isReonboarding ? 'Save Changes' : 'Complete Setup')}
          </button>
        </div>
      </div>
    </div>
  );
}

export default OnboardingDialog;
