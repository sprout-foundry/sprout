/**
 * Preview API types.
 *
 * Mirror the JSON served by pkg/webui/api_preview.go, which in turn
 * serialises pkg/preview.State. The lifecycle words match the preview pane's
 * `status` prop so the API and the pane speak the same language.
 */

/** The dev server's lifecycle state (pkg/preview.Status). */
export type PreviewStatus = 'starting' | 'running' | 'stopped' | 'failed';

/**
 * A lifecycle snapshot of the project's dev server (pkg/preview.State).
 * `url` is present while running; `error` is the reason while stopped/failed;
 * `detected` marks a server found already running on the port (not owned);
 * `hosted` marks a platform-registered preview (register_preview_port).
 */
export interface PreviewState {
  status: PreviewStatus;
  /** Embed URL of the running app (a localhost origin, or a hosted URL). */
  url?: string;
  /** Human-readable reason (a stop/failed explanation). */
  error?: string;
  /** True when the running server was detected, not started by the manager. */
  detected?: boolean;
  /** True when the URL is a platform-registered (hosted) preview. */
  hosted?: boolean;
}
