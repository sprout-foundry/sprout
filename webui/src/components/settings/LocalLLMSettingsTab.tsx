import { Download, Cpu, CheckCircle, Loader2 } from 'lucide-react';
import { useEffect, useState, useCallback, type ReactElement } from 'react';
import { ApiService } from '../../services/api';
import type { LocalLLMStatus } from '../../services/api/types/common';

export function LocalLLMSettingsTab(): ReactElement {
  const [status, setStatus] = useState<LocalLLMStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [downloading, setDownloading] = useState<string | null>(null);
  const [message, setMessage] = useState('');

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
    const interval = setInterval(refresh, 15000);
    return () => clearInterval(interval);
  }, [refresh]);

  const handleStart = async () => {
    setStarting(true);
    setMessage('');
    try {
      const result = await ApiService.getInstance().startLocalLLM();
      setMessage(result.status === 'already_running' ? 'Server already running' : 'Server started');
      await refresh();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : String(e));
    } finally {
      setStarting(false);
    }
  };

  const handleDownload = async (modelId: string) => {
    setDownloading(modelId);
    setMessage('');
    try {
      const result = await ApiService.getInstance().downloadLocalLLMModel(modelId);
      setMessage(result.message);
      // Poll for completion
      const poll = setInterval(async () => {
        const s = await ApiService.getInstance().getLocalLLMStatus();
        setStatus(s);
        if (s.models.some((m: typeof s.models[number]) => m.id === modelId && m.present)) {
          setDownloading(null);
          setMessage(`Downloaded ${modelId}`);
          clearInterval(poll);
        }
      }, 5000);
      setTimeout(() => clearInterval(poll), 600000); // 10min timeout
    } catch (e) {
      setMessage(e instanceof Error ? e.message : String(e));
      setDownloading(null);
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
          Local LLM requires Apple Silicon (M-series Mac). Your platform ({status?.platform || 'unknown'}) is not supported.
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
              <><CheckCircle size={12} /> Running</>
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
        <button
          type="button"
          className="settings-action-btn"
          onClick={handleStart}
          disabled={starting}
        >
          {starting ? <Loader2 size={14} className="spin" /> : null}
          {starting ? 'Starting...' : 'Start Local Server'}
        </button>
      )}

      <h4 className="settings-subsection-title">Models</h4>
      <div className="local-llm-models">
        {status.models.map((model: typeof status.models[number]) => (
          <div key={model.id} className="local-llm-model-card">
            <div className="local-llm-model-info">
              <div className="local-llm-model-name">{model.name}</div>
              <div className="local-llm-model-meta">
                {model.size_hint}
                {model.id === status.recommended_model && <span className="local-llm-recommended-tag">Recommended</span>}
              </div>
            </div>
            <div className="local-llm-model-actions">
              {model.present ? (
                <span className="local-llm-present-badge">
                  <CheckCircle size={12} /> Ready
                </span>
              ) : (
                <button
                  type="button"
                  className="settings-action-btn"
                  onClick={() => handleDownload(model.id)}
                  disabled={downloading !== null}
                >
                  {downloading === model.id ? (
                    <><Loader2 size={14} className="spin" /> Downloading...</>
                  ) : (
                    <><Download size={14} /> Download</>
                  )}
                </button>
              )}
            </div>
          </div>
        ))}
      </div>

      {message && <div className="settings-message">{message}</div>}

      <div className="settings-info-note">
        Models are stored in <code>{status.model_dir}</code>.
        Quantized models use ~2–6 GB of disk space.
      </div>
    </div>
  );
}
