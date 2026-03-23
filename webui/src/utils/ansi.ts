/**
 * ANSI escape code utilities
 */

/**
 * Strip ANSI escape codes from text
 * Removes color codes, formatting, and other terminal control sequences
 */
export function stripAnsiCodes(text: unknown): string {
  const normalized =
    typeof text === 'string'
      ? text
      : text == null
        ? ''
        : typeof text === 'object'
          ? JSON.stringify(text, null, 2)
          : String(text);

  // Normalize common line endings first.
  // Keep lone CR as line breaks so carriage-return updates don't smash text together.
  let cleaned = normalized.replace(/\r\n/g, '\n').replace(/\r(?!\n)/g, '\n');

  // Remove OSC (Operating System Command) sequences:
  // ESC ] ... BEL   or   ESC ] ... ESC \
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B\][\s\S]*?(?:\x07|\x1B\\)/g, '');
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\u009D[\s\S]*?(?:\u0007|\u009C)/g, '');

  // Remove CSI (Control Sequence Introducer) sequences:
  // ESC [ <params> <intermediates> <final>
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, '');
  cleaned = cleaned.replace(/\u009B[0-?]*[ -/]*[@-~]/g, '');

  // Remove other 2-byte ESC sequences.
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B[ -~]/g, '');
  cleaned = cleaned.replace(/\u009B|\u009D|\u009C/g, '');
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/\x1B/g, '');

  // Remove leftover non-printable C0 control chars except tab/newline.
  // eslint-disable-next-line no-control-regex
  cleaned = cleaned.replace(/[\x00-\x08\x0B-\x1F\x7F\u0080-\u009F]/g, '');

  return cleaned;
}

/**
 * Check if text contains ANSI codes
 */
export function hasAnsiCodes(text: unknown): boolean {
  return stripAnsiCodes(text) !== text;
}

// ---------------------------------------------------------------------------
// ANSI → HTML converter
// ---------------------------------------------------------------------------

const FG_CLASSES: readonly string[] = [
  'ansi-black', 'ansi-red', 'ansi-green', 'ansi-yellow',
  'ansi-blue', 'ansi-magenta', 'ansi-cyan', 'ansi-white',
  'ansi-bright-black', 'ansi-bright-red', 'ansi-bright-green', 'ansi-bright-yellow',
  'ansi-bright-blue', 'ansi-bright-magenta', 'ansi-bright-cyan', 'ansi-bright-white',
];

const BG_CLASSES: readonly string[] = [
  'ansi-bg-black', 'ansi-bg-red', 'ansi-bg-green', 'ansi-bg-yellow',
  'ansi-bg-blue', 'ansi-bg-magenta', 'ansi-bg-cyan', 'ansi-bg-white',
];

/** Standard 8-color RGB values (indices 0–7). */
const STD_COLORS: readonly [number, number, number][] = [
  [0, 0, 0],       // 0  black
  [224, 57, 57],   // 1  red
  [57, 224, 57],   // 2  green
  [224, 224, 57],  // 3  yellow
  [57, 124, 224],  // 4  blue
  [224, 57, 224],  // 5  magenta
  [57, 224, 224],  // 6  cyan
  [224, 224, 224], // 7  white
];

/** Bright 8-color RGB values (indices 8–15). */
const BRIGHT_COLORS: readonly [number, number, number][] = [
  [102, 102, 102], // 8  bright black
  [248, 113, 113], // 9  bright red
  [134, 239, 172], // 10 bright green
  [253, 224, 71],  // 11 bright yellow
  [147, 197, 253], // 12 bright blue
  [240, 171, 252], // 13 bright magenta
  [103, 232, 249], // 14 bright cyan
  [255, 255, 255], // 15 bright white
];

/** All 16 CSS-class colors for nearest-match lookups. */
const ALL_16: readonly { rgb: readonly [number, number, number]; fg: string; bg: string }[] = [
  ...STD_COLORS.map((rgb, i) => ({ rgb, fg: FG_CLASSES[i], bg: BG_CLASSES[i] })),
  ...BRIGHT_COLORS.map((rgb, i) => ({
    rgb,
    fg: FG_CLASSES[8 + i],
    bg: BG_CLASSES[i], // bright BG reuses standard BG classes
  })),
];

