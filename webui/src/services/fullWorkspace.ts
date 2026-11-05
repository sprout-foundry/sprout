/**
 * Full (container-backed) workspaces: whether this deployment offers them,
 * and the create-and-enter flow shared by the header and the escalation toast.
 */

import { useEffect, useSyncExternalStore } from 'react';

import { clientFetch } from './clientSession';

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
 * Ask the platform once whether workspaces exist here, through the typed
 * availability endpoint (`GET /workspace/availability`) rather than the
 * legacy list route: a deployment without cloud compute answers
 * `available: false` instead of 503-ing on the probe. Only a definitive
 * `available: false` counts as unavailable; a missing endpoint (a local
 * daemon without the route), a transient outage or a network failure leaves
 * the option on offer so the user can retry.
 */
export function probeFullWorkspaces(): Promise<void> {
  if (!probe) {
    probe = Promise.resolve()
      .then(() => clientFetch('/workspace/availability', { credentials: 'include' }))
      .then(async (res) => {
        if (!res || !res.ok) return;
        const data = (await res.json().catch(() => ({}))) as { available?: unknown };
        if (typeof data.available !== 'boolean') return;
        setAvailability(data.available ? 'available' : 'unavailable');
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
  // The host-agnostic cloud create, not the legacy /workspace/fly start route:
  // host=cloud places on the platform's cloud hosts (the fleet, Fly as
  // overflow) and never the caller's runner — the full-workspace start is a
  // cloud action, so this keeps the legacy create's semantics on the typed
  // surface (SP-BUILDER-12). The create is idempotent: a live cloud
  // workspace for the repo is resumed, not duplicated.
  const response = await clientFetch('/workspace/txn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_url: repoURL, host: 'cloud' }),
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
  // The cloud create answers with the open fields per backend: the Fly
  // path's url/session_token, or the Firecracker path's gateway_url + its
  // access token. Both open through the same /auth/exchange (the gateway's
  // exchange is the Fly open's), so one navigate covers either.
  const openUrl =
    typeof data.url === 'string' && data.url ? data.url : typeof data.gateway_url === 'string' ? data.gateway_url : '';
  const token =
    typeof data.session_token === 'string' && data.session_token
      ? data.session_token
      : typeof data.gateway_access_token === 'string'
        ? data.gateway_access_token
        : '';
  if (openUrl && token) {
    const wsUrl = new URL(openUrl);
    wsUrl.pathname = '/auth/exchange';
    wsUrl.searchParams.set('token', token);
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
