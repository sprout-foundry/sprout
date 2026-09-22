/**
 * DesignView — the design workspace surface (SP-140-3 §3a).
 *
 * Since the workspace-modes rework (SP-140-5) the app sidebar IS the design
 * surface's asset browser: the mode rail (flows/screens/tokens) picks the
 * section and the sidebar's content pane lists that section's assets, from
 * the shared DesignWorkspaceContext. This component is the rest of the
 * surface — the canvas and the ONE right column (§6f rework: Details | Agent
 * tabs instead of a docked detail pane plus a docked chat panel) — and
 * nothing else.
 *
 * Each tab body is its own component (`FlowsCanvas`, `ScreensGrid`,
 * `TokensTree`) so this file never grows past the AGENTS.md 500-line rule.
 *
 * Outside a DesignWorkspaceProvider (standalone renders, component tests)
 * this view falls back to its own inventory fetch and selection state, so it
 * stays usable without the workspace shell.
 */

import { useCallback, useEffect, useRef, useState, type ComponentProps, type ReactNode } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { listAssets } from '../../services/api/designApi';
import { fetchDesignStatus, type DesignStatusDriftRow } from '../../services/api/designStatusApi';
import type { DesignInventory } from '../../services/api/types';
import { assetMatchesSection } from './assetNames';
import DesignDetailPane from './DesignDetailPane';
import { useDesignWorkspace } from './DesignWorkspaceContext';
import DesignSideColumn, { type DesignSideTab } from './DesignSideColumn';
import DesignAgentPanel from './DesignAgentPanel';
import { FlowsCanvasContainer } from './FlowsCanvasContainer';
import { ScreensTabContainer } from './ScreensGrid';
import TokensTree from './TokensTree';
import './DesignView.css';

/** The chat payload the Agent tab renders (the shell's own chat props). */
export type DesignChatProps = ComponentProps<typeof DesignAgentPanel>['chatProps'];

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
  /** The shell's chat payload (§6f) — passed through to the Agent tab. */
  chatProps?: DesignChatProps;
  /** Write transport override for the detail pane's feedback write (tests/hosts). */
  writeFetch?: typeof fetch;
  /** Consent-aware read override for the detail pane's resolution flow (§3f). */
  readFn?: typeof fetch;
  /** Consent-aware write override for the resolution flow (§3f). */
  writeFn?: typeof fetch;
}

export default function DesignView({
  tab,
  onTabChange,
  onOpenFile,
  chatProps,
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

  // §6f rework: the right column's active tab. Selecting an asset shows
  // Details; a prefill flips to Agent (below). Both bodies stay mounted.
  const [sideTab, setSideTab] = useState<DesignSideTab>('details');
  const [prefill, setPrefill] = useState<string | null>(null);

  // SP-140-10b: remount the agent panel's chat whenever the Agent tab is
  // shown again. The Agent body starts hidden (Details is the default tab),
  // so the chat's virtuoso scroller measures zero height on first mount and
  // followOutput / jump-to-latest can't work off that stale metric. A
  // fresh full-height mount on each Details→Agent flip shows the latest
  // message on the flip; the transcript and draft are shell-controlled
  // state, so the §6f "state survives flips" contract still holds.
  const [agentFlipKey, setAgentFlipKey] = useState(0);
  const agentTabVisibleRef = useRef(false);
  useEffect(() => {
    const visible = sideTab === 'agent';
    if (visible && !agentTabVisibleRef.current) setAgentFlipKey((key) => key + 1);
    agentTabVisibleRef.current = visible;
  }, [sideTab]);

  const inventory = workspace ? workspace.inventory : fallbackInventory;
  const rawSelected = workspace ? workspace.selected : fallbackSelected;
  // Selection is section-scoped: switching from Screens (screen selected) to
  // Tokens must not leave the previous section's asset in the detail pane.
  const selectedAsset = rawSelected && assetMatchesSection(rawSelected, activeTab) ? rawSelected : null;

  // §6f rework tab policy: any new selection shows the Details tab — that is
  // what the click was for. Covers both selection paths (sidebar pane and
  // canvas bodies); clearing a selection does NOT flip the tab back.
  useEffect(() => {
    if (rawSelected) setSideTab('details');
  }, [rawSelected]);

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

  /** Prefill the Agent tab and flip to it (never auto-sends). */
  const askAgent = useCallback((prompt: string) => {
    setPrefill(prompt);
    setSideTab('agent');
  }, []);

  // The flows canvas follows the shared selection when the selected asset is
  // one of the flows; otherwise it keeps its first-flow default.
  const flows = inventory?.flows ?? [];
  const activeFlowPath = flows.some((flow) => flow.path === selectedAsset) ? selectedAsset : (flows[0]?.path ?? null);

  // §6g: the selected asset's inventory `modified` (stale-marker input).
  // `assets` can be absent on partial inventories (tests, hosts) — optional
  // chain the array too.
  const selectedEntry = inventory?.assets?.find((asset) => asset.path === selectedAsset) ?? null;

  // §6c/§6g: the drift rows ride the same status the health strip reads. The
  // pane only needs the code-ahead row; refetch on selection change.
  const [codeAhead, setCodeAhead] = useState<DesignStatusDriftRow | null>(null);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const status = await fetchDesignStatus(fetchFn);
      if (!cancelled) setCodeAhead(status?.drift?.codeAhead ?? null);
    })();
    return () => {
      cancelled = true;
    };
  }, [fetchFn, selectedAsset]);

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
              selectedPath={selectedAsset}
              onSelectAsset={handleSelectAsset}
              onSelectTab={changeTab}
            />
          )}
          {activeTab === 'tokens' && (
            <TokensTree inventory={inventory} onSelectAsset={handleSelectAsset} onSelectTab={changeTab} />
          )}
        </section>

        <DesignSideColumn
          sideTab={sideTab}
          onSideTabChange={setSideTab}
          hasSelection={!!selectedAsset}
          details={
            <aside className="design-view-detail" aria-label="Design detail" data-testid="design-detail-pane">
              <DesignDetailPane
                path={selectedAsset}
                onOpenFile={onOpenFile}
                onDetail={setDetail}
                fetchFn={writeFetch}
                readFn={readFn}
                writeFn={writeFn}
                assetModified={selectedEntry?.modified}
                codeAhead={codeAhead}
                onAskAgent={askAgent}
              >
                {detail}
              </DesignDetailPane>
            </aside>
          }
          agent={
            chatProps ? (
              <DesignAgentPanel
                key={agentFlipKey}
                chatProps={chatProps}
                prefill={prefill}
                onPrefillConsumed={() => setPrefill(null)}
              />
            ) : (
              <div className="design-agent-absent">Agent chat is not available in this host.</div>
            )
          }
        />
      </div>
    </div>
  );
}
