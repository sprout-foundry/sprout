import { useEffect, useRef, useState } from 'react';
import { OPEN_NOTIFICATIONS_EVENT } from '../config/layout';
import NotificationHistoryPanel from './NotificationHistoryPanel';

/**
 * The notification history opened from outside the status bar (the layered
 * rail's bell, the phone tab bar), anchored to whatever opened it. Mounted
 * once by the app so it works in every mode, including ones with no status
 * bar (Design).
 */
export default function NotificationCenterHost(): JSX.Element | null {
  const anchorRef = useRef<HTMLElement | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const onOpen = (e: Event) => {
      anchorRef.current = (e as CustomEvent<{ anchor?: HTMLElement }>).detail?.anchor ?? null;
      setOpen(true);
    };
    window.addEventListener(OPEN_NOTIFICATIONS_EVENT, onOpen);
    return () => window.removeEventListener(OPEN_NOTIFICATIONS_EVENT, onOpen);
  }, []);

  if (!open) return null;
  return (
    <NotificationHistoryPanel anchorRef={anchorRef as React.RefObject<HTMLElement>} onClose={() => setOpen(false)} />
  );
}
