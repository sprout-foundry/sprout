/**
 * DesignView — the design workspace surface (SP-140-3 §3a).
 *
 * Shell only: routing entry, tab state, and the three-pane layout. Each tab
 * body is its own component (`FlowsCanvas`, `ScreensGrid`, `TokensTree`) so
 * this file never grows past the AGENTS.md 500-line rule, and the heavy tab
 * code (React Flow, dagre, mermaid, previews) is owned by those modules as
 * they land.
 *
 * Layout follows the spec: the canvas is the primary surface, with a left
 * rail (assets browser) and a right detail pane.
 *
 * The shell owns the inventory (`designApi.listAssets`) because the rail and
 * every tab body read the same asset lists; tabs receive them as props and
 * stay pure functions of workspace state. `DesignDetailPane` takes its content
 * as children so a tab can hand it the selected node/edge detail without the
 * shell learning each tab's shape (SP-140-3 §3a: shell only).
 */

import { ArrowLeft } from 'lucide-react';
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { listAssets } from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types';
import DesignAssetsRail from './DesignAssetsRail';
import DesignDetailPane from './DesignDetailPane';
import { FlowsCanvasContainer } from './FlowsCanvasContainer';
import { ScreensTabContainer } from './ScreensGrid';
import TokensTree from './TokensTree';
import './DesignView.css';

export type DesignTab = 'flows' | 'screens' | 'tokens';

interface DesignTabSpec {
  id: DesignTab;
  label: string;
}

/** Tab order mirrors the asset classes: flows → screens → tokens. */
export const DESIGN_TABS: DesignTabSpec[] = [
  { id: 'flows', label: 'Flows' },
  { id: 'screens', label: 'Screens' },
  { id: 'tokens', label: 'Tokens' },
];

export interface DesignViewProps {
  /**
   * The active tab. Controlled by the shell so the mode's rail drives this
   * surface: a rail that cannot reflect (or set) the current section is
   * decorative, and the in-view tab strip it replaces was a second control for
   * the same state.
   */
  tab?: DesignTab;
  /** Fired when the surface wants to change section (not used while controlled). */
  onTabChange?: (tab: DesignTab) => void;
  /** Called when the user leaves DesignView (back to chat). */
  onBack?: () => void;
  /** Called when a design asset should open in the editor. */
  onOpenFile?: (path: string, lineNumber?: number) => void;
  /** Write transport override for the detail pane's feedback write (tests/hosts). */
  writeFetch?: typeof fetch;
  /** Consent-aware read override for the detail pane's resolution flow (§3f). */
  readFn?: typeof fetch;
  /** Consent-aware write override for the detail pane's resolution flow (§3f). */
  writeFn?: typeof fetch;
}

export default function DesignView({
  tab,
  onTabChange,
  onBack,
  onOpenFile,
  writeFetch,
  readFn,
  writeFn,
}: DesignViewProps = {}) {
  // SP-140-5: the in-view tab strip is gone — the mode's rail
  // (workspaces/rail.ts) is the section control and the shell drives `tab`.
  // The uncontrolled fallback keeps the component usable standalone (tests,
  // hosts without a rail); `onTabChange` is the seam for surface-initiated
  // section changes, so a host that owns the rail state never drifts from
  // the surface.
  const [internalTab, setInternalTab] = useState<DesignTab>('flows');
  const activeTab = tab ?? internalTab;
  const [selectedAsset, setSelectedAsset] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReactNode>(null);
  const [inventory, setInventory] = useState<DesignInventory | null>(null);
  const fetchFn = useSproutFetch();

  /**
   * The surface's section-change path. The mode's rail is the section control
   * and the shell drives `tab`; when the surface itself asks for a section
   * (a tab body wanting to be shown), uncontrolled hosts apply it to local
   * state and every host hears about it through `onTabChange`, so the rail
   * and the surface stay in step. It is handed to the tab bodies as
   * `onSelectTab` so a body can request a switch without the shell learning
   * each body's shape.
   */
  const changeTab = useCallback(
    (next: DesignTab) => {
      if (tab === undefined) {
        setInternalTab(next);
      }
      onTabChange?.(next);
    },
    [tab, onTabChange],
  );

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const next = await listAssets(fetchFn);
        if (!cancelled) setInventory(next);
      } catch {
        if (!cancelled) setInventory(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [fetchFn]);

  const handleSelectAsset = useCallback((path: string) => {
    setSelectedAsset(path);
  }, []);

  return (
    <div className="design-view" data-testid="design-view" data-active-tab={activeTab}>
      <header className="design-view-header">
        {onBack && (
          <button
            type="button"
            className="design-view-back"
            onClick={onBack}
            title="Back to chat"
            aria-label="Back to chat"
          >
            <ArrowLeft size={16} />
          </button>
        )}
        <h1 className="design-view-title">Design</h1>
      </header>

      <div className="design-view-body">
        <aside className="design-view-rail" aria-label="Design assets">
          <DesignAssetsRail
            tab={activeTab}
            inventory={inventory}
            selected={selectedAsset}
            onSelect={handleSelectAsset}
          />
        </aside>

        <section
          className="design-view-canvas"
          id="design-tabpanel"
          role="tabpanel"
          aria-label={`${activeTab} panel`}
          data-testid="design-tabpanel"
        >
          {!selectedAsset && (
            <div className="design-view-hint" role="status">
              Select an asset from the rail to inspect it
            </div>
          )}
          {activeTab === 'flows' && (
            <FlowsCanvasContainer
              flows={inventory ? inventory.flows : []}
              wireframes={inventory ? inventory.wireframes : []}
              layouts={inventory ? inventory.layouts : []}
              activeFlowPath={inventory?.flows?.[0]?.path ?? null}
              onSelectAsset={handleSelectAsset}
              onSelectTab={changeTab}
              onOpenFile={onOpenFile}
            />
          )}
          {activeTab === 'screens' && (
            <ScreensTabContainer inventory={inventory} onSelectAsset={handleSelectAsset} onSelectTab={changeTab} />
          )}
          {activeTab === 'tokens' && (
            <TokensTree inventory={inventory} onSelectAsset={handleSelectAsset} onSelectTab={changeTab} />
          )}
        </section>

        <aside
          className="design-view-detail"
          aria-label="Design detail"
          data-testid="design-detail-pane"
          data-idle={!selectedAsset}
        >
          <DesignDetailPane
            path={selectedAsset}
            onOpenFile={onOpenFile}
            onDetail={setDetail}
            fetchFn={writeFetch}
            readFn={readFn}
            writeFn={writeFn}
          >
            {detail}
          </DesignDetailPane>
        </aside>
      </div>
    </div>
  );
}
