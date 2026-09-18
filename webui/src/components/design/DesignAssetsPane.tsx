/**
 * The Design mode's sidebar content pane (SP-140-5 workspace modes).
 *
 * The app sidebar is the design surface's asset browser: the mode rail's
 * icon (flows/screens/tokens) picks the section and this pane lists that
 * section's assets — the same role the Code mode's Files pane plays for
 * code. One sidebar, one content pane, no second rail inside the surface.
 *
 * Reads the shared DesignWorkspaceContext; outside a provider (hosts/tests
 * without the workspace shell) it renders nothing, matching the old
 * renderContentPane default for mode sections.
 */

import type { DesignAssetEntry, DesignInventory } from '../../services/api/types';
import { useDesignWorkspace } from './DesignWorkspaceContext';
import { assetDisplayName } from './assetNames';

/** The inventory slice for a section. Shared with DesignAssetsRail's logic. */
function entriesFor(tab: string, inventory: DesignInventory | null): DesignAssetEntry[] {
  if (!inventory) return [];
  if (tab === 'flows') return inventory.flows;
  if (tab === 'screens') return inventory.screens;
  if (tab === 'tokens') return inventory.tokenFiles;
  return [];
}

const EMPTY_PLACEHOLDER: Record<string, string> = {
  flows: 'No flows in this workspace.',
  screens: 'No screens in this workspace.',
  tokens: 'No token files in this workspace.',
};

export default function DesignAssetsPane() {
  const workspace = useDesignWorkspace();
  if (!workspace) return null;

  const { tab, inventory, selected, select } = workspace;
  const entries = entriesFor(tab, inventory);

  return (
    <div className="design-rail" data-testid="design-assets-rail" data-tab={tab}>
      <h2 className="design-rail-heading">
        {tab === 'tokens' ? 'Token files' : tab === 'flows' ? 'Flows' : 'Screens'}
      </h2>
      {entries.length === 0 ? (
        <p className="design-rail-placeholder">{inventory ? EMPTY_PLACEHOLDER[tab] : 'Loading design assets…'}</p>
      ) : (
        <ul className="design-rail-list">
          {entries.map((asset) => (
            <li key={asset.path}>
              <button
                type="button"
                className={`design-rail-row${asset.path === selected ? ' selected' : ''}`}
                onClick={() => select(asset.path)}
                data-testid={`design-rail-row-${asset.path}`}
                title={asset.path}
              >
                {assetDisplayName(asset.name)}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
