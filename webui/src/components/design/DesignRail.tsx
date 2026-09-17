/**
 * Design mode's rail.
 *
 * The design surface's own navigation: the three asset classes. Deliberately
 * does NOT carry the Code rail's entries (Git/Files/Search/Automations) — a
 * mode's rail lists what that mode does, and the file tree stays reachable
 * through the design assets rail. That is the point of modes as peers: entering
 * Design replaces the navigation rather than adding to Code's.
 *
 * Styling reuses the shell's `.rail-icon` classes so the rail reads as the same
 * control in both modes.
 */

import { FileText, Layers, Palette } from 'lucide-react';
import type { ModeRailConfig, ModeRailProps } from '../../workspaces/rail';

export const DESIGN_RAIL: ModeRailConfig = {
  label: 'Design navigation',
  entries: [
    { id: 'flows', label: 'Flows', icon: Layers },
    { id: 'screens', label: 'Screens', icon: FileText },
    { id: 'tokens', label: 'Tokens', icon: Palette },
  ],
};

export default function DesignRail({ activeId, onSelect }: ModeRailProps) {
  return (
    <nav aria-label={DESIGN_RAIL.label} data-testid="design-rail">
      {DESIGN_RAIL.entries.map((entry) => {
        const Icon = entry.icon;
        const active = entry.id === activeId;
        return (
          <button
            key={entry.id}
            type="button"
            role="tab"
            aria-selected={active}
            aria-label={entry.label}
            title={entry.label}
            className={`rail-icon ${active ? 'active' : ''}`}
            onClick={() => onSelect(entry.id)}
            data-testid={`design-rail-${entry.id}`}
          >
            <Icon size={18} strokeWidth={1.5} />
          </button>
        );
      })}
    </nav>
  );
}
