/**
 * Sprout Mode Configuration
 *
 * Feature flags for Cloud vs Local mode in the Sprout webui.
 *
 * Capability resolution (host.3): every `supports*` binding below is driven by
 * the active host when one is set — the entry point records it via
 * `setActiveHost()` (host/accessor), which fires HOST_UPDATED_EVENT and these
 * bindings re-derive from `host.capabilities`. The host is selected once at
 * startup and is stable, so when it is active its capabilities are the source
 * of truth.
 *
 * When no host is active (pure test contexts, and the instant before the entry
 * sets the host) the bindings fall back to the adapter's value if an adapter is
 * installed, else the local default. host.8 removed the build-mode read from
 * this module: the host is the only source of truth, and the single place that
 * reads the build flag to pick a host is the app entry. The adapter-refresh path
 * (ADAPTER_INSTALLED_EVENT) still fires but is a no-op while a host is active.
 */

import { getActiveHost, HOST_UPDATED_EVENT, registerActiveHostCapabilitiesHook } from '../host/accessor';
import { getAdapter, ADAPTER_INSTALLED_EVENT, type APIAdapter } from '../services/apiAdapter';

/**
 * capability resolves a feature flag from the adapter when one is installed,
 * falling back to the local default.
 *
 * The adapter is installed asynchronously (after /api/bootstrap fetch),
 * so getAdapter() is null at module load time. The exported flags are
 * computed from the fallback defaults first and RE-EVALUATED when
 * ADAPTER_INSTALLED_EVENT fires (see "Live capability flags" below) —
 * at which point the adapter's capability value takes precedence.
 *
 * host.8: this no longer reads the build mode. With no active host the honest
 * fallback is the adapter's value (a hosted build installs one) or the local
 * default; the hosted build's cloud values come from its host, which the entry
 * always sets. `cloudDefault` is kept in the signature so the call sites keep
 * reading as a (local, cloud) pair, but it is no longer consulted — the cloud
 * values now come from the cloud host, not a build flag.
 *
 * host.3: this is the NO-HOST path only. When an active host exists, the
 * bindings resolve from host.capabilities instead (see refreshFromHost). It is
 * exported so non-React call sites (plain functions, module-scope helpers) can
 * read `getActiveHost()?.capabilities.x ?? capability(...)` — the same
 * host-or-fallback value the exported bindings carry, without importing the
 * live binding itself.
 */
export function capability<K extends keyof APIAdapter>(
  key: K,
  localDefault: NonNullable<APIAdapter[K]>,
  _cloudDefault: NonNullable<APIAdapter[K]>,
): NonNullable<APIAdapter[K]> {
  const adapter = getAdapter();
  if (adapter && adapter[key] !== undefined) {
    return adapter[key] as NonNullable<APIAdapter[K]>;
  }
  return localDefault;
}

// ── Live capability flags ─────────────────────────────────────────────────
//
// These are `export let`, not `const`: the adapter installs ASYNCHRONOUSLY
// (bootstrapAdapter.ts awaits /api/bootstrap after this module has loaded), and
// the entry sets the active host asynchronously too, so the initial values are
// computed from the fallback defaults first and then RE-EVALUATED when
// HOST_UPDATED_EVENT (host set) or ADAPTER_INSTALLED_EVENT (adapter installed,
// no host) fire. ESM live bindings propagate the reassignment to every importer
// that reads the binding at render/use time (module-scope snapshots in
// consuming modules would still freeze — don't cache these at import scope).
//
// Components that need to RE-RENDER on a transition (rare: the host is set
// before the tree mounts) listen for the events directly — the established seam
// used by PlatformNavContext and SproutAdapterContext.

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

/**
 * Automation workflows - local mode only (the platform serves no
 * /api/automate; hosted scheduling lives in the platform's Tasks).
 * host.3: `export let` so the active host's `capabilities.automations` drives
 * it. It is not an adapter key, so there is no adapter-refresh row; the
 * no-host fallback is the local default (true — today's `!isCloud` value).
 */
export let supportsAutomations: boolean = true;

/**
 * Agent change history (the context panel's Agent Changes tab) - local mode
 * only: the in-browser agent does not record a change manifest, and the
 * platform serves no /api/changes.
 * host.3: `export let` so the active host's `capabilities.agentChanges` drives
 * it. It is not an adapter key, so there is no adapter-refresh row; the
 * no-host fallback is the local default (true — today's `!isCloud` value).
 */
