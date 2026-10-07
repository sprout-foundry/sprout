/**
 * Remaining platform credits in the hosted editor's header, matching the
 * platform header's credits pill and linking to Usage & billing. The chip is
 * host-driven: the host supplies the usage summary (through
 * `host.entitlements.usageSummary`), including the label and link target, and
 * refreshes it. Credits are the one usage unit shown to hosted users; a host
 * with no summary (the local build) renders nothing.
 *
 * The figure is fetched by the host: the chip asks the host's
 * `entitlements.resolve` to refresh on mount, on window focus, while the tab
 * is visible, and when the user leaves Home (where plans and credit packs are
 * bought).
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useHost } from '../host/useHost';
import { onPlatformLinkClick, useHomeView } from '../services/homeView';

const REFRESH_MS = 60_000;

const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 });

export function CreditsChip(): JSX.Element | null {
  const host = useHost();
  const entitlements = host.entitlements;
  const [remaining, setRemaining] = useState<number | string | null>(entitlements?.usageSummary?.remaining ?? null);

  // Re-read the (possibly in-place-updated) host summary into local state.
  const read = useCallback(() => setRemaining(entitlements?.usageSummary?.remaining ?? null), [entitlements]);

  // Ask the host to re-resolve, then read. The host owns the fetch.
  const refresh = useCallback(() => {
    if (!entitlements) return;
    if (entitlements.resolve) {
      entitlements
        .resolve()
        .then(read)
        .catch(() => undefined);
    } else {
      read();
    }
  }, [entitlements, read]);

  useEffect(() => {
    // Re-read whenever the host instance changes.
    read();
  }, [read]);

  useEffect(() => {
    if (!entitlements?.usageSummary && !entitlements?.resolve) return;
    refresh();
    window.addEventListener('focus', refresh);
    // Usage moves while the agent works; keep the figure current while the
    // tab is visible.
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') refresh();
    }, REFRESH_MS);
    return () => {
      window.removeEventListener('focus', refresh);
      window.clearInterval(timer);
    };
  }, [entitlements, refresh]);

  // Leaving Home (where plans and credit packs are bought) refreshes too.
  const homeOpen = useHomeView().open;
  const wasHomeOpen = useRef(homeOpen);
  useEffect(() => {
    if (wasHomeOpen.current && !homeOpen) refresh();
    wasHomeOpen.current = homeOpen;
  }, [homeOpen, refresh]);

  const summary = entitlements?.usageSummary;
  // A host with no summary (the local build) renders nothing.
  if (!summary || remaining == null) return null;

  const numeric = typeof remaining === 'number';
  const empty = numeric && remaining <= 0;
  return (
    <a
      href={summary.linkTarget}
      className={`header-credits-chip${empty ? ' is-empty' : ''}`}
      title={`${numeric ? remaining.toLocaleString('en-US') : remaining} ${summary.label} remaining — usage and billing`}
      data-testid="header-credits-chip"
      onClick={onPlatformLinkClick(summary.linkTarget)}
    >
      {numeric ? compact.format(Math.max(remaining, 0)) : remaining} {summary.label}
    </a>
  );
}
