/**
 * Workspace modes.
 *
 * A *workspace* in sprout is the filesystem root the user connected to (see
 * `LocationSwitcher`, where "workspace" means the folder path). A **mode** is
 * what the user is doing with that root: writing code, designing, and later
 * shipping.
 *
 * Modes are peers. Each supplies its own rail and content surface, and entering
 * one replaces the shell around the shared root rather than swapping a panel
 * inside a shared frame. That is what keeps a mode from feeling bolted on: it
 * owns its region instead of competing with another mode's chrome.
 *
 * This module holds mode *identity* — labels, icons, availability — plus the
 * mode's shell component, which composes its surface and its own chrome. The
 * surfaces stay inside their shells, not here.
 *
 * Adding a mode is: one entry here, one rail, one shell. No new
 * `currentView === 'x'` exception in the app.
 */

import { Code2, Palette, type LucideIcon } from 'lucide-react';
import type { ComponentType } from 'react';
import CodeShell from './CodeShell';
import DesignShell from './DesignShell';
import type { WorkspaceShellProps } from './shell';

/** Stable identifier for a mode. Persisted, so treat these as a wire format. */
// The open union mirrors `ViewType` (types/app.ts): known ids complete, and a
// future mode can register without a type change.
export type WorkspaceModeId = 'code' | 'design' | (string & {}); // eslint-disable-line @typescript-eslint/ban-types

export interface WorkspaceModeContext {
  /**
   * Whether the workspace root contains a `design/` directory. A mode whose
   * surface requires specific workspace content declares that here rather than
   * letting the shell guess (the design surface is meaningless without a
   * design tree).
   */
  hasDesignTree: boolean;
}

export interface WorkspaceMode {
  id: WorkspaceModeId;
  /** Label in the switcher. */
  label: string;
  icon: LucideIcon;
  /**
   * One-line description for the switcher's secondary text. Kept short — it is
   * a hint, not documentation.
   */
  hint: string;
  /**
   * Whether this mode is offered for the current workspace. An unavailable mode
   * is not listed at all (rather than listed-and-disabled): offering a surface
   * the workspace cannot render is how a user ends up staring at an empty view
   * wondering what broke.
   */
  available: (ctx: WorkspaceModeContext) => boolean;
  /**
   * The mode's shell: the content column plus the chrome that belongs to it.
   * The app renders the active mode's shell directly, so a mode's chrome is
   * composed by the mode instead of suppressed out of shared components.
   */
  Shell: ComponentType<WorkspaceShellProps>;
}

/** The mode new sessions start in. */
export const DEFAULT_WORKSPACE_MODE: WorkspaceModeId = 'code';

/**
 * Mode registry.
 *
 * `code` is always available: it is the baseline experience over any root.
 * `design` requires a design tree, because its surface has nothing to show
 * without one.
 */
export const WORKSPACE_MODES: WorkspaceMode[] = [
  {
    id: 'code',
    label: 'Code',
    icon: Code2,
    hint: 'Chat, editor, git, terminal',
    available: () => true,
    Shell: CodeShell,
  },
  {
    id: 'design',
    label: 'Design',
    icon: Palette,
    hint: 'Flows, screens, tokens',
    available: (ctx) => ctx.hasDesignTree,
    Shell: DesignShell,
  },
];

/** Modes offered for a workspace, in switcher order. */
export function availableModes(ctx: WorkspaceModeContext): WorkspaceMode[] {
  return WORKSPACE_MODES.filter((mode) => mode.available(ctx));
}

/** Look up a mode, falling back to the default when an id is unknown or unavailable. */
export function resolveWorkspaceMode(id: WorkspaceModeId | null | undefined, ctx: WorkspaceModeContext): WorkspaceMode {
  const modes = availableModes(ctx);
  const match = modes.find((mode) => mode.id === id);
  if (match) return match;
  return modes.find((mode) => mode.id === DEFAULT_WORKSPACE_MODE) ?? modes[0];
}
