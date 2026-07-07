/**
 * Sprout Mode Configuration
 *
 * Feature flags for Cloud vs Local mode in the Sprout webui.
 * Controlled via VITE_SPROUT_MODE environment variable at build time.
 */

import { getAdapter } from '../services/apiAdapter';

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
 * SSH access support - available in cloud mode only.
 * When an adapter is installed, consults the adapter's capability.
 */
export const supportsSSH: boolean = getAdapter()?.supportsSSH ?? true;

/**
 * Git support - available when the adapter or local backend supports it.
 */
export const supportsGit: boolean = getAdapter()?.supportsGit ?? true;

/**
 * Chat support - available when the adapter or local backend supports it.
 */
export const supportsChat: boolean = getAdapter()?.supportsChat ?? true;

/**
 * Workspace switching support - available when the adapter or local backend supports it.
 */
export const supportsWorkspaceSwitching: boolean = getAdapter()?.supportsWorkspaceSwitching ?? true;

/**
 * Export support - available when the adapter or local backend supports it.
 */
export const supportsExport: boolean = getAdapter()?.supportsExport ?? true;

/**
 * Instance management support - available in cloud mode only.
 * When an adapter is installed, consults the adapter's capability.
 */
export const supportsInstances: boolean = getAdapter()?.supportsInstances ?? isCloud;

/**
 * Local PTY terminal support - available in local mode only.
 * When an adapter is installed, consults the adapter's capability.
 */
export const supportsLocalTerminal: boolean = getAdapter()?.supportsLocalTerminal ?? !isCloud;

/**
 * Local settings management support - available in local mode only.
 * When an adapter is installed, consults the adapter's capability.
 */
export const supportsSettings: boolean = getAdapter()?.supportsSettings ?? !isCloud;
