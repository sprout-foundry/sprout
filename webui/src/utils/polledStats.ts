import type { StatsResponse } from '../services/api';

// In the hosted editor the agent runs in the browser: its metrics_update
// events carry the real counters, and the server's stats endpoint can only
// answer zeros for them.
const BROWSER_AGENT_COUNTERS = ['query_count', 'total_tokens', 'prompt_tokens', 'completion_tokens', 'total_cost'];

/** The part of a polled stats response that should overwrite the known stats. */
export function polledStatsPatch(stats: StatsResponse, agentInBrowser: boolean): Partial<StatsResponse> {
  if (!agentInBrowser) return stats;
  const patch: Record<string, unknown> = { ...stats };
  for (const key of BROWSER_AGENT_COUNTERS) delete patch[key];
  return patch as Partial<StatsResponse>;
}
