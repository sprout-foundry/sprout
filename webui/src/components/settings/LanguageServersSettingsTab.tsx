import { Trash2, Plus, Save, X } from 'lucide-react';
import { useState } from 'react';
import type { SproutSettings } from '../../services/api';
import { showThemedConfirm } from '../ThemedDialog';
import ListFilter from './ListFilter';

interface LanguageServerOverride {
  id: string;
  binary: string;
  args?: string[];
  language_ids?: string[];
  install_hint?: string;
}

interface LanguageServersSettingsTabProps {
  settings: SproutSettings | null;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

interface DraftForm {
  id: string;
  binary: string;
  args: string;
  language_ids: string;
  install_hint: string;
}

const FILTER_THRESHOLD = 4;

const emptyDraft: DraftForm = { id: '', binary: '', args: '', language_ids: '', install_hint: '' };

function toCSV(arr?: string[]): string {
  return (arr ?? []).join(', ');
}

function fromCSV(s: string): string[] {
  return s
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t.length > 0);
}

export default function LanguageServersSettingsTab({ settings, updateSetting }: LanguageServersSettingsTabProps) {
  const servers = ((settings as unknown as { language_servers?: LanguageServerOverride[] } | null)?.language_servers ??
    []) as LanguageServerOverride[];

  const [editingIdx, setEditingIdx] = useState<number | null>(null);
  const [draft, setDraft] = useState<DraftForm>(emptyDraft);
  const [adding, setAdding] = useState(false);
  const [serverFilter, setServerFilter] = useState('');
  const normalizedFilter = serverFilter.trim().toLowerCase();
  const filteredServers = normalizedFilter
    ? servers.filter(
        (s) =>
          s.id.toLowerCase().includes(normalizedFilter) ||
          (s.binary || '').toLowerCase().includes(normalizedFilter),
      )
    : servers;

  const persist = (next: LanguageServerOverride[]) => {
    void updateSetting('language_servers', next);
  };

  const startEdit = (idx: number) => {
    const s = servers[idx];
    setEditingIdx(idx);
    setAdding(false);
    setDraft({
      id: s.id,
      binary: s.binary,
      args: toCSV(s.args),
      language_ids: toCSV(s.language_ids),
      install_hint: s.install_hint ?? '',
    });
  };

  const startAdd = () => {
    setAdding(true);
    setEditingIdx(null);
    setDraft(emptyDraft);
  };

  const cancel = () => {
    setAdding(false);
    setEditingIdx(null);
    setDraft(emptyDraft);
  };

  const save = () => {
    if (!draft.id.trim() || !draft.binary.trim()) return;
    const next: LanguageServerOverride = {
      id: draft.id.trim(),
      binary: draft.binary.trim(),
      args: fromCSV(draft.args),
      language_ids: fromCSV(draft.language_ids),
      install_hint: draft.install_hint.trim() || undefined,
    };
    if (editingIdx !== null) {
      const out = [...servers];
      out[editingIdx] = next;
      persist(out);
    } else {
      persist([...servers, next]);
    }
    cancel();
  };

  const remove = async (idx: number) => {
    const target = servers[idx];
    const confirmed = await showThemedConfirm(
      `Remove language server override "${target?.id ?? ''}"? sprout will fall back to the built-in registry for this server.`,
      { title: 'Remove override', type: 'warning', confirmLabel: 'Remove' },
    );
    if (!confirmed) return;
    persist(servers.filter((_, i) => i !== idx));
    if (editingIdx === idx) cancel();
  };

  const formActive = adding || editingIdx !== null;

  return (
    <div className="section">
      <h4>Language Servers</h4>
      <div className="config-help settings-help-spaced">
        Override the binary, arguments, or language IDs that sprout uses for a given LSP server. Overrides apply on top
        of the built-in registry. Leave blank to use the default.
      </div>

      {servers.length === 0 && !formActive && <div className="settings-empty">No language server overrides</div>}

      {servers.length >= FILTER_THRESHOLD && (
        <ListFilter
          value={serverFilter}
          onChange={setServerFilter}
          placeholder={`Filter ${servers.length} servers…`}
          ariaLabel="Filter language servers"
        />
      )}

      {normalizedFilter && filteredServers.length === 0 && (
        <div className="settings-empty">No servers match "{serverFilter}"</div>
      )}

      {filteredServers.length > 0 && (
        <ul className="settings-list settings-help-spaced">
          {filteredServers.map((s) => {
            const idx = servers.indexOf(s);
            return (
              <li key={`${s.id}-${idx}`} className="settings-list-row ls-row">
                <div className="ls-row-body">
                  <div className="ls-row-title">{s.id}</div>
                  <div className="ls-row-cmd">
                    {s.binary} {(s.args ?? []).join(' ')}
                  </div>
                  {s.language_ids && s.language_ids.length > 0 && (
                    <div className="ls-row-langs">Languages: {s.language_ids.join(', ')}</div>
                  )}
                </div>
                <button type="button" className="settings-action-btn" onClick={() => startEdit(idx)}>
                  Edit
                </button>
                <button
                  type="button"
                  className="settings-icon-btn danger"
                  aria-label={`Remove override ${s.id}`}
                  title="Remove override"
                  onClick={() => void remove(idx)}
                >
                  <Trash2 size={14} />
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {formActive ? (
        <div className="ls-form-card">
          <div className="config-item">
            <label htmlFor="ls-id">Server ID</label>
            <input
              id="ls-id"
              type="text"
              className="styled-input"
              placeholder="go, typescript, python…"
              value={draft.id}
              onChange={(e) => setDraft({ ...draft, id: e.target.value })}
            />
          </div>
          <div className="config-item">
            <label htmlFor="ls-binary">Binary path</label>
            <input
              id="ls-binary"
              type="text"
              className="styled-input"
              placeholder="/usr/local/bin/gopls"
              value={draft.binary}
              onChange={(e) => setDraft({ ...draft, binary: e.target.value })}
            />
          </div>
          <div className="config-item">
            <label htmlFor="ls-args">Args (comma-separated)</label>
            <input
              id="ls-args"
              type="text"
              className="styled-input"
              placeholder="--stdio"
              value={draft.args}
              onChange={(e) => setDraft({ ...draft, args: e.target.value })}
            />
          </div>
          <div className="config-item">
            <label htmlFor="ls-langs">Language IDs (comma-separated)</label>
            <input
              id="ls-langs"
              type="text"
              className="styled-input"
              placeholder="go"
              value={draft.language_ids}
              onChange={(e) => setDraft({ ...draft, language_ids: e.target.value })}
            />
          </div>
          <div className="config-item">
            <label htmlFor="ls-hint">Install hint</label>
            <input
              id="ls-hint"
              type="text"
              className="styled-input"
              placeholder="brew install gopls"
              value={draft.install_hint}
              onChange={(e) => setDraft({ ...draft, install_hint: e.target.value })}
            />
          </div>
          <div className="ls-form-actions">
            <button
              type="button"
              className="settings-action-btn"
              onClick={save}
              disabled={!draft.id.trim() || !draft.binary.trim()}
            >
              <Save size={14} /> Save
            </button>
            <button type="button" className="settings-action-btn settings-action-btn--ghost" onClick={cancel}>
              <X size={14} /> Cancel
            </button>
          </div>
        </div>
      ) : (
        <button type="button" className="settings-action-btn" onClick={startAdd}>
          <Plus size={14} /> Add override
        </button>
      )}
    </div>
  );
}
