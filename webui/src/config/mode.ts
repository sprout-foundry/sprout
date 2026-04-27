/**
 * Sprout Mode Configuration
 *
 * Feature flags for Cloud vs Local mode in the Sprout webui.
 * Controlled via REACT_APP_SPROUT_MODE environment variable at build time.
 */

export type SproutMode = 'local' | 'cloud';

/**
 * Resolved mode value from environment variable, defaulting to 'local'
 */
export const mode: SproutMode =
  process.env.REACT_APP_SPROUT_MODE === 'cloud' ? 'cloud' : 'local';

/**
 * Cloud mode flag - true when running in cloud environment
 */
export const isCloud: boolean = mode === 'cloud';

/**
 * SSH access support - available in cloud mode only
 */
export const supportsSSH: boolean = isCloud;

/**
 * Instance management support - available in cloud mode only
 */
export const supportsInstances: boolean = isCloud;

/**
 * Local PTY terminal support - available in local mode only
 */
export const supportsLocalTerminal: boolean = !isCloud;

/**
 * Local settings management support - available in local mode only
 */
export const supportsSettings: boolean = !isCloud;