export let supportsAgentChanges: boolean = true;

/**
 * Host chat session store: when the host advertises `capabilities.chatSessions`
 * it serves the chat session list / create / rename / delete / switch /
 * messages calls itself and finished turns are appended to it, instead of the
 * daemon or browser storage.
 * host.3: `export let` so the active host's `capabilities.chatSessions` drives
 * it. It is not an adapter key, so there is no adapter-refresh row; the
 * no-host fallback is the local default (false — the local build keeps its own
 * daemon store).
 */
export let supportsChatSessions: boolean = false;

// Adapter-installed refresh (no-host path only): re-read every adapter-derived
// capability against the newly installed adapter. The defaults table mirrors
// the initializers above — a per-key map of [localDefault, cloudDefault] and
// the matching binding, so a new flag needs exactly one row here plus its
// `export let`. Skipped entirely while an active host is set: the host's
// capabilities are the source of truth, and the adapter values are redundant.
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

function refreshFromAdapter(): void {
  // The host wins: when one is active its capabilities are the source of
  // truth, so the adapter refresh must not overwrite them.
  if (getActiveHost()) return;
  for (const { key, local, cloud, set } of CAPABILITY_REFRESH) {
    set(capability(key, local, cloud));
  }
}

// Host-set refresh: when an active host exists, every host-derivable binding
// resolves from host.capabilities. The field names map 1:1 to HostCapabilities
// (ssh, git, chat, workspaceSwitching, folderPicker, export, instances,
// localTerminal, settings, automations, agentChanges). Idempotent: re-setting
// from the same (stable) host yields the same values.
const HOST_REFRESH: Array<{
  field:
    | 'ssh'
    | 'git'
    | 'chat'
    | 'workspaceSwitching'
    | 'folderPicker'
    | 'export'
    | 'instances'
    | 'localTerminal'
    | 'settings'
    | 'automations'
    | 'agentChanges'
    | 'chatSessions';
  set: (v: boolean) => void;
}> = [
  { field: 'ssh', set: (v) => (supportsSSH = v) },
  { field: 'git', set: (v) => (supportsGit = v) },
  { field: 'chat', set: (v) => (supportsChat = v) },
  { field: 'workspaceSwitching', set: (v) => (supportsWorkspaceSwitching = v) },
  { field: 'folderPicker', set: (v) => (supportsFolderPicker = v) },
  { field: 'export', set: (v) => (supportsExport = v) },
  { field: 'instances', set: (v) => (supportsInstances = v) },
  { field: 'localTerminal', set: (v) => (supportsLocalTerminal = v) },
  { field: 'settings', set: (v) => (supportsSettings = v) },
  { field: 'automations', set: (v) => (supportsAutomations = v) },
  { field: 'agentChanges', set: (v) => (supportsAgentChanges = v) },
  { field: 'chatSessions', set: (v) => (supportsChatSessions = v) },
];

function refreshFromHost(): void {
  const host = getActiveHost();
  if (!host) return;
  for (const { field, set } of HOST_REFRESH) {
    set(host.capabilities[field]);
  }
}

// Idempotent: the install is once per page load, and re-reading with the same
// adapter yields the same values. A malformed adapter must not take the app
// down, so the refresh is guarded — defaults stay on any throw.
if (typeof window !== 'undefined') {
  window.addEventListener(ADAPTER_INSTALLED_EVENT, () => {
    try {
      refreshFromAdapter();
    } catch {
      // keep mode defaults
    }
  });
  window.addEventListener(HOST_UPDATED_EVENT, () => {
    try {
      refreshFromHost();
    } catch {
      // keep current values
    }
  });
}

// A host shell's declared capabilities (applied to the active host by the
// entry once the adapter installs) are re-read from the host the same way a
// host-change is: no window event needed, so the refresh is deterministically
// scoped to this module instance (matching the host-change-hook pattern).
registerActiveHostCapabilitiesHook(() => {
  try {
    refreshFromHost();
  } catch {
    // keep current values
  }
});

// Seed: the entry may record the active host before this module's listeners are
// attached (or a test may set the host before importing this module). Applying
// the host now makes the initial values correct in both cases; the events keep
// them live from here. A no-op when no host is active.
refreshFromHost();
