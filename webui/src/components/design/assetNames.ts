/**
 * Display-name and section helpers for design assets (SP-140-5).
 *
 * Extracted from the retired in-surface assets rail: the sidebar pane, the
 * detail pane, and the screens grid all present the same asset names, so the
 * trimming rule lives in one import-free module.
 */

/**
 * Display name for an asset: the human name without its serialization
 * extension. The design surface is about the artifact, not the file format —
 * `.mmd`/`.html`/`.svg`/`.tokens.json` leakage reads as a file manager; the
 * full path stays available as a tooltip.
 */
export function assetDisplayName(name: string): string {
  return String(name ?? '').replace(/\.(mmd|html|svg|png|jpg|jpeg|webp|tokens\.json)$/i, '');
}

/**
 * Whether an asset path belongs to a section. Inventory paths are relative to
 * the workspace root in the live app (`design/flows/…`) and to `design/` in
 * some hosts (`flows/…`), so the check is on the path's segment, not a prefix.
 */
export function assetMatchesSection(path: string, tab: string): boolean {
  return String(path ?? '')
    .split(/[/\\]/)
    .includes(tab);
}
