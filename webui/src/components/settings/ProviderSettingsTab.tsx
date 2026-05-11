import { Pencil, Plus, Trash2, Cog } from 'lucide-react';
import { SkeletonText } from '@sprout/ui';
import type { SproutSettings } from '../../services/api';

interface ProviderSettingsTabProps {
  settings: SproutSettings;
  onRequestProviderSetup?: () => void;
  editingProvider: { mode: 'add' | 'edit'; originalName?: string } | null;
  providerName: string;
  providerApiBase: string;
  providerModelName: string;
  providerContextSize: number;
  providerEnvVar: string;
  providerSupportsVision: boolean;
  providerVisionModel: string;
  providerModelContextSizes: string;
  loadingProviderInfo: boolean;
  currentProviderInfo: { provider: string; model: string; hasCredential: boolean } | null;
  setEditingProvider: (v: { mode: 'add' | 'edit'; originalName?: string } | null) => void;
  setProviderName: (v: string) => void;
  setProviderApiBase: (v: string) => void;
  setProviderModelName: (v: string) => void;
  setProviderContextSize: (v: number) => void;
  setProviderEnvVar: (v: string) => void;
  setProviderSupportsVision: (v: boolean) => void;
  setProviderVisionModel: (v: string) => void;
  setProviderModelContextSizes: (v: string) => void;
  resetProviderForm: () => void;
  handleAddProvider: () => Promise<void>;
  handleUpdateProvider: () => Promise<void>;
  handleDeleteProvider: (name: string) => Promise<void>;
}

