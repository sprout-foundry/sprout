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
  /** Initial tab; defaults to Flows. */
  initialTab?: DesignTab;
  /** Called when the user leaves DesignView (back to chat). */
  onBack?: () => void;
  /** Called when a design asset should open in the editor. */
  onOpenFile?: (path: string) => void;
  /** Write transport override for the detail pane's feedback write (tests/hosts). */
  writeFetch?: typeof fetch;
}

export default function DesignView({ initialTab = 'flows', onBack, onOpenFile, writeFetch }: DesignViewProps = {}) {
  const [activeTab, setActiveTab] = useState<DesignTab>(initialTab);
  const [selectedAsset, setSelectedAsset] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReactNode>(null);
  const [inventory, setInventory] = useState<DesignInventory | null>(null);
  const fetchFn = useSproutFetch();

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
        <div className="design-view-tabs" role="tablist" aria-label="Design views">
          {DESIGN_TABS.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="tab"
              id={`design-tab-${tab.id}`}
              aria-selected={activeTab === tab.id}
              aria-controls="design-tabpanel"
              className={`design-view-tab ${activeTab === tab.id ? 'active' : ''}`}
              onClick={() => setActiveTab(tab.id)}
              data-testid={`design-tab-${tab.id}`}
            >
              {tab.label}
            </button>
          ))}
        </div>
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
          aria-labelledby={`design-tab-${activeTab}`}
          data-testid="design-tabpanel"
        >
          {activeTab === 'flows' && (
            <FlowsCanvasContainer
              flows={inventory ? inventory.flows : []}
              wireframes={inventory ? inventory.wireframes : []}
              layouts={inventory ? inventory.layouts : []}
              activeFlowPath={inventory?.flows?.[0]?.path ?? null}
              onSelectAsset={handleSelectAsset}
              onOpenFile={onOpenFile}
            />
          )}
          {activeTab === 'screens' && <ScreensTabContainer inventory={inventory} onSelectAsset={handleSelectAsset} />}
          {activeTab === 'tokens' && <TokensTree onSelectAsset={handleSelectAsset} />}
        </section>

        <aside className="design-view-detail" aria-label="Design detail" data-testid="design-detail-pane">
          <DesignDetailPane path={selectedAsset} onOpenFile={onOpenFile} onDetail={setDetail} fetchFn={writeFetch}>
            {detail}
          </DesignDetailPane>
        </aside>
      </div>
    </div>
  );
}
