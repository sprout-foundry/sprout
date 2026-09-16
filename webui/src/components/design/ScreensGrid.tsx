/**
 * Screens tab body — placeholder for SP-140-3 item 3.7.
 *
 * The real grid (SVG thumbnails, README status chips, device-frame-aware
 * sizing, LivePreview detail pane) lands in item 3.7; this stub keeps the
 * shell's tab structure complete and type-checked.
 */

import type { DesignTabProps } from './DesignTabProps';

export default function ScreensGrid(_props: DesignTabProps = {}) {
  return (
    <div className="design-tab-body" data-testid="design-screens-grid">
      <p className="design-tab-placeholder">Screen browser — not yet implemented (SP-140-3 item 3.7).</p>
    </div>
  );
}
