/**
 * escalationHost.ts — where an escalated command runs: one of the user's
 * runners, the cloud (Fly) workspace, or "auto" (the platform decides).
 *
 * The full app's escalation prompt and terminal toast let the user pick a
 * host; the choice is remembered per repository in localStorage
 * (`sprout.escalationHost.<repo>`) so the next prompt — and "Always allow"
 * runs that don't prompt at all — use the same host. Embedded pages have no
 * picker and run with "auto".
 *
 * Side-effect free apart from localStorage; no React imports.
 */

import type { TxnHostChoice } from './cloudTxn';
import { isRunnerSelectable, type Runner } from './runners';

export type EscalationHost =
  | { kind: 'auto' }
  | { kind: 'cloud' }
  | {
      kind: 'runner';
      runnerId: string;
      /** Display name, kept so messages can name the runner without a lookup. */
      name: string;
    };

export const AUTO_HOST: EscalationHost = { kind: 'auto' };
export const CLOUD_HOST: EscalationHost = { kind: 'cloud' };

const HOST_KEY_PREFIX = 'sprout.escalationHost.';

export function escalationHostKey(repoURL: string): string {
  return HOST_KEY_PREFIX + repoURL;
}

/** The host last chosen for repoURL, or null when none was remembered. */
export function getRememberedHost(repoURL: string | undefined): EscalationHost | null {
  if (!repoURL) return null;
  try {
    const raw = window.localStorage.getItem(escalationHostKey(repoURL));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<{ kind: string; runnerId: string; name: string }> | null;
    if (parsed?.kind === 'cloud') return CLOUD_HOST;
    if (parsed?.kind === 'runner' && typeof parsed.runnerId === 'string' && parsed.runnerId !== '') {
      return { kind: 'runner', runnerId: parsed.runnerId, name: typeof parsed.name === 'string' ? parsed.name : '' };
    }
    return null;
  } catch {
    // Storage unavailable or a corrupt entry: behave as if nothing was remembered.
    return null;
  }
}

/** Remember the host chosen for repoURL ("auto" is never stored — it is the absence of a choice). */
export function rememberHost(repoURL: string | undefined, host: EscalationHost): void {
  if (!repoURL || host.kind === 'auto') return;
  try {
    window.localStorage.setItem(escalationHostKey(repoURL), JSON.stringify(host));
  } catch {
    // Storage unavailable (private mode): the choice lasts for this prompt only.
  }
}

export function hostForRunner(runner: Runner): EscalationHost {
  return { kind: 'runner', runnerId: runner.runner_id, name: runner.name };
}

export function sameHost(a: EscalationHost, b: EscalationHost): boolean {
  if (a.kind !== b.kind) return false;
  return a.kind !== 'runner' || (b.kind === 'runner' && a.runnerId === b.runnerId);
}

/**
 * Pick the host a prompt starts on: the remembered choice when it is still
 * available, else an online runner, else the cloud. `avoidRunnerId` names a
 * runner that just turned the request down (409 runner_unavailable); the
 * prompt then starts on the cloud, which is the offered way out.
 */
export function defaultRunHost(repoURL: string | undefined, runners: Runner[], avoidRunnerId?: string): EscalationHost {
  if (avoidRunnerId) return CLOUD_HOST;
  const remembered = getRememberedHost(repoURL);
  if (remembered?.kind === 'cloud') return CLOUD_HOST;
  if (remembered?.kind === 'runner') {
    const match = runners.find((r) => r.runner_id === remembered.runnerId && isRunnerSelectable(r));
    if (match) return hostForRunner(match);
  }
  const online = runners.find((r) => r.status === 'online');
  return online ? hostForRunner(online) : CLOUD_HOST;
}

/** Display name for a host: the runner's name, or "Cloud". */
export function hostDisplayName(host: EscalationHost): string {
  if (host.kind === 'runner') return host.name || 'your runner';
  return host.kind === 'cloud' ? 'Cloud' : 'your workspace';
}

/** Map the UI choice onto the platform's /workspace/txn host selector. */
export function toTxnHostChoice(host: EscalationHost): TxnHostChoice {
  if (host.kind === 'runner') return { host: 'runner', runnerId: host.runnerId };
  return { host: host.kind === 'cloud' ? 'fly' : 'auto' };
}
