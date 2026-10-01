import type { EditorFileEntry } from '../types/editor';

/**
 * Where a buffer at `current` lives after `from` moved to `to`: the same
 * path, a path beneath a moved folder, or null when the move doesn't touch it.
 * All three paths must already be in the same (resolved) form.
 */
export function movedBufferPath(current: string, from: string, to: string): string | null {
  if (current === from) return to;
  if (current.startsWith(from + '/')) return to + current.slice(from.length);
  return null;
}

/** True when `current` is `target` or lies inside the `target` folder. */
export function isWithinPath(current: string, target: string): boolean {
  return current === target || current.startsWith(target + '/');
}

/** The file entry for a buffer whose file now lives at `path`. */
export function fileEntryAtPath(file: EditorFileEntry, path: string): EditorFileEntry {
  const name = path.split('/').pop() || path;
  const dot = name.lastIndexOf('.');
  return { ...file, path, name, ext: dot > 0 ? name.slice(dot) : '' };
}
