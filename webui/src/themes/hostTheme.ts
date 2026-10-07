import type { SproutHost } from '../host/types';

/**
 * The host theme's token overrides, scoped to the workspace root.
 *
 * A host themes Sprout through `host.theme.tokens` (SP-160 §160d), whose names
 * are the `@sprout-foundry/design` token names. A host page and the workspace
 * it mounts share one document, so the overrides must not leak onto the host's
 * own DOM: they are written as inline custom properties on the workspace root
 * element (`.sprout-workspace`, the element `SproutWorkspace` renders), not on
 * `document.documentElement`. The local app's own theme (its mode/pack and the
 * pack's variables) still lands on `documentElement` — only the host's
 * overrides are confined.
 *
 * They are applied LAST so they win over the theme pack's own variables (packs
 * layer on top of the package's base tokens; host overrides layer on top of
 * the pack). Sprout's `ThemeContext` keeps applying only the pack to
 * `documentElement`, so the app is styled by the pack while the host's
 * overrides stay inside the root.
 *
 * Non-custom-property keys are ignored: a token is a CSS custom property
 * (`--name`), and `style.setProperty` on anything else is meaningless or
 * actively harmful (setting `color` on the root would not be a token
 * override).
 */

/** Whether a host token key is a CSS custom property name. */
function isTokenKey(key: string): boolean {
  return /^--[A-Za-z0-9_-]+$/.test(key);
}

/**
 * The custom-property keys this module last wrote on each element, so a
 * re-application removes exactly what it wrote (not every custom property on
 * the element, which another feature could own). A WeakMap keeps the tracking
 * off the DOM and lets the entry be collected with the element.
 */
const appliedKeys = new WeakMap<HTMLElement, Set<string>>();

/**
 * The token overrides a host supplies, or an empty record when the host has no
 * theme or supplies no tokens. Read at call time (not import time), so a live
 * host replacement reaches the next application.
 */
export function resolveHostThemeOverrides(host: SproutHost | null | undefined): Record<string, string> {
  const tokens = host?.theme?.tokens;
  if (!tokens) return {};
  const overrides: Record<string, string> = {};
  for (const [key, value] of Object.entries(tokens)) {
    if (isTokenKey(key)) overrides[key] = value;
  }
  return overrides;
}

/**
 * Write the host theme's token overrides to `element` as inline custom
 * properties, first removing the keys this function wrote on a previous call —
 * only those, so an unrelated inline custom property on the element (set by
 * some other feature) is left alone. The keys written are tracked per element
 * and also returned.
 */
export function applyHostThemeOverrides(element: HTMLElement, overrides: Record<string, string>): string[] {
  clearHostThemeOverrides(element);
  const keys = Object.keys(overrides);
  for (const key of keys) {
    element.style.setProperty(key, overrides[key]);
  }
  appliedKeys.set(element, new Set(keys));
  return keys;
}

/** Remove the custom-property overrides this module wrote on `element`. */
export function clearHostThemeOverrides(element: HTMLElement): void {
  const keys = appliedKeys.get(element);
  if (!keys) return;
  for (const key of keys) element.style.removeProperty(key);
  appliedKeys.delete(element);
}
