/**
 * Byte-exact file and image handling for the browser build (SP-158 §158b).
 * The WASM shell's readFile returns text through JSON, which mangles
 * binary files; these paths carry bytes instead.
 */

import type { WasmShell } from './wasmShell';

// Binary types go through readFileBytes: readFile returns text through
// JSON, which replaces non-UTF-8 bytes. Mirrors the daemon's MIME switch.
const BINARY_MIME_TYPES: Record<string, string> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  bmp: 'image/bmp',
  ico: 'image/x-icon',
  avif: 'image/avif',
  pdf: 'application/pdf',
  woff: 'font/woff',
  woff2: 'font/woff2',
  ttf: 'font/ttf',
  otf: 'font/otf',
};

export function binaryMimeType(path: string): string | null {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  return BINARY_MIME_TYPES[ext] ?? null;
}

/** Reads an upload body (FormData with an `image` field, Blob, or buffer) as bytes. */
export async function uploadBodyBytes(body: unknown): Promise<Uint8Array | null> {
  if (body instanceof FormData) {
    const image = body.get('image');
    return image instanceof Blob ? new Uint8Array(await image.arrayBuffer()) : null;
  }
  if (body instanceof Blob) return new Uint8Array(await body.arrayBuffer());
  if (body instanceof ArrayBuffer) return new Uint8Array(body);
  if (body instanceof Uint8Array) return body;
  return null;
}

/** POST /api/upload/image — stores the image in the VFS; same reply shape as the daemon. */
export function handleWasmImageUpload(shell: WasmShell, bytes: Uint8Array): Response {
  const result = shell.saveImage(bytes);
  if (result.error || !result.path) {
    const message = result.error ?? 'Failed to save image';
    return json({ error: message, message }, 400);
  }
  return json({ path: result.path, filename: result.filename ?? '' }, 200);
}

function json(data: unknown, status: number): Response {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
}
