/**
 * GitHub account card for the hosted editor. GitHub is connected once, on
 * the Foundry account, and the platform supplies it to git — so this card
 * shows that connection and links to where it is managed instead of asking
 * for a token.
 */

import { ExternalLink, Loader2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import type { ReactElement } from 'react';
import './GitHubAccountPanel.css';
import { onPlatformLinkClick } from '../services/homeView';
import { fetchPlatformGitHubConnected, platformGitHubSettingsHref } from '../services/platformGitHub';

interface PlatformGitHubAccountCardProps {
  /** Connection state when the parent already knows it; fetched otherwise. */
  connected?: boolean | null;
  compact?: boolean;
}

export default function PlatformGitHubAccountCard({
  connected: connectedProp,
  compact = false,
}: PlatformGitHubAccountCardProps): ReactElement {
  const controlled = connectedProp !== undefined;
  const [fetched, setFetched] = useState<boolean | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (controlled) return;
    let active = true;
    fetchPlatformGitHubConnected()
      .then((value) => active && setFetched(value))
      .catch((err) => active && setError(err instanceof Error ? err.message : String(err)));
    return () => {
      active = false;
    };
  }, [controlled]);

  const connected = controlled ? connectedProp : fetched;
  const href = platformGitHubSettingsHref();
  const cardClass = `gh-account-card gh-account-card--platform${compact ? ' gh-account-card--compact' : ''}`;

  if (error) {
    return (
      <div className={cardClass} data-testid="platform-gh-card">
        <span className="gh-account-name">{error}</span>
      </div>
    );
  }

  if (connected == null) {
    return (
      <div className={cardClass} data-testid="platform-gh-card">
        <Loader2 size={14} className="spin" />
        <span className="gh-account-name">Checking your GitHub connection…</span>
      </div>
    );
  }

  return (
    <div className={cardClass} data-testid="platform-gh-card">
      <span className="gh-account-name">
        {connected
          ? 'GitHub is connected through your Sprout Foundry account.'
          : 'Connect GitHub on your Sprout Foundry account to list and clone your repositories, including private ones.'}
      </span>
      <a
        className="gh-account-signout"
        href={href}
        target="_top"
        onClick={onPlatformLinkClick('/settings')}
        data-testid="platform-gh-manage"
      >
        {connected ? 'Manage' : 'Connect GitHub'} <ExternalLink size={12} aria-hidden="true" />
      </a>
    </div>
  );
}
