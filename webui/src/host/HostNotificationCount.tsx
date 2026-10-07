/**
 * The host's unread-notification count, rendered in Sprout's header chrome
 * (host.7). The host owns the number (`host.notifications.count`): Sprout only
 * renders the value it is given and never hardcodes or infers one. When the
 * host provides no count (localHost, the headless default) this renders
 * nothing — "render only when data exists."
 *
 * Reads the count through the module accessor (getActiveHost) rather than
 * useHost() so it works in any tree, provider or not, and re-reads on
 * HOST_UPDATED_EVENT so a host that updates its count live is followed.
 */

import { useEffect, useState } from 'react';
import { getActiveHost, HOST_UPDATED_EVENT } from './accessor';

export default function HostNotificationCount(): JSX.Element | null {
  const [count, setCount] = useState<number | undefined>(() => getActiveHost()?.notifications.count);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const refresh = () => setCount(getActiveHost()?.notifications.count);
    window.addEventListener(HOST_UPDATED_EVENT, refresh);
    return () => window.removeEventListener(HOST_UPDATED_EVENT, refresh);
  }, []);

  if (typeof count !== 'number' || count <= 0) return null;

  return (
    <span
      className="header-host-notification-count"
      data-testid="host-notification-count"
      title={`${count} unread notification${count === 1 ? '' : 's'}`}
      aria-label={`${count} unread notification${count === 1 ? '' : 's'}`}
    >
      {count}
    </span>
  );
}
