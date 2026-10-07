/**
 * Settings › Model in the hosted editor: run the agent on the platform's
 * managed model, or on a model from a provider the user saved their own key
 * for. Own-key requests cost no platform credits.
 */

import { useEffect, useId, useState } from 'react';
import type { FormEvent, ReactElement } from 'react';
import { useHost } from '../host/useHost';
import { platformHref } from '../host/platformUrl';
import { onPlatformLinkClick } from '../services/homeView';
import {
  getEditorModel,
  listProviderModels,
  PROVIDER_LABELS,
  setEditorModel,
  type EditorModelState,
  type ProviderModel,
} from '../services/editorModel';
import './EditorModelSection.css';

export default function EditorModelSection(): ReactElement {
  const { navigation } = useHost();
  // Destinations come from the host's intent resolution; the paths live only
  // on the host side.
  const accountSettingsPath = navigation.intentPath?.({ type: 'account' }) ?? null;
  const usagePath = navigation.intentPath?.({ type: 'usage' }) ?? null;
  const [state, setState] = useState<EditorModelState | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [mode, setMode] = useState<'managed' | 'own'>('managed');
  const [provider, setProvider] = useState('');
  const [model, setModel] = useState('');
  const [models, setModels] = useState<ProviderModel[]>([]);
  const [modelsNote, setModelsNote] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ ok: boolean; text: string } | null>(null);
  const listId = useId();

  useEffect(() => {
    let live = true;
    getEditorModel()
      .then((s) => {
        if (!live) return;
        setState(s);
        setMode(s.provider ? 'own' : 'managed');
        setProvider(s.provider || s.key_providers[0] || '');
        setModel(s.model);
      })
      .catch((err) => live && setLoadError(err instanceof Error ? err.message : String(err)));
    return () => {
      live = false;
    };
  }, []);

  useEffect(() => {
    if (mode !== 'own' || !provider) return;
    let live = true;
    setModels([]);
    setModelsNote(null);
    listProviderModels(provider)
      .then((list) => live && setModels(list))
      .catch((err) => live && setModelsNote(`${err instanceof Error ? err.message : err}. Type a model name.`));
    return () => {
      live = false;
    };
  }, [mode, provider]);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setStatus(null);
    try {
      const choice = mode === 'managed' ? { provider: '', model: '' } : { provider, model: model.trim() };
      await setEditorModel(choice);
      setState((s) => (s ? { ...s, ...choice } : s));
      setStatus({ ok: true, text: 'Saved. The next message uses it.' });
      // The agent's context window follows the model.
      void import('../services/platformProvider').then(({ loadManagedContextWindow }) =>
        loadManagedContextWindow(window.location.origin),
      );
    } catch (err) {
      setStatus({ ok: false, text: err instanceof Error ? err.message : String(err) });
    } finally {
      setSaving(false);
    }
  };

  if (loadError) {
    return (
      <div className="config-item" data-testid="editor-model-section">
        <p className="settings-section-desc">Couldn&apos;t load the model setting: {loadError}</p>
      </div>
    );
  }
  if (!state) {
    return (
      <div className="config-item" data-testid="editor-model-section">
        <p className="settings-section-desc">Loading…</p>
      </div>
    );
  }

  const noKeys = state.key_providers.length === 0;
  const dirty =
    mode === 'managed' ? state.provider !== '' : provider !== state.provider || model.trim() !== state.model;

  return (
    <form
      className="config-item editor-model-section"
      data-testid="editor-model-section"
      onSubmit={(e) => void save(e)}
    >
      <label className="editor-model-option">
        <input type="radio" name="editor-model" checked={mode === 'managed'} onChange={() => setMode('managed')} />
        <span>
          <strong>Sprout Foundry managed model</strong>
          <span className="settings-section-desc">Picks a model for each request. Uses platform credits.</span>
        </span>
      </label>
      <label className="editor-model-option">
        <input type="radio" name="editor-model" checked={mode === 'own'} onChange={() => setMode('own')} />
        <span>
          <strong>Your own API key</strong>
          <span className="settings-section-desc">
            A model from your provider account. No platform credits; your provider bills you.
          </span>
        </span>
      </label>

      {mode === 'own' &&
        (noKeys ? (
          <p className="settings-section-desc">
            Save an API key first, in{' '}
            {accountSettingsPath ? (
              <a href={platformHref(accountSettingsPath)} onClick={onPlatformLinkClick(accountSettingsPath)}>
                account settings
              </a>
            ) : (
              'account settings'
            )}
            .
          </p>
        ) : (
          <div className="editor-model-fields">
            <label>
              Provider
              <select className="styled-select" value={provider} onChange={(e) => setProvider(e.target.value)}>
                {state.key_providers.map((p) => (
                  <option key={p} value={p}>
                    {PROVIDER_LABELS[p] ?? p}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Model
              <input
                className="styled-input"
                list={listId}
                value={model}
                placeholder="Model name"
                onChange={(e) => setModel(e.target.value)}
                aria-label="Model"
              />
              <datalist id={listId}>
                {models.map((m) => (
                  <option key={m.id} value={m.id} />
                ))}
              </datalist>
            </label>
            {modelsNote && <p className="settings-section-desc">{modelsNote}</p>}
          </div>
        ))}

      <div className="editor-model-actions">
        <button
          type="submit"
          className="settings-link-btn editor-model-save"
          disabled={saving || !dirty || (mode === 'own' && (noKeys || !model.trim()))}
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
        <a
          className="editor-model-billing"
          href={usagePath ? platformHref(usagePath) : undefined}
          target="_blank"
          rel="noopener noreferrer"
          onClick={usagePath ? onPlatformLinkClick(usagePath) : undefined}
        >
          Usage and billing
        </a>
      </div>
      {status && (
        <p className={`settings-section-desc${status.ok ? '' : ' editor-model-error'}`} role="status">
          {status.text}
        </p>
      )}
    </form>
  );
}
