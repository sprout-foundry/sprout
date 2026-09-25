/**
 * SP-143 §143.4 — the screen-ref rewriter's contract.
 *
 * The preview shows a screen document whose relative refs must resolve inside
 * a srcDoc iframe (no base URL) through the app's file proxy. The rewrite must
 * cover every reference form the kit produces (links, scripts, images, inline
 * CSS, style attributes), leave foreign URLs and the edited bytes alone, and
 * resolve `./`/`../` against the screen's own directory — the same algebra the
 * Go runtime applies to documents it swaps in.
 */

import { describe, expect, it } from 'vitest';
import { injectPreviewMarker, rewriteScreenRefs } from './screenRefs';

const PROXY = (path: string) => `/api/file?path=${encodeURIComponent(path)}`;
const rewrite = (html: string, previewPath = 'design/screens/login.html') => rewriteScreenRefs(html, { previewPath });

describe('rewriteScreenRefs — href/src attributes', () => {
  it('rewrites stylesheet links into proxy URLs', () => {
    expect(rewrite('<link rel="stylesheet" href="../generated/tokens.css">')).toBe(
      `<link rel="stylesheet" href="${PROXY('design/generated/tokens.css')}">`,
    );
  });

  it('rewrites script, img, and sibling-screen links', () => {
    const out = rewrite(
      '<script defer src="../runtime/sprout-screens.js"></script><img src="../icons/logo.svg"><a href="inbox.html">inbox</a>',
    );
    expect(out).toContain(`src="${PROXY('design/runtime/sprout-screens.js')}"`);
    expect(out).toContain(`src="${PROXY('design/icons/logo.svg')}"`);
    expect(out).toContain(`href="${PROXY('design/screens/inbox.html')}"`);
  });

  it('resolves ./ and ../ against the screen directory, however deep', () => {
    expect(rewrite('<a href="./inbox.html">', 'design/screens/wizard/step-one.html')).toContain(
      PROXY('design/screens/wizard/inbox.html'),
    );
    expect(rewrite('<a href="../../tokens/color.tokens.json">', 'design/screens/wizard/step-one.html')).toContain(
      PROXY('design/tokens/color.tokens.json'),
    );
  });

  it('handles single-quoted and bare attribute values', () => {
    expect(rewrite("<a href='../generated/tokens.css'>")).toContain(PROXY('design/generated/tokens.css'));
    expect(rewrite('<a href=../generated/tokens.css>')).toContain(PROXY('design/generated/tokens.css'));
  });
});

describe('rewriteScreenRefs — inline CSS', () => {
  it('rewrites url() inside <style> blocks', () => {
    const out = rewrite('<style>body{background:url("../icons/dot.svg")}</style>');
    expect(out).toContain(`url(${PROXY('design/icons/dot.svg')})`);
  });

  it('rewrites url() and @import in style="" attributes', () => {
    const out = rewrite(
      '<div style="background:url(../icons/dot.svg)"><style>@import "../generated/tokens.css";</style>',
    );
    expect(out).toContain(`url(${PROXY('design/icons/dot.svg')})`);
    expect(out).toContain(`@import "${PROXY('design/generated/tokens.css')}"`);
  });
});

describe('rewriteScreenRefs — untouched references', () => {
  it('leaves absolute, protocol-relative, data:, and pure-hash refs alone', () => {
    const html =
      '<link href="https://cdn.example.com/x.css">' +
      '<a href="//cdn.example.com/y.css">p</a>' +
      '<img src="data:image/png;base64,aGk=">' +
      '<a href="#section">jump</a>';
    expect(rewrite(html)).toBe(html);
  });

  it('keeps already-proxied URLs idempotent', () => {
    const html = `<link rel="stylesheet" href="${PROXY('design/generated/tokens.css')}">`;
    expect(rewrite(html)).toBe(html);
  });

  it('returns non-HTML and empty input unchanged', () => {
    expect(rewrite('')).toBe('');
    expect(rewrite('<html lang="en"><head></head><body></body></html>', '')).toBe(
      '<html lang="en"><head></head><body></body></html>',
    );
  });
});

describe('injectPreviewMarker', () => {
  it('stamps data-sprout-preview on the <html> tag', () => {
    expect(injectPreviewMarker('<html lang="en" data-states="a,b">')).toBe(
      '<html lang="en" data-states="a,b" data-sprout-preview>',
    );
  });

  it('is idempotent and tolerates fragment documents', () => {
    expect(injectPreviewMarker('<html data-sprout-preview>')).toBe('<html data-sprout-preview>');
    expect(injectPreviewMarker('<body>fragment</body>')).toBe('<body>fragment</body>');
  });
});

describe('preview composition — marker + rewrite together', () => {
  it('produces a document the runtime resolves: base-template refs proxy, marker stamped', () => {
    const screen =
      '<html lang="en" data-device="phone"><head>' +
      '<link rel="stylesheet" href="../generated/tokens.css">' +
      '<link rel="stylesheet" href="../runtime/chrome.css">' +
      '<script defer src="../runtime/sprout-screens.js"></script>' +
      '</head><body data-sprout-screen="login"></body></html>';
    const out = injectPreviewMarker(rewrite(screen));
    expect(out).toContain('data-sprout-preview');
    expect(out).toContain(`href="${PROXY('design/generated/tokens.css')}"`);
    expect(out).toContain(`href="${PROXY('design/runtime/chrome.css')}"`);
    expect(out).toContain(`src="${PROXY('design/runtime/sprout-screens.js')}"`);
  });
});
