/**
 * Full (container-backed) dev spaces: whether this deployment offers them,
 * and the create-and-enter flow shared by the header and the escalation toast.
 */

import { useEffect, useSyncExternalStore } from 'react';
import { getActiveHost } from '../host/accessor';
import { clientFetch } from './clientSession';

/** The platform's 503 wording when no dev space compute is configured. */
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
 * Ask the platform once whether dev spaces exist here, through the typed
 * availability endpoint (`GET /dev-space/availability`) rather than the
 * legacy list route: a deployment without cloud compute answers
 * `available: false` instead of 503-ing on the probe. Only a definitive
 * `available: false` counts as unavailable; a missing endpoint (a local
 * daemon without the route), a transient outage or a network failure leaves
 * the option on offer so the user can retry.
 *
 * The probe is a platform call, so it goes to the host transport's own API
 * base rather than through clientFetch, which sends paths to the agent's
 * backend — inside an embedded dev space, that dev space's daemon.
 */
export function probeDevSpaces(): Promise<void> {
  if (!probe) {
    probe = Promise.resolve()
      .then(() =>
        fetch(`${getActiveHost()?.transport.apiBaseURL ?? ''}/dev-space/availability`, { credentials: 'include' }),
      )
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
 * False once the deployment has said it has no dev space compute. Pass
 * `enabled: false` where full dev spaces don't apply (local mode) to skip
 * the probe.
 */
export function useDevSpacesAvailable(enabled = true): boolean {
  useEffect(() => {
    if (enabled) void probeDevSpaces();
  }, [enabled]);
  return useSyncExternalStore(subscribe, getAvailability, getAvailability) !== 'unavailable';
}

export type StartDevSpaceResult =
  | { kind: 'redirecting' }
  | { kind: 'unavailable' }
  | { kind: 'error'; message: string }
  | { kind: 'status'; status: string };

/**
 * Create (or resume) the repo's dev space and enter it through the
 * dev space's auth exchange. Rejects only on network failure.
 */
export async function startDevSpace(repoURL: string): Promise<StartDevSpaceResult> {
  // The host-agnostic cloud create, not the legacy /dev-space/fly start route:
  // host=cloud places on the platform's cloud hosts (the fleet, Fly as
  // overflow) and never the caller's runner — the full-dev-space start is a
  // cloud action, so this keeps the legacy create's semantics on the typed
  // surface (SP-BUILDER-12). The create is idempotent: a live cloud
  // dev space for the repo is resumed, not duplicated.
  const response = await clientFetch('/dev-space/txn', {
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

export function __resetDevSpacesForTests(): void {
  availability = 'unknown';
  probe = null;
  listeners.clear();
}
