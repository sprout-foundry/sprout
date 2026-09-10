import { useCallback, useEffect, useState } from 'react';

/**
 * UI Size (P4.5 option C) — a persisted, user-facing density control.
 *
 * Scales the entire webui two ways at once:
 *  1. CSS `zoom` on <html> — multiplies EVERYTHING (all px values,
 *     including the ~858 hardcoded font sizes that never adopted the
 *     tokens; token-only scaling moved just 30% of on-screen text and
 *     read as "barely larger" on device).
 *  2. Token bumps in App.css (the --text-... and --space-... custom
 *     properties) — adds extra weight on the tokenized majority so text
 *     outgrows chrome, not just scales with it.
 *
 * zoom is Safari/WebKit-origin, reflow-aware (media queries and JS
 * layout see the zoomed geometry), supported in WKWebView, Android
 * WebView and all current browsers. Fixed-position layers zoom too —
 * which is exactly the point.
 *
 * Tiers: compact 0.90x, default 1x, large 1.12x, xlarge 1.30x.
 *
 * Scale is chosen explicitly via the Appearance settings section, or
 * implicitly on first run by the touch-large heuristic (see
 * resolveInitialUIScale): a coarse-pointer viewport wider than the phone
 * breakpoint (i.e. tablets) starts at large. The choice persists in
 * localStorage — the heuristic runs at most once, so a user who shrinks
 * back to default on a tablet keeps default.
 */
export const UI_SCALE_STORAGE_KEY = 'sprout.ui-scale';

export type UIScale = 'compact' | 'default' | 'large' | 'xlarge';

const VALID: readonly UIScale[] = ['compact', 'default', 'large', 'xlarge'];

/** Paint factor per tier — MUST mirror the transform: scale() values on
 * #root in App.css. Exposed as --ui-scale for portal overlays (they
 * render to document.body, OUTSIDE #root's transform, so each portal
 * root applies the same scale itself; see .portal-scale in App.css). */
export const UI_SCALE_FACTOR: Record<UIScale, number> = {
  compact: 0.9,
  default: 1,
  large: 1.12,
  xlarge: 1.3,
};

export function isUIScale(v: unknown): v is UIScale {
  return typeof v === 'string' && (VALID as readonly string[]).includes(v);
}

/**
 * First-run heuristic (P4.5 option A): coarse pointer (finger as the
 * primary input) on a viewport wider than the 768px phone breakpoint —
 * the iPad band. Phone widths keep default (their layout is chat-first
 * sheets; large would waste the small canvas) and pointer-fine devices
 * (desktop/mouse) keep default.
 */
export function touchLargeEligible(win: Window = window): boolean {
  try {
    const coarse = win.matchMedia?.('(hover: none) and (pointer: coarse)').matches ?? false;
    return coarse && win.innerWidth > 768;
  } catch {
    return false;
  }
}

function readStored(): UIScale | null {
  try {
    const v = localStorage.getItem(UI_SCALE_STORAGE_KEY);
    return isUIScale(v) ? v : null;
  } catch {
    return null;
  }
}

/** Resolve the scale to apply on boot: stored choice, else heuristic. */
export function resolveInitialUIScale(win: Window = window): UIScale {
  const stored = readStored();
  if (stored) return stored;
  return touchLargeEligible(win) ? 'large' : 'default';
}

export function useUIScale(): {
  uiScale: UIScale;
  setUIScale: (scale: UIScale) => void;
} {
  // SSR/test safety: resolve lazily in an effect-primed state.
  const [uiScale, setUIScaleState] = useState<UIScale>(() =>
    typeof window === 'undefined' ? 'default' : resolveInitialUIScale(),
  );

  // Apply to <html> (and keep it applied on changes). CSS does the scaling;
  // this attribute is the single seam (mirrors data-theme). The numeric
  // factor is ALSO exposed as --ui-scale for portal overlays (below).
  useEffect(() => {
    document.documentElement.setAttribute('data-ui-scale', uiScale);
    document.documentElement.style.setProperty('--ui-scale', UI_SCALE_FACTOR[uiScale].toString());
  }, [uiScale]);

  const setUIScale = useCallback((scale: UIScale) => {
    if (!isUIScale(scale)) return;
    setUIScaleState(scale);
    try {
      localStorage.setItem(UI_SCALE_STORAGE_KEY, scale);
    } catch {
      // Private mode / quota: the attribute still applies for this session.
    }
  }, []);

  return { uiScale, setUIScale };
}
