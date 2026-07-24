/**
 * Repo VFS bridge — sync cloned repo files from lightning-fs to the WASM VFS.
 *
 * The agent's file tools read from the WASM VFS (a Go-level filesystem).
 * Cloned repos live in lightning-fs (IndexedDB). This bridge copies working-tree
 * files between the two so the agent can operate on cloned repos.
 */

import { gitClient } from './gitClient';

/** Minimal interface for the WASM shell's write capability. */
interface WasmWriter {
  writeFile(path: string, content: string): string; // returns error or ""
}

const SKIP_DIRS = new Set(['.git', 'node_modules', 'target', 'dist', '.next', 'build']);

export interface SyncProgress {
  total: number;
  done: number;
  current: string;
}

/**
 * Copy all working-tree files from a cloned repo (lightning-fs) to the WASM VFS.
 * Skips binary files, large files, and ignored directories.
 */
export async function syncRepoToWasmVfs(
  repoDir: string,
  wasmDir: string,
  wasm: WasmWriter,
  onProgress?: (p: SyncProgress) => void
): Promise<{ copied: number; skipped: number; errors: string[] }> {
  let copied = 0;
  let skipped = 0;
  const errors: string[] = [];

  const allFiles = await gitClient.listAllFiles(repoDir);
  const textFiles = allFiles.filter((f) => f.type === 'file' && f.size < 1_000_000);

  for (let i = 0; i < textFiles.length; i++) {
    const file = textFiles[i];
    if (onProgress) {
      onProgress({ total: textFiles.length, done: i, current: file.path });
    }

    // Check if any parent directory is in SKIP_DIRS
    const parts = file.path.split('/').filter(Boolean);
    if (parts.some((p) => SKIP_DIRS.has(p))) {
      skipped++;
      continue;
    }

    try {
      const content = await gitClient.readFile(repoDir, file.path);
      const wasmPath = `${wasmDir}${file.path}`;
      const err = wasm.writeFile(wasmPath, content);
      if (err) {
        errors.push(`${file.path}: ${err}`);
      } else {
        copied++;
      }
    } catch (err: any) {
      errors.push(`${file.path}: ${err.message ?? err}`);
    }
  }

  if (onProgress) {
    onProgress({ total: textFiles.length, done: textFiles.length, current: '' });
  }

  return { copied, skipped, errors };
}

/**
 * Copy a single file from the WASM VFS back to the cloned repo (for agent edits).
 */
export async function syncWasmFileToRepo(
  repoDir: string,
  wasmPath: string,
  content: string
): Promise<void> {
  // Strip the WASM workspace prefix to get the repo-relative path
  // Convention: files are at /workspace/repo/<path> in WASM VFS
  const match = wasmPath.match(/^\/workspace\/repo\/(.+)$/);
  const relPath = match ? match[1] : wasmPath.replace(/^\//, '');
  await gitClient.writeFile(repoDir, relPath, content);
}