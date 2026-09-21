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
import { classify } from '../../services/api/designApiPaths';

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

/** How the workspace's `design/` directory relates to Sprout's tree idiom. */
export type DesignTreeState =
  /** No design/ paths anywhere in the listing. */
  | 'none'
  /** design/ exists but nothing in it matches Sprout's asset vocabulary —
   * someone else's folder (PSD exports, docs, scattered images). */
  | 'foreign'
  /** At least one canonical design asset (tokens, wireframes, screens,
   * flows, feedback, brand, icons, or the README manifest). */
  | 'recognized';

/**
 * Classify the workspace's design/ directory against Sprout's asset
 * vocabulary. Reuses `classify()` (designApiPaths) so there is one spelling
 * of what counts as a tree — a folder holding only foreign material (PSDs,
 * docs, unrelated exports) reads as `foreign`, never as a tree, and the
 * surface responds with the onboarding variant that acknowledges it instead
 * of an empty live grid.
 */
export function classifyDesignTree(files: FilesResponse | null | undefined): DesignTreeState {
  if (!files || !Array.isArray(files.files)) return 'none';
  let sawDesign = false;
  for (const entry of files.files) {
    const path = (entry?.path ?? '').replace(/\\/g, '/').replace(/^\.\//, '');
    if (!isDesignPath(path)) continue;
    sawDesign = true;
    if (classify(path)) return 'recognized';
  }
  return sawDesign ? 'foreign' : 'none';
}

/** Path suffixes/segments that indicate frontend code in the workspace. */
const FRONTEND_CODE_EXTENSIONS = ['.tsx', '.jsx', '.vue', '.svelte', '.html'];
const FRONTEND_CODE_FILES = ['package.json'];

/**
 * True when the workspace listing shows frontend code — component sources,
 * a package manifest, or HTML entry points — and no `design/` tree.
 *
 * This is the Design empty state's position signal (greenfield vs
 * code-first): a workspace that already renders something benefits from
 * discovery cards first; a bare workspace from drafting/import cards.
 * Deliberately shallow: any nesting depth counts (monorepos), and `design/`
 * assets are excluded because their presence makes the question moot (the
 * live surface renders instead of the empty state).
 */
export function hasFrontendSignal(files: FilesResponse | null | undefined): boolean {
  if (!files || !Array.isArray(files.files)) return false;
  return files.files.some((entry) => {
    const path = (entry?.path ?? '').replace(/\\/g, '/');
    if (isDesignPath(path)) return false;
    const lower = path.toLowerCase();
    if (FRONTEND_CODE_FILES.some((name) => lower === name || lower.endsWith(`/${name}`))) return true;
    return FRONTEND_CODE_EXTENSIONS.some((ext) => lower.endsWith(ext));
  });
}
