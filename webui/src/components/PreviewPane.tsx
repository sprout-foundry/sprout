import { CircleStop, CircleX, RotateCcw } from 'lucide-react';
import './PreviewPane.css';

/**
 * Lifecycle states of the app the preview pane renders. The pane is a pure,
 * presentational component: the parent (the 155.4 dev-server backend or the
 * 155.5 hosted preview registration) drives it entirely through props, so it
 * can be composed into a Code-mode panel or the SP-143 screen preview slot
 * without knowing how the underlying server is started.
 */
export type PreviewPaneStatus = 'starting' | 'running' | 'stopped' | 'failed';

const STATUS_LABELS: Record<PreviewPaneStatus, string> = {
  starting: 'Starting',
  running: 'Running',
  stopped: 'Stopped',
  failed: 'Failed',
};

/**
 * Embeddable URL of the running app (a `localhost` origin for a local dev
 * server, or the URL a hosted `register_preview_port` returned). Present in
 * the `running` state; the pane shows a placeholder if it is missing.
 */
export interface PreviewPaneProps {
  /** Lifecycle state of the app. Drives which body the pane renders. */
  status: PreviewPaneStatus;
  /** URL to embed in the preview iframe. Optional until the app is running. */
  url?: string;
  /** Human-readable reason, shown in the `failed` state when present. */
  error?: string;
  /**
   * Invoked by the Restart action. The parent decides what restarting means
   * (start the dev server, re-register a hosted port, ...).
   */
  onRestart: () => void;
  /**
   * Opaque reload token. When the parent bumps it (typically on a file
   * change), the embedded app is re-loaded. A plain counter or a
   * modification timestamp both work.
   */
  reloadKey?: number | string;
  /** Label shown in the header. Defaults to "Preview". */
  title?: string;
  /** Force the Restart action to be disabled regardless of state. */
  restartDisabled?: boolean;
}

/**
 * Preview pane for the project's running app (SP-155 §155a). Renders the app
 * in an iframe when it is running, and shows a clear placeholder for the
 * starting / stopped / failed states with a restart action. Re-mounts the
 * iframe when `reloadKey` changes so file changes reload the app.
 */
export function PreviewPane({
  status,
  url,
  error,
  onRestart,
  reloadKey,
  title = 'Preview',
  restartDisabled = false,
}: PreviewPaneProps): JSX.Element {
  const restarting = status === 'starting';
  const restartEnabled = !restarting && !restartDisabled;
  const showIframe = status === 'running' && Boolean(url);

  const handleRestart = (): void => {
    if (restartEnabled) {
      onRestart();
    }
  };

  return (
    <div
      className="preview-pane"
      data-testid="preview-pane"
      role="region"
      aria-label={`${title} preview`}
      aria-busy={restarting}
    >
      <div className="preview-pane__header">
        <span className="preview-pane__title" data-testid="preview-pane-title">
          {title}
        </span>
        <span className={`preview-pane__status preview-pane__status--${status}`} data-testid="preview-pane-status">
          {STATUS_LABELS[status]}
        </span>
        <button
          type="button"
          className="preview-pane__restart"
          data-testid="preview-pane-restart"
          onClick={handleRestart}
          disabled={!restartEnabled}
          aria-label={`Restart ${title}`}
        >
          <RotateCcw size={14} aria-hidden="true" />
          <span>Restart</span>
        </button>
      </div>

      <div className="preview-pane__body">
        {status === 'starting' && (
          <div className="preview-pane__placeholder" data-testid="preview-pane-starting">
            <span className="preview-pane__spinner" aria-hidden="true" />
            <p>Starting the app…</p>
          </div>
        )}

        {status === 'running' &&
          (showIframe ? (
            <iframe
              key={`preview-iframe-${reloadKey ?? 0}`}
              className="preview-pane__iframe"
              data-testid="preview-pane-iframe"
              src={url}
              title={`${title} preview`}
              sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
            />
          ) : (
            <div className="preview-pane__placeholder" data-testid="preview-pane-no-url">
              <p>The app is running, but no preview URL is available yet.</p>
            </div>
          ))}

        {status === 'stopped' && (
          <div className="preview-pane__placeholder" data-testid="preview-pane-stopped">
            <CircleStop size={28} aria-hidden="true" />
            <p>Preview is stopped.</p>
            <p className="preview-pane__hint">Start or restart the dev server to see your app.</p>
          </div>
        )}

        {status === 'failed' && (
          <div
            className="preview-pane__placeholder preview-pane__placeholder--failed"
            data-testid="preview-pane-failed"
            role="alert"
          >
            <CircleX size={28} aria-hidden="true" />
            <p>Preview failed to start.</p>
            {error ? <p className="preview-pane__error">{error}</p> : null}
          </div>
        )}
      </div>
    </div>
  );
}

export default PreviewPane;
