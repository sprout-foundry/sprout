import { useCallback, useEffect, useState } from 'react';

/**
 * UI Size (P4.5 option C) — a persisted, user-facing density control.
 *
 * Scales the entire webui by setting `data-ui-scale` on <html>; CSS
 * (index.css) multiplies the text and spacing tokens per scale:
 *
 *   compact  ~0.92x  (laptop-docked, desktop default feel)
 *   default  1.00x  (current sizing, the historical default)
 *   large    1.08x  (tablet/touch distance reading)
 *
 * Scale is chosen explicitly via the Appearance settings section, or
 * implicitly on first run by the touch-large heuristic (see
 * resolveInitialUIScale): a coarse-pointer viewport wider than the phone
 * breakpoint (i.e. tablets) starts at large. The choice persists in
 * localStorage — the heuristic runs at most once, so a user who shrinks
 * back to default on a tablet keeps default.
 */
export const UI_SCALE_STORAGE_KEY = 'sprout.ui-scale';

export type UIScale = 'compact' | 'default' | 'large';

const VALID: readonly UIScale[] = ['compact', 'default', 'large'];

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
  // this attribute is the single seam (mirrors data-theme).
  useEffect(() => {
    document.documentElement.setAttribute('data-ui-scale', uiScale);
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
