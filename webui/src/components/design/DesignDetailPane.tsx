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
import type { DesignStatusDriftRow } from '../../services/api/designStatusApi';
import { assetDisplayName } from './assetNames';
import DesignFeedbackAffordance from './DesignFeedbackAffordance';
import DesignFeedbackResolution from './DesignFeedbackResolution';
import LoopResults from './LoopResults';

/** The pane heading shows the artifact name; the path stays as a caption. */
function detailName(path: string): string {
  return assetDisplayName(path.split('/').pop() ?? path);
}

/**
 * §4d feedback and §6g critique flows are asset-scoped (screens, wireframes,
 * flows — any artifact a reviewer can annotate per SP-140-4): only token
 * selections skip them, because a DTCG file's detail pane is an editor, not
 * a review target. Hosts spell screen paths two ways — design-root-relative
 * (`screens/login.html`, the workspace context's selection) and
 * `design/`-prefixed (status findings, some tests) — so accept both.
 */
function isAnnotatableAsset(path: string): boolean {
  if (path.includes('/tokens/') || path.startsWith('tokens/') || path.endsWith('.tokens.json')) {
    return false;
  }
  return true;
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
  /** The selected asset's inventory `modified` (unix seconds), for §6g. */
  assetModified?: number;
  /** The code-ahead drift row from the status endpoint, when ahead (§6g). */
  codeAhead?: DesignStatusDriftRow | null;
  /** Prefill the agent panel (§6f/§6g). */
  onAskAgent?: (prompt: string) => void;
}

export default function DesignDetailPane({
  path,
  onOpenFile,
  children,
  fetchFn,
  readFn,
  writeFn,
  assetModified,
  codeAhead,
  onAskAgent,
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
          {/* §4d feedback flows are asset-scoped, tokens excluded
              (see isAnnotatableAsset). */}
          {isAnnotatableAsset(path) ? (
            <>
              <DesignFeedbackAffordance path={path} fetchFn={fetchFn} />
              <DesignFeedbackResolution path={path} fetchFn={fetchFn} readFn={readFn} writeFn={writeFn} />
            </>
          ) : null}
          {children}
          {/* §6g: last critique + drift context. Advisory; last in the pane. */}
          {isAnnotatableAsset(path) ? (
            <LoopResults path={path} assetModified={assetModified} codeAhead={codeAhead} onAskAgent={onAskAgent} />
          ) : null}
        </>
      ) : (
        <div className="design-detail-empty">
          <p className="design-detail-placeholder">Select an asset to inspect it.</p>
        </div>
      )}
    </div>
  );
}
