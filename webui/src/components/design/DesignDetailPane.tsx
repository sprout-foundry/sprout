/**
 * Right detail pane for DesignView (SP-140-3 §3a).
 *
 * Holds the pane's structure: the selected asset's heading, the open-in-editor
 * hand-off, the feedback affordance (§3e), and a slot for tab-specific detail.
 * The tab that owns the selection renders that detail (a flow's node/edge
 * source line, a screen's LivePreview split, a token's JSON) and registers it
 * through `onDetail`, so the shell never learns each tab's shape.
 *
 * The feedback annotation affordance is mounted here because every tab's
 * selection flows through this pane's `path` — that is the asset an annotation
 * attaches to, whatever surface picked it.
 */

import type { ReactNode } from 'react';
import DesignFeedbackAffordance from './DesignFeedbackAffordance';

export interface DesignDetailPaneProps {
  /** Currently selected asset path, relative to the design/ root. */
  path?: string | null;
  /** Fired when the user asks for the asset to open in the editor. */
  onOpenFile?: (path: string) => void;
  /** Registers the active tab's detail content for this pane to render. */
  onDetail?: (content: ReactNode) => void;
  /** Tab-supplied detail content. */
  children?: ReactNode;
  /** Test/host seam for the feedback write transport. */
  fetchFn?: typeof fetch;
}

export default function DesignDetailPane({ path, onOpenFile, children, fetchFn }: DesignDetailPaneProps) {
  return (
    <div className="design-detail" data-testid="design-detail-content" data-selected={path ?? ''}>
      {path ? (
        <>
          <h2 className="design-detail-heading" title={path}>
            {path}
          </h2>
          {onOpenFile ? (
            <button type="button" className="design-detail-open" onClick={() => onOpenFile(path)}>
              Open in editor
            </button>
          ) : null}
          <DesignFeedbackAffordance path={path} fetchFn={fetchFn} />
          {children}
        </>
      ) : (
        <p className="design-detail-placeholder">Select an asset to inspect it.</p>
      )}
    </div>
  );
}
