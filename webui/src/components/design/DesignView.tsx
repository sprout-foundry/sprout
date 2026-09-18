/**
 * DesignView — the design workspace surface (SP-140-3 §3a).
 *
 * Since the workspace-modes rework (SP-140-5) the app sidebar IS the design
 * surface's asset browser: the mode rail (flows/screens/tokens) picks the
 * section and the sidebar's content pane lists that section's assets, from
 * the shared DesignWorkspaceContext. This component is the rest of the
 * surface — the canvas and the detail pane — and nothing else. An earlier
 * revision rendered its own header and assets rail in here; that read as a
 * separate app pasted beside the sidebar (two left panels, a nested title
 * bar) and was folded into the shell instead.
 *
 * Each tab body is its own component (`FlowsCanvas`, `ScreensGrid`,
 * `TokensTree`) so this file never grows past the AGENTS.md 500-line rule.
 *
 * Outside a DesignWorkspaceProvider (standalone renders, component tests)
 * this view falls back to its own inventory fetch and selection state, so it
 * stays usable without the workspace shell.
 */

import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { listAssets } from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types';
import { useDesignWorkspace } from './DesignWorkspaceContext';
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

  // Shared state when the workspace shell provides it (sidebar assets pane,
  // canvas, and detail pane are views of one selection); own state otherwise.
  const workspace = useDesignWorkspace();
  const [fallbackInventory, setFallbackInventory] = useState<DesignInventory | null>(null);
  const [fallbackSelected, setFallbackSelected] = useState<string | null>(null);
  // Tab-body detail content (a flow's source line, a screen's LivePreview),
  // registered through the pane's onDetail contract.
  const [detail, setDetail] = useState<ReactNode>(null);
  const fetchFn = useSproutFetch();

  const inventory = workspace ? workspace.inventory : fallbackInventory;
  const selectedAsset = workspace ? workspace.selected : fallbackSelected;

  useEffect(() => {
    if (workspace) return;
    let cancelled = false;
    (async () => {
      try {
        const next = await listAssets(fetchFn);
        if (!cancelled) setFallbackInventory(next);
      } catch {
        if (!cancelled) setFallbackInventory(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [workspace, fetchFn]);

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

  const handleSelectAsset = useCallback(
    (path: string) => {
      if (workspace) workspace.select(path);
      else setFallbackSelected(path);
    },
    [workspace],
  );

  // The flows canvas follows the shared selection when the selected asset is
  // one of the flows; otherwise it keeps its first-flow default.
  const flows = inventory?.flows ?? [];
  const activeFlowPath = flows.some((flow) => flow.path === selectedAsset) ? selectedAsset : (flows[0]?.path ?? null);

  return (
    <div className="design-view" data-testid="design-view" data-active-tab={activeTab}>
      <div className="design-view-body">
        <section
          className="design-view-canvas"
          id="design-tabpanel"
          role="tabpanel"
          aria-label={`${activeTab} panel`}
          data-testid="design-tabpanel"
        >
          {!selectedAsset && (
            <div className="design-view-hint" role="status">
              Select an asset from the sidebar to inspect it
            </div>
          )}
          {activeTab === 'flows' && (
            <FlowsCanvasContainer
              flows={flows}
              wireframes={inventory ? inventory.wireframes : []}
              layouts={inventory ? inventory.layouts : []}
              activeFlowPath={activeFlowPath}
              onSelectAsset={handleSelectAsset}
              onSelectTab={changeTab}
              onOpenFile={onOpenFile}
            />
          )}
          {activeTab === 'screens' && (
            <ScreensTabContainer
              inventory={inventory}
              selectedPath={workspace ? workspace.selected : undefined}
              onSelectAsset={handleSelectAsset}
              onSelectTab={changeTab}
            />
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
