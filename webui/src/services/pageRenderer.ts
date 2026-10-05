/**
 * In-page rasterizer for the browser build's design tools (SP-158 §158c).
 *
 * The WASM agent has no headless browser, but it runs inside one. Go's
 * page renderer (pkg/webcontent/browser_page_js.go) calls
 * `globalThis.__sproutRender({ path, width, height })` for a workspace file
 * and writes the PNG bytes it gets back, so design_render, design_critique
 * and every other screenshot caller work unchanged.
 *
 * Rendering: build a self-contained copy of the document (workspace refs
 * inlined as data: URLs), load it in an offscreen same-origin iframe, let its
 * scripts run and the DOM settle, then serialize the live DOM into an SVG
 * <foreignObject> and draw that onto a canvas.
 */

import { isBinaryAsset, referencedAssetPaths, rewriteScreenRefs } from '../design/screenRefs';
import type { WasmShell } from './wasmShell';

export interface PageRenderRequest {
  /** Absolute VFS path of the file to render. */
  path: string;
  width?: number;
  height?: number;
}

const DEFAULT_WIDTH = 1280;
const DEFAULT_HEIGHT = 720;
/** The DOM counts as settled after this long without a mutation. */
const QUIET_MS = 150;
/** Upper bound on waiting for scripts (screen runtime, mermaid) to settle. */
const SETTLE_LIMIT_MS = 5000;

type FileReader = Pick<WasmShell, 'readFile' | 'readFileBytes'>;

function toDataUrl(bytes: Uint8Array, mime: string): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
  return `data:${mime};base64,${btoa(binary)}`;
}

function binaryMime(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  const known: Record<string, string> = {
    png: 'image/png',
    jpg: 'image/jpeg',
    jpeg: 'image/jpeg',
    gif: 'image/gif',
    webp: 'image/webp',
    avif: 'image/avif',
    woff: 'font/woff',
    woff2: 'font/woff2',
    ttf: 'font/ttf',
    otf: 'font/otf',
  };
  return known[ext] ?? 'application/octet-stream';
}

/** Build the document to render: every workspace-relative reference inlined. */
export function buildRenderDocument(shell: FileReader, path: string, html: string): string {
  const previewPath = path.replace(/^\/+/, '');
  const inline: Record<string, string> = {};
  for (const ref of referencedAssetPaths(html, previewPath)) {
    const abs = `/${ref}`;
    if (isBinaryAsset(ref)) {
      const res = shell.readFileBytes(abs);
      if (res.bytes) inline[ref] = toDataUrl(res.bytes, binaryMime(ref));
    } else {
      const res = shell.readFile(abs);
      if (!res.error) inline[ref] = res.content;
    }
  }
  return rewriteScreenRefs(html, { previewPath, inline });
}

function nextFrame(win: Window): Promise<void> {
  return new Promise((resolve) => win.requestAnimationFrame(() => resolve()));
}

/** Resolves once the document has gone QUIET_MS without a DOM mutation, or after SETTLE_LIMIT_MS. */
function settle(doc: Document): Promise<void> {
  return new Promise((resolve) => {
    const timers: { quiet?: ReturnType<typeof setTimeout>; limit?: ReturnType<typeof setTimeout> } = {};
    const observer = new MutationObserver(() => {
      clearTimeout(timers.quiet);
      timers.quiet = setTimeout(finish, QUIET_MS);
    });
    function finish(): void {
      observer.disconnect();
      clearTimeout(timers.quiet);
      clearTimeout(timers.limit);
      resolve();
    }
    observer.observe(doc, { subtree: true, childList: true, attributes: true, characterData: true });
    timers.quiet = setTimeout(finish, QUIET_MS);
    timers.limit = setTimeout(finish, SETTLE_LIMIT_MS);
  });
}

async function loadIntoFrame(html: string, width: number, height: number): Promise<HTMLIFrameElement> {
  const frame = document.createElement('iframe');
  frame.setAttribute('sandbox', 'allow-scripts allow-same-origin');
  frame.setAttribute('aria-hidden', 'true');
  frame.tabIndex = -1;
  Object.assign(frame.style, {
    position: 'fixed',
    left: '-100000px',
    top: '0',
    width: `${width}px`,
    height: `${height}px`,
    border: '0',
    pointerEvents: 'none',
  });
  const loaded = new Promise<void>((resolve, reject) => {
    frame.onload = () => resolve();
    frame.onerror = () => reject(new Error('render frame failed to load'));
  });
  frame.srcdoc = html;
  document.body.appendChild(frame);
  await loaded;
  return frame;
}

function serializeForSvg(doc: Document): string {
  const clone = doc.documentElement.cloneNode(true) as HTMLElement;
  clone.querySelectorAll('script').forEach((el) => el.remove());
  return new XMLSerializer().serializeToString(clone);
}

async function rasterizeSvg(svg: string, width: number, height: number): Promise<Uint8Array> {
  const img = new Image();
  img.width = width;
  img.height = height;
  img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  await img.decode();

  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('2D canvas unavailable');
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(img, 0, 0, width, height);

  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'));
  if (!blob) throw new Error('canvas produced no PNG');
  return new Uint8Array(await blob.arrayBuffer());
}

/** Render one workspace file to PNG bytes. */
export async function renderToPng(shell: FileReader, req: PageRenderRequest): Promise<Uint8Array> {
  const width = req.width && req.width > 0 ? Math.round(req.width) : DEFAULT_WIDTH;
  const height = req.height && req.height > 0 ? Math.round(req.height) : DEFAULT_HEIGHT;
  const source = shell.readFile(req.path);
  if (source.error) throw new Error(`cannot read ${req.path}: ${source.error}`);

  if (req.path.toLowerCase().endsWith('.svg')) {
    return rasterizeSvg(source.content, width, height);
  }

  const frame = await loadIntoFrame(buildRenderDocument(shell, req.path, source.content), width, height);
  try {
    const win = frame.contentWindow;
    const doc = frame.contentDocument;
    if (!win || !doc) throw new Error('render frame has no document');
    await doc.fonts?.ready;
    await settle(doc);
    await nextFrame(win);
    const markup = serializeForSvg(doc);
    const svg =
      `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">` +
      `<foreignObject x="0" y="0" width="100%" height="100%">${markup}</foreignObject></svg>`;
    return await rasterizeSvg(svg, width, height);
  } finally {
    frame.remove();
  }
}

declare global {
  /* eslint-disable no-var -- only `var` augments `typeof globalThis` */
  var __sproutRender: ((req: PageRenderRequest) => Promise<Uint8Array>) | undefined;
  /* eslint-enable no-var */
}

/** Expose the renderer to the WASM agent. Safe to call more than once. */
export function registerPageRenderer(shell: FileReader): void {
  globalThis.__sproutRender = (req: PageRenderRequest) => renderToPng(shell, req);
}
