/**
 * useRunHostChoice — the runner list and the selected host for one
 * escalation request (a consent prompt or a terminal toast).
 *
 * Runners are fetched fresh for every request (`requestKey` identity) so the
 * online/offline state is current; until they arrive `loaded` is false and
 * callers should hold the run buttons. The selection starts on the default
 * (remembered host for the repo, else an online runner, else the cloud) and
 * follows the user's clicks for the same request.
 */

import { useCallback, useEffect, useState } from 'react';
import { defaultHost, type EscalationHost } from '../services/escalationHost';
import { listRunners, type Runner } from '../services/runners';

export interface RunHostChoice {
  /** The user's runners ([] until loaded, or when none / the list failed). */
  runners: Runner[];
  loaded: boolean;
  host: EscalationHost;
  setHost: (host: EscalationHost) => void;
}

export function useRunHostChoice(
  repoURL: string | undefined,
  requestKey: object | null,
  avoidRunnerId?: string,
): RunHostChoice {
  const [fetched, setFetched] = useState<{ key: object; runners: Runner[] } | null>(null);
  const [picked, setPicked] = useState<{ key: object; host: EscalationHost } | null>(null);

  useEffect(() => {
    if (!requestKey) return undefined;
    let cancelled = false;
    listRunners().then((runners) => {
      if (!cancelled) setFetched({ key: requestKey, runners });
    });
    return () => {
      cancelled = true;
    };
  }, [requestKey]);

  const loaded = requestKey !== null && fetched?.key === requestKey;
  const runners = loaded && fetched ? fetched.runners : [];
  const host =
    requestKey !== null && picked?.key === requestKey ? picked.host : defaultHost(repoURL, runners, avoidRunnerId);

  const setHost = useCallback(
    (next: EscalationHost) => {
      if (requestKey) setPicked({ key: requestKey, host: next });
    },
    [requestKey],
  );

  return { runners, loaded, host, setHost };
}
