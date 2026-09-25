/**
 * Screen-ref rewriting for the HTML preview iframes (SP-143 §143.4).
 *
 * `LivePreview` renders a screen as `<iframe srcDoc>`, and srcDoc has no base
 * URL — every workspace-relative reference in the document silently breaks in
 * the tool while resolving fine in the agent's `file://` render path. The
 * app's `X-Frame-Options: DENY` also rules out framing `/api/file` directly,
 * so the fix is in-memory: rewrite the refs of the *preview copy only* to the
 * app's file proxy (`/api/file?path=<workspace path>`, the `fileUrl` shape)
 * and stamp the preview marker the runtime's switcher gates on.
 *
 * The rewritten string is never persisted: callers feed it to an iframe's
 * srcDoc while the edited content (the textarea, the write-back) stays the
 * untouched original. `pkg/design/templates/runtime/sprout-screens.js` applies
 * the same URL algebra to documents it swaps in, so the preview and the
 * runtime agree on one proxy shape.
 */

import { fileUrl } from '../services/api/designApiPaths';

/** Attribute references the rewrite covers (href/src across the tag set screens use). */
const ATTR_RE = /\s(href|src)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/gi;

/** CSS url(…) tokens, quoted or bare (the runtime carries the same scanner). */
const CSS_URL_RE = /url\(\s*(?:'([^']*)'|"([^"]*)"|([^)'"]*))\s*\)/gi;

/** @import "…" / @import url(…) string forms. */
const CSS_IMPORT_RE = /@import\s+(?:url\(\s*)?(['"])([^'"]+)\1/gi;

/**
 * Not workspace-relative: absolute (https:, mailto:), protocol-relative
 * (//host/x), root-relative (/api/file?path=… — an already-proxied URL), or a
 * same-document fragment. data: URLs match the scheme rule.
 */
const NOT_RELATIVE_RE = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#|\/)/i;

export interface RewriteScreenRefsOptions {
  /** The previewed document's own path (`design/screens/login.html`). */
  previewPath: string;
}

/**
 * Join `ref` onto the directory of `previewPath`, honouring `./` and `../` —
 * the same algebra the browser applies to a document at that path (and the
 * runtime applies to swapped documents).
 */
function resolveAgainstPreview(previewPath: string, ref: string): string {
  const dir = previewPath.split('/').slice(0, -1);
  const out: string[] = [];
  for (const seg of dir) {
    if (seg !== '' && seg !== '.') out.push(seg);
  }
  for (const seg of ref.split('/')) {
    if (seg === '' || seg === '.') continue;
    if (seg === '..') out.pop();
    else out.push(seg);
  }
  return out.join('/');
}

function rewriteOneRef(previewPath: string, raw: string): string {
  const value = raw.trim();
  if (!value || NOT_RELATIVE_RE.test(value)) return raw;
  return fileUrl(resolveAgainstPreview(previewPath, value));
}

function rewriteCSS(previewPath: string, css: string): string {
  let out = css.replace(
    CSS_URL_RE,
    (whole, sq: string | undefined, dq: string | undefined, bare: string | undefined) => {
      const target = sq !== undefined ? sq : dq !== undefined ? dq : (bare ?? '');
      const next = rewriteOneRef(previewPath, target);
      return next === target ? whole : `url(${next})`;
    },
  );
  out = out.replace(CSS_IMPORT_RE, (whole, quote: string, target: string) => {
    const next = rewriteOneRef(previewPath, target);
    return next === target ? whole : `@import ${quote}${next}${quote}`;
  });
  return out;
}

/**
 * Rewrite the workspace-relative references of `html` — href=/src= attributes
 * and the same targets inside `<style>` blocks and style="" attributes — to
 * `/api/file` proxy URLs resolved against `previewPath`. Absolute,
 * protocol-relative, data:, and pure-fragment references pass through
 * untouched; unparseable HTML comes back unchanged.
 */
export function rewriteScreenRefs(html: string, options: RewriteScreenRefsOptions): string {
  const previewPath = options.previewPath.replace(/\\/g, '/').replace(/^\.\//, '');
  if (!html || !previewPath) return html;
  let out = html.replace(
    ATTR_RE,
    (whole, attr: string, dq: string | undefined, sq: string | undefined, bare: string | undefined) => {
      const value = dq !== undefined ? dq : sq !== undefined ? sq : (bare ?? '');
      const next = rewriteOneRef(previewPath, value);
      return next === value ? whole : ` ${attr}="${next.replace(/"/g, '&quot;')}"`;
    },
  );
  out = out.replace(
    /<style\b[^>]*>([\s\S]*?)<\/style>/gi,
    (_whole, css: string) => `<style>${rewriteCSS(previewPath, css)}</style>`,
  );
  out = out.replace(/\sstyle\s*=\s*"([^"]*)"/gi, (whole, css: string) => {
    const next = rewriteCSS(previewPath, css);
    return next === css ? whole : ` style="${next}"`;
  });
  return out;
}

/**
 * Stamp `<html data-sprout-preview>` so the screen runtime renders its
 * state switcher in this iframe only (SP-143 §143.3's preview gate).
 * Fragment documents without an `<html>` tag come back unchanged.
 */
export function injectPreviewMarker(html: string): string {
  const openTag = /<html\b([^>]*)>/i.exec(html);
  if (!openTag) return html;
  if (/\bdata-sprout-preview(?:\s|=|$)/i.test(openTag[1])) return html;
  return `${html.slice(0, openTag.index)}<html${openTag[1]} data-sprout-preview>${html.slice(openTag.index + openTag[0].length)}`;
}
