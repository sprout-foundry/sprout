/**
 * Right detail pane for DesignView (SP-140-3 §3a).
 *
 * Shell-stage stub: the real detail pane (flow edge list + source, LivePreview
 * split for screens, token JSON open-in-editor, feedback annotation
 * affordance) arrives with items 3.5/3.7/3.8/3.9. For now it holds the pane's
 * structure and the open-in-editor hand-off contract.
 */

export interface DesignDetailPaneProps {
  /** Currently selected asset path, relative to the design/ root. */
  path?: string | null;
  /** Fired when the user asks for the asset to open in the editor. */
  onOpenFile?: (path: string) => void;
}

export default function DesignDetailPane({ path, onOpenFile }: DesignDetailPaneProps) {
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
        </>
      ) : (
        <p className="design-detail-placeholder">Select an asset to inspect it.</p>
      )}
    </div>
  );
}
