import { useEffect, useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import './DisconnectedOverlay.css';

// Grace before the blocking overlay appears. Ordinary reconnects (a brief
// network blip, a laptop wake) recover well within this, so they keep the
// current behavior with no overlay. The overlay only shows for a sustained,
// effectively-permanent disconnect — the CLI was closed, the daemon stopped,
// or (when embedded) the host detached — where retrying won't help and the
// on-screen state is now stale.
const DISCONNECT_GRACE_MS = 10_000;

interface DisconnectedOverlayProps {
  isConnected: boolean;
}

/**
 * Full-screen blocking overlay shown when the WebSocket has been disconnected
 * for longer than the grace period. It disables the whole Web UI (captures all
 * input) so the user can't act on stale state, and offers a reload to
 * reconnect. Hidden immediately once the connection is restored.
 */
export function DisconnectedOverlay({ isConnected }: DisconnectedOverlayProps): JSX.Element | null {
  const [show, setShow] = useState(false);

  useEffect(() => {
    if (isConnected) {
      setShow(false);
      return;
    }
    const timer = setTimeout(() => setShow(true), DISCONNECT_GRACE_MS);
    return () => clearTimeout(timer);
  }, [isConnected]);

  if (!show) return null;

  return (
    <div
      className="disconnected-overlay"
      role="alertdialog"
      aria-modal="true"
      aria-label="Disconnected from sprout"
      data-testid="disconnected-overlay"
    >
      <div className="disconnected-overlay__card">
        <div className="disconnected-overlay__icon" aria-hidden="true">
          <TriangleAlert size={32} />
        </div>
        <h2 className="disconnected-overlay__title">Disconnected from sprout</h2>
        <p className="disconnected-overlay__body">
          The connection to the sprout server was lost and isn&apos;t coming back. The CLI may have been closed, the
          daemon stopped, or the host detached. What you see here may be out of date.
        </p>
        <button
          type="button"
          className="disconnected-overlay__reload"
          onClick={() => window.location.reload()}
          autoFocus
        >
          Reload
        </button>
      </div>
    </div>
  );
}

export default DisconnectedOverlay;
