// @vitest-environment node
/**
 * The repository import against real isomorphic-git: the browser store is a
 * temp directory standing in for lightning-fs, the VFS is an in-memory map,
 * and the host's git proxy is `git http-backend` serving a bare repository
 * (so the clone speaks real smart HTTP through the proxy URL). Skipped when
 * no `git` binary is available.
 */

import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync, writeFileSync, mkdirSync } from 'node:fs';
import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const env = vi.hoisted(() => ({
  storeRoot: '',
  proxy: undefined as string | undefined,
  requests: [] as string[],
  projectRoot: '',
}));

vi.mock('@isomorphic-git/lightning-fs', async () => {
  const nodeFs = (await import('node:fs/promises')).default;
  const at = (p: string) => `${env.storeRoot}${p}`;
  const promises = {
    readFile: (p: string, opts?: unknown) => nodeFs.readFile(at(p), opts as never),
    writeFile: (p: string, data: unknown, opts?: unknown) => nodeFs.writeFile(at(p), data as never, opts as never),
    unlink: (p: string) => nodeFs.unlink(at(p)),
    readdir: (p: string) => nodeFs.readdir(at(p)),
    mkdir: (p: string) => nodeFs.mkdir(at(p)),
    rmdir: (p: string) => nodeFs.rmdir(at(p)),
    stat: (p: string) => nodeFs.stat(at(p)),
    lstat: (p: string) => nodeFs.lstat(at(p)),
    readlink: (p: string) => nodeFs.readlink(at(p)),
    symlink: (target: string, p: string) => nodeFs.symlink(target, at(p)),
    chmod: (p: string, mode: number) => nodeFs.chmod(at(p), mode),
  };
  return {
    default: class {
      promises = promises;
    },
  };
});

vi.mock('isomorphic-git/http/web', () => ({
  default: {
    async request(req: {
      url: string;
      method?: string;
      headers?: Record<string, string>;
      body?: AsyncIterable<Uint8Array>;
    }) {
      env.requests.push(req.url);
      const prefix = `${env.proxy}/github.com/`;
      if (!env.proxy || !req.url.startsWith(prefix)) throw new Error(`request bypassed the git proxy: ${req.url}`);
      const url = new URL(req.url);
      const chunks: Uint8Array[] = [];
      if (req.body) for await (const c of req.body) chunks.push(c);
      const input = Buffer.concat(chunks);
      const headers = Object.fromEntries(Object.entries(req.headers ?? {}).map(([k, v]) => [k.toLowerCase(), v]));
      const out = spawnSync('git', ['http-backend'], {
        input,
        env: {
          ...process.env,
          GIT_PROJECT_ROOT: env.projectRoot,
          GIT_HTTP_EXPORT_ALL: '1',
          PATH_INFO: url.pathname.slice(new URL(prefix).pathname.length - 1),
          QUERY_STRING: url.search.replace(/^\?/, ''),
          REQUEST_METHOD: req.method ?? 'GET',
          CONTENT_TYPE: headers['content-type'] ?? '',
          CONTENT_LENGTH: String(input.length),
          GIT_PROTOCOL: headers['git-protocol'] ?? '',
        },
      });
      const raw = out.stdout;
      const split = raw.indexOf('\r\n\r\n');
      const head = raw.subarray(0, split).toString('utf8');
      const body = raw.subarray(split + 4);
      const resHeaders: Record<string, string> = {};
      let statusCode = 200;
      for (const line of head.split('\r\n')) {
        const i = line.indexOf(':');
        const key = line.slice(0, i).trim().toLowerCase();
        const value = line.slice(i + 1).trim();
        if (key === 'status') statusCode = parseInt(value, 10);
        else resHeaders[key] = value;
      }
      async function* stream() {
        for (let i = 0; i < body.length; i += 4096) yield new Uint8Array(body.subarray(i, i + 4096));
      }
      return { url: req.url, method: req.method, statusCode, statusMessage: 'OK', headers: resHeaders, body: stream() };
    },
  },
}));

vi.mock('./gitCorsProxy', () => ({ gitCorsProxy: () => env.proxy }));

