/**
 * Sprout Mode Configuration
 *
 * Feature flags for Cloud vs Local mode in the Sprout webui.
 * Controlled via VITE_SPROUT_MODE environment variable at build time.
 */

import { getAdapter, type APIAdapter } from '../services/apiAdapter';

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
 * so getAdapter() is null at module load time. Because capability exports
 * are `const` (frozen once evaluated), the fallback default must be correct
 * for BOTH modes during that async-installation window. The localDefault
 * is used in local mode; the cloudDefault is used in cloud mode.
 *
 * Once the adapter is installed, its capability value takes precedence.
 * In local mode, the adapter is typically null (local mode IS the "no
 * adapter installed" state), so the localDefault is what sticks.
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

/**
 * SSH access support - available in local mode only (requires host access).
 */
export const supportsSSH: boolean = capability('supportsSSH', true, false);

/**
 * Git support - available in local mode; WASM shell handles git differently in cloud.
 */
export const supportsGit: boolean = capability('supportsGit', true, false);

/**
 * Chat support - available in both modes (BYOK proxy in cloud, local LLM in desktop).
 */
export const supportsChat: boolean = capability('supportsChat', true, true);

/**
 * Workspace switching support - local mode only (single virtual FS in cloud).
 */
export const supportsWorkspaceSwitching: boolean = capability('supportsWorkspaceSwitching', true, false);

/**
 * Export support - local mode only (no local filesystem to export to in cloud).
 */
export const supportsExport: boolean = capability('supportsExport', true, false);

/**
 * Instance management support - cloud mode only (platform instances API).
 */
export const supportsInstances: boolean = capability('supportsInstances', false, true);

/**
 * Local PTY terminal support - local mode only (WASM terminal in cloud).
 */
export const supportsLocalTerminal: boolean = capability('supportsLocalTerminal', true, false);

/**
 * Settings panel support - available in both modes (BYOK settings in cloud).
 */
export const supportsSettings: boolean = capability('supportsSettings', true, true);
