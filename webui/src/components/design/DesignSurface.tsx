/**
 * Design mode's surface (SP-140-5 workspace modes; §6c health strip).
 *
 * A peer of the Code surface, not a tenant of it. Design mode is entered via
 * the top-left mode switcher; when it is active this is what the shell puts in
 * the content area. That is why the design surface does NOT live inside
 * EditorWorkspace's view switch: sitting in the editor's slot meant the
 * editor's chrome (menubar, activity panel, status bar) stayed wrapped around
 * a surface that has nothing to do with editing a buffer, which read as a
 * different app pasted into the editor pane.
 *
 * §6c: the health strip sits across the top — validate tallies, the two drift
 * rows, pending feedback — with click-throughs into the surfaces it names.
 * Finding/feedback clicks select the asset (the shared workspace context);
 * design-ahead opens Tokens; code-ahead prefills the agent (the prefill
 * handoff lives in DesignView's side column).
 *
 * An empty workspace (no `design/` tree) is a first-class state here, not a
 * redirect: the surface renders the onboarding empty state (starter cards
 * + the agent chat), which is how a tree comes to exist in the first place.
 * The DesignView chunk stays unloaded until the tree exists — the empty
 * state is cheap, the view is not (AC 5's bundle split still holds).
 */

import { SkeletonText } from '@sprout/ui';
import React, { Suspense, lazy } from 'react';
import ErrorBoundary from '../ErrorBoundary';
import DesignAgentPanel from './DesignAgentPanel';
import DesignEmptyState from './DesignEmptyState';
import type { DesignChatProps, DesignTab } from './DesignView';
import { useDesignWorkspace } from './DesignWorkspaceContext';
import './DesignEmptyState.css';
import HealthStrip from './HealthStrip';

const DesignView = lazy(() => import('./DesignView').then((m) => ({ default: m.default })));

const SurfaceFallback: React.FC = () => (
  <div className="editor-workspace-route-fallback" data-testid="design-surface-fallback">
    <SkeletonText lines={6} />
  </div>
);

export interface DesignSurfaceProps {
  /** True while the design-presence probe is in flight. */
  loading: boolean;
  /** True when the workspace has a `design/` tree. */
  present: boolean;
  /** How design/ relates to Sprout's idiom (empty state's banner signal). */
  treeState?: 'none' | 'foreign' | 'recognized';
  /** True when the workspace shows frontend code (empty-state card order). */
  frontendLike?: boolean;
  /** The active section, driven by the mode's rail. */
  tab: DesignTab;
  onTabChange: (tab: DesignTab) => void;
  onOpenFile?: (path: string, lineNumber?: number) => void;
  /** The shell's chat payload (§6f) for the side column's Agent tab. */
  chatProps?: DesignChatProps;
  /** Re-run the design-presence probe (empty state's "Check again"). */
  onRecheck?: () => void;
}

/** No-op when the host hasn't wired a recheck (probe becomes mount-only). */
// eslint-disable-next-line @typescript-eslint/no-empty-function
const noop = () => {};

const DesignSurface: React.FC<DesignSurfaceProps> = ({
  loading,
  present,
  treeState = 'none',
  frontendLike = false,
  tab,
  onTabChange,
  onOpenFile,
  chatProps,
  onRecheck,
}) => {
  const workspace = useDesignWorkspace();
  // §6c/§6a: the strip's refresh control refetches the inventory too — one
  // control, both views of the tree. Bumping refreshKey re-runs the strip's
  // fetch effect; workspace.refresh() re-runs the inventory fetch.
  const [stripRefreshKey, setStripRefreshKey] = React.useState(0);
  const handleStripRefresh = React.useCallback(() => {
    setStripRefreshKey((key) => key + 1);
    workspace?.refresh();
  }, [workspace]);

  // The empty state's prefill handoff — the same contract DesignView's side
  // column uses: fill the input, focus the agent side, never auto-send.
  const [emptyPrefill, setEmptyPrefill] = React.useState<string | null>(null);
  const handleAskAgent = React.useCallback((prompt: string) => {
    setEmptyPrefill(prompt);
  }, []);

  if (loading) return <SurfaceFallback />;

  // No design/ tree: the onboarding surface. The agent column sits beside the
  // starter cards because the chat is how a tree gets started.
  if (!present) {
    return (
      <div className="design-surface design-surface--empty" data-testid="design-surface-empty">
        <ErrorBoundary panelName="Design empty state">
          <div className="design-empty-layout">
            <DesignEmptyState
              onAskAgent={handleAskAgent}
              onRecheck={onRecheck ?? noop}
              frontendCode={frontendLike}
              foreignTree={treeState === 'foreign'}
            />
            <aside className="design-empty-agent" aria-label="Agent chat" data-testid="design-empty-agent">
              <div className="design-empty-agent-head">
                <span className="design-empty-agent-title">Agent</span>
                <span className="design-empty-agent-sub">Ask, answer, iterate — the tree grows from the chat.</span>
              </div>
              {chatProps ? (
                <DesignAgentPanel
                  chatProps={chatProps}
                  prefill={emptyPrefill}
                  onPrefillConsumed={() => setEmptyPrefill(null)}
                />
              ) : (
                <div className="design-agent-absent">Agent chat is not available in this host.</div>
              )}
            </aside>
          </div>
        </ErrorBoundary>
      </div>
    );
  }

  return (
    <div className="design-surface" data-testid="design-surface">
      {/* §6c health strip: the loop's status line (validate/drift/feedback). */}
      <ErrorBoundary panelName="Design health strip">
        <HealthStrip
          refreshKey={stripRefreshKey}
          onRefresh={handleStripRefresh}
          onOpenFinding={(path) => {
            // The inventory's paths are design-root-relative (§3f vocabulary);
            // the shared selection expects the same shape.
            workspace?.select(path);
          }}
          onOpenSection={onTabChange}
        />
      </ErrorBoundary>
      <ErrorBoundary panelName="Design">
        <Suspense fallback={<SurfaceFallback />}>
          <DesignView onOpenFile={onOpenFile} tab={tab} onTabChange={onTabChange} chatProps={chatProps} />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
};

export default DesignSurface;
