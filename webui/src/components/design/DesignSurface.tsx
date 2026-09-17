/**
 * Design mode's surface (SP-140-5 workspace modes).
 *
 * A peer of the Code surface, not a tenant of it. Design mode is entered via
 * the top-left mode switcher; when it is active this is what the shell puts in
 * the content area. That is why the design surface does NOT live inside
 * EditorWorkspace's view switch: sitting in the editor's slot meant the
 * editor's chrome (menubar, activity panel, status bar) stayed wrapped around
 * a surface that has nothing to do with editing a buffer, which read as a
 * different app pasted into the editor pane.
 *
 * The presence gate is kept here rather than in the router: a workspace with no
 * `design/` tree has no Design mode to offer, so the chunk must never load and
 * the surface must never render. `AppContent` owns the redirect back to Code
 * for that case; this component only decides whether it can render at all.
 */

import React, { Suspense, lazy } from 'react';
import { SkeletonText } from '@sprout/ui';
import ErrorBoundary from '../ErrorBoundary';
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
  onBack?: () => void;
  onOpenFile?: (path: string, lineNumber?: number) => void;
}

const DesignSurface: React.FC<DesignSurfaceProps> = ({
  loading,
  present,
  tab,
  onTabChange,
  onBack,
  onOpenFile,
}) => {
  if (loading) return <SurfaceFallback />;

  // No design/ tree: AppContent moves us back to Code; hold the fallback
  // meanwhile rather than rendering a dead surface.
  if (!present) return <SurfaceFallback />;

  return (
    <div className="design-surface" data-testid="design-surface">
      <ErrorBoundary panelName="Design">
        <Suspense fallback={<SurfaceFallback />}>
          <DesignView onBack={onBack} onOpenFile={onOpenFile} tab={tab} onTabChange={onTabChange} />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
};

export default DesignSurface;
