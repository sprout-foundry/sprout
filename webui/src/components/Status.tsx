import type { FC } from 'react';
import './Status.css';

interface StatusProps {
  isConnected: boolean;
  position?: 'top' | 'bottom';
  stats?: {
    provider?: string;
    model?: string;
    total_tokens?: number;
    prompt_tokens?: number;
    completion_tokens?: number;
    cached_tokens?: number;
    current_context_tokens?: number;
    max_context_tokens?: number;
    context_usage_percent?: number;
    cache_efficiency?: number;
    total_cost?: number;
    cached_cost_savings?: number;
    last_tps?: number;
    current_iteration?: number;
    max_iterations?: number;
    streaming_enabled?: boolean;
    debug_mode?: boolean;
    context_warning_issued?: boolean;
    uptime?: string;
    query_count?: number;
  };
}

const Status: FC<StatusProps> = ({ isConnected, position = 'top', stats }) => {
  const formatTokens = (tokens: number): string => {
    if (tokens >= 1000000) {
      return `${(tokens / 1000000).toFixed(1)}M`;
    } else if (tokens >= 1000) {
      return `${(tokens / 1000).toFixed(1)}K`;
    }
    return tokens.toString();
  };

  const formatCost = (cost: number): string => {
    return `$${cost.toFixed(4)}`;
  };

  const getContextStatus = () => {
    if (!stats?.context_usage_percent) return 'unknown';
    const percent = stats.context_usage_percent;
    if (percent > 90) return 'critical';
    if (percent > 75) return 'high';
    if (percent > 50) return 'medium';
    return 'low';
  };

  const contextStatus = getContextStatus();

  return (
    <div
      className={`status-bar ${position === 'bottom' ? 'status-bar-bottom' : 'status-bar-top'} ${isConnected ? 'connected' : 'disconnected'}`}
    >
      <div className="status-indicator">
        <span className={`indicator ${isConnected ? 'on' : 'off'}`}></span>
        <span className="status-text">
          {isConnected ? 'Connected to ledit server' : 'Backend not connected - Start with: ./ledit agent'}
        </span>
      </div>

      <div className="status-info">
        {/* Connection Status */}
        <span className={`status-item status-item-primary ${isConnected ? 'connected' : 'disconnected'}`}>
          WebSocket: {isConnected ? 'Live' : 'Offline'}
        </span>

        {!isConnected && (
          <span className="status-item disconnected-help status-item-priority">
            Run: <code>./ledit agent</code> in parent directory
          </span>
        )}

        {isConnected && stats && (
          <>
            {/* Provider and Model */}
            <span className="status-item status-item-priority">
              {stats.provider}:{stats.model}
            </span>

            {/* Token Usage */}
            <span
              className="status-item status-item-priority"
              title={`Prompt: ${formatTokens(stats.prompt_tokens || 0)} | Completion: ${formatTokens(stats.completion_tokens || 0)} | Cached: ${formatTokens(stats.cached_tokens || 0)}`}
            >
              Tokens: {formatTokens(stats.total_tokens || 0)}
            </span>

            {/* Context Usage */}
            <span
              className={`status-item status-item-priority context-${contextStatus}`}
              title={`Current: ${formatTokens(stats.current_context_tokens || 0)} / Max: ${formatTokens(stats.max_context_tokens || 0)}`}
            >
              Context:{' '}
              {stats.context_usage_percent !== undefined && stats.context_usage_percent !== null
                ? `${stats.context_usage_percent.toFixed(1)}%`
                : 'N/A'}
            </span>

            {/* Cache Efficiency */}
            {(stats.cache_efficiency || 0) > 0 && (
              <span className="status-item status-item-secondary" title="Cache efficiency percentage">
                Cache: {stats.cache_efficiency?.toFixed(1)}%
              </span>
            )}

            {/* Cost */}
            <span
              className="status-item status-item-secondary"
              title={`Total: ${formatCost(stats.total_cost || 0)} | Saved: ${formatCost(stats.cached_cost_savings || 0)}`}
            >
              Cost: {formatCost(stats.total_cost || 0)}
            </span>

            {/* TPS */}
            {stats.last_tps && stats.last_tps > 0 && (
              <span className="status-item status-item-secondary" title="Tokens per second">
                TPS: {stats.last_tps.toFixed(1)}
              </span>
            )}

            {/* Iterations */}
            <span
              className="status-item status-item-priority"
              title={`Current: ${stats.current_iteration || 0} / Max: ${stats.max_iterations || 0}`}
            >
              Iter: {stats.current_iteration || 0}/{stats.max_iterations || 0}
            </span>

            {/* Status Indicators */}
            <span className="status-item status-item-secondary">
              {stats.streaming_enabled && (
                <span className="status-badge streaming" title="Streaming enabled">
                  S
                </span>
              )}
              {stats.debug_mode && (
                <span className="status-badge debug" title="Debug mode">
                  D
                </span>
              )}
              {stats.context_warning_issued && (
                <span className="status-badge warning" title="Context limit warning">
                  !
                </span>
              )}
            </span>
          </>
        )}
      </div>
    </div>
  );
};

export default Status;
