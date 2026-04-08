import './Status.css';

interface StatusProps {
  isConnected: boolean;
  stats?: {
    provider?: string;
    model?: string;
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

  if (!isConnected || !stats) {
    return (
      <div className={`status-bar ${isConnected ? 'connected' : 'disconnected'}`} />
    );
  }

  const contextPercent = stats.context_usage_percent as number | undefined;
  const totalTokens = stats.total_tokens as number | undefined;
  const provider = stats.provider as string | undefined;
  const model = stats.model as string | undefined;
  const contextStatus = getContextStatus();

  return (
    <div className="status-bar connected">
      <div className="status-info">
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
          <span className="status-item status-provider" title="Provider and model">
            {provider} : {model}
          </span>
        )}
      </div>
    </div>
  );
}

export default Status;
