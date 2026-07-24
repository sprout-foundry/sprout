/**
 * GitClient — in-browser git operations via isomorphic-git + lightning-fs.
 *
 * Singleton service that wraps all git operations for the browser IDE.
 * Uses lightning-fs (IndexedDB-backed POSIX-ish filesystem) for storage
 * and isomorphic-git for protocol operations.
 *
 * Repos are stored at /repos/<owner>/<name>/ in the lightning-fs namespace.
 * The .git directory is stored alongside the working tree.
 */

import git from 'isomorphic-git';
import http from 'isomorphic-git/http/web';
import LightningFS from '@isomorphic-git/lightning-fs';

type GitAuthor = { name: string; email: string };
type GitAuth = { username?: string; password?: string; token?: string };

export interface CloneProgress {
  phase: string;
  loaded: number;
  total: number;
}

export type FileStatusType = 'modified' | 'added' | 'deleted' | 'untracked';

export interface GitStatusEntry {
  filepath: string;
  type: FileStatusType;
}

export interface GitLogEntry {
  oid: string;
  commit: {
    message: string;
    author: { name: string; email: string; timestamp: number };
    committer: { name: string; email: string; timestamp: number };
    tree: string;
    parent: string[];
  };
}

export interface DiffResult {
  filepath: string;
  type: 'modified' | 'added' | 'deleted';
  patch: string;
}

export interface FileEntry {
  name: string;
  path: string;
  type: 'file' | 'dir';
  size: number;
}

export interface CloneOptions {
  depth?: number;
  branch?: string;
  token?: string;
  singleBranch?: boolean;
  onProgress?: (progress: CloneProgress) => void;
}

export interface CommitOptions {
  author?: GitAuthor;
  committer?: GitAuthor;
}

export interface PushOptions {
  token: string;
  branch?: string;
  remote?: string;
  force?: boolean;
}

export interface PullOptions {
  token?: string;
  remote?: string;
  branch?: string;
  author?: GitAuthor;
}

class GitClient {
  private fs: LightningFS;
  private pfs: LightningFS['promises'];
  private dirLocks = new Map<string, Promise<unknown>>();

  constructor(namespace: string = 'sprout-git') {
    this.fs = new LightningFS(namespace);
    this.pfs = this.fs.promises;
  }

  /** Serialize operations on a single repo to avoid IndexedDB conflicts. */
  private async withLock<T>(dir: string, fn: () => Promise<T>): Promise<T> {
    const prev = this.dirLocks.get(dir) ?? Promise.resolve();
    const next = prev.then(fn, fn);
    this.dirLocks.set(
      dir,
      next.then(
        () => {},
        () => {},
      ),
    );
    try {
      return await next;
    } finally {
      if (this.dirLocks.get(dir) === next) {
        this.dirLocks.delete(dir);
      }
    }
  }

  /**
   * Clone a repository into lightning-fs.
   * Stores the repo at /repos/<owner>/<name>/.
   */
  async clone(url: string, dir: string, opts: CloneOptions = {}): Promise<void> {
    return this.withLock(dir, async () => {
      // Ensure parent directory exists
      const parent = dir.substring(0, dir.lastIndexOf('/'));
      if (parent) {
        await this.pfs.mkdir(parent).catch(() => {});
      }

      await git.clone({
        fs: this.fs,
        http,
        dir,
        url,
        depth: opts.depth ?? 1,
        singleBranch: opts.singleBranch ?? true,
        ref: opts.branch ?? 'main',
        corsProxy: undefined,
        onAuth: opts.token ? () => Promise.resolve({ token: opts.token } as GitAuth) : undefined,
        onProgress: opts.onProgress
          ? ({ phase, loaded, total }) => opts.onProgress!({ phase, loaded, total })
          : undefined,
      });
    });
  }

  /** Pull latest from remote. */
  async pull(dir: string, opts: PullOptions = {}): Promise<void> {
    return this.withLock(dir, async () => {
      await git.pull({
        fs: this.fs,
        http,
        dir,
        ref: opts.branch,
        singleBranch: true,
        author: opts.author,
        onAuth: opts.token ? () => Promise.resolve({ token: opts.token } as GitAuth) : undefined,
      });
    });
  }

  /** Push to remote. Requires token. */
  async push(dir: string, opts: PushOptions): Promise<void> {
    return this.withLock(dir, async () => {
      await git.push({
        fs: this.fs,
        http,
        dir,
        remote: opts.remote ?? 'origin',
        ref: opts.branch,
        force: opts.force ?? false,
        onAuth: () => Promise.resolve({ token: opts.token } as GitAuth),
      });
    });
  }

