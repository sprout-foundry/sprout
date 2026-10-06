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
  /**
   * Workspace-relative path → file content, inlined as data: URLs instead of
   * `/api/file` proxy URLs. The hosted editor's iframe fetches bypass the
   * page's fetch interceptor (a real HTTP request the platform server
   * refuses), so there the refs must carry their bytes with them. Text
   * assets (CSS, SVG) inline as base64 data: URLs — unknown paths fall back
   * to the proxy form.
   */
  inline?: Record<string, string>;
}

/** Text asset types the inliner knows a MIME type for. */
const INLINE_MIME: Array<{ suffix: string; mime: string }> = [
  { suffix: '.css', mime: 'text/css' },
  { suffix: '.svg', mime: 'image/svg+xml' },
  { suffix: '.js', mime: 'text/javascript' },
  { suffix: '.json', mime: 'application/json' },
  { suffix: '.txt', mime: 'text/plain' },
];

/**
 * Binary asset types (images, fonts). Their inline value is already a data:
 * URL — the host reads the bytes (`readAssetDataUrl`), since a text read
 * would mangle them.
 */
const BINARY_SUFFIXES = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.avif', '.woff', '.woff2', '.ttf', '.otf'];

/** Whether `path` is an asset the inliner can carry: text it encodes, or binary passed as a data: URL. */
export function isInlinableAsset(path: string): boolean {
  const lower = path.toLowerCase();
  return INLINE_MIME.some((e) => lower.endsWith(e.suffix)) || BINARY_SUFFIXES.some((s) => lower.endsWith(s));
}

/** Whether `path` must be read as bytes and inlined as a data: URL. */
export function isBinaryAsset(path: string): boolean {
  const lower = path.toLowerCase();
  return BINARY_SUFFIXES.some((s) => lower.endsWith(s));
}

/** Base64 (btoa, UTF-8-safe via code points) data URL for a text asset. */
function dataUrlFor(path: string, content: string): string | null {
  if (content.startsWith('data:')) return content;
  const entry = INLINE_MIME.find((e) => path.toLowerCase().endsWith(e.suffix));
  if (!entry || typeof btoa !== 'function') return null;
  const bytes = new TextEncoder().encode(content);
  let binary = '';
  for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
  return `data:${entry.mime};base64,${btoa(binary)}`;
}

/**
 * Join `ref` onto the directory of `previewPath`, honouring `./` and `../` —
 * the same algebra the browser applies to a document at that path (and the
 * runtime applies to swapped documents).
 */
export function resolveAgainstPreview(previewPath: string, ref: string): string {
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

function rewriteOneRef(previewPath: string, raw: string, inline?: Record<string, string>): string {
  const value = raw.trim();
  if (!value || NOT_RELATIVE_RE.test(value)) return raw;
  const resolved = resolveAgainstPreview(previewPath, value);
  if (inline) {
    const content = inline[resolved];
    if (content !== undefined) {
      const dataUrl = dataUrlFor(resolved, content);
      if (dataUrl) return dataUrl;
    }
  }
  return fileUrl(resolved);
}

function rewriteCSS(previewPath: string, css: string, inline?: Record<string, string>): string {
  let out = css.replace(
    CSS_URL_RE,
    (whole, sq: string | undefined, dq: string | undefined, bare: string | undefined) => {
      const target = sq !== undefined ? sq : dq !== undefined ? dq : (bare ?? '');
      const next = rewriteOneRef(previewPath, target, inline);
      return next === target ? whole : `url(${next})`;
    },
  );
  out = out.replace(CSS_IMPORT_RE, (whole, quote: string, target: string) => {
    const next = rewriteOneRef(previewPath, target, inline);
    return next === target ? whole : `@import ${quote}${next}${quote}`;
  });
  return out;
}

const INLINE_SCRIPT_RE = /(<script\b[^>]*>)([\s\S]*?)(<\/script>)/gi;
const HELD_SCRIPT_MARK = '__sproutHeldScript__';
const HELD_SCRIPT_RE = /__sproutHeldScript__(\d+)__sproutHeldScript__/g;

const EXTERNAL_SCRIPT_RE = /<script\b([^>]*?)\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)')([^>]*)>\s*<\/script>/gi;

/**
 * Inline workspace scripts for a srcdoc copy. Each `<script src>` with
 * inlined bytes becomes a non-executing marker that keeps its src in proxy
 * form, and its code runs inline at the end of <body> (where `defer` would
 * have run it). The screen runtime finds itself by its script src, so a
 * data: URL would leave it unbooted; an inline script also runs under a CSP
 * that allows inline scripts but not data: sources.
 */
