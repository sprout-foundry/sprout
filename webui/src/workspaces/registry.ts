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
 * Modes are defined by one public registration API (SP-155 §155b): an
 * embedding shell registers its modes with it, and the built-in Code and
 * Design modes register at module load through the same write path (minus the
 * API's built-in guard, which exists to protect them). Adding a mode is: one
 * registration, one rail, one shell. No new
 * `currentView === 'x'` exception in the app.
 *
 * Two invariants hold for the whole module. First, the built-ins are always
 * registered and irremovable: they cannot be replaced (registration rejects
 * their ids) or disposed (disposal refuses built-in entries), so the registry
 * is never without them — `availableModes` can only ever be empty if a
 * mode's `available` predicate hides it, and the built-ins are `() => true`.
 * Second, mode resolution never returns nothing: with the built-ins
 * guaranteed present, there is always something to resolve to.
 */

import { Code2, Palette, type LucideIcon } from 'lucide-react';
import type { ComponentType } from 'react';
import { configuredDefaultWorkspaceMode } from '../config/workspaceMode';
import CodeShell from './CodeShell';
import DesignShell from './DesignShell';
import type { WorkspaceShellProps } from './shell';

/** Stable identifier for a mode. Persisted, so treat these as a wire format. */
// The open union mirrors `ViewType` (types/app.ts): known ids complete, and a
// future mode can register without a type change.
export type WorkspaceModeId = 'code' | 'design' | (string & {}); // eslint-disable-line @typescript-eslint/ban-types

export interface WorkspaceModeContext {
  /**
   * Whether the workspace root contains a `design/` directory. No longer
   * gates availability (Design is always offered; its surface owns the
   * empty state), but kept so the switcher and surface can still observe
   * tree presence.
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

/**
 * Public registration payload (SP-155 §155b): id, label, icon, shell
 * component, and an availability predicate.
 *
 * This is the only way an extension's mode gets into the registry; the
 * built-ins go through the same write path at module load (via the internal
 * entry point that the public API's built-in guard exempts). `hint` is
 * the only optional field: the switcher's secondary text is a hint, and a
 * mode without one simply renders none.
 */
export interface WorkspaceModeRegistration {
  id: WorkspaceModeId;
  label: string;
  icon: LucideIcon;
  /** Switcher secondary text. Optional — defaults to empty. */
  hint?: string;
  /** Whether this mode is offered for the given workspace. */
  available: (ctx: WorkspaceModeContext) => boolean;
  /** The mode's shell component. */
  Shell: ComponentType<WorkspaceShellProps>;
}

/** Removes the registration created by `registerWorkspaceMode`. */
export type UnregisterWorkspaceMode = () => void;

/**
 * The built-in baseline: the mode every workspace offers, and the last resort
 * when a configured default points at a mode the workspace does not. New
 * sessions start in the configured default (SP-155 §155b) — see
 * `defaultWorkspaceMode` — which falls back to this when unset or unusable.
 */
export const BUILTIN_DEFAULT_WORKSPACE_MODE: WorkspaceModeId = 'code';

/**
 * The built-in mode ids. These entries are part of the app's contract with
 * every workspace: the mode switcher, the default-mode resolution, and the
 * surfaces themselves all assume Code and Design exist. They are protected in
 * both directions — `registerWorkspaceMode` refuses to register over them,
 * and the returned disposers refuse to remove them — so no embedding shell or
 * accidental double-registration can leave the app without a usable mode.
 */
export const BUILTIN_WORKSPACE_MODE_IDS: readonly WorkspaceModeId[] = ['code', 'design'];

/** True when `id` names a built-in mode that cannot be replaced or removed. */
export function isBuiltinWorkspaceModeId(id: WorkspaceModeId): boolean {
  return (BUILTIN_WORKSPACE_MODE_IDS as readonly string[]).includes(id);
}

/**
 * Mode registry.
 *
 * The live array registrations write into: the built-ins register into it at
 * module load (below), and `registerWorkspaceMode` appends or replaces
 * entries. Consumers import this one array, so a registration made after
 * import is visible to them — the switcher lists whatever the registry holds.
 *
 * Registration is a boot-time concern: the built-ins register at module load
 * and an embedding shell is expected to register its modes before the shell
 * renders. A later `availableModes` call reflects the registry as it stands;
 * a memoized mode list (e.g. inside `useWorkspaceMode`) picks up
 * late registrations on its next recomputation, not mid-render.
 */
export const WORKSPACE_MODES: WorkspaceMode[] = [];

/**
 * Public mode registration API (SP-155 §155b).
 *
 * A new id is appended, after the built-ins, in switcher order. Re-registering
 * an existing id replaces that entry in place (the mode keeps its position),
 * so registration is idempotent and an embedding shell can override a
 * non-built-in mode's label, icon, or shell without reordering the switcher.
 *
 * Built-in ids are rejected: the app's baseline modes cannot be replaced, so
 * a definition offered under `code` or `design` keeps the built-in one. The
 * rejection is a warned no-op rather than a throw because registration is a
 * boot-time concern — a misbehaving embedder must not crash the app before it
 * renders. The returned disposer removes nothing (nothing was added or
 * replaced).
 *
 * Returns a disposer that removes the entry this call created. It checks
 * identity, so a disposer held across a re-registration of the same id removes
 * nothing — the newer definition stays.
 */
export function registerWorkspaceMode(definition: WorkspaceModeRegistration): UnregisterWorkspaceMode {
  if (isBuiltinWorkspaceModeId(definition.id)) {
    console.warn(`registerWorkspaceMode: '${definition.id}' is a built-in mode and cannot be replaced; ignoring.`);
    return () => {};
  }
  return registerMode(definition);
}

/**
 * The one registry write path: append `definition` or replace its id's entry
 * in place, and return the matching disposer. Both registration entries point
 * here, so the public and built-in paths cannot drift.
 */
function registerMode(definition: WorkspaceModeRegistration): UnregisterWorkspaceMode {
  const mode: WorkspaceMode = {
    id: definition.id,
    label: definition.label,
    icon: definition.icon,
    hint: definition.hint ?? '',
    available: definition.available,
    Shell: definition.Shell,
  };
  const existing = WORKSPACE_MODES.findIndex((entry) => entry.id === mode.id);
  if (existing === -1) {
    WORKSPACE_MODES.push(mode);
  } else {
    WORKSPACE_MODES[existing] = mode;
  }
  return () => {
    const index = WORKSPACE_MODES.findIndex((entry) => entry === mode);
    // Identity, not id: a stale disposer held across a re-registration of the
    // same id must remove nothing, so the newest definition stays. Refusing to
    // remove a built-in entry keeps the baseline in place even if a built-in
    // disposer is somehow invoked.
    if (index !== -1 && !isBuiltinWorkspaceModeId(WORKSPACE_MODES[index].id)) WORKSPACE_MODES.splice(index, 1);
  };
}

/**
 * Built-in modes, registered through the internal registration path (the
 * public API refuses their ids).
 *
 * `code` is always available: it is the baseline experience over any root.
 * `design` is also always offered: an empty workspace gets the Design empty
 * state (onboarding cards + the agent chat), which is how a `design/` tree
 * comes to exist in the first place. The surface itself decides what an
 * absent tree looks like; hiding the mode only meant the tree could never be
 * started from the UI.
 */
registerMode({
  id: 'code',
  label: 'Code',
  icon: Code2,
  hint: 'Chat, editor, git, terminal',
  available: () => true,
  Shell: CodeShell,
});

registerMode({
  id: 'design',
  label: 'Design',
  icon: Palette,
  hint: 'Flows, screens, tokens',
  available: () => true,
  Shell: DesignShell,
});

/** Modes offered for a workspace, in switcher order. */
export function availableModes(ctx: WorkspaceModeContext): WorkspaceMode[] {
  return WORKSPACE_MODES.filter((mode) => mode.available(ctx));
}

/**
 * The default mode new sessions start in (SP-155 §155b).
 *
 * The configured default (`config/workspaceMode.ts`) when it names a mode this
 * workspace actually offers — a mode that is unregistered, or registered but
 * unavailable here, degrades to the built-in baseline rather than stranding a
 * session in a mode with nothing to render. With no configuration, this is the
 * built-in default.
 */
export function defaultWorkspaceMode(ctx: WorkspaceModeContext): WorkspaceModeId {
  const configured = configuredDefaultWorkspaceMode();
  if (configured !== null && availableModes(ctx).some((mode) => mode.id === configured)) {
    return configured;
  }
  return BUILTIN_DEFAULT_WORKSPACE_MODE;
}

/** Look up a mode, falling back to the default when an id is unknown or unavailable. */
export function resolveWorkspaceMode(id: WorkspaceModeId | null | undefined, ctx: WorkspaceModeContext): WorkspaceMode {
  const modes = availableModes(ctx);
  const match = modes.find((mode) => mode.id === id);
  if (match) return match;
  // No match: degrade to the configured default if the workspace offers it,
  // then the built-in baseline, then any registered built-in, then the first
  // mode at all — each rung guarantees a usable mode rather than `undefined`
  // (whose return type forbids and whose dereference would crash the shell).
  const fallback = modes.find((mode) => mode.id === defaultWorkspaceMode(ctx));
  if (fallback) return fallback;
  const builtin = modes.find((mode) => isBuiltinWorkspaceModeId(mode.id));
  if (builtin) return builtin;
  if (modes[0]) return modes[0];
  // Unreachable while the built-ins are registered and always available, but
  // the module must still return a WorkspaceMode, so recreate the built-in
  // default's definition rather than return nothing.
  const code = WORKSPACE_MODES.find((mode) => mode.id === BUILTIN_DEFAULT_WORKSPACE_MODE);
  if (code) return code;
  throw new Error('resolveWorkspaceMode: no workspace modes registered');
}
