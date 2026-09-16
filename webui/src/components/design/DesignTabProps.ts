/**
 * Shared props for the DesignView tab bodies (SP-140-3 §3a).
 *
 * Each tab (Flows / Screens / Tokens) is its own component so DesignView
 * stays a shell under the AGENTS.md 500-line rule. They share one prop
 * contract: report the asset the user wants inspected in the detail pane.
 */

export interface DesignTabProps {
  /** Fired when the user picks an asset to inspect in the detail pane. */
  onSelectAsset?: (path: string) => void;
}
