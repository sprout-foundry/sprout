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
 * design-ahead opens Tokens; code-ahead asks the agent (the shell owns the
 * panel, so the intent crosses as a callback).
 *
 * The presence gate is kept here rather than in the router: a workspace with no
 * `design/` tree has no Design mode to offer, so the chunk must never load and
 * the surface must never render. `AppContent` owns the redirect back to Code
 * for that case; this component only decides whether it can render at all.
 */

import React, { Suspense, lazy } from 'react';
import { SkeletonText } from '@sprout/ui';
import ErrorBoundary from '../ErrorBoundary';
import { useDesignWorkspace } from './DesignWorkspaceContext';
import HealthStrip from './HealthStrip';
import type { DesignTab } from './DesignView';

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
  /** The active section, driven by the mode's rail. */
  tab: DesignTab;
  onTabChange: (tab: DesignTab) => void;
  onOpenFile?: (path: string, lineNumber?: number) => void;
  /** Code-ahead click: open the agent panel with this prefill (§6c/§6f). */
  onAskAgent?: (prompt: string) => void;
}

const DesignSurface: React.FC<DesignSurfaceProps> = ({ loading, present, tab, onTabChange, onOpenFile, onAskAgent }) => {
  const workspace = useDesignWorkspace();

  if (loading) return <SurfaceFallback />;

  // No design/ tree: AppContent moves us back to Code; hold the fallback
  // meanwhile rather than rendering a dead surface.
  if (!present) return <SurfaceFallback />;

  return (
    <div className="design-surface" data-testid="design-surface">
      {/* §6c health strip: the loop's status line (validate/drift/feedback). */}
      <ErrorBoundary panelName="Design health strip">
        <HealthStrip
          onOpenFinding={(path) => {
            // The inventory's paths are design-root-relative (§3f vocabulary);
            // the shared selection expects the same shape.
            workspace?.select(path);
          }}
          onOpenSection={onTabChange}
          onAskAgent={onAskAgent}
        />
      </ErrorBoundary>
      <ErrorBoundary panelName="Design">
        <Suspense fallback={<SurfaceFallback />}>
          <DesignView onOpenFile={onOpenFile} tab={tab} onTabChange={onTabChange} />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
};

export default DesignSurface;