import { configureBrowserGit, executeGitOp, gitCommit, gitLog, __resetBrowserGitForTest } from './browserGit';
import { CloneTooLargeError } from './gitHttpLimit';
import { restoreRepoWithBrowserGit, type RepoRestoreResult } from './repoGitImport';
import type { WasmShell } from './wasmShell';

const hasGit = spawnSync('git', ['--version']).status === 0;
const PNG = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0xff, 0xfe]);
const REPO_FILES = { 'README.md': '# web\n', 'src/app.ts': 'export const answer = 42;\n' };

let scratch = '';
let vfs: Map<string, string>;

function wireVfs() {
  configureBrowserGit({
    name: 'Tester',
    email: 'tester@example.com',
    readVfsFiles: async () => [...vfs].map(([p, content]) => ({ path: p, content })),
    writeVfsFiles: async (files) => files.forEach((f) => vfs.set(f.path, f.content)),
    deleteVfsFiles: async (paths) => paths.forEach((p) => vfs.delete(p)),
  });
}

function importFilesFromHost(): (url: string) => Promise<RepoRestoreResult> {
  return async () => {
    for (const [p, c] of Object.entries(REPO_FILES)) vfs.set(p, c);
    return { success: true, repo: 'acme/web', fromCache: false };
  };
}

const deps = (overrides?: { maxCloneBytes?: number }) => ({
  shell: async () => ({}) as WasmShell,
  importFiles: importFilesFromHost(),
  ...overrides,
});

type Status = {
  in_git_repo: boolean;
  note?: string;
  status: {
    branch: string;
    modified: Array<{ path: string }>;
    untracked: Array<{ path: string }>;
    deleted: unknown[];
    staged: unknown[];
  };
};
const status = () => executeGitOp('status') as Promise<Status>;

