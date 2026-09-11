/**
 * Sprout Mode Configuration
 *
 * Feature flags for Cloud vs Local mode in the Sprout webui.
 * Controlled via VITE_SPROUT_MODE environment variable at build time.
 */

import { getAdapter, ADAPTER_INSTALLED_EVENT, type APIAdapter } from '../services/apiAdapter';

export type SproutMode = 'local' | 'cloud';

/**
 * Resolved mode value from environment variable, defaulting to 'local'.
 *
 * Vite replaces VITE_* vars at build time,
 * so this resolves to a compile-time constant. Dead code is tree-shaken.
 *
 * Strict comparison — any non-'cloud' value (including typos) safely
 * defaults to local mode.
 */
export const mode: SproutMode = (import.meta.env.VITE_SPROUT_MODE as SproutMode) === 'cloud' ? 'cloud' : 'local';

/**
 * Cloud mode flag - true when running in cloud environment
 */
export const isCloud: boolean = mode === 'cloud';

/**
 * capability resolves a feature flag from the adapter when one is installed,
 * falling back to a mode-aware default.
 *
 * The adapter is installed asynchronously (after /api/bootstrap fetch),
 * so getAdapter() is null at module load time. The exported flags are
 * computed from the fallback defaults first and RE-EVALUATED when
 * ADAPTER_INSTALLED_EVENT fires (see "Live capability flags" below) —
 * at which point the adapter's capability value takes precedence.
 *
 * This helper replaces the previous inline `isCloud ? X : (getAdapter()?.Y ?? Z)`
 * pattern that was duplicated across every export — the logic is identical,
 * now centralized and documented once.
 */
function capability<K extends keyof APIAdapter>(
  key: K,
  localDefault: NonNullable<APIAdapter[K]>,
  cloudDefault: NonNullable<APIAdapter[K]>,
): NonNullable<APIAdapter[K]> {
  const adapter = getAdapter();
  if (adapter && adapter[key] !== undefined) {
    return adapter[key] as NonNullable<APIAdapter[K]>;
  }
  return isCloud ? cloudDefault : localDefault;
}

// ── Live capability flags ─────────────────────────────────────────────────
//
// These are `export let`, not `const`: the adapter installs ASYNCHRONOUSLY
// (bootstrapAdapter.ts awaits /api/bootstrap after this module has loaded),
// so the mode-aware fallback defaults are computed first and the adapter's
// own values replace them when ADAPTER_INSTALLED_EVENT fires. ESM live
// bindings propagate the reassignment to every importer that reads the
// binding at render/use time (module-scope snapshots in consuming modules
// would still freeze — don't cache these at import scope).
//
// Components that need to RE-RENDER on the transition (rare: the install
// usually lands before the tree mounts) listen for ADAPTER_INSTALLED_EVENT
// directly — the established seam used by PlatformNavContext and
// SproutAdapterContext.

export let supportsSSH: boolean = capability('supportsSSH', true, false);

/**
 * Git support - available in both modes.
 *
 * In local mode, git runs on the host. In cloud mode, git runs in-browser via
 * isomorphic-git + lightning-fs (IndexedDB). Not every git operation is
 * implemented in browser mode yet — browserGit.ts returns an honest error for
 * unimplemented ops (unstage, reset, pull, discard, revert, etc.) rather than
 * faking success — but the core flow (status, add, commit, push, clone, diff)
 * is functional. See webui/src/services/browserGit.ts for the capability matrix.
 *
 * Track R (--native-git) seam: this is a runtime, mode-based capability (both
 * defaults true) — NOT a compile-time branch — so the `--native-git` flag does
 * not gate it here (doing so would change default-build behavior). In a
 * `--native-git` build the webui does not advertise browser git as functional:
 * the boot wiring (useAppInitialization) is short-circuited on
 * NATIVE_GIT_ENABLED and the git UI surfaces render the "Git provided by the
 * native shell" placeholder. The native shell provides git natively.
 */
export let supportsGit: boolean = capability('supportsGit', true, true);

/**
 * Chat support - available in both modes (BYOK proxy in cloud, local LLM in desktop).
 */
export let supportsChat: boolean = capability('supportsChat', true, true);

/**
 * Workspace switching support - local mode only (single virtual FS in cloud).
 */
export let supportsWorkspaceSwitching: boolean = capability('supportsWorkspaceSwitching', true, false);

/**
 * Native folder-picker support - studio shells only (bridge files channel).
 */
export let supportsFolderPicker: boolean = capability('supportsFolderPicker', false, false);

/**
 * Export support - local mode only (no local filesystem to export to in cloud).
 */
export let supportsExport: boolean = capability('supportsExport', true, false);

/**
 * Instance management support - cloud mode only (platform instances API).
 */
export let supportsInstances: boolean = capability('supportsInstances', false, true);

/**
 * Local PTY terminal support - local mode only (WASM terminal in cloud).
 */
export let supportsLocalTerminal: boolean = capability('supportsLocalTerminal', true, false);

/**
 * Settings panel support - available in both modes (BYOK settings in cloud).
 */
export let supportsSettings: boolean = capability('supportsSettings', true, true);

// Adapter-installed refresh: re-read every capability against the newly
// installed adapter. The defaults table mirrors the initializers above — a
// per-key map of [localDefault, cloudDefault] and the matching binding, so a
// new flag needs exactly one row here plus its `export let`.
const CAPABILITY_REFRESH: Array<{
  key:
    | 'supportsSSH'
    | 'supportsGit'
    | 'supportsChat'
    | 'supportsWorkspaceSwitching'
    | 'supportsFolderPicker'
    | 'supportsExport'
    | 'supportsInstances'
    | 'supportsLocalTerminal'
    | 'supportsSettings';
  local: boolean;
  cloud: boolean;
  set: (v: boolean) => void;
}> = [
  { key: 'supportsSSH', local: true, cloud: false, set: (v) => (supportsSSH = v) },
  { key: 'supportsGit', local: true, cloud: true, set: (v) => (supportsGit = v) },
  { key: 'supportsChat', local: true, cloud: true, set: (v) => (supportsChat = v) },
  { key: 'supportsWorkspaceSwitching', local: true, cloud: false, set: (v) => (supportsWorkspaceSwitching = v) },
  { key: 'supportsFolderPicker', local: false, cloud: false, set: (v) => (supportsFolderPicker = v) },
  { key: 'supportsExport', local: true, cloud: false, set: (v) => (supportsExport = v) },
  { key: 'supportsInstances', local: false, cloud: true, set: (v) => (supportsInstances = v) },
  { key: 'supportsLocalTerminal', local: true, cloud: false, set: (v) => (supportsLocalTerminal = v) },
  { key: 'supportsSettings', local: true, cloud: true, set: (v) => (supportsSettings = v) },
];

function refreshCapabilities(): void {
  for (const { key, local, cloud, set } of CAPABILITY_REFRESH) {
    set(capability(key, local, cloud));
  }
}

// Idempotent: the install is once per page load, and re-reading with the same
// adapter yields the same values. A malformed adapter must not take the app
// down, so the refresh is guarded — defaults stay on any throw.
if (typeof window !== 'undefined') {
  window.addEventListener(ADAPTER_INSTALLED_EVENT, () => {
    try {
      refreshCapabilities();
    } catch {
      // keep mode defaults
    }
  });
}
