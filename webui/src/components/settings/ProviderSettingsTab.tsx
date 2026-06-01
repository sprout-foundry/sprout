import { SkeletonText } from '@sprout/ui';
import { ArrowDown, ArrowUp, Pencil, Plus, Trash2, Cog } from 'lucide-react';
import { useState } from 'react';
import { ApiService, type SproutSettings, type ProviderOption } from '../../services/api';
import { showThemedConfirm } from '../ThemedDialog';
import ListFilter from './ListFilter';

const PROVIDER_FILTER_THRESHOLD = 4;

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
  /** Provider catalog for the inline switcher dropdowns. Populated by
   *  useSettingsState whenever the env-providers or subagents tab is
   *  open. Empty list falls back to the read-only display. */
  availableProviders?: ProviderOption[];
  /** Notify the parent that the persisted provider/model changed so it
   *  can refresh the "Current Provider" panel (which reads from a
   *  separate endpoint). */
  onPrimaryProviderChanged?: () => void;
  /** Settings PUT mutation used to persist primary provider/model changes
   *  from the inline switcher. Matches the same callback consumed by
   *  SubagentSettingsTab and CommitReviewSettingsTab for parity. */
  updateSetting?: (keyOrPath: string, value: unknown) => Promise<void>;
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
  availableProviders,
  onPrimaryProviderChanged,
  updateSetting,
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
  const [providerFilter, setProviderFilter] = useState('');
  const normalizedProviderFilter = providerFilter.trim().toLowerCase();
  const filteredProviderEntries = normalizedProviderFilter
    ? providerEntries.filter(
        ([name, cfg]) =>
          name.toLowerCase().includes(normalizedProviderFilter) ||
          (cfg.endpoint || '').toLowerCase().includes(normalizedProviderFilter),
      )
    : providerEntries;

  // The inline switcher only renders when we have both a provider catalog
  // and a persistence callback — otherwise we fall back to the read-only
  // "Current Provider" panel + Provider Setup button (the legacy path).
  const canSwitchInline = !!updateSetting && (availableProviders?.length ?? 0) > 0;
  const currentProviderId = currentProviderInfo?.provider || '';
  const currentModelId = currentProviderInfo?.model || '';
  const selectedProviderEntry = availableProviders?.find((p) => p.id === currentProviderId);
  const availableModelsForCurrent = selectedProviderEntry?.models || [];

  // Primary provider/model changes must persist to disk, not stay in the
  // current chat session's ConfigOverrides — so call the API directly with
  // an explicit "global" layer instead of going through updateSetting
  // (which routes via configViewLayer and would land in session scope
  // when the user is viewing the session layer). This matches the
  // existing onboarding finalize path which also writes globally.
  const [primarySaving, setPrimarySaving] = useState<'provider' | 'model' | null>(null);
  const persistPrimary = async (key: 'provider' | 'model', value: string) => {
    setPrimarySaving(key);
    try {
      await ApiService.getInstance().updateSettings({ [key]: value }, 'global');
      // Refresh the "Current Provider" panel — it reads from a separate
      // endpoint (see useSettingsState `loadCurrentProviderInfo`), so a
      // simple state update isn't enough.
      onPrimaryProviderChanged?.();
    } catch (err) {
      console.error('Failed to persist primary', key, err);
    } finally {
      setPrimarySaving(null);
    }
  };

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
            {canSwitchInline ? (
              <>
                <div className="config-item">
                  <label htmlFor="primary-provider-select">Provider</label>
                  <select
                    id="primary-provider-select"
                    className="styled-select"
                    value={currentProviderId}
                    onChange={(e) => persistPrimary('provider', e.target.value)}
                    disabled={primarySaving !== null}
                  >
                    {!currentProviderId && <option value="">Not configured</option>}
                    {availableProviders!.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="config-item">
                  <label htmlFor="primary-model-select">Model</label>
                  <select
                    id="primary-model-select"
                    className="styled-select"
                    value={currentModelId}
                    onChange={(e) => persistPrimary('model', e.target.value)}
                    disabled={primarySaving !== null || !currentProviderId || availableModelsForCurrent.length === 0}
                  >
                    {currentModelId && !availableModelsForCurrent.includes(currentModelId) && (
                      <option value={currentModelId}>{currentModelId}</option>
                    )}
                    {availableModelsForCurrent.length === 0 && <option value="">No models available</option>}
                    {availableModelsForCurrent.map((m) => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="current-provider-detail">
                  <span className="label">Credential:</span>
                  <span className={`value ${currentProviderInfo.hasCredential ? 'configured' : 'missing'}`}>
                    {currentProviderInfo.hasCredential ? '✓ Configured' : 'Missing'}
                  </span>
                </div>
              </>
            ) : (
              <>
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
              </>
            )}
            <button
              type="button"
              className="onboarding-reopen-btn"
              onClick={() => onRequestProviderSetup?.()}
              title="Change provider, model, or API key via guided setup"
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

      <ProviderPrioritySection
        settings={settings}
        availableProviders={availableProviders ?? []}
        updateSetting={updateSetting}
      />

      <h4 className="settings-h4-spaced">Custom Providers ({providerEntries.length})</h4>

      {providerEntries.length >= PROVIDER_FILTER_THRESHOLD && (
        <ListFilter
          value={providerFilter}
          onChange={setProviderFilter}
          placeholder={`Filter ${providerEntries.length} providers…`}
          ariaLabel="Filter custom providers"
        />
      )}

      {providerEntries.length === 0 && !editingProvider && (
        <div className="settings-empty">No custom providers configured</div>
      )}

      {normalizedProviderFilter && filteredProviderEntries.length === 0 && (
        <div className="settings-empty">No providers match “{providerFilter}”</div>
      )}

      <div className="crud-list">
        {filteredProviderEntries.map(([name, cfg]) => {
          return (
            <div key={name} className="crud-item">
              <span className="crud-item-name">{name}</span>
              <span className="crud-item-detail">{cfg.endpoint || ''}</span>
              <button
                type="button"
                className="crud-btn"
                title="Edit provider"
                onClick={() => {
                  setEditingProvider({ mode: 'edit', originalName: name });
                  setProviderName(name);
                  setProviderApiBase(cfg.endpoint || '');
                  setProviderModelName(cfg.model_name || '');
                  setProviderContextSize(cfg.context_size || 32768);
                  setProviderEnvVar(cfg.env_var || '');
                  setProviderSupportsVision(!!cfg.supports_vision);
                  setProviderVisionModel(cfg.vision_model || '');
                  const mcs = cfg.model_context_sizes;
                  if (mcs && typeof mcs === 'object') {
                    const pairs = Object.entries(mcs)
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
                onClick={async () => {
                  const confirmed = await showThemedConfirm(
                    `Delete custom provider "${name}"? This removes its config.`,
                    { title: 'Delete custom provider', type: 'danger', confirmLabel: 'Delete' },
                  );
                  if (!confirmed) return;
                  void handleDeleteProvider(name);
                }}
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
              <small className="config-help">Format: model_name:context_size, separated by commas</small>
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

interface ProviderPrioritySectionProps {
  settings: SproutSettings;
  availableProviders: ProviderOption[];
  updateSetting?: (keyOrPath: string, value: unknown) => Promise<void>;
}

/**
 * Ordered fallback list. When the active provider can't be reached (no
 * credential, endpoint down, etc.), sprout walks this list and switches
 * to the first usable entry. Empty list = no fallback; only the active
 * provider is used.
 */
function ProviderPrioritySection({ settings, availableProviders, updateSetting }: ProviderPrioritySectionProps) {
  const priority = ((settings as unknown as { provider_priority?: string[] }).provider_priority ?? []) as string[];
  const [pendingAdd, setPendingAdd] = useState('');

  const persist = (next: string[]) => {
    if (!updateSetting) return;
    void updateSetting('provider_priority', next);
  };

  const remove = (idx: number) => {
    persist(priority.filter((_, i) => i !== idx));
  };
  const moveUp = (idx: number) => {
    if (idx <= 0) return;
    const next = [...priority];
    [next[idx - 1], next[idx]] = [next[idx], next[idx - 1]];
    persist(next);
  };
  const moveDown = (idx: number) => {
    if (idx >= priority.length - 1) return;
    const next = [...priority];
    [next[idx], next[idx + 1]] = [next[idx + 1], next[idx]];
    persist(next);
  };
  const add = () => {
    const id = pendingAdd.trim();
    if (!id || priority.includes(id)) {
      setPendingAdd('');
      return;
    }
    persist([...priority, id]);
    setPendingAdd('');
  };

  const candidateOptions = availableProviders.filter((p) => !priority.includes(p.id));

  // Hide the section entirely if there's no way to mutate (no updateSetting).
  if (!updateSetting) return null;

  return (
    <div className="settings-block-spaced">
      <h4>Provider Priority</h4>
      <div className="config-help settings-help-spaced">
        Fallback order when the active provider is unreachable. The first usable provider in this list wins. Empty list
        means no fallback.
      </div>

      {priority.length === 0 ? (
        <div className="settings-empty">No providers in priority list</div>
      ) : (
        <ol className="settings-list">
          {priority.map((id, idx) => {
            const meta = availableProviders.find((p) => p.id === id);
            return (
              <li key={id} className="settings-list-row">
                <span className="settings-list-row-index">#{idx + 1}</span>
                <span className="settings-list-row-label">{meta?.name ?? id}</span>
                <div className="settings-list-row-actions">
                  <button
                    type="button"
                    aria-label={`Move ${id} up`}
                    title="Move up"
                    className="settings-icon-btn"
                    disabled={idx === 0}
                    onClick={() => moveUp(idx)}
                  >
                    <ArrowUp size={14} />
                  </button>
                  <button
                    type="button"
                    aria-label={`Move ${id} down`}
                    title="Move down"
                    className="settings-icon-btn"
                    disabled={idx === priority.length - 1}
                    onClick={() => moveDown(idx)}
                  >
                    <ArrowDown size={14} />
                  </button>
                  <button
                    type="button"
                    aria-label={`Remove ${id}`}
                    title="Remove from priority list"
                    className="settings-icon-btn danger"
                    onClick={async () => {
                      const confirmed = await showThemedConfirm(
                        `Remove "${meta?.name ?? id}" from the priority fallback list?`,
                        { title: 'Remove from priority list', type: 'warning', confirmLabel: 'Remove' },
                      );
                      if (!confirmed) return;
                      remove(idx);
                    }}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              </li>
            );
          })}
        </ol>
      )}

      <div className="config-item settings-help-spaced-top">
        <div className="settings-inline-row">
          <select
            className="styled-select"
            value={pendingAdd}
            onChange={(e) => setPendingAdd(e.target.value)}
          >
            <option value="">
              {candidateOptions.length === 0 ? 'All providers already in list' : 'Select provider to add…'}
            </option>
            {candidateOptions.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="settings-action-btn"
            onClick={add}
            disabled={pendingAdd === '' || candidateOptions.length === 0}
          >
            Add
          </button>
        </div>
      </div>
    </div>
  );
}
