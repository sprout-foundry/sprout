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
 */

import { useCallback, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import FlowsCanvas from './FlowsCanvas';
import ScreensGrid from './ScreensGrid';
import TokensTree from './TokensTree';
import DesignAssetsRail from './DesignAssetsRail';
import DesignDetailPane from './DesignDetailPane';
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
}

export default function DesignView({ initialTab = 'flows', onBack, onOpenFile }: DesignViewProps = {}) {
  const [activeTab, setActiveTab] = useState<DesignTab>(initialTab);
  const [selectedAsset, setSelectedAsset] = useState<string | null>(null);

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
          <DesignAssetsRail tab={activeTab} onSelect={handleSelectAsset} />
        </aside>

        <section
          className="design-view-canvas"
          id="design-tabpanel"
          role="tabpanel"
          aria-labelledby={`design-tab-${activeTab}`}
          data-testid="design-tabpanel"
        >
          {activeTab === 'flows' && <FlowsCanvas onSelectAsset={handleSelectAsset} />}
          {activeTab === 'screens' && <ScreensGrid onSelectAsset={handleSelectAsset} />}
          {activeTab === 'tokens' && <TokensTree onSelectAsset={handleSelectAsset} />}
        </section>

        <aside className="design-view-detail" aria-label="Design detail" data-testid="design-detail-pane">
          <DesignDetailPane path={selectedAsset} onOpenFile={onOpenFile} />
        </aside>
      </div>
    </div>
  );
}
