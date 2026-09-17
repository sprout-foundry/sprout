/**
 * Left assets rail for DesignView (SP-140-3 §3a).
 *
 * Lists the asset class for the active tab from the inventory DesignView
 * already fetched (wireframes, screens, flows, tokens — §3a's "canvas as the
 * primary surface, with a left rail (assets browser)"). Rows report the
 * selection through the rail → detail-pane contract; with no inventory yet
 * (still loading, or a workspace without `design/`) the rail shows its
 * placeholder rather than an empty box.
 */

import type { DesignAssetEntry, DesignInventory } from '../../services/api/types';
import type { DesignTab } from './DesignView';

export interface DesignAssetsRailProps {
  /** The active tab — the rail lists the asset class for this tab. */
  tab: DesignTab;
  /** Design inventory for the workspace, when it has been resolved. */
  inventory?: DesignInventory | null;
  /** Currently selected asset path, highlighted in the list. */
  selected?: string | null;
  /** Fired when the user picks an asset to inspect. */
  onSelect?: (path: string) => void;
}

const RAIL_LABELS: Record<DesignTab, string> = {
  flows: 'Flows',
  screens: 'Screens',
  tokens: 'Tokens',
};

/** The inventory slice the rail lists for a tab. */
export function assetsForTab(tab: DesignTab, inventory?: DesignInventory | null): DesignAssetEntry[] {
  if (!inventory) return [];
  if (tab === 'flows') return inventory.flows;
  if (tab === 'screens') return inventory.screens;
  return inventory.tokenFiles;
}

export default function DesignAssetsRail({ tab, inventory, selected, onSelect }: DesignAssetsRailProps) {
  const label = RAIL_LABELS[tab];
  const assets = assetsForTab(tab, inventory);

  return (
    <div className="design-rail" data-testid="design-assets-rail" data-tab={tab}>
      <h2 className="design-rail-heading">{label}</h2>
      {assets.length === 0 ? (
        <p className="design-rail-placeholder">No {label.toLowerCase()} in this workspace.</p>
      ) : (
        <ul className="design-rail-list">
          {assets.map((asset) => (
            <li key={asset.path}>
              <button
                type="button"
                className={`design-rail-row${asset.path === selected ? ' selected' : ''}`}
                onClick={() => onSelect?.(asset.path)}
                data-testid={`design-rail-row-${asset.path}`}
                title={asset.path}
              >
                {asset.name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