export default function ProviderSettingsTab({
  settings,
  onRequestProviderSetup,
  editingProvider,
  providerName,
  providerApiBase,
  providerModelName,
  providerContextSize,
  providerEnvVar,
  providerSupportsVision,
  providerVisionModel,
  providerModelContextSizes,
  loadingProviderInfo,
  currentProviderInfo,
  setEditingProvider,
  setProviderName,
  setProviderApiBase,
  setProviderModelName,
  setProviderContextSize,
  setProviderEnvVar,
  setProviderSupportsVision,
  setProviderVisionModel,
  setProviderModelContextSizes,
  resetProviderForm,
  handleAddProvider,
  handleUpdateProvider,
  handleDeleteProvider,
}: ProviderSettingsTabProps) {
  const customProviders = settings.custom_providers || {};
  const providerEntries = Object.entries(customProviders);

  return (
    <div className="section">
      <div className="current-provider-section">
        <h4>Current Provider</h4>
        {loadingProviderInfo ? (
          <div className="settings-skeleton" role="status" aria-label="Loading provider info">
            <SkeletonText lines={3} gap="12px" lineHeight="20px" />
            <span className="sr-only">Loading provider info...</span>
          </div>
        ) : currentProviderInfo ? (
          <div className="current-provider-info">
            <div className="current-provider-detail">
              <span className="label">Provider:</span>
              <span className="value">{currentProviderInfo.provider || 'Not configured'}</span>
            </div>
            <div className="current-provider-detail">
              <span className="label">Model:</span>
              <span className="value">{currentProviderInfo.model || '—'}</span>
            </div>
            <div className="current-provider-detail">
              <span className="label">Credential:</span>
              <span className={`value ${currentProviderInfo.hasCredential ? 'configured' : 'missing'}`}>
                {currentProviderInfo.hasCredential ? '✓ Configured' : 'Missing'}
              </span>
            </div>
            <button
              type="button"
              className="onboarding-reopen-btn"
              onClick={() => onRequestProviderSetup?.()}
              title="Change provider, model, or API key"
            >
              <Cog size={14} />
              Provider Setup
            </button>
          </div>
        ) : (
          <div className="settings-empty">
            No provider configured
            <button type="button" className="onboarding-reopen-btn" onClick={() => onRequestProviderSetup?.()}>
              <Cog size={14} />
              Set up provider
            </button>
          </div>
        )}
      </div>

      <h4 style={{ marginTop: '24px' }}>Custom Providers ({providerEntries.length})</h4>

      {providerEntries.length === 0 && !editingProvider && (
        <div className="settings-empty">No custom providers configured</div>
      )}

      <div className="crud-list">
        {providerEntries.map(([name, cfg]) => {
          const p = cfg as unknown as Record<string, unknown>;
          return (
            <div key={name} className="crud-item">
              <span className="crud-item-name">{name}</span>
              <span className="crud-item-detail">{(p.endpoint as string) || (p.api_base as string) || ''}</span>
              <button
                type="button"
                className="crud-btn"
                title="Edit provider"
                onClick={() => {
                  setEditingProvider({ mode: 'edit', originalName: name });
                  setProviderName(name);
                  setProviderApiBase((p.endpoint as string) || (p.api_base as string) || '');
                  setProviderModelName(
                    (p.model_name as string) ||
                      (Array.isArray(p.models) && (p.models as unknown[]).length > 0
                        ? String((p.models as unknown[])[0])
                        : ''),
                  );
                  setProviderContextSize((p.context_size as number) || 32768);
                  setProviderEnvVar((p.env_var as string) || '');
                  setProviderSupportsVision(!!p.supports_vision);
                  setProviderVisionModel((p.vision_model as string) || '');
                  const mcs = p.model_context_sizes;
                  if (mcs && typeof mcs === 'object') {
                    const pairs = Object.entries(mcs as Record<string, unknown>)
                      .map(([model, size]) => `${model}:${size}`)
                      .join(',');
                    setProviderModelContextSizes(pairs);
                  } else {
                    setProviderModelContextSizes('');
                  }
                }}
              >
                <Pencil size={12} />
              </button>
              <button
                type="button"
                className="crud-btn danger"
                title="Delete provider"
                onClick={() => handleDeleteProvider(name)}
              >
                <Trash2 size={12} />
              </button>
            </div>
          );
        })}

        {editingProvider && (
          <div className="crud-inline-form">
            <div className="form-row">
              <label>Name</label>
              <input
                type="text"
                className="styled-input"
                value={providerName}
                onChange={(e) => setProviderName(e.target.value)}
                placeholder="provider-name"
                disabled={editingProvider.mode === 'edit'}
              />
            </div>
            <div className="form-row">
              <label>API Base URL</label>
              <input
                type="text"
                className="styled-input"
                value={providerApiBase}
                onChange={(e) => setProviderApiBase(e.target.value)}
                placeholder="https://api.example.com/v1"
              />
            </div>
            <div className="form-row">
              <label>Default Model</label>
              <input
                type="text"
                className="styled-input"
                value={providerModelName}
                onChange={(e) => setProviderModelName(e.target.value)}
                placeholder="gpt-4o-mini"
              />
            </div>
            <div className="form-row">
              <label>Default Context Size (tokens)</label>
              <input
                type="number"
                className="styled-input config-row-input"
                value={providerContextSize}
                onChange={(e) => setProviderContextSize(parseInt(e.target.value) || 32768)}
                placeholder="32768"
                min="0"
              />
            </div>
            <div className="form-row">
              <label>Per-Model Context Sizes (optional)</label>
              <input
                type="text"
                className="styled-input"
                value={providerModelContextSizes}
                onChange={(e) => setProviderModelContextSizes(e.target.value)}
                placeholder="model1:8192,model2:131072,model3:2097152"
              />
              <small
                style={{
                  color: '#888',
                  fontSize: '12px',
                  marginTop: '4px',
                  display: 'block',
                }}
              >
                Format: model_name:context_size, separated by commas
              </small>
            </div>
            <div className="form-row">
              <label>API Key Env Var (optional)</label>
              <input
                type="text"
                className="styled-input"
                value={providerEnvVar}
                onChange={(e) => setProviderEnvVar(e.target.value)}
                placeholder="OPENAI_API_KEY"
              />
            </div>
            <label className="styled-toggle">
              <input
                type="checkbox"
                checked={providerSupportsVision}
                onChange={(e) => setProviderSupportsVision(e.target.checked)}
              />
              <span className="toggle-track" />
              <span className="toggle-label">Supports Vision</span>
            </label>
            {providerSupportsVision && (
              <div className="form-row">
                <label>Vision Model (optional)</label>
                <input
                  type="text"
                  className="styled-input"
                  value={providerVisionModel}
                  onChange={(e) => setProviderVisionModel(e.target.value)}
                  placeholder="Leave empty to use default model"
                />
              </div>
            )}
            <div className="form-actions">
              <button
                type="button"
                className="form-btn primary"
                onClick={editingProvider.mode === 'edit' ? handleUpdateProvider : handleAddProvider}
              >
                {editingProvider.mode === 'edit' ? 'Update' : 'Add'}
              </button>
              <button type="button" className="form-btn cancel" onClick={resetProviderForm}>
                Cancel
              </button>
            </div>
          </div>
        )}

        {!editingProvider && (
          <button
            type="button"
            className="crud-add-btn"
            onClick={() => {
              setEditingProvider({ mode: 'add' });
              setProviderName('');
              setProviderApiBase('');
              setProviderModelName('');
              setProviderContextSize(32768);
              setProviderEnvVar('');
              setProviderSupportsVision(false);
              setProviderVisionModel('');
              setProviderModelContextSizes('');
            }}
          >
            <Plus size={14} /> Add provider
          </button>
        )}
      </div>
    </div>
  );
}
