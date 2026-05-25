/**
 * Resource usage formatting utilities.
 * Shared between SubagentActivityFeed and DelegateActivityTree.
 */

export function formatCost(cost: number): string {
  return `$${cost.toFixed(4)}`;
}

export function formatTokens(tokens: number): string {
  if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}k`;
  }
  return String(tokens);
}
