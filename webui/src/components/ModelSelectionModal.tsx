import { useEffect, useState, useCallback, useRef } from 'react';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';
import './ThemedDialog.css';

export interface ModelSelectionModalProps {
  provider: string;
  onClose: () => void;
  onSelectModel: (model: string) => void;
}

function ModelSelectionModal({
  provider,
  onClose,
  onSelectModel,
}: ModelSelectionModalProps): JSX.Element {
  const [models, setModels] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedModel, setSelectedModel] = useState<string>('');
  const listRef = useRef<HTMLUListElement>(null);

  const fetchModels = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const apiService = ApiService.getInstance();
      const response = await apiService.getProviderModels(provider);
      setModels(response.models || []);
      debugLog('[ModelSelectionModal] Fetched models:', response.models?.length || 0);
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

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    }
    if (e.key === 'Enter' && selectedModel) {
      e.preventDefault();
      handleSelect();
    }
  }, [onClose, selectedModel, handleSelect]);

  // Auto-focus the first model in the list when loaded
  useEffect(() => {
    if (!loading && !error && models.length > 0 && !selectedModel) {
      setSelectedModel(models[0]);
    }
  }, [loading, error, models, selectedModel]);

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    // Lock scroll while modal is open
    document.body.style.overflow = 'hidden';

    // Focus first item in list after mount
    const timer = setTimeout(() => {
      const firstItem = listRef.current?.querySelector('button') as HTMLButtonElement;
      firstItem?.focus();
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
    <div className="model-selection-overlay" role="dialog" aria-modal="true" aria-label="Model selection required">
      <div className="model-selection-card" onClick={(e) => e.stopPropagation()}>
        {/* Accent bar - warning color */}
        <div className="model-selection-accent-bar" />

        {/* Header */}
        <div className="model-selection-header">
          <span className="model-selection-icon">⚠</span>
          <h2 className="model-selection-title">Model Not Available</h2>
        </div>

        {/* Body */}
        <div className="model-selection-body">
          <div className="model-selection-message">
            The configured model is not available for provider <strong>{provider}</strong>.
            Please select a different model to continue.
          </div>

          {loading && (
            <div className="model-selection-loading">Loading available models...</div>
          )}

          {error && (
            <div className="model-selection-error">
              {error}
            </div>
          )}

          {!loading && !error && models.length === 0 && (
            <div className="model-selection-empty">No models available for this provider.</div>
          )}

          {!loading && !error && models.length > 0 && (
            <div className="model-selection-list-wrapper">
              <ul
                ref={listRef}
                className="model-selection-list"
                role="listbox"
                aria-label="Available models"
              >
                {models.map((model) => (
                  <li key={model}>
                    <button
                      type="button"
                      className={`model-selection-item ${
                        selectedModel === model ? 'model-selection-item--selected' : ''
                      }`}
                      onClick={() => setSelectedModel(model)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          handleSelect();
                        }
                      }}
                      aria-selected={selectedModel === model}
                      role="option"
                    >
                      <span className="model-selection-item-text">{model}</span>
                      {selectedModel === model && (
                        <span className="model-selection-item-check">✓</span>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            </div>
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
