/**
 * Shared props for the DesignView tab bodies (SP-140-3 §3a).
 *
 * Each tab (Flows / Screens / Tokens) is its own component so DesignView
 * stays a shell under the AGENTS.md 500-line rule. They share one prop
 * contract: report the asset the user wants inspected in the detail pane.
 */

import type { DesignTab } from './DesignView';

export interface DesignTabProps {
  /** Fired when the user picks an asset to inspect in the detail pane. */
  onSelectAsset?: (path: string) => void;
  /**
   * Fired when the body wants the surface to switch sections (SP-140-5). The
   * mode's rail drives the section from the shell; a body requesting a switch
   * goes through this so the rail and the surface stay in step.
   */
  onSelectTab?: (tab: DesignTab) => void;
}
