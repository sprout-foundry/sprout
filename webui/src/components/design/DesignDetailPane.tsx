/**
 * Right detail pane for DesignView (SP-140-3 §3a).
 *
 * Holds the pane's structure: the selected asset's heading, the open-in-editor
 * hand-off, the feedback affordance (§3e), the resolution flow (§4d, item 4.8),
 * and a slot for tab-specific detail. The tab that owns the selection renders
 * that detail (a flow's node/edge source line, a screen's LivePreview split, a
 * token's JSON) and registers it through `onDetail`, so the shell never learns
 * each tab's shape.
 *
 * The feedback annotation affordance and its resolution flow are mounted here
 * because every tab's selection flows through this pane's `path` — that is the
 * asset an annotation attaches to (and the feedback file it is keyed on),
 * whatever surface picked it.
 */

import type { ReactNode } from 'react';
import DesignFeedbackAffordance from './DesignFeedbackAffordance';
import DesignFeedbackResolution from './DesignFeedbackResolution';
import { assetDisplayName } from './DesignAssetsRail';

/** The pane heading shows the artifact name; the path stays as a caption. */
function detailName(path: string): string {
  return assetDisplayName(path.split('/').pop() ?? path);
}

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
  /** Consent-aware read override for the resolution flow (§3f). */
  readFn?: typeof fetch;
  /** Consent-aware write override for the resolution flow (§3f). */
  writeFn?: typeof fetch;
}

export default function DesignDetailPane({
  path,
  onOpenFile,
  children,
  fetchFn,
  readFn,
  writeFn,
}: DesignDetailPaneProps) {
  return (
    <div className="design-detail" data-testid="design-detail-content" data-selected={path ?? ''}>
      {path ? (
        <>
          <div className="design-detail-title">
            <h2 className="design-detail-heading">{detailName(path)}</h2>
            <p className="design-detail-path" title={path}>
              {path}
            </p>
          </div>
          {onOpenFile ? (
            <button type="button" className="design-detail-open" onClick={() => onOpenFile(path)}>
              Open in editor
            </button>
          ) : null}
          <DesignFeedbackAffordance path={path} fetchFn={fetchFn} />
          <DesignFeedbackResolution path={path} fetchFn={fetchFn} readFn={readFn} writeFn={writeFn} />
          {children}
        </>
      ) : (
        <div className="design-detail-empty">
          <p className="design-detail-placeholder">Select an asset to inspect it.</p>
        </div>
      )}
    </div>
  );
}