describe.skipIf(!hasGit)('repository import into browser git', () => {
  beforeAll(() => {
    // The store is mocked onto a temp directory; IndexedDB only has to exist.
    vi.stubGlobal('indexedDB', {});
    scratch = mkdtempSync(path.join(os.tmpdir(), 'sprout-repo-import-'));
    const src = path.join(scratch, 'src');
    mkdirSync(path.join(src, 'src'), { recursive: true });
    for (const [p, c] of Object.entries(REPO_FILES)) writeFileSync(path.join(src, p), c);
    writeFileSync(path.join(src, 'logo.png'), PNG);
    const git = (...args: string[]) =>
      execFileSync('git', ['-c', 'user.name=T', '-c', 'user.email=t@example.com', ...args], { cwd: src });
    git('init', '-q', '-b', 'main');
    git('add', '.');
    git('commit', '-q', '-m', 'first');
    writeFileSync(path.join(src, 'README.md'), REPO_FILES['README.md']);
    git('commit', '-q', '--allow-empty', '-m', 'second');
    env.projectRoot = path.join(scratch, 'projects');
    mkdirSync(path.join(env.projectRoot, 'acme'), { recursive: true });
    execFileSync('git', ['clone', '-q', '--bare', src, path.join(env.projectRoot, 'acme', 'web.git')]);
  });

  afterAll(() => {
    vi.unstubAllGlobals();
    rmSync(scratch, { recursive: true, force: true });
  });

  beforeEach(async () => {
    env.storeRoot = path.join(scratch, `store-${Math.random().toString(36).slice(2)}`);
    await fsp.mkdir(env.storeRoot);
    env.requests = [];
    env.proxy = undefined;
    vfs = new Map();
    __resetBrowserGitForTest();
  });

  it('answers the boot guards before browser git is wired', async () => {
    expect((await status()).in_git_repo).toBe(false);
    expect(await executeGitOp('diff', undefined, { path: 'README.md' })).toMatchObject({ has_unstaged: false });
    expect(await executeGitOp('branches')).toMatchObject({ branches: [] });
  });

  it('clones through the host proxy: clean status, then modified, diff and commit', async () => {
    env.proxy = 'https://host.test/git-proxy';
    wireVfs();
    const result = await restoreRepoWithBrowserGit('https://github.com/acme/web', deps());
    expect(result).toMatchObject({ success: true, history: 'clone', repo: 'acme/web' });
    expect(env.requests.every((u) => u.startsWith('https://host.test/git-proxy/github.com/acme/web.git/'))).toBe(true);

    expect(Object.fromEntries(vfs)).toEqual(REPO_FILES);
    const clean = await status();
    expect(clean).toMatchObject({
      in_git_repo: true,
      status: { branch: 'main', modified: [], untracked: [], deleted: [] },
    });
    expect(clean.note).toBeUndefined();
    expect((await gitLog()).map((c) => c.message.trim())).toEqual(['second']);

    vfs.set('src/app.ts', 'export const answer = 43;\n');
    expect((await status()).status.modified.map((f) => f.path)).toEqual(['src/app.ts']);
    const diff = (await executeGitOp('diff', undefined, { path: 'src/app.ts' })) as { diff: string };
    expect(diff.diff).toContain('-export const answer = 42;');
    expect(diff.diff).toContain('+export const answer = 43;');

    await executeGitOp('stage', { path: 'src/app.ts' });
    await gitCommit('bump answer');
    expect((await status()).status.modified).toEqual([]);
    expect((await gitLog()).map((c) => c.message.trim())).toEqual(['bump answer', 'second']);
    expect((await executeGitOp('branches')) as { current: string }).toMatchObject({ current: 'main' });
  });

  it('keeps a binary file in git only, never reporting it deleted', async () => {
    env.proxy = 'https://host.test/git-proxy';
    wireVfs();
    await restoreRepoWithBrowserGit('acme/web', deps());
    expect(vfs.has('logo.png')).toBe(false);
    expect((await status()).status.deleted).toEqual([]);
    expect(await fsp.readFile(path.join(env.storeRoot, 'repo', 'logo.png'))).toEqual(PNG);
  });

  it('falls back to an imported-snapshot repository when the host has no proxy', async () => {
    wireVfs();
    const result = await restoreRepoWithBrowserGit('https://github.com/acme/web', deps());
    expect(result).toMatchObject({ success: true, history: 'snapshot' });
    expect(env.requests).toEqual([]);

    const clean = await status();
    expect(clean).toMatchObject({ in_git_repo: true, status: { branch: 'main', modified: [], untracked: [] } });
    expect(clean.note).toMatch(/without git history/);
    const log = await gitLog();
    expect(log).toHaveLength(1);
    expect(log[0].message).toMatch(/^Imported snapshot of acme\/web/);

    vfs.set('README.md', '# web, edited\n');
    vfs.set('NOTES.md', 'new\n');
    const changed = await status();
    expect(changed.status.modified.map((f) => f.path)).toEqual(['README.md']);
    expect(changed.status.untracked.map((f) => f.path)).toEqual(['NOTES.md']);
    await expect(executeGitOp('push')).rejects.toThrow(/imported without git history/);
  });

  it('keeps the snapshot and its local commits when the same repository is opened again', async () => {
    wireVfs();
    await restoreRepoWithBrowserGit('acme/web', deps());
    vfs.set('README.md', '# local\n');
    await executeGitOp('stage', { path: 'README.md' });
    await gitCommit('local work');

    vfs = new Map();
    __resetBrowserGitForTest();
    wireVfs();
    const again = await restoreRepoWithBrowserGit('https://github.com/acme/web', deps());
    expect(again).toMatchObject({ fromCache: true, history: 'snapshot' });
    expect(vfs.get('README.md')).toBe('# local\n');
    expect((await gitLog())[0].message.trim()).toBe('local work');
  });

  it('stops a clone past the size limit and imports a snapshot instead', async () => {
    env.proxy = 'https://host.test/git-proxy';
    wireVfs();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const result = await restoreRepoWithBrowserGit('acme/web', deps({ maxCloneBytes: 64 }));
    expect(result).toMatchObject({ success: true, history: 'snapshot' });
    expect(warn.mock.calls.some(([, err]) => err instanceof CloneTooLargeError)).toBe(true);
    warn.mockRestore();
    expect((await status()).status.modified).toEqual([]);
  });

  it('waits for an import in progress before answering status', async () => {
    env.proxy = 'https://host.test/git-proxy';
    wireVfs();
    const importing = restoreRepoWithBrowserGit('acme/web', deps());
    const early = await status();
    await importing;
    expect(early.status.deleted).toEqual([]);
    expect(early.status.untracked).toEqual([]);
    expect(early.status.branch).toBe('main');
  });
});
