/**
 * Path helpers for the canvas (SP-140-3 §3b).
 *
 * `designApi` classifies asset paths relative to `design/`, but React Flow
 * positions live in canvas units, so a node's synthesized position is plain
 * path arithmetic — its own import-free module keeps the canvas model
 * data-layer independent.
 */

/** Last path segment, POSIX or Windows separators. */
export function basename(path: string): string {
  const parts = String(path ?? '').split(/[/\\]/);
  return parts[parts.length - 1] || String(path ?? '');
}
