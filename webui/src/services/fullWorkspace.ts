/**
 * Full (container-backed) workspaces: whether this deployment offers them,
 * and the create-and-enter flow shared by the header and the escalation toast.
 */

import { useEffect, useSyncExternalStore } from 'react';

/** The platform's 503 wording when no workspace compute is configured. */
const NOT_CONFIGURED = /not available on this deployment/i;

type Availability = 'unknown' | 'available' | 'unavailable';

let availability: Availability = 'unknown';
let probe: Promise<void> | null = null;
const listeners = new Set<() => void>();

function setAvailability(next: Availability): void {
  if (next === availability) return;
  availability = next;
  for (const listener of listeners) listener();
}

/**
 * Ask the platform once whether workspaces exist here. Only the "not
 * configured" 503 counts as unavailable; a transient outage or a network
 * failure leaves the option on offer so the user can retry.
 */
export function probeFullWorkspaces(): Promise<void> {
  if (!probe) {
    probe = Promise.resolve()
      .then(() => fetch(`${window.location.origin}/workspace/fly`, { credentials: 'include' }))
      .then(async (res) => {
        if (!res || res.status !== 503) {
          setAvailability('available');
          return;
        }
        const body = await res.text().catch(() => '');
        setAvailability(NOT_CONFIGURED.test(body) ? 'unavailable' : 'available');
      })
      .catch(() => undefined);
  }
  return probe;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getAvailability(): Availability {
  return availability;
}

/**
 * False once the deployment has said it has no workspace compute. Pass
 * `enabled: false` where full workspaces don't apply (local mode) to skip
 * the probe.
 */
export function useFullWorkspacesAvailable(enabled = true): boolean {
  useEffect(() => {
    if (enabled) void probeFullWorkspaces();
  }, [enabled]);
  return useSyncExternalStore(subscribe, getAvailability, getAvailability) !== 'unavailable';
}

export type StartFullWorkspaceResult =
  | { kind: 'redirecting' }
  | { kind: 'unavailable' }
  | { kind: 'error'; message: string }
  | { kind: 'status'; status: string };

/**
 * Create (or resume) the repo's workspace and enter it through the
 * workspace's auth exchange. Rejects only on network failure.
 */
export async function startFullWorkspace(repoURL: string): Promise<StartFullWorkspaceResult> {
  const response = await fetch(`${window.location.origin}/workspace/fly`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_url: repoURL }),
    credentials: 'include',
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    const message: string = errData.error || `HTTP ${response.status}`;
    if (response.status === 503 && NOT_CONFIGURED.test(message)) {
      setAvailability('unavailable');
      return { kind: 'unavailable' };
    }
    return { kind: 'error', message };
  }

  const data = await response.json();
  if (data.url && data.session_token) {
    const wsUrl = new URL(data.url);
    wsUrl.pathname = '/auth/exchange';
    wsUrl.searchParams.set('token', data.session_token);
    window.location.href = wsUrl.toString();
    return { kind: 'redirecting' };
  }
  return { kind: 'status', status: data.status || 'unknown' };
}

export function __resetFullWorkspaceForTests(): void {
  availability = 'unknown';
  probe = null;
  listeners.clear();
}