function colorDistSq(a: readonly [number, number, number], b: readonly [number, number, number]): number {
  const dr = a[0] - b[0];
  const dg = a[1] - b[1];
  const db = a[2] - b[2];
  return dr * dr + dg * dg + db * db;
}

/** Find nearest of our 16 CSS colors to (r,g,b). Returns { fg, bg } class pair. */
function nearestColor(r: number, g: number, b: number): { fg: string; bg: string } {
  const target: [number, number, number] = [r, g, b];
  let bestIdx = 0;
  let bestDist = Infinity;
  for (let i = 0; i < ALL_16.length; i++) {
    const d = colorDistSq(target, ALL_16[i].rgb);
    if (d < bestDist) {
      bestDist = d;
      bestIdx = i;
    }
  }
  return ALL_16[bestIdx];
}

/**
 * Map a 256-color index (0–255) to the nearest CSS class pair.
 *   0–7 → standard, 8–15 → bright,
 *  16–231 → 6×6×6 cube, 232–255 → grayscale ramp.
 */
function color256ToClass(n: number, prefix: 'fg' | 'bg'): string {
  if (n < 0 || n > 255) n = 7;
  if (n < 8) return prefix === 'bg' ? BG_CLASSES[n] : FG_CLASSES[n];
  if (n < 16) return prefix === 'bg' ? BG_CLASSES[n - 8] : FG_CLASSES[8 + (n - 8)];

  if (n >= 232) {
    const v = 8 + (n - 232) * 10;
    const { fg, bg } = nearestColor(v, v, v);
    return prefix === 'bg' ? bg : fg;
  }

  const i = n - 16;
  const r = Math.floor(i / 36) * 40 + 55;
  const g = Math.floor((i % 36) / 6) * 40 + 55;
  const b = (i % 6) * 40 + 55;
  const { fg, bg } = nearestColor(r, g, b);
  return prefix === 'bg' ? bg : fg;
}

