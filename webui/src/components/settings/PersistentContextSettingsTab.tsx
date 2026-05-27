import { useState } from 'react';
import { Search, Loader2 } from 'lucide-react';
import type { SproutSettings } from '../../services/api';

interface PersistentContextSettings {
  proactiveContextEnabled?: boolean;
  maxContextualResults?: number;
  minRelevanceScore?: number;
  maxContextChars?: number;
  workspaceScopedRetrieval?: boolean;
  driftDetectionEnabled?: boolean;
  driftThreshold?: number;
  driftCheckInterval?: number;
  retentionDays?: number;
}

interface PersistentContextSettingsTabProps {
  settings: SproutSettings | null;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

const DEFAULTS: Required<PersistentContextSettings> = {
  proactiveContextEnabled: true,
  maxContextualResults: 5,
  minRelevanceScore: 0.5,
  maxContextChars: 4000,
  workspaceScopedRetrieval: true,
  driftDetectionEnabled: true,
  driftThreshold: 0.6,
  driftCheckInterval: 5,
  retentionDays: 0,
};

export default function PersistentContextSettingsTab({ settings, updateSetting }: PersistentContextSettingsTabProps) {
  const pc = (settings as unknown as { persistent_context?: PersistentContextSettings } | null)?.persistent_context ?? {};

  const get = <K extends keyof Required<PersistentContextSettings>>(k: K): Required<PersistentContextSettings>[K] => {
    const v = pc[k];
    return (v ?? DEFAULTS[k]) as Required<PersistentContextSettings>[K];
  };

  const update = (next: PersistentContextSettings) => {
    void updateSetting('persistent_context', { ...pc, ...next });
  };

  return (
    <div className="section">
      <h4>Memory & Context</h4>
      <div className="config-help" style={{ marginBottom: 'var(--space-3)' }}>
        Controls how sprout primes new chats with relevant prior turns and detects topic drift. Stored under
        <code> persistent_context </code>in config.json.
      </div>

      <div className="config-item">
        <label className="styled-toggle">
          <input
            type="checkbox"
            checked={get('proactiveContextEnabled')}
            onChange={(e) => update({ proactiveContextEnabled: e.target.checked })}
          />
          <span className="toggle-track" />
          <span className="toggle-label">Proactive context retrieval</span>
        </label>
        <div className="config-help">Inject semantically-relevant past turns when a new session starts.</div>
      </div>

      <div className="config-item">
        <label className="styled-toggle">
          <input
            type="checkbox"
            checked={get('workspaceScopedRetrieval')}
            onChange={(e) => update({ workspaceScopedRetrieval: e.target.checked })}
          />
          <span className="toggle-track" />
          <span className="toggle-label">Workspace-scoped retrieval</span>
        </label>
        <div className="config-help">Limit retrieval to turns from the current workspace only.</div>
      </div>

      <div className="config-item">
        <label htmlFor="pc-max-results">Max retrieved turns</label>
        <input
          id="pc-max-results"
          type="number"
          min={0}
          max={50}
          className="styled-input config-row-input"
          value={get('maxContextualResults')}
          onChange={(e) => update({ maxContextualResults: Math.max(0, Number(e.target.value) || 0) })}
        />
        <div className="config-help">How many past turns to retrieve at most (default 5).</div>
      </div>

      <div className="config-item">
        <label htmlFor="pc-min-score">Minimum relevance score</label>
        <input
          id="pc-min-score"
          type="number"
          min={0}
          max={1}
          step={0.05}
          className="styled-input config-row-input"
          value={get('minRelevanceScore')}
          onChange={(e) => update({ minRelevanceScore: Math.max(0, Math.min(1, Number(e.target.value) || 0)) })}
        />
        <div className="config-help">Cosine-similarity floor for retrieved turns. 0.0–1.0 (default 0.50).</div>
      </div>

      <div className="config-item">
        <label htmlFor="pc-max-chars">Max injected characters</label>
        <input
          id="pc-max-chars"
          type="number"
          min={0}
          className="styled-input config-row-input"
          value={get('maxContextChars')}
          onChange={(e) => update({ maxContextChars: Math.max(0, Number(e.target.value) || 0) })}
        />
        <div className="config-help">Hard cap on total chars injected as context (default 4000).</div>
      </div>

      <div className="config-item">
        <label htmlFor="pc-retention">Retention (days)</label>
        <input
          id="pc-retention"
          type="number"
          min={0}
          className="styled-input config-row-input"
          value={get('retentionDays')}
          onChange={(e) => update({ retentionDays: Math.max(0, Number(e.target.value) || 0) })}
        />
        <div className="config-help">Discard memory older than N days at startup. 0 disables cleanup (default).</div>
      </div>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <h4>Drift Detection</h4>

        <div className="config-item">
          <label className="styled-toggle">
            <input
              type="checkbox"
              checked={get('driftDetectionEnabled')}
              onChange={(e) => update({ driftDetectionEnabled: e.target.checked })}
            />
            <span className="toggle-track" />
            <span className="toggle-label">Enable drift detection</span>
          </label>
          <div className="config-help">Flag when the conversation drifts from its original intent.</div>
        </div>

        <div className="config-item">
          <label htmlFor="pc-drift-threshold">Drift threshold</label>
          <input
            id="pc-drift-threshold"
            type="number"
            min={0}
            max={1}
            step={0.05}
            className="styled-input config-row-input"
            value={get('driftThreshold')}
            onChange={(e) => update({ driftThreshold: Math.max(0, Math.min(1, Number(e.target.value) || 0)) })}
          />
          <div className="config-help">Similarity floor below which drift is flagged. 0.0–1.0 (default 0.60).</div>
        </div>

        <div className="config-item">
          <label htmlFor="pc-drift-interval">Drift check interval (turns)</label>
          <input
            id="pc-drift-interval"
            type="number"
            min={1}
            className="styled-input config-row-input"
            value={get('driftCheckInterval')}
            onChange={(e) => update({ driftCheckInterval: Math.max(1, Number(e.target.value) || 1) })}
          />
          <div className="config-help">Check every N turns (default 5).</div>
        </div>
      </div>

      <PreviewRetrievalPanel />
    </div>
  );
}

interface PreviewResult {
  user_message: string;
  summary: string;
  workspace: string;
  score: number;
  relative_time: string;
}

interface PreviewResponse {
  query: string;
  workspace: string;
  enabled: boolean;
  config: {
    min_relevance_score: number;
    max_contextual_results: number;
    max_context_chars: number;
    workspace_scoped_retrieval: boolean;
  };
  results: PreviewResult[];
  note?: string;
}

/**
 * Hits /api/search/semantic/preview-context to show what proactive context the
 * agent would inject *right now* given the saved Memory settings. Read-only
 * — does not mutate state. Lets users tune MinRelevanceScore and see the
 * effect before they commit to it.
 */
function PreviewRetrievalPanel() {
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<PreviewResponse | null>(null);

  const run = async () => {
    const q = query.trim();
    if (!q) return;
    setLoading(true);
    setError(null);
    setPreview(null);
    try {
      const r = await fetch(`/api/search/semantic/preview-context?query=${encodeURIComponent(q)}`);
      if (!r.ok) {
        const body = await r.text();
        throw new Error(`HTTP ${r.status}: ${body || r.statusText}`);
      }
      const data = (await r.json()) as PreviewResponse;
      setPreview(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ marginTop: 'var(--space-6)' }}>
      <h4>Preview retrieval</h4>
      <div className="config-help" style={{ marginBottom: 'var(--space-3)' }}>
        See exactly which past turns the saved settings above would inject for a query, so you can tune the
        relevance score / result count before committing.
      </div>

      <div style={{ display: 'flex', gap: 'var(--space-2)', marginBottom: 'var(--space-3)' }}>
        <input
          type="text"
          className="styled-input"
          placeholder="e.g. how did we wire embeddings into the webui?"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              void run();
            }
          }}
          style={{ flex: 1 }}
          disabled={loading}
        />
        <button
          type="button"
          className="settings-action-btn"
          onClick={() => void run()}
          disabled={loading || query.trim().length === 0}
        >
          {loading ? <Loader2 size={14} className="spinning" /> : <Search size={14} />}
          {loading ? 'Searching…' : 'Preview'}
        </button>
      </div>

      {error && (
        <div
          style={{
            padding: 'var(--space-2) var(--space-3)',
            background: 'color-mix(in srgb, var(--accent-error) 12%, transparent)',
            border: '1px solid var(--accent-error)',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--accent-error)',
            fontSize: 'var(--text-xs)',
            marginBottom: 'var(--space-3)',
          }}
        >
          {error}
        </div>
      )}

      {preview && (
        <div>
          {preview.note && (
            <div
              style={{
                padding: 'var(--space-2) var(--space-3)',
                background: 'color-mix(in srgb, var(--accent-warning) 12%, transparent)',
                border: '1px solid var(--accent-warning)',
                borderRadius: 'var(--radius-sm)',
                color: 'var(--accent-warning-fg)',
                fontSize: 'var(--text-xs)',
                marginBottom: 'var(--space-3)',
              }}
            >
              {preview.note}
            </div>
          )}

          <div
            style={{
              fontSize: 'var(--text-xs)',
              color: 'var(--text-tertiary)',
              marginBottom: 'var(--space-2)',
            }}
          >
            score ≥ {preview.config.min_relevance_score.toFixed(2)} · top{' '}
            {preview.config.max_contextual_results} · workspace-scoped:{' '}
            {preview.config.workspace_scoped_retrieval ? 'yes' : 'no'}
          </div>

          {preview.results.length === 0 ? (
            <div className="settings-empty">No retrievals matched. Lower the relevance score or try a different query.</div>
          ) : (
            <ol style={{ listStyle: 'none', padding: 0, margin: 0 }}>
              {preview.results.map((r, idx) => (
                <li
                  key={idx}
                  style={{
                    padding: 'var(--space-3)',
                    background: 'var(--bg-elevated)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-sm)',
                    marginBottom: 'var(--space-2)',
                  }}
                >
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      gap: 'var(--space-3)',
                      marginBottom: 'var(--space-1)',
                    }}
                  >
                    <span style={{ color: 'var(--text-primary)', fontWeight: 500 }}>
                      #{idx + 1} · score {r.score.toFixed(3)}
                    </span>
                    <span style={{ fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)' }}>
                      {r.relative_time}
                    </span>
                  </div>
                  <div
                    style={{
                      fontSize: 'var(--text-xs)',
                      color: 'var(--text-secondary)',
                      marginBottom: 'var(--space-1)',
                    }}
                  >
                    <strong>User:</strong> {r.user_message}
                  </div>
                  {r.summary && (
                    <div style={{ fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)' }}>
                      <strong>Summary:</strong> {r.summary}
                    </div>
                  )}
                  {r.workspace && (
                    <div
                      style={{
                        fontSize: 'var(--text-xs)',
                        fontFamily: 'var(--font-mono)',
                        color: 'var(--text-muted)',
                        marginTop: 'var(--space-1)',
                      }}
                    >
                      {r.workspace}
                    </div>
                  )}
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
    </div>
  );
}
