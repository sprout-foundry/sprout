import React from 'react';
import { usePreviewStatus } from '../hooks/usePreviewStatus';
import PreviewPane from './PreviewPane';
import './PreviewPanel.css';

export interface PreviewPanelProps {
  /** Whether the panel is open (open renders the pane; closed renders none). */
  open: boolean;
  /** Collapse the panel (wired to the pane's close affordance). */
  onClose: () => void;
}

/**
 * PreviewPanel — the Code-mode preview panel (SP-155 §155a, TODO 155.6).
 *
 * Places the presentational PreviewPane (155.3) into the Code surface and
 * drives it from the dev-server lifecycle API through usePreviewStatus
 * (155.6): it polls the status while open, wires the pane's Restart action
 * to the right lifecycle call (a running app is restarted; a stopped or
 * failed one is started), bumps the reload token on file changes, and
 * disables restart for a hosted (platform-registered) preview, which the
 * local start/restart/stop actions never apply to.
 */
export function PreviewPanel({ open, onClose }: PreviewPanelProps): JSX.Element | null {
  const preview = usePreviewStatus(open);

  if (!open) return null;

  const handleRestart = (): void => {
    if (preview.status === 'running') preview.restart();
    else preview.start();
  };

  return (
    <aside className="preview-panel" data-testid="preview-panel">
      <PreviewPane
        status={preview.status}
        url={preview.url}
        error={preview.error}
        onRestart={handleRestart}
        reloadKey={preview.reloadKey}
        restartDisabled={preview.hosted === true}
        onClose={onClose}
      />
    </aside>
  );
}

export default PreviewPanel;