/** HTML-encode &, <, >. */
function esc(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Compose CSS-class string from active style state (empty string = no styling). */
function buildClasses(
  fg: string | null,
  bg: string | null,
  b: boolean, it: boolean, u: boolean, bl: boolean, rev: boolean,
): string {
  const c: string[] = [];
  if (fg) c.push(fg);
  if (bg) c.push(bg);
  if (b) c.push('ansi-bold');
  if (it) c.push('ansi-italic');
  if (u) c.push('ansi-underline');
  if (bl) c.push('ansi-blink');
  if (rev) c.push('ansi-reverse');
  return c.join(' ');
}

/**
 * Convert ANSI escape sequences to HTML using the CSS classes defined
 * in Terminal.css (.ansi-red, .ansi-bold, etc.).
 *
 * The returned string is already HTML-escaped and may be used with
 * `dangerouslySetInnerHTML`.
 */
export function ansiToHtml(text: unknown): string {
  const raw =
    typeof text === 'string'
      ? text
      : text == null
        ? ''
        : typeof text === 'object'
          ? JSON.stringify(text, null, 2)
          : String(text);

  let str = raw;
  // Normalize line endings
  str = str.replace(/\r\n/g, '\n').replace(/\r(?!\n)/g, '\n');

  // Strip OSC sequences (ESC ] … BEL / ESC \)
  // eslint-disable-next-line no-control-regex
  str = str.replace(/\x1B\][\s\S]*?(?:\x07|\x1B\\)/g, '');
  // eslint-disable-next-line no-control-regex
  str = str.replace(/\u009D[\s\S]*?(?:\u0007|\u009C)/g, '');

  // Remove other 2-byte ESC sequences while preserving CSI (ESC [ ...),
  // which we parse below.
  // eslint-disable-next-line no-control-regex
  str = str.replace(/\x1B(?!\[|\])[ -~]/g, '');
  str = str.replace(/\u009C/g, '');

  // Strip non-printable control chars except tab/newline and ESC.
  // Keep ESC so CSI parsing below can consume sequences like ESC[31m.
  // eslint-disable-next-line no-control-regex
  str = str.replace(/[\x00-\x08\x0B\x0C\x0E-\x1A\x1C-\x1F\x7F\u0080-\u009A\u009C-\u009F]/g, '');

  // Walk through CSI sequences. Only SGR (final byte 'm') affects styling;
  // every other CSI is silently stripped.
  // eslint-disable-next-line no-control-regex
  const CSI_RE = /(?:\x1B\[|\u009B)([0-?]*)([@-~])/g;

  let fgClass: string | null = null;
  let bgClass: string | null = null;
  let sBold = false;
  let sItalic = false;
  let sUnderline = false;
  let sBlink = false;
  let sReverse = false;

  let openClasses = ''; // the class string on the currently-open <span>, or '' if none
  const out: string[] = [];
  let lastIdx = 0;
  let m: RegExpExecArray | null;

  while ((m = CSI_RE.exec(str)) !== null) {
    // Emit plain text before this CSI
    const chunk = str.slice(lastIdx, m.index);
    if (chunk) out.push(esc(chunk));

    lastIdx = m.index + m[0].length;

    // Only process SGR (final byte 'm'); strip everything else
    if (m[2] !== 'm') continue;

    // Parse SGR params  — "CSI <params> m"
    const paramStr = m[1];
    const params =
      paramStr === ''
        ? [0]
        : paramStr.split(';').map(p => (p === '' ? 0 : parseInt(p, 10) || 0));

    let pi = 0;
    while (pi < params.length) {
      const p = params[pi];

      if (p === 0) {
        fgClass = null; bgClass = null;
        sBold = false; sItalic = false; sUnderline = false; sBlink = false; sReverse = false;
      } else if (p === 1) { sBold = true; }
      else if (p === 3) { sItalic = true; }
      else if (p === 4) { sUnderline = true; }
      else if (p === 5 || p === 6) { sBlink = true; }
      else if (p === 7) { sReverse = true; }
      else if (p === 22) { sBold = false; }
      else if (p === 23) { sItalic = false; }
      else if (p === 24) { sUnderline = false; }
      else if (p === 25) { sBlink = false; }
      else if (p === 27) { sReverse = false; }
      else if (p >= 30 && p <= 37) { fgClass = FG_CLASSES[p - 30]; }
      else if (p === 38) {
        if (params[pi + 1] === 5 && pi + 2 < params.length) {
          fgClass = color256ToClass(params[pi + 2], 'fg'); pi += 2;
        } else if (params[pi + 1] === 2 && pi + 4 < params.length) {
          fgClass = nearestColor(params[pi + 2], params[pi + 3], params[pi + 4]).fg; pi += 4;
        }
      } else if (p === 39) { fgClass = null; }
      else if (p >= 40 && p <= 47) { bgClass = BG_CLASSES[p - 40]; }
      else if (p === 48) {
        if (params[pi + 1] === 5 && pi + 2 < params.length) {
          bgClass = color256ToClass(params[pi + 2], 'bg'); pi += 2;
        } else if (params[pi + 1] === 2 && pi + 4 < params.length) {
          bgClass = nearestColor(params[pi + 2], params[pi + 3], params[pi + 4]).bg; pi += 4;
        }
      } else if (p === 49) { bgClass = null; }
      else if (p >= 90 && p <= 97) { fgClass = FG_CLASSES[8 + (p - 90)]; }
      else if (p >= 100 && p <= 107) { bgClass = BG_CLASSES[p - 100]; }

      pi++;
    }

    // Emit span transition if the active style changed
    const newClasses = buildClasses(fgClass, bgClass, sBold, sItalic, sUnderline, sBlink, sReverse);
    if (newClasses !== openClasses) {
      if (openClasses) out.push('</span>');
      if (newClasses) out.push(`<span class="${esc(newClasses)}">`);
      openClasses = newClasses;
    }
  }

  // Emit remaining text
  if (lastIdx < str.length) {
    out.push(esc(str.slice(lastIdx)));
  }

  // Close any open span
  if (openClasses) out.push('</span>');

  return out.join('');
}