function inlineExternalScripts(
  html: string,
  previewPath: string,
  inline: Record<string, string>,
  appended: string[],
): string {
  return html.replace(EXTERNAL_SCRIPT_RE, (whole, _pre: string, dq: string | undefined, sq: string | undefined) => {
    const src = (dq ?? sq ?? '').trim();
    if (!src || NOT_RELATIVE_RE.test(src)) return whole;
    const resolved = resolveAgainstPreview(previewPath, src);
    const code = inline[resolved];
    if (code === undefined || !resolved.toLowerCase().endsWith('.js')) return whole;
    appended.push(code.replace(/<\/script/gi, '<\\/script'));
    return `<script type="text/x-sprout-src" src="${fileUrl(resolved)}"></script>`;
  });
}

/**
 * Rewrite the workspace-relative references of `html` — href=/src= attributes
 * and the same targets inside `<style>` blocks and style="" attributes — to
 * `/api/file` proxy URLs resolved against `previewPath` (or data: URLs when
 * `inline` carries the asset bytes). Absolute, protocol-relative, data:, and
 * pure-fragment references pass through untouched; unparseable HTML comes
 * back unchanged.
 */
export function rewriteScreenRefs(html: string, options: RewriteScreenRefsOptions): string {
  const previewPath = options.previewPath.replace(/\\/g, '/').replace(/^\.\//, '');
  if (!html || !previewPath) return html;
  const inline = options.inline;
  const appended: string[] = [];
  // Inline script bodies are code, not markup: hold them out of the rewrite so
  // a string like `src="x"` or `url(x)` inside a bundle is left alone.
  const held: string[] = [];
  const source = (inline ? inlineExternalScripts(html, previewPath, inline, appended) : html).replace(
    INLINE_SCRIPT_RE,
    (whole, open: string, body: string, close: string) => {
      if (!body.trim()) return whole;
      held.push(body);
      return `${open}${HELD_SCRIPT_MARK}${held.length - 1}${HELD_SCRIPT_MARK}${close}`;
    },
  );
  let out = source.replace(
    ATTR_RE,
    (whole, attr: string, dq: string | undefined, sq: string | undefined, bare: string | undefined) => {
      const value = dq !== undefined ? dq : sq !== undefined ? sq : (bare ?? '');
      const next = rewriteOneRef(previewPath, value, inline);
      return next === value ? whole : ` ${attr}="${next.replace(/"/g, '&quot;')}"`;
    },
  );
  out = out.replace(
    /<style\b[^>]*>([\s\S]*?)<\/style>/gi,
    (_whole, css: string) => `<style>${rewriteCSS(previewPath, css, inline)}</style>`,
  );
  out = out.replace(/\sstyle\s*=\s*"([^"]*)"/gi, (whole, css: string) => {
    const next = rewriteCSS(previewPath, css, inline);
    return next === css ? whole : ` style="${next}"`;
  });
  if (held.length > 0) {
    out = out.replace(HELD_SCRIPT_RE, (_whole, index: string) => held[Number(index)] ?? '');
  }
  if (appended.length === 0) return out;
  const tail = appended.map((code) => `<script>${code}</script>`).join('');
  return /<\/body>/i.test(out) ? out.replace(/<\/body>/i, `${tail}</body>`) : out + tail;
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

/**
 * The workspace-relative asset paths `html` references (href=/src= plus CSS
 * url()/@import inside <style> and style=""), resolved against `previewPath`
 * and filtered to the assets the preview inliner can carry. This is the
 * read list a hosted host resolves through the page's own file path before
 * handing the map to `rewriteScreenRefs` — the iframe's own fetches bypass
 * the page's fetch interceptor, so the bytes must ride in the document.
 */
export function referencedAssetPaths(html: string, previewPath: string): string[] {
  const preview = previewPath.replace(/\\/g, '/').replace(/^\.\//, '');
  if (!html || !preview) return [];
  const found = new Set<string>();
  const consider = (raw: string): void => {
    const value = raw.trim();
    if (!value || NOT_RELATIVE_RE.test(value)) return;
    const resolved = resolveAgainstPreview(preview, value);
    if (isInlinableAsset(resolved)) found.add(resolved);
  };
  for (const m of html.matchAll(ATTR_RE)) {
    const value = m[2] ?? m[3] ?? m[4] ?? '';
    consider(value);
  }
  for (const css of [...html.matchAll(/<style\b[^>]*>([\s\S]*?)<\/style>/gi)].map((m) => m[1])) {
    for (const m of css.matchAll(CSS_URL_RE)) consider(m[3] ?? m[2] ?? m[1] ?? '');
    for (const m of css.matchAll(CSS_IMPORT_RE)) consider(m[2]);
  }
  return [...found].sort();
}
