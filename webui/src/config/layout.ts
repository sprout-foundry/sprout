/**
 * Shell layout choice: the classic icon-rail layout, or the layered layout
 * (a project rail, a project sidebar, the main view, and a contextual
 * sidebar). `?layout=layered` or `?layout=classic` switches and remembers the
 * choice for this browser; classic stays the default.
 */

export type ShellLayout = 'classic' | 'layered';

const STORAGE_KEY = 'sprout-shell-layout';

function readLayout(): ShellLayout {
  if (typeof window === 'undefined') return 'classic';
  const param = new URLSearchParams(window.location.search).get('layout');
  if (param === 'layered' || param === 'classic') {
    try {
      window.localStorage.setItem(STORAGE_KEY, param);
    } catch {
      // Storage can be unavailable (private mode); the URL still applies.
    }
    return param;
  }
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'layered' ? 'layered' : 'classic';
  } catch {
    return 'classic';
  }
}

export const shellLayout: ShellLayout = readLayout();
export const isLayeredLayout = shellLayout === 'layered';

// Layout-specific CSS keys off this class (see components/layered/Layered.css).
if (isLayeredLayout && typeof document !== 'undefined') {
  document.documentElement.classList.add('layout-layered');
}

/** Width (logical px) of the layered layout's left sidebars, for chrome fixed to the viewport. */
export const LEFT_INSET_VAR = '--sprout-left-inset';

/** Window events the layered chrome uses to reach surfaces owned elsewhere. */
export const OPEN_COMMAND_PALETTE_EVENT = 'sprout:open-command-palette';
export const OPEN_NOTIFICATIONS_EVENT = 'sprout:open-notifications';
