/**
 * DesignView visibility rule (SP-140-3 §3a).
 *
 * The design surface activates when a `design/` directory exists in the
 * workspace and is hidden otherwise. Presence is *directory* presence — the
 * same signal the agent uses (`pkg/design`), not the presence of any specific
 * file inside it, so an empty scaffold still shows the view.
 *
 * Kept as a pure function so the rule is unit-testable without mounting
 * anything: both the Sidebar nav affordance and the EditorWorkspace branch
 * consult it, and neither may drift from the other.
 */

import type { FilesResponse } from '../../services/api';

/** The workspace directory whose presence activates DesignView. */
export const DESIGN_DIR = 'design';

/**
 * True when `path` is the design directory itself or lives inside it.
 * Case-sensitive; a sibling file like `design.md` does NOT match.
 */
export function isDesignPath(path: string): boolean {
  const normalized = (path ?? '').replace(/\\/g, '/').replace(/^\.\//, '').replace(/^\/+/, '');
  const segments = normalized.split('/').filter(Boolean);
  return segments.includes(DESIGN_DIR);
}

/**
 * Detect a `design/` directory from the workspace file list. Directory
 * presence is inferred from the listed paths: the directory entry itself
 * (`design/`, `design`) or anything nested underneath it.
 */
export function hasDesignDir(files: FilesResponse | null | undefined): boolean {
  if (!files || !Array.isArray(files.files)) return false;
  return files.files.some((entry) => isDesignPath(entry?.path ?? ''));
}
