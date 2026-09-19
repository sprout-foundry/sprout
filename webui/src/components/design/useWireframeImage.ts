/**
 * Object-URL imagery for canvas nodes (SP-140-3 §3b).
 *
 * Wireframe SVGs render inside React Flow nodes as `<img>` imagery. The
 * canvas already has the file's *text* (from `designApi.readAsset`), so the
 * source of truth stays the workspace file: the text is wrapped in a Blob and
 * handed to the browser as an object URL. Nothing is inlined with
 * `dangerouslySetInnerHTML`, so a wireframe's own scripts/handlers never run
 * inside the canvas document.
 *
 * The creating/mounting component owns revocation: `revoke()` releases a URL
 * this hook created, and the hook releases whatever it still holds on unmount.
 * `URL.createObjectURL` is unavailable in some jsdom builds, so every call is
 * feature-checked — a missing implementation degrades to "no imagery", never
 * a thrown render.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

/** MIME type for SVG imagery handed to `<img>` via an object URL. */
export const SVG_MIME_TYPE = 'image/svg+xml';

/** Create an object URL for SVG text, or null when the API is unavailable. */
export function createSvgObjectUrl(text: string): string | null {
  if (!text) return null;
  if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return null;
  try {
    return URL.createObjectURL(new Blob([text], { type: SVG_MIME_TYPE }));
  } catch {
    return null;
  }
}

/** Release an object URL; a no-op for null or when the API is unavailable. */
export function revokeObjectUrl(url: string | null | undefined): void {
  if (!url) return;
  if (typeof URL === 'undefined' || typeof URL.revokeObjectURL !== 'function') return;
  try {
    URL.revokeObjectURL(url);
  } catch {
    // A URL the browser already released — nothing to clean up.
  }
}

export interface WireframeImage {
  /** Object URL for the wireframe's SVG text, or null when unavailable. */
  url: string | null;
  /** Load failed (a broken object URL or an unrenderable SVG). */
  failed: boolean;
  /** Report a load failure from the `<img>` element. */
  onError: () => void;
}

/**
 * Object URL for a wireframe's SVG text. A changed `svgText` (a re-read of the
 * file) mints a fresh URL and releases the previous one, so long-lived canvases
 * do not leak blob URLs while the user pans and selects.
 */
export function useWireframeImage(svgText: string | null | undefined): WireframeImage {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const held = useRef<string | null>(null);

  useEffect(() => {
    const text = svgText ?? '';
    if (!text) {
      revokeObjectUrl(held.current);
      held.current = null;
      setUrl(null);
      setFailed(false);
      return;
    }
    const next = createSvgObjectUrl(text);
    revokeObjectUrl(held.current);
    held.current = next;
    setUrl(next);
    setFailed(false);
    return () => {
      // Only release a URL this effect instance still owns. A later effect
      // (or the `!text` branch) may already have replaced or revoked
      // `held.current`, so revoking unconditionally here could double-release.
      if (held.current !== next) return;
      revokeObjectUrl(held.current);
      held.current = null;
    };
  }, [svgText]);

  const onError = useCallback(() => setFailed(true), []);

  return { url, failed, onError };
}
