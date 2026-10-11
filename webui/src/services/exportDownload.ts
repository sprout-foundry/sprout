/**
 * Download a server-rendered export (e.g. /api/sessions/{id}/export) as a file.
 *
 * The export is fetched through clientFetch rather than navigated to with a
 * plain `<a href>`: an embedding host's backend may live on another origin,
 * where a relative href would hit the page origin and the `download`
 * attribute is ignored anyway. The body is handed to the browser as a blob
 * URL, named from the response's Content-Disposition.
 */

import { clientFetch } from './clientSession';

export type ExportDownloadResult = { ok: true; filename: string } | { ok: false; status: number };

/** Fallback name when the server sends no usable Content-Disposition. */
export const DEFAULT_EXPORT_FILENAME = 'export';

/** Blob URLs are released after the browser has had time to start the download. */
const REVOKE_DELAY_MS = 30_000;

/** Extract the filename from a Content-Disposition header (RFC 6266). */
export function filenameFromContentDisposition(header: string | null): string {
  if (!header) return DEFAULT_EXPORT_FILENAME;
  const extended = /filename\*\s*=\s*(?:UTF-8|utf-8)''([^;]+)/.exec(header);
  if (extended) {
    try {
      const decoded = decodeURIComponent(extended[1].trim());
      if (decoded) return decoded;
    } catch {
      // Malformed percent-encoding: fall back to the plain filename parameter.
    }
  }
  const plain = /filename\s*=\s*(?:"([^"]*)"|([^;]+))/.exec(header);
  const name = (plain?.[1] ?? plain?.[2] ?? '').trim();
  return name || DEFAULT_EXPORT_FILENAME;
}

/**
 * Fetch `url` and save the body as a file. Resolves `{ ok: false, status }`
 * for a non-2xx response; rejects only on a network failure.
 */
export async function downloadExport(url: string): Promise<ExportDownloadResult> {
  const res = await clientFetch(url, { method: 'GET' });
  if (!res.ok) return { ok: false, status: res.status };
  const blob = await res.blob();
  const filename = filenameFromContentDisposition(res.headers.get('Content-Disposition'));
  const objectURL = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = objectURL;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  setTimeout(() => URL.revokeObjectURL(objectURL), REVOKE_DELAY_MS);
  return { ok: true, filename };
}
