/**
 * Generate a unique message ID with browser compatibility fallback.
 * Uses crypto.randomUUID() if available (modern browsers), otherwise falls back
 * to a timestamp-based random string for older browser support.
 */
export function generateMessageId(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    try {
      return crypto.randomUUID();
    } catch {
      // Fall through to fallback if crypto.randomUUID fails
    }
  }
  // Fallback for older browsers: timestamp + random suffix
  return `${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;
}
