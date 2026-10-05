import { describe, expect, it } from 'vitest';
import { buildRenderDocument } from './pageRenderer';

const files: Record<string, string> = {
  '/workspace/design/generated/tokens.css': ':root{--c:#000}',
  '/workspace/design/runtime/sprout-screens.js': 'window.booted = 1;',
};
const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);

const shell = {
  readFile: (path: string) => (path in files ? { content: files[path] } : { content: '', error: 'not found' }),
  readFileBytes: (path: string) =>
    path === '/workspace/design/brand/logo.png' ? { bytes: png } : { error: 'not found' },
};

describe('buildRenderDocument', () => {
  it('inlines every workspace asset the screen references, binary included', () => {
    const html =
      '<html><head><link rel="stylesheet" href="../generated/tokens.css">' +
      '<script src="../runtime/sprout-screens.js" defer></script></head>' +
      '<body><img src="../brand/logo.png"></body></html>';
    const doc = buildRenderDocument(shell, '/workspace/design/screens/login.html', html);

    expect(doc).toContain('href="data:text/css;base64,');
    expect(doc).toContain(`src="data:image/png;base64,${btoa(String.fromCharCode(...png))}"`);
    expect(doc).toContain('<script>window.booted = 1;</script></body>');
    expect(doc).toContain('type="text/x-sprout-src"');
  });

  it('leaves unreadable refs in proxy form rather than failing', () => {
    const doc = buildRenderDocument(shell, '/workspace/design/screens/a.html', '<link href="missing.css">');
    expect(doc).toContain('/api/file?path=');
  });
});
