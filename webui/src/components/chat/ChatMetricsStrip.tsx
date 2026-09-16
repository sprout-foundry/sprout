import { getPersonaColor } from '@sprout/ui';
import { Cloud, Cpu, Server } from 'lucide-react';
import { useProviderCatalog } from '../../contexts/ProviderCatalogContext';
import './ChatMetricsStrip.css';

/**
 * Per-chat LLM metrics strip, rendered inside the chat shell above the
 * input (SP-053-3a content, relocated from the app-wide footers).
 *
 * Why inside the chat: the metrics describe THIS chat's session —
 * provider, model, persona, context usage, cost. Showing them under a
 * file the user is editing is noise, and with multiple chats each chat
 * needs its own strip, fed by its own stats blob.
 *
 * Cost color thresholds match the CLI footer: yellow above $1, red
 * above $5. The model name is a button when onModelClick is supplied
 * (opens the model picker, CLI /model parity).
 */

interface ChatMetricsStripProps {
  /** This chat's live stats blob (metrics_update event payload). */
  stats?: Record<string, unknown> | null;
  /** WebSocket transport state — drives the disconnected pill. */
  isConnected?: boolean;
  /** Clicking the model name opens the picker scoped to this provider. */
  onModelClick?: (provider: string) => void;
}

const COST_WARN = 1.0;
const COST_ALERT = 5.0;

function formatCost(cost: number): string {
  if (!Number.isFinite(cost)) return '—';
  // More precision for tiny values so users see the cost moving.
  if (cost < 0.01) return `$${cost.toFixed(4)}`;
  if (cost < 1.0) return `$${cost.toFixed(3)}`;
  return `$${cost.toFixed(2)}`;
}

function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

function costClass(cost: number): string {
  if (cost >= COST_ALERT) return 'chat-metrics-cost--alert';
  if (cost >= COST_WARN) return 'chat-metrics-cost--warn';
  return '';
}

function formatPersonaLabel(id: string): string {
  return id.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

/** Tiny lucide glyph keyed off provider name. Generic on purpose —
 * per-brand icons are a follow-up. */
function ProviderIcon({ provider }: { provider?: string }): JSX.Element | null {
  if (!provider) return null;
  const p = provider.toLowerCase();
  if (p.includes('ollama') || p.includes('local') || p === 'localhost') {
    return <Cpu size={11} aria-hidden="true" />;
  }
  if (p.includes('server') || p.includes('custom') || p.includes('self')) {
    return <Server size={11} aria-hidden="true" />;
  }
  return <Cloud size={11} aria-hidden="true" />;
}

export function ChatMetricsStrip({ stats, isConnected, onModelClick }: ChatMetricsStripProps): JSX.Element | null {
  const { getProviderName } = useProviderCatalog();

  const provider = typeof stats?.provider === 'string' ? stats.provider : '';
  const model = typeof stats?.model === 'string' ? stats.model : '';
  // The metrics event carries the raw provider id (`openrouter`); resolve
  // through the shared catalog so the label matches Settings dropdowns.
  const providerDisplay = getProviderName(provider) || provider;
  const persona = typeof stats?.persona === 'string' ? stats.persona : '';
  const totalTokens = Number(stats?.total_tokens ?? NaN);
  const contextPercent = Number(stats?.context_usage_percent ?? NaN);
  const currentCtx = Number(stats?.current_context_tokens ?? NaN);
  const maxCtx = Number(stats?.max_context_tokens ?? NaN);
  const totalCost = Number(stats?.total_cost ?? NaN);
  const connectionPhase =
    (stats?.connection_phase as string | undefined) || (isConnected ? 'connected' : 'disconnected');

  const hasAny =
    provider ||
    model ||
    persona ||
    Number.isFinite(totalTokens) ||
    Number.isFinite(contextPercent) ||
    Number.isFinite(currentCtx) ||
    Number.isFinite(totalCost);
  // Nothing populated and connected — render nothing rather than an
  // empty stub. The disconnected pill is worth showing on its own.
  if (!hasAny && isConnected !== false) return null;

  const segments: JSX.Element[] = [];

  if (isConnected === false) {
    segments.push(
      <span
        key="conn"
        className="chat-metrics-item chat-metrics-conn chat-metrics-conn--off"
        title="WebSocket disconnected — events will resume on reconnect"
      >
        <span className="chat-metrics-conn-dot" aria-hidden="true" />
        disconnected
      </span>,
    );
  }

  if (persona) {
    segments.push(
      <span
        key="persona"
        className="chat-metrics-item chat-metrics-persona"
        style={persona !== 'orchestrator' ? { color: getPersonaColor(persona) } : undefined}
        title={`Active persona: ${persona}`}
      >
        {formatPersonaLabel(persona)}
      </span>,
    );
  }

  const showModel = (provider || model) && (isConnected === false || (persona && persona !== 'orchestrator'));
  if (showModel) {
    const modelLabel = model || providerDisplay;
    const tooltip = onModelClick
      ? `${providerDisplay} · ${model} — click to change model`
      : `${providerDisplay} · ${model}`;
    segments.push(
      <span key="provider" className="chat-metrics-item chat-metrics-model" title={tooltip}>
        <ProviderIcon provider={provider} />
        {onModelClick ? (
          <button
            type="button"
            className="chat-metrics-model-button"
            onClick={() => onModelClick(provider)}
            aria-label={`Change model (currently ${modelLabel})`}
          >
            {modelLabel}
          </button>
        ) : (
          <span>{modelLabel}</span>
        )}
      </span>,
    );
  }

  if (Number.isFinite(contextPercent)) {
    segments.push(
      <span key="ctxpct" className="chat-metrics-item" title="Context usage">
        {contextPercent.toFixed(1)}%
      </span>,
    );
  } else if (Number.isFinite(currentCtx) && Number.isFinite(maxCtx) && maxCtx > 0) {
    segments.push(
      <span key="ctx" className="chat-metrics-item" title="Context usage">
        {formatTokens(currentCtx)}/{formatTokens(maxCtx)} ctx
      </span>,
    );
  }
  if (Number.isFinite(totalTokens)) {
    segments.push(
      <span key="tok" className="chat-metrics-item" title="Total tokens">
        {formatTokens(totalTokens)} tok
      </span>,
    );
  }

  if (Number.isFinite(totalCost)) {
    segments.push(
      <span
        key="cost"
        className={`chat-metrics-item chat-metrics-cost ${costClass(totalCost)}`}
        data-testid="status-bar-cost"
        title="Session cost"
      >
        {formatCost(totalCost)}
      </span>,
    );
  }

  segments.push(
    <span key="link" className="chat-metrics-item chat-metrics-link" title="Connection state">
      Link: {connectionPhase}
    </span>,
  );

  const out: JSX.Element[] = [];
  segments.forEach((seg, i) => {
    if (i > 0) {
      out.push(
        <span key={`sep-${i}`} className="chat-metrics-sep" aria-hidden="true">
          ·
        </span>,
      );
    }
    out.push(seg);
  });

  return (
    <div className="chat-metrics-strip" data-testid="chat-metrics-strip">
      {out}
    </div>
  );
}

export default ChatMetricsStrip;
