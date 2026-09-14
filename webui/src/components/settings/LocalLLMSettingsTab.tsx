import { Download, Cpu, CheckCircle, Loader2, XCircle, Square } from 'lucide-react';
import { useEffect, useState, useCallback, type ReactElement } from 'react';
import { ApiService } from '../../services/api';
import type { LocalLLMStatus, LocalLLMModel } from '../../services/api/types/common';

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export function LocalLLMSettingsTab(): ReactElement {
  const [status, setStatus] = useState<LocalLLMStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [message, setMessage] = useState('');

  // Any active download switches the status poll from 15s to 2s so the
  // progress bars feel live.
  const anyDownloading = !!status?.models.some((m) => m.download?.status === 'downloading');

  const refresh = useCallback(async () => {
    try {
      const s = await ApiService.getInstance().getLocalLLMStatus();
      setStatus(s);
    } catch {
      setStatus(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, anyDownloading ? 2000 : 15000);
    return () => clearInterval(interval);
  }, [refresh, anyDownloading]);

  const handleStart = async (modelId?: string) => {
    setStarting(true);
    setMessage('');
    try {
      const params = modelId ? `?model=${encodeURIComponent(modelId)}` : '';
      const response = await fetch(`/api/local-llm/start${params}`, { method: 'POST' });
      const result = await response.json();
      if (!response.ok) throw new Error(String(result.message || result.error || `HTTP ${response.status}`));
      setMessage(result.status === 'already_running' ? 'Server already running' : 'Server started');
      await refresh();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : String(e));
    } finally {
      setStarting(false);
    }
  };

  const handleDownload = async (modelId: string) => {
    setMessage('');
    try {
      await ApiService.getInstance().downloadLocalLLMModel(modelId);
      await refresh();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : String(e));
    }
  };

  const handleCancel = async (modelId: string) => {
    setMessage('');
    try {
      await ApiService.getInstance().cancelLocalLLMDownload(modelId);
      await refresh();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : String(e));
    }
  };

  if (loading) {
    return (
      <div className="settings-tab-loading">
        <Loader2 size={20} className="spin" />
        Checking local LLM status...
      </div>
    );
  }

  if (!status?.available) {
    return (
      <div className="settings-section">
        <h3 className="settings-section-title">Local LLM</h3>
        <p className="settings-description">
          Local LLM requires Apple Silicon (M-series Mac). Your platform ({status?.platform || 'unknown'}) is not
          supported.
        </p>
      </div>
    );
  }

  return (
    <div className="settings-section">
      <h3 className="settings-section-title">Local LLM</h3>
      <p className="settings-description">
        Run inference fully on-device using Apple MLX. No API key, no network required.
      </p>

      <div className="local-llm-status-row">
        <div className="local-llm-status-item">
          <span className="local-llm-status-label">Platform</span>
          <span className="local-llm-status-value">
            <Cpu size={12} /> {status.platform}
          </span>
        </div>
        <div className="local-llm-status-item">
          <span className="local-llm-status-label">Server</span>
          <span className={`local-llm-status-value ${status.running ? 'status-ok' : 'status-off'}`}>
            {status.running ? (
              <>
                <CheckCircle size={12} /> Running
              </>
            ) : (
              'Stopped'
            )}
          </span>
        </div>
        {status.running && (
          <div className="local-llm-status-item">
            <span className="local-llm-status-label">Endpoint</span>
            <span className="local-llm-status-value">{status.endpoint}</span>
          </div>
        )}
      </div>

      {!status.running && status.model_present && (
        <button type="button" className="settings-action-btn" onClick={() => handleStart()} disabled={starting}>
          {starting ? <Loader2 size={14} className="spin" /> : null}
          {starting ? 'Starting...' : 'Start Local Server'}
        </button>
      )}

      <h4 className="settings-subsection-title">Models</h4>
      <div className="local-llm-models">
        {status.models.map((model) => (
          <ModelCard
            key={model.id}
            model={model}
            recommended={model.id === status.recommended_model}
            running={status.running}
            starting={starting}
            onStart={handleStart}
            onDownload={handleDownload}
            onCancel={handleCancel}
          />
        ))}
      </div>

      {message && <div className="settings-message">{message}</div>}

      <div className="settings-info-note">
        Models are stored in <code>{status.model_dir}</code>. Quantized models use ~2–20 GB of disk space depending on
        model size.
      </div>
    </div>
  );
}

interface ModelCardProps {
  model: LocalLLMModel;
  recommended: boolean;
  running: boolean;
  starting: boolean;
  onStart: (modelId: string) => void;
  onDownload: (modelId: string) => void;
  onCancel: (modelId: string) => void;
}

function ModelCard({
  model,
  recommended,
  running,
  starting,
  onStart,
  onDownload,
  onCancel,
}: ModelCardProps): ReactElement {
  const dl = model.download;
  const downloading = dl?.status === 'downloading';
  const pct =
    downloading && dl.total_bytes ? Math.min(100, Math.round((dl.bytes_downloaded * 100) / dl.total_bytes)) : null;

  return (
    <div className="local-llm-model-card">
      <div className="local-llm-model-info">
        <div className="local-llm-model-name">
          {model.name}
          {recommended && <span className="local-llm-recommended-tag">Recommended</span>}
        </div>
        <div className="local-llm-model-meta">
          {model.size_hint}
          {model.description ? ` — ${model.description}` : ''}
        </div>
        {downloading && (
          <div className="local-llm-download-progress" role="progressbar" aria-label={`Downloading ${model.name}`}>
            <div className="local-llm-download-bar">
              <div
                className="local-llm-download-fill"
                style={{ width: pct !== null ? `${pct}%` : '100%', opacity: pct !== null ? 1 : 0.4 }}
              />
            </div>
            <span className="local-llm-download-label">
              {formatBytes(dl.bytes_downloaded)}
              {dl.total_bytes ? ` / ${formatBytes(dl.total_bytes)}` : ''}
              {pct !== null ? ` (${pct}%)` : ''}
            </span>
          </div>
        )}
        {dl?.status === 'failed' && (
          <div className="local-llm-download-error">
            <XCircle size={12} /> {dl.error || 'Download failed'}
          </div>
        )}
        {dl?.status === 'canceled' && <div className="local-llm-download-error">Download canceled</div>}
      </div>
      <div className="local-llm-model-actions">
        {model.present ? (
          running ? (
            <span className="local-llm-present-badge">
              <CheckCircle size={12} /> Ready
            </span>
          ) : (
            <button
              type="button"
              className="settings-action-btn"
              onClick={() => onStart(model.id)}
              disabled={starting}
              title={`Start the local server with ${model.name}`}
            >
              {starting ? (
                <>
                  <Loader2 size={14} className="spin" /> Starting...
                </>
              ) : (
                'Use'
              )}
            </button>
          )
        ) : downloading ? (
          <button type="button" className="settings-action-btn" onClick={() => onCancel(model.id)}>
            <Square size={12} /> Cancel
          </button>
        ) : (
          <button type="button" className="settings-action-btn" onClick={() => onDownload(model.id)}>
            <Download size={14} /> Download
          </button>
        )}
      </div>
    </div>
  );
}
