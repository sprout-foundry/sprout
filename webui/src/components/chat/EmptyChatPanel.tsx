import { Settings, CloudOff } from 'lucide-react';
import { forwardRef } from 'react';
import SproutLogo from '../SproutLogo';

interface EmptyChatPanelProps {
  /** Show offline panel when backend requires health check and is unreachable */
  showOffline?: boolean;
  /** Show "no provider configured" state */
  providerAvailable?: boolean;
  onRetryConnection?: () => void;
  onRequestProviderSetup?: () => void;
}

export const EmptyChatPanel = forwardRef<HTMLDivElement, EmptyChatPanelProps>(function EmptyChatPanel(
  { showOffline = false, providerAvailable, onRetryConnection, onRequestProviderSetup },
  ref,
) {
  if (showOffline) {
    return (
      <div className="chat-container chat-container--empty" ref={ref}>
        <div className="chat-offline-panel" role="status" data-testid="chat-offline-panel">
          <CloudOff size={48} className="chat-offline-icon" aria-hidden="true" />
          <h3 className="chat-offline-title">No Server Connection</h3>
          <p className="chat-offline-description">
            Chat requires a connection to your Sprout server. Your editor and terminal remain available while offline.
          </p>
          <button
            className="chat-offline-retry-btn"
            onClick={onRetryConnection}
            type="button"
            aria-label="Retry connection"
            data-testid="chat-offline-retry"
          >
            Retry Connection
          </button>
        </div>
      </div>
    );
  }

  if (providerAvailable === false) {
    return (
      <div className="chat-container chat-container--empty" ref={ref}>
        <div className="welcome-message no-provider-state" data-testid="chat-no-provider">
          <div className="welcome-icon">
            <SproutLogo showWordmark={false} />
          </div>
          <div className="welcome-text">No AI provider configured</div>
          <div className="welcome-hint">
            AI features require a provider to be set up. The editor, terminal, file tree, and git panels are fully
            functional without one.
          </div>
          {onRequestProviderSetup && (
            <button
              type="button"
              className="provider-setup-btn"
              onClick={onRequestProviderSetup}
              aria-label="Open provider setup"
              data-testid="chat-provider-setup"
            >
              <Settings size={14} />
              Configure Provider
            </button>
          )}
        </div>
      </div>
    );
  }

  // Default welcome
  return (
    <div className="chat-container chat-container--empty" ref={ref}>
      <div className="welcome-message" data-testid="chat-welcome">
        <div className="welcome-icon">
          <SproutLogo showWordmark={false} />
        </div>
        <div className="welcome-text">
          Welcome to sprout! I&apos;m ready to help you with code analysis, editing, and more.
        </div>
        <div className="welcome-hint">
          Try asking: &quot;Show me the project structure&quot; or &quot;Find the main function&quot;
        </div>
      </div>
    </div>
  );
});
