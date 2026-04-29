/**
 * Simple debug logging utility for the ui package.
 */

/**
 * Debug log - only shows in non-production environments.
 */
export function debugLog(...args: unknown[]) {
  if (process.env.NODE_ENV !== 'production') {
    console.log('[debug]', ...args);
  }
}
