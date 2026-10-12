/**
 * The VFS bridge browser git reads and writes the working tree through: the
 * WASM shell's workspace, the same files the editor, the agent and its shell
 * see. Paths are workspace-relative.
 */

import { configureBrowserGit, getBrowserGitVfsBridge, type BrowserGitConfig } from './browserGitStore';
import { listAllVfsFiles, trackFileWrite, untrackFileWrite } from './vfsFiles';
import type { WasmShell } from './wasmShell';

export interface BrowserGitAuthor {
  name?: string;
  email?: string;
}

export function browserGitBridgeFor(shell: WasmShell, author?: BrowserGitAuthor): BrowserGitConfig {
  return {
    name: author?.name || 'Browser IDE',
    email: author?.email || 'browser-ide@sprout.dev',
    readVfsFiles: () => listAllVfsFiles(shell),
    writeVfsFiles: async (files) => {
      for (const f of files) {
        const err = shell.writeFile(f.path, f.content);
        if (err) {
          console.warn(`[browserGit] could not write ${f.path} to the workspace: ${err}`);
          continue;
        }
        trackFileWrite(f.path);
      }
    },
    deleteVfsFiles: async (paths) => {
      for (const p of paths) {
        shell.deleteFile(p);
        untrackFileWrite(p);
      }
    },
  };
}

/**
 * Wire browser git to `shell` unless something already has (the hosted app's
 * initialization wires it with the signed-in account as author; a mounted
 * workspace with an in-browser agent has nothing else that would).
 */
export function ensureBrowserGitBridge(shell: WasmShell, author?: BrowserGitAuthor): void {
  if (getBrowserGitVfsBridge()) return;
  configureBrowserGit(browserGitBridgeFor(shell, author));
}
