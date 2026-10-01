import { openPlatformPage } from './homeView';
import { platformHref } from '../utils/platformUrl';
import { notificationBus } from './notificationBus';

/**
 * Turn a failed browser-agent run into chat text. A 402 from the platform's
 * credit gate carries a complete, user-facing sentence after "HTTP 402:";
 * everything around it is provider plumbing ("process query: chat failed:
 * …") that only obscures what to do next.
 */
export function describeAgentError(raw: string): { message: string; creditsBlocked: boolean } {
  const match = /HTTP 402:\s*([\s\S]+)$/.exec(raw);
  if (match) return { message: match[1].trim(), creditsBlocked: true };
  return { message: `Agent error: ${raw}`, creditsBlocked: false };
}

/**
 * Offer the way out of a credit block. When the platform suggests your own
 * key (free tier, managed credits used up), the action opens the editor's
 * model setting; otherwise it opens billing.
 */
export function notifyCreditsBlocked(message: string): void {
  const ownKey = /own API key/i.test(message);
  notificationBus.notify('warning', 'Out of platform credits', message, undefined, {
    label: ownKey ? 'Use your own key' : 'Buy credits',
    onClick: () => {
      if (ownKey) {
        window.dispatchEvent(new CustomEvent('sprout:open-settings-focus', { detail: { focus: 'provider' } }));
        return;
      }
      if (!openPlatformPage('/account/billing')) window.open(platformHref('/#/account/billing'), '_blank', 'noopener');
    },
  });
}
