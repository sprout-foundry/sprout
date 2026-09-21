/**
 * Design mode's single right column (the un-sidebar, SP-140-6 §6f rework).
 *
 * One column, two tabs — **Details** (the selected asset's pane: feedback,
 * critique, loop results) and **Agent** (the chat) — replacing the earlier
 * layout that docked a detail pane AND an always-open agent panel beside the
 * canvas (four columns with the sidebar; the canvas squeezed in the middle).
 *
 * Presentational only: the slots come from DesignView, which owns the detail
 * wiring and the chat payload. Tab policy lives there too:
 * - Selecting an asset shows Details (that is what the click was for).
 * - A prefill ("Adopt via agent", remedy rows, run-critique) flips to Agent
 *   with the prompt seeded in the input — never auto-sent.
 * - Both tab bodies stay mounted; `hidden` only hides. The chat keeps its
 *   transcript and draft across flips, and the detail content keeps its
 *   registered state.
 */

import type { ReactNode } from 'react';

/** The right column's tabs. */
export type DesignSideTab = 'details' | 'agent';

export interface DesignSideColumnProps {
  /** The visible tab. */
  sideTab: DesignSideTab;
  onSideTabChange: (tab: DesignSideTab) => void;
  /** Details tab body: the selected asset's pane. */
  details: ReactNode;
  /** Agent tab body: the chat. */
  agent: ReactNode;
  /** True when an asset is selected (drives the idle marker). */
  hasSelection: boolean;
}

export default function DesignSideColumn({
  sideTab,
  onSideTabChange,
  details,
  agent,
  hasSelection,
}: DesignSideColumnProps) {
  return (
    <aside className="design-side-column" data-testid="design-side-column" data-tab={sideTab}>
      <div className="design-side-tabs" role="tablist" aria-label="Design side panel">
        <button
          type="button"
          role="tab"
          id="design-side-tab-details"
          aria-selected={sideTab === 'details'}
          aria-controls="design-side-panel-details"
          className={`design-side-tab ${sideTab === 'details' ? 'active' : ''}`}
          onClick={() => onSideTabChange('details')}
          data-testid="design-side-tab-details"
        >
          Details
        </button>
        <button
          type="button"
          role="tab"
          id="design-side-tab-agent"
          aria-selected={sideTab === 'agent'}
          aria-controls="design-side-panel-agent"
          className={`design-side-tab ${sideTab === 'agent' ? 'active' : ''}`}
          onClick={() => onSideTabChange('agent')}
          data-testid="design-side-tab-agent"
        >
          Agent
        </button>
      </div>
      <div
        className="design-side-body"
        role="tabpanel"
        id="design-side-panel-details"
        aria-labelledby="design-side-tab-details"
        hidden={sideTab !== 'details'}
        data-testid="design-side-panel-details"
        data-idle={!hasSelection}
      >
        {details}
      </div>
      <div
        className="design-side-body"
        role="tabpanel"
        id="design-side-panel-agent"
        aria-labelledby="design-side-tab-agent"
        hidden={sideTab !== 'agent'}
        data-testid="design-side-panel-agent"
      >
        {agent}
      </div>
    </aside>
  );
}