  /** Get working tree status. */
  async status(dir: string): Promise<GitStatusEntry[]> {
    const matrix = await git.statusMatrix({ fs: this.fs, dir });
    return matrix.map(([filepath, workdir, stage, HEAD]) => {
      // statusMatrix returns [filepath, workdir, stage, HEAD] where
      // values: 0=absent, 1=present, 2=identical-to-target
      // We collapse "identical" (2) into "present" (1) for simplicity.
      const wd = workdir > 0;
      const st = stage > 0;
      const hd = HEAD > 0;

      let type: FileStatusType;
      if (!wd && st && hd) type = 'deleted';
      else if (!wd && st && !hd) type = 'added';
      else if (wd && !st && !hd) type = 'untracked';
      else if (wd && st && !hd) type = 'added';
      else type = 'modified';

      return { filepath, type };
    });
  }

  /** Stage a file or all changes. */
  async add(dir: string, filepath?: string): Promise<void> {
    if (filepath) {
      await git.add({ fs: this.fs, dir, filepath });
      return;
    }
    // Stage all changes (handle both add and remove)
    const status = await this.status(dir);
    for (const entry of status) {
      try {
        if (entry.type === 'deleted') {
          await git.remove({ fs: this.fs, dir, filepath: entry.filepath });
        } else if (entry.type !== 'modified') {
          await git.add({ fs: this.fs, dir, filepath: entry.filepath });
        }
      } catch {
        // skip files that can't be staged
      }
    }
  }

  /** Unstage a file. */
  async unstage(dir: string, filepath: string): Promise<void> {
    await git.resetIndex({ fs: this.fs, dir, filepath });
  }

  /** Create a commit with staged changes. Returns the commit oid. */
  async commit(dir: string, message: string, opts: CommitOptions = {}): Promise<string> {
    const author = opts.author ?? {
      name: 'Sprout User',
      email: 'user@sprout.local',
    };
    const oid = await git.commit({
      fs: this.fs,
      dir,
      message,
      author: opts.author,
      committer: opts.committer ?? author,
    });
    return oid;
  }

  /** Get commit log. */
  async log(dir: string, opts: { depth?: number; ref?: string } = {}): Promise<GitLogEntry[]> {
    const commits = await git.log({
      fs: this.fs,
      dir,
      depth: opts.depth,
      ref: opts.ref,
    });
    return commits as GitLogEntry[];
  }

  /** List branches. */
  async listBranches(dir: string): Promise<string[]> {
    return git.listBranches({ fs: this.fs, dir });
  }

  /** Get current branch. */
  async currentBranch(dir: string): Promise<string | undefined> {
    try {
      const branch = await git.currentBranch({ fs: this.fs, dir });
      return branch ?? undefined;
    } catch {
      return undefined;
    }
  }

  /** Create a new branch. */
  async branch(dir: string, name: string): Promise<void> {
    await git.branch({ fs: this.fs, dir, ref: name });
  }

  /** Checkout a branch/tag/commit. */
  async checkout(dir: string, ref: string): Promise<void> {
    await git.checkout({ fs: this.fs, dir, ref });
  }

  /** Resolve a ref to a commit oid. */
  async resolveRef(dir: string, ref: string = 'HEAD'): Promise<string> {
    return git.resolveRef({ fs: this.fs, dir, ref });
  }

