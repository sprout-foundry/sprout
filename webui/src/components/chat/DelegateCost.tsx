import type { DelegateActivity } from '@sprout/ui';

interface DelegateCostProps {
  activities: DelegateActivity[];
}

function formatCost(cost: number): string {
  return `$${cost.toFixed(4)}`;
}

function formatTokens(tokens: number): string {
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(1)}M`;
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(1)}k`;
  return String(tokens);
}

export function DelegateCost({ activities }: DelegateCostProps) {
  if (activities.length === 0) return null;

  const totals = activities.reduce(
    (acc, a) => ({
      tokens: acc.tokens + a.tokensUsed,
      cost: acc.cost + a.cost,
    }),
    { tokens: 0, cost: 0 },
  );

  if (totals.tokens === 0 && totals.cost === 0) return null;

  return (
    <span className="delegate-cost-badge">
      <span className="delegate-cost-label">Delegates:</span>
      <span className="delegate-cost-value">{formatTokens(totals.tokens)} tok</span>
      <span className="delegate-cost-separator">·</span>
      <span className="delegate-cost-value">{formatCost(totals.cost)}</span>
    </span>
  );
}
