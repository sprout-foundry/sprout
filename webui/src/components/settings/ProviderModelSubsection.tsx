import { useState } from 'react';
import { SkeletonText } from '@sprout/ui';
import type { ProviderOption } from '../../services/api';

export type SettingsScope = 'session' | 'workspace' | 'global';

export interface ProviderModelSubsectionProps {
  /** Section heading displayed above the controls */
  label: string;
  /** Currently selected provider ID (empty string = none) */
  provider: string;
  /** Currently selected model ID (empty string = none) */
  model: string;
  /** Available providers with their model lists */
  providers: ProviderOption[];
  /** Available models for the currently selected provider */
  models: string[];
  /** Human-readable inherited value string, e.g. "Inherited from global: anthropic/claude-3" */
  inheritedValue?: string;
  /** Called when the user selects a provider */
  onProviderChange: (providerId: string) => void;
  /** Called when the user selects a model */
  onModelChange: (modelId: string) => void;
  /** Which scope this subsection targets */
  scope: SettingsScope;
  /** When true, disable all interactive controls */
  disabled?: boolean;
  /** When true, show a loading skeleton instead of controls */
  loading?: boolean;
}

export default function ProviderModelSubsection({
  label,
  provider,
  model,
  providers,
  models,
  inheritedValue,
  onProviderChange,
  onModelChange,
  scope,
  disabled = false,
  loading = false,
}: ProviderModelSubsectionProps) {
  // Local state to track whether the user has opted into override mode
  const [isOverriding, setIsOverriding] = useState(false);
  const hasActiveOverride = !!provider || !!model;
  const showInheritedDisplay = !!inheritedValue && !hasActiveOverride && !isOverriding;

  const handleOverrideClick = () => {
    setIsOverriding(true);
  };

  const handleClearOverride = () => {
    onProviderChange('');
    onModelChange('');
    setIsOverriding(false);
  };

  const handleProviderChange = (newProviderId: string) => {
    onProviderChange(newProviderId);

    // If the new provider's models don't include the current model, reset model selection
    const newProvider = providers.find((p) => p.id === newProviderId);
    const availableForProvider = newProvider?.models || [];
    const shouldResetModel = !newProviderId || (availableForProvider.length > 0 && !availableForProvider.includes(model));
    if (shouldResetModel) {
      onModelChange('');
    }
  };

  // Determine unique select IDs based on scope to avoid duplicate IDs on the page
  const providerId = `provider-select-${scope}`;
  const modelId = `model-select-${scope}`;

  return (
    <div className="provider-model-subsection">
      <h4>{label}</h4>

      {loading ? (
        <div className="settings-skeleton" role="status" aria-label="Loading provider options">
          <SkeletonText lines={3} gap="12px" lineHeight="20px" />
          <span className="sr-only">Loading provider options...</span>
        </div>
      ) : showInheritedDisplay ? (
        /* --- Inherited display mode --- */
        <div className="inherited-display">
          <span className="inherited-label" title="Value inherited from a higher scope">
            Inherited:
          </span>
          <span className="inherited-value" title={inheritedValue}>
            {inheritedValue}
          </span>
          <button
            type="button"
            className="crud-btn"
            onClick={handleOverrideClick}
            disabled={disabled}
            title="Override inherited value with a custom selection"
          >
            Override
          </button>
        </div>
      ) : (
        /* --- Override / edit mode --- */
        <>
          <div className="config-item">
            <label htmlFor={providerId}>Provider</label>
            <select
              id={providerId}
              className="styled-select"
              value={provider}
              onChange={(e) => handleProviderChange(e.target.value)}
              disabled={disabled}
              title={`Select ${scope} provider`}
            >
              <option value="">Default (inherit from {scope === 'session' ? 'workspace or global' : scope === 'workspace' ? 'global' : 'provider default'})</option>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>

          <div className="config-item">
            <label htmlFor={modelId}>Model</label>
            <select
              id={modelId}
              className="styled-select"
              value={model}
              onChange={(e) => onModelChange(e.target.value)}
              disabled={disabled || !provider || models.length === 0}
              title={`Select ${scope} model`}
            >
              {model && !models.includes(model) && (
                <option value={model}>{model}</option>
              )}
              {models.length === 0 && <option value="">No models available</option>}
              {models.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>

          {inheritedValue && (
            <button
              type="button"
              className="form-btn cancel"
              onClick={handleClearOverride}
              disabled={disabled}
              title="Clear override and revert to inherited value"
            >
              Clear override
            </button>
          )}
        </>
      )}
    </div>
  );
}
