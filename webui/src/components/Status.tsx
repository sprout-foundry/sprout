import './Status.css';

interface StatusProps {
  isConnected: boolean;
  stats?: {
    provider?: string;
    model?: string;
    persona?: string;
    total_tokens?: number;
    context_usage_percent?: number;
    [key: string]: unknown;
  };
}

function Status({ isConnected, stats }: StatusProps): JSX.Element {
  const formatTokens = (tokens: number): string => {
    if (tokens >= 1000000) {
      return `${(tokens / 1000000).toFixed(1)}M`;
    } else if (tokens >= 1000) {
      return `${(tokens / 1000).toFixed(1)}K`;
    }
    return tokens.toString();
  };

  const getContextStatus = () => {
    if (stats?.context_usage_percent == null) return 'unknown';
    const percent = stats.context_usage_percent as number;
    if (percent > 90) return 'critical';
    if (percent > 75) return 'high';
    if (percent > 50) return 'medium';
    return 'low';
  };

  const handlePersonaClick = () => {
    window.dispatchEvent(new CustomEvent('ledit:open-settings-focus', { detail: { focus: 'persona' } }));
  };

  const handleProviderModelClick = () => {
    window.dispatchEvent(new CustomEvent('ledit:open-settings-focus', { detail: { focus: 'provider' } }));
  };

  /** Format the internal persona ID (e.g. "code_reviewer") into a display label (e.g. "Code Reviewer"). */
  const formatPersonaLabel = (id: string): string => {
    return id
      .replace(/_/g, ' ')
      .replace(/\b\w/g, (c) => c.toUpperCase());
  };

  if (!isConnected || !stats) {
    return (
      <div className={`status-bar ${isConnected ? 'connected' : 'disconnected'}`} />
    );
  }

  const contextPercent = stats.context_usage_percent as number | undefined;
  const totalTokens = stats.total_tokens as number | undefined;
  const provider = stats.provider as string | undefined;
  const model = stats.model as string | undefined;
  const persona = stats.persona as string | undefined;
  const contextStatus = getContextStatus();

  return (
    <div className="status-bar connected">
      <div className="status-info">
        {persona && (
          <button
            type="button"
            className="status-item status-persona clickable"
            title="Active persona – click to change"
            onClick={handlePersonaClick}
          >
            {formatPersonaLabel(persona)}
          </button>
        )}
        {contextPercent != null && (
          <span
            className={`status-item context-${contextStatus}`}
            title="Context usage"
          >
            {contextPercent.toFixed(1)}%
          </span>
        )}
        {totalTokens != null && (
          <span className="status-item" title="Total tokens">
            Tokens: {formatTokens(totalTokens)}
          </span>
        )}
        {provider && model && (
          <button
            type="button"
            className="status-item status-provider clickable"
            title="Provider and model – click to change"
            onClick={handleProviderModelClick}
          >
            {provider} : {model}
          </button>
        )}
      </div>
    </div>
  );
}

export default Status;
