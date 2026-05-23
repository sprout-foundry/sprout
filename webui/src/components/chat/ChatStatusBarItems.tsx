import { Cloud, Server, Cpu } from 'lucide-react';
import { getPersonaColor } from '@sprout/ui';
import './ChatStatusBarItems.css';

/**
 * SP-053-3a: chat-context status-bar items (persona · provider · model ·
 * context · cost), mirroring the CLI's status footer at
 * `pkg/console/status_footer.go:281-303`.
 *
 * Cost color thresholds match the CLI: yellow above $1, red above $5.
 * Rendered into the shared `@sprout/ui` StatusBar's `rightItems` slot so the
 * editor's metadata segment is replaced when a chat is active.
 *
 * The model name is rendered as a button when `onModelClick` is supplied —
 * clicking it surfaces the model picker (parity with the CLI's `/model`
 * slash command).
 */

interface ChatStatusBarItemsProps {
  /**
   * The chat's live stats blob — what the WebUI receives via the
   * metrics_update event. Fields are read defensively so a missing or
   * partial payload omits that segment rather than crashing.
   */
  stats?: Record<string, unknown> | null;
  /**
   * If supplied, the model name becomes a clickable button that invokes
   * this callback (typically opens the ModelSelectionModal). Without it
   * the model name renders as plain text.
   */
  onModelClick?: (provider: string) => void;
}

const COST_WARN = 1.0;
const COST_ALERT = 5.0;

/** Format a fractional cost USD value into a compact, two-decimal string. */
function formatCost(cost: number): string {
  if (!Number.isFinite(cost)) return '—';
  // Match the CLI footer's scheme: more precision for tiny values so users
  // see the cost moving rather than staring at $0.00.
  if (cost < 0.01) return `$${cost.toFixed(4)}`;
  if (cost < 1.0) return `$${cost.toFixed(3)}`;
  return `$${cost.toFixed(2)}`;
}

/** Format a token count into k/M with one decimal. */
function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** Pick the right cost-tier CSS class for color emphasis. */
function costClass(cost: number): string {
  if (cost >= COST_ALERT) return 'chat-statusbar-cost--alert';
  if (cost >= COST_WARN) return 'chat-statusbar-cost--warn';
  return '';
}

/** Tiny lucide glyph keyed off provider name. Generic on purpose — we
 * defer per-brand icons (Anthropic / OpenAI / etc.) to a follow-up. */
function ProviderIcon({ provider }: { provider?: string }): JSX.Element | null {
  if (!provider) return null;
  const p = provider.toLowerCase();
  // Local providers
  if (p.includes('ollama') || p.includes('local') || p === 'localhost') {
    return <Cpu size={11} aria-hidden="true" />;
  }
  // Self-hosted / enterprise
  if (p.includes('server') || p.includes('custom') || p.includes('self')) {
    return <Server size={11} aria-hidden="true" />;
  }
  // Default — cloud providers (anthropic, openai, openrouter, zai, etc.)
  return <Cloud size={11} aria-hidden="true" />;
}

export function ChatStatusBarItems({ stats, onModelClick }: ChatStatusBarItemsProps): JSX.Element | null {
  if (!stats) return null;

  const provider = typeof stats.provider === 'string' ? stats.provider : '';
  const model = typeof stats.model === 'string' ? stats.model : '';
  const persona = typeof stats.persona === 'string' ? stats.persona : '';
  const totalTokens = Number(stats.total_tokens ?? NaN);
  const currentCtx = Number(stats.current_context_tokens ?? NaN);
  const maxCtx = Number(stats.max_context_tokens ?? NaN);
  const totalCost = Number(stats.total_cost ?? NaN);

  // If literally nothing is populated, render nothing rather than an empty
  // "·  ·  ·" stub. The shared StatusBar then falls back to today's editor
  // metadata via its baseline rightItems path.
  const hasAny =
    provider ||
    model ||
    persona ||
    Number.isFinite(totalTokens) ||
    Number.isFinite(currentCtx) ||
    Number.isFinite(totalCost);
  if (!hasAny) return null;

  const segments: JSX.Element[] = [];

  // Persona badge — only when the agent's active persona is something
  // other than the unmarked primary. The CLI shows persona via tool-line
  // badges; this surfaces the same signal at the status level so users
  // can see at a glance which sub-agent is currently running.
  if (persona && persona !== 'orchestrator') {
    segments.push(
      <span
        key="persona"
        className="chat-statusbar-item chat-statusbar-persona"
        style={{ color: getPersonaColor(persona) }}
        title={`Active persona: ${persona}`}
      >
        [{persona}]
      </span>,
    );
  }

  if (provider || model) {
    const modelLabel = model || provider;
    const tooltip = onModelClick ? `${provider} · ${model} — click to change model` : `${provider} · ${model}`;
    segments.push(
      <span key="provider" className="chat-statusbar-item chat-statusbar-model" title={tooltip}>
        <ProviderIcon provider={provider} />
        {onModelClick ? (
          <button
            type="button"
            className="chat-statusbar-model-button"
            onClick={() => onModelClick(provider)}
            aria-label={`Change model (currently ${modelLabel})`}
          >
            <span className="chat-statusbar-model-name">{modelLabel}</span>
          </button>
        ) : (
          <span className="chat-statusbar-model-name">{modelLabel}</span>
        )}
      </span>,
    );
  }

  if (Number.isFinite(currentCtx) && Number.isFinite(maxCtx) && maxCtx > 0) {
    segments.push(
      <span key="ctx" className="chat-statusbar-item" title="Context usage">
        {formatTokens(currentCtx)}/{formatTokens(maxCtx)} ctx
      </span>,
    );
  } else if (Number.isFinite(totalTokens)) {
    segments.push(
      <span key="tok" className="chat-statusbar-item" title="Total tokens">
        {formatTokens(totalTokens)} tok
      </span>,
    );
  }

  if (Number.isFinite(totalCost)) {
    segments.push(
      <span
        key="cost"
        className={`chat-statusbar-item chat-statusbar-cost ${costClass(totalCost)}`}
        title="Session cost"
      >
        {formatCost(totalCost)}
      </span>,
    );
  }

  // Interleave a separator between segments so we don't render a trailing
  // dot when a segment is missing.
  const out: JSX.Element[] = [];
  segments.forEach((seg, i) => {
    if (i > 0) out.push(<span key={`sep-${i}`} className="chat-statusbar-sep" aria-hidden="true">·</span>);
    out.push(seg);
  });

  return <>{out}</>;
}
