import { Loader2, Database, CheckCircle2, AlertTriangle, XCircle, RefreshCw } from 'lucide-react';
import { useEffect, useState, useCallback } from 'react';
import { ApiService } from '../../services/api';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface EmbeddingStatus {
  available: boolean;
  initialized: boolean;
  building: boolean;
  record_count: number;
  init_error?: string;
}

interface EmbeddingSettingsTabProps {
  renderToggle: FieldRenderers['renderToggle'];
  renderTextInput: FieldRenderers['renderTextInput'];
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

export default function EmbeddingSettingsTab({
  renderToggle,
  renderTextInput,
  updateSetting,
}: EmbeddingSettingsTabProps) {
  const apiService = ApiService.getInstance();
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);
  const [isRebuilding, setIsRebuilding] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
      <div className="settings-card" style={{ marginBottom: 'var(--space-8)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-4)', marginBottom: 'var(--space-4)' }}>
          <Database size={16} />
          <span style={{ fontWeight: 500 }}>Index Status</span>
        </div>
        {status === null ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: '13px' }}>Unable to fetch status</div>
        ) : status.building || isRebuilding ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--accent-primary)' }}>
            <Loader2 size={14} className="spinning" />
            <span style={{ fontSize: '13px' }}>Building index...</span>
          </div>
        ) : status.initialized ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--accent-success)' }}>
            <CheckCircle2 size={14} />
            <span style={{ fontSize: '13px' }}>{status.record_count.toLocaleString()} functions indexed</span>
          </div>
        ) : status.available && status.init_error ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--accent-error)' }}>
            <AlertTriangle size={14} />
            <span style={{ fontSize: '13px' }}>Initialization failed: {status.init_error}</span>
          </div>
        ) : status.available ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--text-secondary)' }}>
            <AlertTriangle size={14} />
            <span style={{ fontSize: '13px' }}>Not initialized — will build on next startup or search</span>
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--accent-error)' }}>
            <XCircle size={14} />
            <span style={{ fontSize: '13px' }}>Failed to initialize embedding provider</span>
          </div>
        )}
      </div>

      {/* Model Info */}
      <div className="settings-card" style={{ marginBottom: '16px' }}>
        <div style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
          <div style={{ marginBottom: '4px' }}>
            <span style={{ color: 'var(--text-primary)' }}>Provider:</span> bge-base-en-v1.5-256d
          </div>
          <div style={{ marginBottom: '4px' }}>
            <span style={{ color: 'var(--text-primary)' }}>Model:</span> bge-base-en-v1.5 (INT8 quantized)
          </div>
          <div>
            <span style={{ color: 'var(--text-primary)' }}>Dimensions:</span> 256
          </div>
        </div>
      </div>

      {/* Configuration */}
      {renderToggle('embedding_index.enabled', 'Enable embedding index')}
      {renderToggle('embedding_index.auto_index', 'Auto-build on startup')}
      {renderTextInput('embedding_index.similarity_threshold', 'Similarity threshold', '0.0 – 1.0')}
      {renderTextInput('embedding_index.max_results', 'Max duplicate results', '1 – 10')}

      {/* Rebuild Action — uses the now-defined .settings-action-btn CSS
       * for visual consistency with other tab-local action buttons. Inline
       * style hooks dropped now that the class carries the look. */}
      <div style={{ marginTop: 'var(--space-6)', display: 'flex', alignItems: 'center', gap: 'var(--space-4)' }}>
        <button
          className="settings-action-btn"
          type="button"
          onClick={handleRebuild}
          disabled={isRebuilding || status?.building}
        >
          {isRebuilding || status?.building ? <Loader2 size={14} className="spinning" /> : <RefreshCw size={14} />}
          {isRebuilding || status?.building ? 'Building...' : 'Rebuild Index'}
        </button>
        {error && <span style={{ color: 'var(--accent-error)', fontSize: '12px' }}>{error}</span>}
      </div>
    </div>
  );
}
