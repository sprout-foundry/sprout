/**
 * Left assets rail for DesignView (SP-140-3 §3a).
 *
 * Shell-stage stub: the tab-aware inventory list (wireframes, screens, flows,
 * tokens from `designApi.listAssets`) is built out with the tabs in items
 * 3.5–3.8. Until then it renders the rail's structure plus a single
 * placeholder row per tab, which keeps the rail → detail-pane selection
 * contract live (and covered) rather than inert.
 */

import type { DesignTab } from './DesignView';

export interface DesignAssetsRailProps {
  /** The active tab — the rail lists the asset class for this tab. */
  tab: DesignTab;
  /** Fired when the user picks an asset to inspect. */
  onSelect?: (path: string) => void;
}

const RAIL_LABELS: Record<DesignTab, string> = {
  flows: 'Flows',
  screens: 'Screens',
  tokens: 'Tokens',
};

/** Placeholder stem per tab, used for the shell-stage row's selection path. */
const RAIL_STUBS: Record<DesignTab, string> = {
  flows: 'flows/',
  screens: 'screens/',
  tokens: 'tokens/',
};

export default function DesignAssetsRail({ tab, onSelect }: DesignAssetsRailProps) {
  const label = RAIL_LABELS[tab];
  return (
    <div className="design-rail" data-testid="design-assets-rail" data-tab={tab}>
      <h2 className="design-rail-heading">{label}</h2>
      <p className="design-rail-placeholder">Asset browser lands with the {label} tab.</p>
      {onSelect && (
        <button
          type="button"
          className="design-rail-stub-row"
          onClick={() => onSelect(RAIL_STUBS[tab])}
          data-testid="design-rail-stub-row"
        >
          {label} (placeholder)
        </button>
      )}
    </div>
  );
}
