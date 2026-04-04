import type { ReactElement } from 'react';
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
  onRefresh,
  onInstallWsl,
  onInstallGitBash,
  updateOnboarding,
}: OnboardingDialogProps): ReactElement | null {
  if (!onboarding.open) {
    return null;
  }

  return (
    <div className="onboarding-overlay" role="dialog" aria-modal="true" aria-label="Set up ledit">
      <div className="onboarding-card">
        <h2>Set Up Ledit</h2>
        <p>
          {onboarding.reason === 'missing_provider_credential'
            ? 'The selected provider is missing credentials.'
            : 'Choose a provider and model to get started.'}
        </p>

        {windowsGuidance && (
          <div className={`onboarding-platform-panel ${windowsGuidance.tone}`}>
            <div className="onboarding-platform-title">{windowsGuidance.title}</div>
            <div className="onboarding-platform-body">{windowsGuidance.body}</div>
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
              className={`onboarding-provider-card ${onboarding.provider === providerOption.id ? 'selected' : ''}`}
              onClick={() => onProviderChange(providerOption.id)}
              disabled={onboarding.submitting || onboarding.checking}
            >
              <span className="onboarding-provider-name">{providerOption.name}</span>
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
                      {p.requires_api_key && !p.has_credential ? ' (API key required)' : ''}
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

        <div className="onboarding-step-title">2. Choose a model</div>
        <label htmlFor="onboarding-model">Model</label>
        <input
          id="onboarding-model"
          value={onboarding.model}
          onChange={(e) => updateOnboarding((prev) => ({ ...prev, model: e.target.value }))}
          placeholder="Enter model name"
          list="onboarding-models"
          disabled={onboarding.submitting || onboarding.checking}
        />
        <datalist id="onboarding-models">
          {(selectedProvider?.models || []).map((modelName) => (
            <option key={modelName} value={modelName} />
          ))}
        </datalist>

        {selectedProvider?.recommended_model && (
          <div className="onboarding-note">
            Recommended model: <strong>{selectedProvider.recommended_model}</strong>
            {selectedProvider.recommended_model_why ? ` — ${selectedProvider.recommended_model_why}` : ''}
            {onboarding.model !== selectedProvider.recommended_model && (
              <>
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
              </>
            )}
          </div>
        )}

        {selectedProvider?.requires_api_key && !selectedProvider?.has_credential && (
          <>
            <div className="onboarding-step-title">3. Add your API key</div>
            <label htmlFor="onboarding-api-key">{selectedProvider.api_key_label || 'API Key'}</label>
            <input
              id="onboarding-api-key"
              type="password"
              value={onboarding.apiKey}
              onChange={(e) => updateOnboarding((prev) => ({ ...prev, apiKey: e.target.value }))}
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
        {onboarding.platformActionMessage && <div className="onboarding-help">{onboarding.platformActionMessage}</div>}

        <div className="onboarding-actions">
          <button type="button" onClick={onRefresh} disabled={onboarding.submitting}>
            Refresh
          </button>
          <button
            type="button"
            className="primary"
            onClick={onComplete}
            disabled={onboarding.submitting || onboarding.checking}
          >
            {onboarding.submitting ? 'Saving...' : 'Complete Setup'}
          </button>
        </div>
      </div>
    </div>
  );
}

export default OnboardingDialog;
