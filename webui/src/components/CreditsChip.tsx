/**
 * Remaining platform credits in the hosted editor's header, matching the
 * platform header's credits pill and linking to Usage & billing. Credits are
 * the one usage unit shown to hosted users; renders nothing in local mode or
 * on a platform still on the legacy ledger.
 */

import { useEffect, useState } from 'react';
import { isCloud } from '../config/mode';
import { isLayeredLayout } from '../config/layout';
import { openHome } from '../services/homeView';
import { platformHref } from '../utils/platformUrl';

const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 });

async function fetchRemainingCredits(): Promise<number | null> {
  const res = await fetch(platformHref('/billing/status'), { credentials: 'include' });
  if (!res.ok) return null;
  const status = (await res.json()) as { ledger?: string; total_remaining?: number };
  return status.ledger === 'v2' && typeof status.total_remaining === 'number' ? status.total_remaining : null;
}

export function CreditsChip(): JSX.Element | null {
  const [remaining, setRemaining] = useState<number | null>(null);

  useEffect(() => {
    if (!isCloud) return;
    let active = true;
    const load = () => {
      fetchRemainingCredits()
        .then((value) => active && setRemaining(value))
        .catch(() => active && setRemaining(null));
    };
    load();
    window.addEventListener('focus', load);
    return () => {
      active = false;
      window.removeEventListener('focus', load);
    };
  }, []);

  if (!isCloud || remaining == null) return null;

  return (
    <a
      href={platformHref('/?from=editor#/account/billing')}
      className={`header-credits-chip${remaining <= 0 ? ' is-empty' : ''}`}
      title={`${remaining.toLocaleString('en-US')} credits remaining — usage and billing`}
      data-testid="header-credits-chip"
      onClick={(e) => {
        if (isLayeredLayout && !e.metaKey && !e.ctrlKey) {
          e.preventDefault();
          openHome('/account/billing');
        }
      }}
    >
      {compact.format(Math.max(remaining, 0))} credits
    </a>
  );
}
