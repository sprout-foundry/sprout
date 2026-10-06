/**
 * runners.ts — the user's own runners (SP-159), as listed by the platform.
 *
 * GET /runners (session cookie, relative path so the CloudAdapter/same-origin
 * platform answers it) returns every runner attached to the account with the
 * status and self-reported mode/sandbox the escalation host picker shows.
 *
 * Listing is best-effort: escalation must keep working with the cloud host
 * alone, so a signed-out session, a deployment without runners, a network
 * failure or a malformed body all read as "no runners".
 *
 * Side-effect free and framework agnostic (no React imports).
 */

export type RunnerStatus = 'online' | 'busy' | 'offline';
export type RunnerMode = 'container' | 'native' | 'bare-metal' | '';

export interface Runner {
  runner_id: string;
  name: string;
  status: RunnerStatus | string;
  os?: string;
  arch?: string;
  capacity?: number;
  mode?: RunnerMode | string;
  sandbox?: string;
  runner_version?: string;
  toolchains?: string[];
}

function isRunner(value: unknown): value is Runner {
  if (!value || typeof value !== 'object') return false;
  const r = value as Record<string, unknown>;
  return typeof r.runner_id === 'string' && r.runner_id !== '' && typeof r.name === 'string';
}

/** List the caller's runners; any failure yields []. */
export async function listRunners(): Promise<Runner[]> {
  try {
    const res = await fetch('/runners', { method: 'GET', credentials: 'include' });
    if (!res.ok) return [];
    const body = (await res.json()) as unknown;
    return Array.isArray(body) ? body.filter(isRunner) : [];
  } catch {
    // best-effort: no runners means the cloud host is the only choice.
    return [];
  }
}

/** Whether a runner can be offered as a host right now (busy ones may free up). */
export function isRunnerSelectable(runner: Runner): boolean {
  return runner.status === 'online' || runner.status === 'busy';
}

/**
 * Short label for how a runner isolates commands: "container",
 * "native · <sandbox>", "bare metal", or '' when the runner hasn't reported.
 */
export function runnerModeLabel(runner: Pick<Runner, 'mode' | 'sandbox'>): string {
  switch (runner.mode) {
    case 'container':
      return 'container';
    case 'native':
      return runner.sandbox ? `native · ${runner.sandbox}` : 'native';
    case 'bare-metal':
      return 'bare metal';
    default:
      return '';
  }
}

/** Bare-metal runners run commands directly on the host, with no sandbox. */
export function isBareMetal(runner: Pick<Runner, 'mode'>): boolean {
  return runner.mode === 'bare-metal';
}
