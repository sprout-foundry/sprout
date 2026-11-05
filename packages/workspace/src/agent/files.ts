/**
 * The agent's workspace files, read and written through the WASM module's
 * in-memory filesystem (mirrored to the host's file store, when it gave one).
 */

import { toAbsolutePath, toWorkspacePath } from './events';
import type { AgentFileEntry, AgentFiles, SproutWasmBridge } from './types';

function parseJSON<T>(raw: string, fallback: T): T {
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

/** Directories `snapshot` and `walk` leave out: VCS state and installed dependencies. */
const SKIPPED_DIRS = new Set(['.git', 'node_modules']);

export function workspaceRootOf(bridge: SproutWasmBridge): string {
  return bridge.getWorkspaceRoot?.() || '/workspace';
}

export function readWorkspaceFile(bridge: SproutWasmBridge, absPath: string): string | null {
  const res = parseJSON<{ content?: string; error?: string }>(bridge.readFile(absPath), { error: 'unreadable' });
  return res.error ? null : (res.content ?? '');
}

export function createAgentFiles(bridge: SproutWasmBridge): AgentFiles {
  const root = () => workspaceRootOf(bridge);

  const list = (dir = '.'): AgentFileEntry[] => {
    const abs = toAbsolutePath(dir, root());
    const parsed = parseJSON<unknown>(bridge.listDir(abs), null);
    // The module returns a bare array (null for an empty directory) or {error}.
    const entries = Array.isArray(parsed) ? (parsed as Array<{ name: string; type: string; size?: number }>) : [];
    return entries.map((e) => {
      const childAbs = abs === '/' ? `/${e.name}` : `${abs.replace(/\/+$/, '')}/${e.name}`;
      return {
        name: e.name,
        path: toWorkspacePath(childAbs, root()),
        type: e.type === 'dir' ? 'dir' : 'file',
        size: e.size ?? 0,
      };
    });
  };

  const walk = (): string[] => {
    const out: string[] = [];
    const visit = (dir: string) => {
      for (const entry of list(dir)) {
        if (entry.type === 'dir') {
          if (!SKIPPED_DIRS.has(entry.name)) visit(entry.path);
        } else {
          out.push(entry.path);
        }
      }
    };
    visit('.');
    return out.sort();
  };

  return {
    get root() {
      return root();
    },
    read: (path) => readWorkspaceFile(bridge, toAbsolutePath(path, root())),
    list,
    walk,
    write: (path, content) => {
      const err = bridge.writeFile(toAbsolutePath(path, root()), content);
      if (err) throw new Error(`Sprout agent: write ${path}: ${err}`);
    },
    snapshot: () => {
      const files: Record<string, string> = {};
      for (const path of walk()) {
        const content = readWorkspaceFile(bridge, toAbsolutePath(path, root()));
        if (content !== null) files[path] = content;
      }
      return files;
    },
  };
}

/** A file store that keeps files in memory (for tests, or a host persisting elsewhere). */
export function memoryFileStore(initial: Array<{ path: string; content: string }> = []) {
  const files = new Map(initial.map((f) => [f.path, f.content]));
  return {
    files,
    load: () => [...files].map(([path, content]) => ({ path, content })),
    save: (path: string, content: string) => {
      files.set(path, content);
    },
    delete: (path: string) => {
      files.delete(path);
    },
  };
}
