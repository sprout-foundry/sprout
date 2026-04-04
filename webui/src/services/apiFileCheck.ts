import { clientFetch } from './clientSession';
import { debugLog } from '../utils/log';

export interface FileCheckEntry {
  path: string;
  mtime: number; // unix seconds
}

export interface FileModifiedResult {
  path: string;
  mod_time: number;
  size: number;
}

export interface FileCheckModifiedResponse {
  modified: FileModifiedResult[];
}

/**
 * Check which files have been modified on disk since their known mtime.
 * Returns only the files that actually changed.
 */
export async function checkFilesModified(files: FileCheckEntry[]): Promise<FileCheckModifiedResponse> {
  const body: Record<string, number> = {};
  for (const f of files) {
    body[f.path] = f.mtime;
  }

  const response = await clientFetch('/api/file/check-modified', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ files: body }),
  });

  if (!response.ok) {
    const text = await response.text().catch((err) => { debugLog('[checkFilesModified] failed to read error response body:', err); return response.statusText; });
    throw new Error(`File check failed (${response.status}): ${text.slice(0, 200)}`);
  }

  const text = await response.text();
  try {
    return JSON.parse(text) as FileCheckModifiedResponse;
  } catch (err) {
    debugLog('[checkFilesModified] failed to parse response JSON:', err);
    throw new Error(`File check returned invalid JSON: ${text.slice(0, 200)}`);
  }
}
