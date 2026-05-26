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
    </div>
  );
}
