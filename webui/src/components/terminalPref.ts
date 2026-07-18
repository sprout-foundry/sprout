/**
 * Terminal localStorage-backed preferences and their parse/clamp helpers.
 *
 * Pref-keys are stored in localStorage and restored across sessions. The
 * parsers here tolerate bad input (returns the default rather than NaN);
 * the clamp helpers bound the parsed value to a usable range.
 *
 * `terminalConstants.ts` carries cross-package default values (re-exported
 * from @sprout/ui); this file carries per-key storage strings and the
 * localStorage-shaped parse/clamp logic. Kept as two siblings rather than
 * one merged file because the parsers have no equivalent in @sprout/ui
 * and we don't want to grow the shared package with webui-only concerns.
 */

import { FONT_SIZE_DEFAULT } from './terminalConstants';

// ---------- Terminal height ----------

/** Minimum usable collapsed height (px). Below this the resize handle is unusable. */
export const TERMINAL_HEIGHT_MIN = 120;
/** Default terminal height (px) when none has been persisted. */
export const TERMINAL_HEIGHT_DEFAULT = 400;
/** Pixel reservation from the bottom of the viewport so the terminal never fully covers it. */
export const TERMINAL_HEIGHT_MAX_FACTOR = 100;
/** localStorage key for the terminal height pref. */
export const TERMINAL_HEIGHT_STORAGE_KEY = 'sprout-terminal-height';

/** Parses a stored terminal height; falls back to default on any non-finite value. */
export const parseTerminalHeight = (raw: string | null): number => {
  if (!raw) return TERMINAL_HEIGHT_DEFAULT;
  const n = Number(raw);
  return Number.isFinite(n) ? n : TERMINAL_HEIGHT_DEFAULT;
};

/** Clamps to [MIN, viewport_innerHeight - MAX_FACTOR]; default for non-finite or SSR. */
export const clampTerminalHeight = (value: number): number => {
  if (!Number.isFinite(value)) return TERMINAL_HEIGHT_DEFAULT;
  if (typeof window === 'undefined') return TERMINAL_HEIGHT_DEFAULT;
  return Math.max(TERMINAL_HEIGHT_MIN, Math.min(window.innerHeight - TERMINAL_HEIGHT_MAX_FACTOR, value));
};

// ---------- Font size ----------

/** Minimum font size (px). Matches the smallest readable setting in the zoom UI. */
export const FONT_SIZE_MIN = 8;
/** Maximum font size (px). Above this xterm renders unacceptably large glyphs. */
export const FONT_SIZE_MAX = 32;
/** localStorage key for the font size pref. */
export const FONT_SIZE_STORAGE_KEY = 'sprout-terminal-font-size';

/** Parses a stored font size; falls back to FONT_SIZE_DEFAULT on any non-finite value. */
export const parseFontSize = (raw: string | null): number => {
  if (!raw) return FONT_SIZE_DEFAULT;
  const n = Number(raw);
  return Number.isFinite(n) ? n : FONT_SIZE_DEFAULT;
};

/** Clamps to [MIN, MAX]; default for non-finite values. */
export const clampFontSize = (value: number): number => {
  if (!Number.isFinite(value)) return FONT_SIZE_DEFAULT;
  return Math.max(FONT_SIZE_MIN, Math.min(FONT_SIZE_MAX, value));
};

// ---------- Copy on select ----------

/** localStorage key for the copy-on-select pref. Webui-specific (other consumers don't share). */
export const COPY_ON_SELECT_STORAGE_KEY = 'sprout-terminal-copy-on-select';

/**
 * Parses a stored copy-on-select preference. The stored representation is
 * the literal string "true" or "false"; anything else (including "TRUE",
 * null, empty) returns the supplied fallback. Case-sensitive by design —
 * a malformed value should surface as "use the default" rather than
 * silently being interpreted as on.
 */
export const parseCopyOnSelect = (raw: string | null, fallback: boolean): boolean => {
  if (raw === null) return fallback;
  return raw === 'true';
};
