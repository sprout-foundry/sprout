/**
 * Workspace mode rails.
 *
 * Each mode supplies the icon rail rendered while it is active. The shell
 * renders `mode.Rail` and knows nothing about any mode's entries, which is what
 * lets a new mode be added without the shell growing another special case.
 *
 * A rail entry is a *view within the mode* (Flows/Screens/Tokens inside Design,
 * Git/Files/Search inside Code). Mode switching lives on the top-left switcher,
 * not here, so a rail never has to know the mode list.
 *
 * Entries that are not workspace-domain features — platform nav, plugins,
 * costs, settings, logs — stay in the shell's shared rail region rather than
 * being duplicated per mode.
 */

import type { LucideIcon } from 'lucide-react';

export interface ModeRailEntry {
  id: string;
  label: string;
  icon: LucideIcon;
}

export interface ModeRailProps {
  /** The active entry's id, so the rail can show current state. */
  activeId: string;
  /** Fired when the user picks an entry. */
  onSelect: (id: string) => void;
  /** Rendered in the collapsed rail (icons only). */
  collapsed?: boolean;
}

export interface ModeRailConfig {
  /** Accessible name for the rail's nav element. */
  label: string;
  entries: ModeRailEntry[];
}
