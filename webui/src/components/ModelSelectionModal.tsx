import { useEffect, useState, useCallback, useMemo, useRef } from 'react';
import { ApiService } from '../services/api';
import type { ProviderModel } from '../services/api/types';
import { debugLog } from '../utils/log';
import './ThemedDialog.css';

// normalizeModels tolerates both the rich object shape and a legacy plain-string
// list, so the modal renders correctly regardless of server/adapter variation.
function normalizeModels(raw: unknown): ProviderModel[] {
  if (!Array.isArray(raw)) return [];
  return raw
    .map((m): ProviderModel | null => {
      if (typeof m === 'string') return { id: m };
      if (m && typeof m === 'object' && typeof (m as ProviderModel).id === 'string') return m as ProviderModel;
      return null;
    })
    .filter((m): m is ProviderModel => m !== null);
}

export interface ModelSelectionModalProps {
  provider: string;
  /**
   * Why the modal opened. `unavailable` shows the warning-styled "Model
   * Not Available" treatment (the original error-recovery use case);
   * `switch` shows a neutral "Choose a model" treatment for proactive
   * switching from the status bar.
   */
  reason?: 'unavailable' | 'switch';
  onClose: () => void;
  onSelectModel: (model: string) => void;
}

function ModelSelectionModal({
  provider,
  reason = 'unavailable',
  onClose,
  onSelectModel,
}: ModelSelectionModalProps): JSX.Element {
  const [models, setModels] = useState<ProviderModel[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedModel, setSelectedModel] = useState<string>('');
  const [filter, setFilter] = useState('');
  const listRef = useRef<HTMLUListElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  // Filter models against the search input — handy when a provider lists
  // dozens of variants (Anthropic claude-*-*, OpenRouter's full catalog).
  const visibleModels = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return models;
    return models.filter((m) => m.id.toLowerCase().includes(q) || (m.name ?? '').toLowerCase().includes(q));
  }, [models, filter]);

  // Copy + intent vary by why the modal opened.
  const isWarning = reason !== 'switch';
  const title = isWarning ? 'Model Not Available' : 'Choose a model';
  const icon = isWarning ? '⚠' : '✱';
  const description = isWarning ? (
    <>
      The configured model is not available for provider <strong>{provider}</strong>. Please select a different model
      to continue.
    </>
  ) : (
    <>
      Select a model available on <strong>{provider}</strong>. The change applies immediately and is saved to your
      session.
    </>
  );

  const fetchModels = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const apiService = ApiService.getInstance();
      const response = await apiService.getProviderModels(provider);
      const normalized = normalizeModels(response.models);
      setModels(normalized);
      debugLog('[ModelSelectionModal] Fetched models:', normalized.length);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to fetch available models';
      setError(errorMessage);
      debugLog('[ModelSelectionModal] Failed to fetch models:', err);
    } finally {
      setLoading(false);
    }
  }, [provider]);

  const handleSelect = useCallback(() => {
    if (selectedModel) {
      onSelectModel(selectedModel);
    }
  }, [selectedModel, onSelectModel]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
      if (e.key === 'Enter' && selectedModel) {
        e.preventDefault();
        handleSelect();
      }
    },
    [onClose, selectedModel, handleSelect],
  );

  // Auto-preselect the first visible model when the list/filter changes.
  useEffect(() => {
    if (!loading && !error && visibleModels.length > 0 && !visibleModels.some((m) => m.id === selectedModel)) {
      setSelectedModel(visibleModels[0].id);
    }
  }, [loading, error, visibleModels, selectedModel]);

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    // Lock scroll while modal is open
    document.body.style.overflow = 'hidden';

    // Auto-focus the search input on mount — typing immediately filters,
    // which is the fastest path for providers with many models.
    const timer = setTimeout(() => {
      searchRef.current?.focus();
    }, 60);

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
      clearTimeout(timer);
    };
  }, [handleKeyDown]);

  useEffect(() => {
    fetchModels();
  }, [fetchModels]);

  return (
    <div
      className={`model-selection-overlay${isWarning ? '' : ' model-selection-overlay--switch'}`}
      role="dialog"
      aria-modal="true"
      aria-label={isWarning ? 'Model selection required' : 'Choose a model'}
    >
      <div
        className={`model-selection-card${isWarning ? '' : ' model-selection-card--switch'}`}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Accent bar — warning yellow for the recovery case, brand blue
         * for the proactive-switch case so the modal doesn't read as
         * alarming when the user opened it themselves. */}
        <div className="model-selection-accent-bar" />

        {/* Header */}
        <div className="model-selection-header">
          <span className="model-selection-icon">{icon}</span>
          <h2 className="model-selection-title">{title}</h2>
        </div>

        {/* Body */}
        <div className="model-selection-body">
          <div className="model-selection-message">{description}</div>

          {loading && <div className="model-selection-loading">Loading available models...</div>}

          {error && <div className="model-selection-error">{error}</div>}

          {!loading && !error && models.length === 0 && (
            <div className="model-selection-empty">No models available for this provider.</div>
          )}

          {!loading && !error && models.length > 0 && (
            <>
              <input
                ref={searchRef}
                type="text"
                className="model-selection-search"
                placeholder="Filter models…"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                aria-label="Filter models"
              />
              {visibleModels.length === 0 && (
                <div className="model-selection-empty">No models match &quot;{filter}&quot;.</div>
              )}
              {visibleModels.length > 0 && (
                <div className="model-selection-list-wrapper">
                  <ul ref={listRef} className="model-selection-list" role="listbox" aria-label="Available models">
                    {visibleModels.map((model) => {
                      const recommended = model.recommended_roles ?? [];
                      const eligible = model.eligible_roles ?? [];
                      const warnings = model.warnings ?? [];
                      const recommendedRole = recommended.includes('primary') ? 'primary' : recommended[0];
                      return (
                  <li key={model.id}>
                    <button
                      type="button"
                      className={`model-selection-item ${
                        selectedModel === model.id ? 'model-selection-item--selected' : ''
                      }`}
                      onClick={() => setSelectedModel(model.id)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          handleSelect();
                        }
                      }}
                      aria-selected={selectedModel === model.id}
                      role="option"
                    >
                      <span className="model-selection-item-text">{model.id}</span>
                      {recommendedRole && (
                        <span
                          className="model-selection-badge model-selection-badge--recommended"
                          title={`Recommended for ${recommended.join(', ')} — passed the capability probe`}
                        >
                          ★ {recommendedRole}
                        </span>
                      )}
                      {!recommendedRole && eligible.length > 0 && (
                        <span
                          className="model-selection-badge model-selection-badge--eligible"
                          title={`Eligible for ${eligible.join(', ')} (not yet probe-verified)`}
                        >
                          eligible
                        </span>
                      )}
                      {warnings.length > 0 && (
                        <span
                          className="model-selection-badge model-selection-badge--warning"
                          title={warnings.join('; ')}
                          aria-label={`Warning: ${warnings.join('; ')}`}
                        >
                          ⚠
                        </span>
                      )}
                      {selectedModel === model.id && <span className="model-selection-item-check">✓</span>}
                    </button>
                  </li>
                      );
                    })}
                  </ul>
                </div>
              )}
            </>
          )}
        </div>

        {/* Footer */}
        <div className="model-selection-footer">
          <button
            type="button"
            className="model-selection-btn model-selection-btn--cancel"
            onClick={onClose}
            disabled={loading}
          >
            Cancel
          </button>
          <button
            type="button"
            className="model-selection-btn model-selection-btn--select"
            onClick={handleSelect}
            disabled={!selectedModel || loading}
          >
            Select Model
          </button>
        </div>
      </div>
    </div>
  );
}

export default ModelSelectionModal;
