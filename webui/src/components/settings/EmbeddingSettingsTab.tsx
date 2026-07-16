import { Loader2, Database, CheckCircle2, AlertTriangle, XCircle, RefreshCw } from 'lucide-react';
import { useEffect, useState, useCallback, useRef } from 'react';
import { ApiService, type SproutSettings } from '../../services/api';
import { getNestedValue } from './settingsHelpers';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface EmbeddingStatus {
  available: boolean;
  initialized: boolean;
  building: boolean;
  record_count: number;
  init_error?: string;
}

interface EmbeddingSettingsTabProps {
  settings: SproutSettings | null;
  renderToggle: FieldRenderers['renderToggle'];
  renderTextInput: FieldRenderers['renderTextInput'];
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

export default function EmbeddingSettingsTab({
  settings,
  renderToggle,
  renderTextInput,
  updateSetting,
}: EmbeddingSettingsTabProps) {
  const apiService = ApiService.getInstance();
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);
  const [isRebuilding, setIsRebuilding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const rawExclude = settings ? (getNestedValue(settings, 'embedding_index.exclude_paths') as unknown) : [];
  const persistedExclude: string[] = Array.isArray(rawExclude) ? rawExclude : [];
  const [excludeDraft, setExcludeDraft] = useState<string>(persistedExclude.join('\n'));
  const excludeSaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastPersistedJoinedRef = useRef<string>(persistedExclude.join('\n'));

  // Sync local draft when settings refresh from the server (e.g., scope switch).
  useEffect(() => {
    const joined = persistedExclude.join('\n');
    if (joined !== lastPersistedJoinedRef.current) {
      lastPersistedJoinedRef.current = joined;
      setExcludeDraft(joined);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [persistedExclude.join('\n')]);

  const commitExcludePaths = (raw: string) => {
    const list = raw
      .split('\n')
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
    lastPersistedJoinedRef.current = list.join('\n');
    void updateSetting('embedding_index.exclude_paths', list);
  };

  const fetchStatus = useCallback(async (): Promise<boolean> => {
    try {
      const s = await apiService.searchSemanticStatus();
      setStatus(s);
      return s.building;
    } catch {
      setStatus(null);
      return false;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Fetch status on mount and poll while building
  useEffect(() => {
    let cancelled = false;

    const init = async () => {
      const isBuilding = await fetchStatus();
      if (cancelled) return;

      if (isBuilding) {
        const poll = async () => {
          if (cancelled) return;
          const stillBuilding = await fetchStatus();
          if (stillBuilding && !cancelled) {
            setTimeout(poll, 2000);
          }
        };
        setTimeout(poll, 2000);
      }
    };

    init();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRebuild = async () => {
    setIsRebuilding(true);
    setError(null);

    const poll = async () => {
      try {
        await apiService.searchSemanticBuild();
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to start rebuild');
        setIsRebuilding(false);
        return;
      }

      // Poll until done
      const check = async () => {
        try {
          const s = await apiService.searchSemanticStatus();
          setStatus(s);
          if (s.building) {
            setTimeout(check, 2000);
          } else {
            setIsRebuilding(false);
          }
        } catch {
          setIsRebuilding(false);
        }
      };
      setTimeout(check, 1000);
    };

    poll();
  };

  return (
    <div className="section">
      <h4>Embedding Index</h4>

      {/* Status Card */}
      <div className="settings-card embedding-status-card">
        <div className="embedding-status-header">
          <Database size={16} />
          <span className="embedding-status-title">Index Status</span>
        </div>
        {status === null ? (
          <div className="embedding-status-row">Unable to fetch status</div>
        ) : status.building || isRebuilding ? (
          <div className="embedding-status-row embedding-status-row--info">
            <Loader2 size={14} className="spinning" />
            <span>Building index...</span>
          </div>
        ) : status.initialized ? (
          <div className="embedding-status-row embedding-status-row--success">
            <CheckCircle2 size={14} />
            <span>{status.record_count.toLocaleString()} functions indexed</span>
          </div>
        ) : status.available && status.init_error ? (
          <div className="embedding-status-row embedding-status-row--error">
            <AlertTriangle size={14} />
            <span>Initialization failed: {status.init_error}</span>
          </div>
        ) : status.available ? (
          <div className="embedding-status-row embedding-status-row--muted">
            <AlertTriangle size={14} />
            <span>Not initialized — will build on next startup or search</span>
          </div>
        ) : (
          <div className="embedding-status-row embedding-status-row--error">
            <XCircle size={14} />
            <span>Failed to initialize embedding provider</span>
          </div>
        )}
      </div>

      {/* Model Info */}
      <div className="settings-card embedding-model-card">
        <div className="embedding-model-row">
          <span className="embedding-model-label">Provider:</span> bge-base-en-v1.5-256d
        </div>
        <div className="embedding-model-row">
          <span className="embedding-model-label">Model:</span> bge-base-en-v1.5 (INT8 quantized)
        </div>
        <div className="embedding-model-row">
          <span className="embedding-model-label">Dimensions:</span> 256
        </div>
      </div>

      {/* Configuration */}
      {renderToggle('embedding_index.enabled', 'Enable embedding index')}
      {renderToggle('embedding_index.auto_index', 'Auto-build on startup')}
      {renderTextInput('embedding_index.similarity_threshold', 'Similarity threshold', '0.0 – 1.0')}
      {renderTextInput('embedding_index.max_results', 'Max duplicate results', '1 – 10')}

      <div className="config-item">
        <label htmlFor="setting-embedding-exclude-paths">Exclude paths</label>
        <textarea
          id="setting-embedding-exclude-paths"
          className="styled-input styled-textarea"
          rows={6}
          value={excludeDraft}
          placeholder={'node_modules\n.git\ndist'}
          onChange={(e) => {
            const next = e.target.value;
            setExcludeDraft(next);
            if (excludeSaveTimer.current) clearTimeout(excludeSaveTimer.current);
            excludeSaveTimer.current = setTimeout(() => {
              excludeSaveTimer.current = null;
              commitExcludePaths(next);
            }, 500);
          }}
          onBlur={() => {
            if (excludeSaveTimer.current) {
              clearTimeout(excludeSaveTimer.current);
              excludeSaveTimer.current = null;
            }
            if (excludeDraft !== lastPersistedJoinedRef.current) {
              commitExcludePaths(excludeDraft);
            }
          }}
        />
        <div className="config-help">
          One path per line. Matching files are skipped when indexing. Lines are trimmed; blank lines are ignored.
        </div>
      </div>

      <div className="embedding-action-row">
        <button
          className="settings-action-btn"
          type="button"
          onClick={handleRebuild}
          disabled={isRebuilding || status?.building}
        >
          {isRebuilding || status?.building ? <Loader2 size={14} className="spinning" /> : <RefreshCw size={14} />}
          {isRebuilding || status?.building ? 'Building...' : 'Rebuild Index'}
        </button>
        {error && <span className="embedding-action-error">{error}</span>}
      </div>
    </div>
  );
}
