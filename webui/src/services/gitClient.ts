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

import LightningFS from '@isomorphic-git/lightning-fs';
import git from 'isomorphic-git';
import http from 'isomorphic-git/http/web';
import { gitCorsProxy } from './gitCorsProxy';

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
  /** Omitted when the git proxy supplies the account's credentials. */
  token?: string;
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

export interface FetchOptions {
  /** Omitted when the git proxy supplies the account's credentials. */
  token?: string;
  /** Remote to fetch from (default: "origin"). */
  remote?: string;
  /** Single ref to fetch (e.g. "refs/heads/feature"); omitted fetches all. */
  ref?: string;
  /** Fetch only the ref's own branch. Default false (all branches). */
  singleBranch?: boolean;
  /** Shallow-fetch depth. Omitted fetches full history for the refs. */
  depth?: number;
  /** Also fetch tags. Default false. */
  tags?: boolean;
  /** Prune remote-tracking refs that no longer exist on the remote. */
  prune?: boolean;
}

// isomorphic-git reads username/password (sent as Basic auth); GitHub takes
// a token as the password with any username.
function tokenAuth(token: string | undefined): GitAuth {
  return { username: 'x-access-token', password: token ?? '' };
}

// Fallback identity for commit/pull when no author is supplied and no
// user.name/user.email is configured. Without it isomorphic-git refuses to
// write a commit ("No name was provided for author").
const DEFAULT_AUTHOR: GitAuthor = { name: 'Sprout User', email: 'user@sprout.local' };

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
   *
   * Defaults to a multi-branch clone (all remote refs) at a bounded depth so
   * origin/<branch> refs exist for checkout — a depth-1 single-branch clone
   * leaves only the default branch reachable, so PR branches fail to resolve.
   * Callers wanting a shallow single-branch clone pass the options explicitly.
   */
  async clone(url: string, dir: string, opts: CloneOptions = {}): Promise<void> {
    return this.withLock(dir, async () => {
      // Ensure parent directory exists (EEXIST from an existing parent is fine)
      const parent = dir.substring(0, dir.lastIndexOf('/'));
      if (parent) {
        await this.pfs.mkdir(parent).catch(() => undefined);
      }

      await git.clone({
        fs: this.fs,
        http,
        dir,
        url,
        depth: opts.depth ?? 50,
        singleBranch: opts.singleBranch ?? false,
        ref: opts.branch,
        corsProxy: gitCorsProxy(),
        onAuth: opts.token ? () => Promise.resolve(tokenAuth(opts.token)) : undefined,
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
        corsProxy: gitCorsProxy(),
        dir,
        ref: opts.branch,
        singleBranch: true,
        // A pull that merges creates a commit, and isomorphic-git requires an
        // author for it. Without a default, a merge needs a configured
        // user.name/user.email and otherwise fails with "No name was provided".
        author: opts.author ?? DEFAULT_AUTHOR,
        onAuth: opts.token ? () => Promise.resolve(tokenAuth(opts.token)) : undefined,
      });
    });
  }

  /** Read a git config value (e.g. "user.name"), or null when unset. */
  async getConfig(dir: string, path: string): Promise<string | null> {
    try {
      const value = await git.getConfig({ fs: this.fs, dir, path });
      return typeof value === 'string' ? value : null;
    } catch {
      // best-effort: an unset key reads as null.
      return null;
    }
  }

  /** Set a git config value (e.g. "user.name") for the repo. */
  async setConfig(dir: string, path: string, value: string): Promise<void> {
    await git.setConfig({ fs: this.fs, dir, path, value });
  }

  /**
   * Fetch refs from a remote without touching the working tree.
   *
   * isomorphic-git has no standalone "fetch command"; git.fetch is the
   * underlying primitive. A shallow clone (depth 1) carries only the default
   * branch, so origin/<branch> can't be resolved until those refs are fetched
   * — this is what makes PR branches reachable after a shallow clone.
   */
  async fetch(dir: string, opts: FetchOptions = {}): Promise<void> {
    return this.withLock(dir, async () => {
      await git.fetch({
        fs: this.fs,
        http,
        corsProxy: gitCorsProxy(),
        dir,
        remote: opts.remote ?? 'origin',
        ref: opts.ref,
        singleBranch: opts.singleBranch ?? false,
        depth: opts.depth,
        tags: opts.tags ?? false,
        prune: opts.prune ?? false,
        onAuth: opts.token ? () => Promise.resolve(tokenAuth(opts.token)) : undefined,
      });
    });
  }

  /** Push to remote. */
  async push(dir: string, opts: PushOptions): Promise<void> {
    return this.withLock(dir, async () => {
      await git.push({
        fs: this.fs,
        http,
        corsProxy: gitCorsProxy(),
        dir,
        remote: opts.remote ?? 'origin',
        ref: opts.branch,
        force: opts.force ?? false,
        onAuth: opts.token ? () => Promise.resolve(tokenAuth(opts.token)) : undefined,
      });
    });
  }

  /** Get working tree status (changed files only). */
  async status(dir: string): Promise<GitStatusEntry[]> {
    const matrix = await git.statusMatrix({ fs: this.fs, dir });
    // statusMatrix rows are [filepath, HEAD, WORKDIR, STAGE]:
    //   HEAD:    0=absent, 1=present
    //   WORKDIR: 0=absent, 1=identical-to-HEAD, 2=different-from-HEAD
    //   STAGE:   0=absent, 1=identical-to-HEAD, 2=identical-to-WORKDIR,
    //            3=different-from-WORKDIR
    const entries: GitStatusEntry[] = [];
    for (const [filepath, HEAD, WORKDIR, STAGE] of matrix) {
      if (HEAD === 0) {
        // New file (never committed): untracked when not staged, else added.
        entries.push({ filepath, type: STAGE === 0 ? 'untracked' : 'added' });
        continue;
      }
      // File exists in HEAD.
      if (STAGE === 0 || WORKDIR === 0) {
        // Absent from index (staged deletion) or removed from the workdir.
        entries.push({ filepath, type: 'deleted' });
        continue;
      }
      // Present in workdir and index; unmodified when both equal HEAD.
      if (WORKDIR === 1 && (STAGE === 1 || STAGE === 2)) continue;
      entries.push({ filepath, type: 'modified' });
    }
    return entries;
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
    const author = opts.author ?? DEFAULT_AUTHOR;
    const oid = await git.commit({
      fs: this.fs,
      dir,
      message,
      author,
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

  /** List branches. Pass a remote to list that remote's tracking branches. */
  async listBranches(dir: string, opts: { remote?: string } = {}): Promise<string[]> {
    return git.listBranches({ fs: this.fs, dir, remote: opts.remote });
  }

  /**
   * List remote-tracking refs as "<remote>/<branch>" names.
   * Useful after a fetch to see which PR branches are now reachable.
   */
  async listRemoteBranches(dir: string, remote: string = 'origin'): Promise<string[]> {
    const branches = await git.listBranches({ fs: this.fs, dir, remote });
    return branches.map((b) => `${remote}/${b}`);
  }

  /** Get current branch. */
  async currentBranch(dir: string): Promise<string | undefined> {
    try {
      const branch = await git.currentBranch({ fs: this.fs, dir });
      return branch ?? undefined;
    } catch {
      // best-effort: unborn/invalid HEAD reports as "no branch".
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
        // best-effort: unreadable directory is skipped.
        return;
      }
      for (const name of entries) {
        if (name === '.git') continue;
        const fullPath = `${path}/${name}`;
        let stats;
        try {
          stats = await pfs.stat(fullPath);
        } catch {
          // best-effort: entries that can't be stat'd are skipped.
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
        // best-effort: parent may already exist (EEXIST)
        await this.pfs.mkdir(parentPath).catch(() => undefined);
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
      // best-effort: absent .git simply means "not a repo".
      return false;
    }
  }

  /** Delete a local repo. */
  async delete(dir: string): Promise<void> {
    return this.withLock(dir, async () => {
      const entries = await this.listAllFiles(dir);
      for (const entry of entries.reverse()) {
        if (entry.type === 'dir') {
          await this.pfs.rmdir(`${dir}${entry.path}`).catch(() => undefined);
        } else {
          await this.pfs.unlink(`${dir}${entry.path}`).catch(() => undefined);
        }
      }
      // best-effort: rmdir can fail on a non-empty dir; the workspaceFs layer
      // surfaces delete failures from the unlink loop above.
      await this.pfs.rmdir(dir).catch(() => undefined);
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
            // best-effort: blob unreadable at this commit — label as deleted.
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

  /**
   * Read a file's content from a specific commit tree.
   * Returns null if the file didn't exist at that commit.
   */
  async readFileAtCommit(dir: string, filepath: string, oid: string): Promise<string | null> {
    try {
      const blob = await git.readBlob({
        fs: this.fs,
        dir,
        oid,
        filepath,
      });
      return new TextDecoder().decode(blob.blob);
    } catch {
      // best-effort: file absent at that commit reads as null.
      return null;
    }
  }

  /**
   * Get the list of files that changed between two commits.
   * Returns the filepaths with their change type.
   */
  async getChangedFiles(
    dir: string,
    sha: string,
    parentSha?: string,
  ): Promise<Array<{ filepath: string; type: 'added' | 'deleted' | 'modified' }>> {
    const currentTree = await git.readTree({ fs: this.fs, dir, oid: sha });

    let parentTree: Awaited<ReturnType<typeof git.readTree>> | null = null;
    if (parentSha) {
      try {
        parentTree = await git.readTree({ fs: this.fs, dir, oid: parentSha });
      } catch {
        // best-effort: a root commit has no parent tree.
        parentTree = null;
      }
    }

    const changed: Array<{ filepath: string; type: 'added' | 'deleted' | 'modified' }> = [];

    if (!parentTree) {
      // First commit — all files are added
      for (const entry of currentTree.tree) {
        if (entry.path !== '.git') {
          changed.push({ filepath: entry.path, type: 'added' });
        }
      }
      return changed;
    }

    const parentMap = new Map(parentTree.tree.map((e) => [e.path, e]));
    const currentMap = new Map(currentTree.tree.map((e) => [e.path, e]));

    for (const [path, entry] of currentMap) {
      if (path === '.git') continue;
      const parentEntry = parentMap.get(path);
      if (!parentEntry) {
        changed.push({ filepath: path, type: 'added' });
      } else if (parentEntry.oid !== entry.oid) {
        changed.push({ filepath: path, type: 'modified' });
      }
    }

    for (const [path] of parentMap) {
      if (path === '.git') continue;
      if (!currentMap.has(path)) {
        changed.push({ filepath: path, type: 'deleted' });
      }
    }

    return changed;
  }

  /** Standard repo path: /repos/<owner>/<name>. */
  static repoPath(owner: string, name: string): string {
    return `/repos/${owner}/${name}`;
  }
}

export const gitClient = new GitClient('sprout-git');
