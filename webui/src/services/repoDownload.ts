/**
 * Download a cloned repo as a ZIP file.
 *
 * Uses JSZip to assemble all files from lightning-fs into
 * a download-able Blob. Streams files one at a time to avoid OOM
 * on large repos.
 */

import JSZip from 'jszip';
import { gitClient, FileEntry } from './gitClient';

const MAX_ZIP_SIZE = 500 * 1024 * 1024; // 500MB limit
const LARGE_REPO_WARNING_MB = 100; // warn above 100MB

/**
 * Download a cloned repository as a ZIP file.
 * @param repoDir The lightning-fs repo directory (e.g. /repos/owner/name)
 * @param repoName Display name for the ZIP file
 * @param onProgress Optional progress callback
 */
export async function downloadRepoAsZip(
  repoDir: string,
  repoName: string,
  onProgress?: (done: number, total: number) => void,
): Promise<void> {
  const zip = new JSZip();
  const allFiles = await gitClient.listAllFiles(repoDir);
  const textFiles = allFiles.filter((f) => f.type === 'file');
  const total = textFiles.length;

  let totalBytes = 0;
  let warnedLarge = false;

  for (let i = 0; i < total; i++) {
    const file = textFiles[i];
    const content = await gitClient.readFileBinary(repoDir, file.path);
    totalBytes += content.byteLength;

    if (!warnedLarge && totalBytes > LARGE_REPO_WARNING_MB * 1024 * 1024) {
      console.warn(
        `[zip] Repo exceeds ${LARGE_REPO_WARNING_MB}MB — ZIP may be large. ` +
          `Current size: ${Math.round(totalBytes / (1024 * 1024))}MB`,
      );
      warnedLarge = true;
    }

    if (totalBytes > MAX_ZIP_SIZE) {
      throw new Error(
        `Repo too large for ZIP download (${Math.round(totalBytes / (1024 * 1024))}MB). ` +
          `Maximum ZIP size is ${MAX_ZIP_SIZE / 1024 / 1024}MB. Clone locally with 'git clone' instead.`,
      );
    }

    // Strip leading slash for ZIP path
    const zipPath = file.path.startsWith('/') ? file.path.slice(1) : file.path;
    zip.file(zipPath, content, { binary: true });

    if (onProgress) {
      onProgress(i + 1, total);
    }
  }

  // Generate the ZIP blob
  const blob = await zip.generateAsync({ type: 'blob', compression: 'DEFLATE', compressionOptions: { level: 6 } });

  // Trigger download
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${repoName.replace(/[^a-zA-Z0-9_.-]/g, '_')}.zip`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
