import React from 'react';
import { usePreviewStatus } from '../hooks/usePreviewStatus';
import MockupPanel from './MockupPanel';
import PreviewPane from './PreviewPane';
import './PreviewPanel.css';

export interface PreviewPanelProps {
  /** Whether the panel is open (open renders the pane; closed renders none). */
  open: boolean;
  /** Collapse the panel (wired to the pane's close affordance). */
  onClose: () => void;
}

/**
 * The preview body: the presentational PreviewPane driven by the dev-server
 * lifecycle API through usePreviewStatus.
 *
 * Polls the status while enabled, wires the pane's Restart action to the
 * right lifecycle call (a running app is restarted; a stopped or failed one
 * is started), bumps the reload token on file changes, and disables restart
 * for a hosted (platform-registered) preview, which the local
 * start/restart/stop actions never apply to.
 *
 * Split from PreviewPanel so the same body renders both as the docked panel
 * and as an editor pane's content (`kind: 'preview'`).
 */
export function PreviewBody({ onClose }: { onClose?: () => void }): JSX.Element {
  const preview = usePreviewStatus(true);

  const handleRestart = (): void => {
    if (preview.status === 'running') preview.restart();
    else preview.start();
  };

  return (
    <PreviewPane
      status={preview.status}
      url={preview.url}
      error={preview.error}
      onRestart={handleRestart}
      reloadKey={preview.reloadKey}
      restartDisabled={preview.hosted === true}
      onClose={onClose}
    />
  );
}

/**
 * The preview surface an editor pane renders: the workspace's mockup when it
 * has one, otherwise the dev server's output. Both are a rendered view of the
 * work, so they share one buffer and one slot beside the code.
 */
export function PreviewSurface({ showMockup, onClose }: { showMockup: boolean; onClose?: () => void }): JSX.Element {
  if (showMockup) return <MockupPanel onClose={onClose ?? (() => undefined)} />;
  return <PreviewBody onClose={onClose} />;
}

/**
 * PreviewPanel — the docked Code-mode preview panel.
 *
 * Kept for hosts that place the `previewPanel` view kind in an arrangement.
 * The built-in webui renders the preview as an editor pane instead (see
 * `PreviewSurface` and `WorkspacePane`).
 */
export function PreviewPanel({ open, onClose }: PreviewPanelProps): JSX.Element | null {
  if (!open) return null;

  return (
    <aside className="preview-panel" data-testid="preview-panel">
      <PreviewBody onClose={onClose} />
    </aside>
  );
}

export default PreviewPanel;