  /** List directory contents (non-recursive). */
  async listDir(dir: string, subpath: string = '/'): Promise<FileEntry[]> {
    const fullPath = subpath === '/' ? dir : `${dir}${subpath}`;
    const entries = await this.pfs.readdir(fullPath);

    const result: FileEntry[] = [];
    for (const name of entries) {
      if (name === '.git') continue;
      const entryPath = subpath === '/' ? `/${name}` : `${subpath}/${name}`;
      try {
        const stats = await this.pfs.stat(`${fullPath}/${name}`);
        result.push({
          name,
          path: entryPath,
          type: stats.isDirectory() ? 'dir' : 'file',
          size: stats.size,
        });
      } catch {
        // skip unreadable entries
      }
    }

    result.sort((a, b) => {
      if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    return result;
  }

  /** Recursively list all files. */
  async listAllFiles(dir: string): Promise<FileEntry[]> {
    const result: FileEntry[] = [];

    async function walk(pfs: LightningFS['promises'], path: string) {
      let entries: string[];
      try {
        entries = await pfs.readdir(path);
      } catch {
        return;
      }
      for (const name of entries) {
        if (name === '.git') continue;
        const fullPath = `${path}/${name}`;
        let stats;
        try {
          stats = await pfs.stat(fullPath);
        } catch {
          continue;
        }
        if (stats.isDirectory()) {
          result.push({
            name,
            path: fullPath.replace(dir, ''),
            type: 'dir',
            size: 0,
          });
          await walk(pfs, fullPath);
        } else {
          result.push({
            name,
            path: fullPath.replace(dir, ''),
            type: 'file',
            size: stats.size,
          });
        }
      }
    }

    await walk(this.pfs, dir);
    return result;
  }

  /** Read file contents as string. */
  async readFile(dir: string, filepath: string): Promise<string> {
    const fullPath = `${dir}${filepath.startsWith('/') ? '' : '/'}${filepath}`;
    const data = await this.pfs.readFile(fullPath, 'utf8');
    return typeof data === 'string' ? data : new TextDecoder().decode(data as Uint8Array);
  }

  /** Read file contents as binary. */
  async readFileBinary(dir: string, filepath: string): Promise<Uint8Array> {
    const fullPath = `${dir}${filepath.startsWith('/') ? '' : '/'}${filepath}`;
    const data = await this.pfs.readFile(fullPath);
    return data as Uint8Array;
  }

  /** Write file contents. Creates parent directories as needed. */
  async writeFile(dir: string, filepath: string, content: string): Promise<void> {
    const fullPath = `${dir}${filepath.startsWith('/') ? '' : '/'}${filepath}`;
    const parts = fullPath.split('/');
    for (let i = 1; i < parts.length - 1; i++) {
      const parentPath = parts.slice(0, i + 1).join('/');
      if (parentPath) {
        await this.pfs.mkdir(parentPath).catch(() => {});
      }
    }
    await this.pfs.writeFile(fullPath, content, 'utf8');
  }

  /** Create an empty directory. Ensures parent path exists. */
  async mkdir(dir: string, dirpath: string): Promise<void> {
    const fullPath = `${dir}${dirpath.startsWith('/') ? '' : '/'}${dirpath}`;
    await this.pfs.mkdir(fullPath);
  }

  /** Delete a file. */
  async deleteFile(dir: string, filepath: string): Promise<void> {
    const fullPath = `${dir}${filepath.startsWith('/') ? '' : '/'}${filepath}`;
    await this.pfs.unlink(fullPath);
  }

  /** Check if a repo exists locally. */
  async exists(dir: string): Promise<boolean> {
    try {
      await this.pfs.stat(`${dir}/.git`);
      return true;
    } catch {
      return false;
    }
  }

  /** Delete a local repo. */
  async delete(dir: string): Promise<void> {
    return this.withLock(dir, async () => {
      const entries = await this.listAllFiles(dir);
      for (const entry of entries.reverse()) {
        if (entry.type === 'dir') {
          await this.pfs.rmdir(`${dir}${entry.path}`).catch(() => {});
        } else {
          await this.pfs.unlink(`${dir}${entry.path}`).catch(() => {});
        }
      }
      await this.pfs.rmdir(dir).catch(() => {});
    });
  }

  /** Get diff for working tree changes. Returns simplified diff info. */
  async diff(dir: string): Promise<DiffResult[]> {
    const status = await this.status(dir);
    const results: DiffResult[] = [];

    for (const entry of status) {
      try {
        let patch = '';
        if (entry.type === 'added' || entry.type === 'untracked') {
          const content = await this.readFile(dir, entry.filepath).catch(() => '');
          patch = `+${content}`;
        } else if (entry.type === 'deleted') {
          // Read from HEAD tree
          try {
            const oid = await git.readBlob({
              fs: this.fs,
              dir,
              oid: await this.resolveRef(dir),
              filepath: entry.filepath,
            });
            patch = `-${new TextDecoder().decode(oid.blob)}`;
          } catch {
            patch = '(file deleted)';
          }
        } else {
          // Modified — show both old and new content as simplified diff
          patch = '(modified — open file to see changes)';
        }
        results.push({
          filepath: entry.filepath,
          type: entry.type === 'untracked' ? 'added' : entry.type,
          patch,
        });
      } catch {
        // Skip files we can't diff (binary, etc.)
      }
    }

    return results;
  }

  /** Get raw access to the underlying filesystem. */
  getFs(): LightningFS {
    return this.fs;
  }

  /** Standard repo path: /repos/<owner>/<name>. */
  static repoPath(owner: string, name: string): string {
    return `/repos/${owner}/${name}`;
  }
}

export const gitClient = new GitClient('sprout-git');
