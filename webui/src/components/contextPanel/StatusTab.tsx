import { Activity, Bot, Wrench, FileCode, BarChart3 } from 'lucide-react';
import { formatDurationMs, formatTokens, formatCost } from './helpers';
import type { ChatContextPanelProps, StatusMetrics } from './types';

interface StatusTabProps {
  chatProps: ChatContextPanelProps | null;
  statusMetrics: StatusMetrics;
  liveDurationMs: number | null;
}

export function StatusTab({ chatProps, statusMetrics, liveDurationMs }: StatusTabProps) {
  const chatLastError = chatProps?.lastError ?? null;
  const chatQueryProgress = chatProps?.queryProgress ?? null;
  const chatIsProcessing = chatProps?.isProcessing ?? false;
  const chatMessages = chatProps?.messages ?? [];
  const chatStats = chatProps?.stats ?? null;

  return (
    <div className="context-panel-status">
      <div className="status-section">
        <div className="status-section-title">
          <Activity size={12} /> Processing
        </div>
        <div className="status-row">
          {chatIsProcessing ? (
            <>
              <span className="status-dot-indicator active" />
              <span className="status-label">{chatQueryProgress ? chatQueryProgress.message : 'Working...'}</span>
            </>
          ) : chatLastError ? (
            <>
              <span className="status-dot-indicator error" />
              <span className="status-label">{chatLastError}</span>
            </>
          ) : chatMessages.length === 0 ? (
            <>
              <span className="status-dot-indicator" />
              <span className="status-label">Idle — waiting for input</span>
            </>
          ) : (
            <>
              <span className="status-dot-indicator idle" />
              <span className="status-label">Ready</span>
            </>
          )}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Bot size={12} /> Conversation
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.userMsgs}</span>
            <span className="status-metric-label">User</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.assistantMsgs}</span>
            <span className="status-metric-label">Assistant</span>
          </div>
          {(() => {
            const displayMs = liveDurationMs ?? statusMetrics.duration;
            if (!displayMs || isNaN(displayMs) || displayMs <= 0) return null;
            return (
              <div className="status-metric status-metric-wide">
                <span className="status-metric-value">{formatDurationMs(displayMs)}</span>
                <span className="status-metric-label">Duration</span>
              </div>
            );
          })()}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Wrench size={12} /> Tool Usage
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.completedTools}</span>
            <span className="status-metric-label">Completed</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.failedTools}</span>
            <span className="status-metric-label">Failed</span>
          </div>
          {statusMetrics.activeTools > 0 && (
            <div className="status-metric">
              <span className="status-metric-value status-metric-active">{statusMetrics.activeTools}</span>
              <span className="status-metric-label">Active</span>
            </div>
          )}
        </div>
        {statusMetrics.topTools.length > 0 && (
          <div className="status-tool-bars">
            {statusMetrics.topTools.map(([name, count]) => (
              <div key={name} className="status-tool-bar-row">
                <span className="status-tool-bar-name">{name}</span>
                <div className="status-tool-bar-track">
                  <div
                    className="status-tool-bar-fill"
                    style={{ width: `${(count / statusMetrics.maxToolCount) * 100}%` }}
                  />
                </div>
                <span className="status-tool-bar-count">{count}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <FileCode size={12} /> Changes
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{statusMetrics.filesTouched}</span>
            <span className="status-metric-label">Files</span>
          </div>
          <div className="status-metric">
            <span className={`status-metric-value${statusMetrics.totalAdditions > 0 ? ' status-metric-add' : ''}`}>
              +{statusMetrics.totalAdditions}
            </span>
            <span className="status-metric-label">Added</span>
          </div>
          <div className="status-metric">
            <span className={`status-metric-value${statusMetrics.totalDeletions > 0 ? ' status-metric-del' : ''}`}>
              -{statusMetrics.totalDeletions}
            </span>
            <span className="status-metric-label">Removed</span>
          </div>
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <BarChart3 size={12} /> Token Usage
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{chatStats ? formatTokens(chatStats.total_tokens || 0) : '—'}</span>
            <span className="status-metric-label">Total</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">{chatStats ? formatTokens(chatStats.prompt_tokens || 0) : '—'}</span>
            <span className="status-metric-label">Prompt</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats ? formatTokens(chatStats.completion_tokens || 0) : '—'}
            </span>
            <span className="status-metric-label">Completion</span>
          </div>
          {(chatStats?.cached_tokens || 0) > 0 && (
            <div className="status-metric">
              <span className="status-metric-value">{formatTokens(chatStats?.cached_tokens || 0)}</span>
              <span className="status-metric-label">Cached</span>
            </div>
          )}
        </div>
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Activity size={12} /> Context Window
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats?.context_usage_percent != null ? `${chatStats.context_usage_percent.toFixed(1)}%` : '—'}
            </span>
            <span className="status-metric-label">Used</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats ? formatTokens(chatStats.current_context_tokens || 0) : '—'}
            </span>
            <span className="status-metric-label">Current</span>
          </div>
          <div className="status-metric">
            <span className="status-metric-value">
              {chatStats ? formatTokens(chatStats.max_context_tokens || 0) : '—'}
            </span>
            <span className="status-metric-label">Max</span>
          </div>
        </div>
        {chatStats && chatStats.context_usage_percent != null && (
          <div className="status-context-bar">
            <div
              className={`status-context-bar-fill ${
                chatStats.context_usage_percent > 90 ? 'critical' : chatStats.context_usage_percent > 75 ? 'high' : ''
              }`}
              style={{
                width: `${Math.max(0, Math.min(100, chatStats.context_usage_percent))}%`,
              }}
            />
          </div>
        )}
      </div>

      <div className="status-section">
        <div className="status-section-title">
          <Activity size={12} /> Costs
        </div>
        <div className="status-metrics-grid">
          <div className="status-metric">
            <span className="status-metric-value">{chatStats ? formatCost(chatStats.total_cost || 0) : '—'}</span>
            <span className="status-metric-label">Total Cost</span>
          </div>
          {(chatStats?.cached_cost_savings || 0) > 0 && (
            <div className="status-metric">
              <span className="status-metric-value">{formatCost(chatStats?.cached_cost_savings || 0)}</span>
              <span className="status-metric-label">Cache Savings</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
