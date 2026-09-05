/**
 * workspaceFs seam contract tests.
 *
 * Runs the ENTIRE contract against the memory backend, plus the
 * backend-resolution rules and the gitFs adapter's mapping (ENOENT codes,
 * readdir shapes, write coalescing) against memory too — the memory
 * backend implements the same contract the native and REST backends
 * implement, so these tests pin the semantics every backend must honor.
 */

import { describe, expect, it } from 'vitest';
import git from 'isomorphic-git';
import { createMemoryFs } from './memoryFs';
import { createGitFs } from './gitFs';
import { cloneRepo, listRepos, parseRepoRef, removeRepo, repoDir } from './workspaceGit';
import { isWorkspaceFs, normalizeFsPath, WRITE_BATCH_CAP } from './types';
import type { WorkspaceFs } from './types';

/** isomorphic-git requires raw fs objects to pass through its binding layer. */
function bindGitFs(gitFs: ReturnType<typeof createGitFs>) {
  const bind = (git as unknown as { bindFs?: (fs: unknown) => unknown }).bindFs;
  return bind ? (bind(gitFs) as ReturnType<typeof createGitFs>) : gitFs;
}

function fsWithSeed(seed: Record<string, string> = {}): WorkspaceFs {
  return createMemoryFs(seed);
}

describe('workspaceFs contract (memory backend)', () => {
  it('is a WorkspaceFs', () => {
    expect(isWorkspaceFs(createMemoryFs())).toBe(true);
  });

  it('write then read round-trips utf-8', async () => {
    const fs = fsWithSeed();
    expect(await fs.write('src/a.ts', 'hello')).toEqual({ ok: true });
    const r = await fs.read('src/a.ts');
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.content).toBe('hello');
  });

  it('read missing → notFound; read dir → isDirectory', async () => {
    const fs = fsWithSeed({ 'x.txt': 'x' });
    expect(await fs.read('nope.txt')).toEqual({ ok: false, error: 'notFound' });
    await fs.mkdir('dir');
    const r = await fs.read('dir');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toBe('isDirectory');
  });

  it('write creates parent dirs; write over dir fails', async () => {
    const fs = fsWithSeed();
    await fs.write('a/b/c.txt', 'deep');
    const stat = await fs.stat('a/b');
    expect(stat.ok).toBe(true);
    if (stat.ok) expect(stat.isDir).toBe(true);
    await fs.mkdir('d');
    expect(await fs.write('d', 'nope')).toEqual({ ok: false, error: 'isDirectory' });
  });

  it('rejects unsafe paths', async () => {
    const fs = fsWithSeed();
    expect((await fs.write('../escape.txt', 'x')).ok).toBe(false);
    // Leading/trailing slashes are stripped by convention (normalizeFsPath),
    // so '/abs' writes 'abs' — but '..' anywhere is always rejected.
    expect((await fs.write('a/../..//b.txt', 'x')).ok).toBe(false);
    expect((await fs.remove('a/../../b')).ok).toBe(false);
  });

  it('mkdir is idempotent for dirs, fails on files', async () => {
    const fs = fsWithSeed();
    expect(await fs.mkdir('p/q')).toEqual({ ok: true });
    expect(await fs.mkdir('p/q')).toEqual({ ok: true });
    await fs.write('f.txt', 'f');
    expect(await fs.mkdir('f.txt')).toEqual({ ok: false, error: 'exists' });
  });

  it('stat reports size and isDir', async () => {
    const fs = fsWithSeed();
    await fs.write('s.txt', '12345');
    const r = await fs.stat('s.txt');
    expect(r).toEqual({ ok: true, path: 's.txt', size: 5, isDir: false });
  });

  it('remove deletes files and subtrees', async () => {
    const fs = fsWithSeed();
    await fs.write('t/a/1.txt', '1');
    await fs.write('t/a/2.txt', '2');
    expect(await fs.remove('t')).toEqual({ ok: true });
    expect(await fs.stat('t')).toEqual({ ok: false, error: 'notFound' });
    expect(await fs.remove('t')).toEqual({ ok: false, error: 'notFound' });
  });

  it('rename moves subtrees and validates targets', async () => {
    const fs = fsWithSeed();
    await fs.write('old/inner/f.txt', 'f');
    expect(await fs.rename('old', 'new')).toEqual({ ok: true });
    expect((await fs.read('new/inner/f.txt')).ok).toBe(true);
    expect(await fs.stat('old')).toEqual({ ok: false, error: 'notFound' });
    expect(await fs.rename('ghost', 'x')).toEqual({ ok: false, error: 'notFound' });
    expect(await fs.rename('new', 'new/inner')).toEqual({ ok: false, error: 'exists' });
  });

  it('list returns immediate children with dirs', async () => {
    const fs = fsWithSeed();
    await fs.write('root/a.txt', 'a');
    await fs.write('root/sub/b.txt', 'b');
    const r = await fs.list('root');
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.files.map((f) => f.path)).toEqual(['root/a.txt', 'root/sub']);
      expect(r.files.find((f) => f.path === 'root/sub')?.isDir).toBe(true);
    }
  });

  it('writeBatch continues past failures and caps at 5000', async () => {
    const fs = fsWithSeed();
    await fs.mkdir('locked');
    const r = await fs.writeBatch([
      { path: 'ok1.txt', content: '1' },
      { path: 'locked', content: 'is-a-dir' },
      { path: 'ok2.txt', contentBase64: btoa('2') },
    ]);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.written).toBe(2);
      expect(r.errors).toEqual([{ path: 'locked', error: 'isDirectory' }]);
    }
    const tooBig: Array<{ path: string; content: string }> = [];
    for (let i = 0; i < WRITE_BATCH_CAP + 1; i += 1) tooBig.push({ path: `f${i}`, content: 'x' });
    const rb = await fs.writeBatch(tooBig);
    expect(rb.ok).toBe(false);
  });
});

describe('workspaceFs resolution', () => {
  it('normalizeFsPath strips slashes', () => {
    expect(normalizeFsPath('/a/b/')).toBe('a/b');
    expect(normalizeFsPath('')).toBe('');
  });
});

describe('gitFs adapter', () => {
  it('maps seam errors to ENOENT codes', async () => {
    const gfs = createGitFs(fsWithSeed());
    await expect(gfs.readFile('missing.txt')).rejects.toMatchObject({ code: 'ENOENT' });
  });

  it('readFile returns string with encoding, bytes without', async () => {
    const gfs = createGitFs(fsWithSeed({ 'a.txt': 'héllo' }));
    expect(await gfs.readFile('a.txt', 'utf8')).toBe('héllo');
    const bytes = (await gfs.readFile('a.txt')) as Uint8Array;
    expect(bytes).toBeInstanceOf(Uint8Array);
  });

  it('writeFile coalesces into writeBatch and readdir sees them', async () => {
    const fs = fsWithSeed();
    const gfs = createGitFs(fs);
    await gfs.writeFile('o/x', new Uint8Array([1]));
    await gfs.writeFile('o/y', new Uint8Array([2]));
    expect((await fs.stat('o/x')).ok).toBe(false); // still buffered
    await gfs.mkdir('o'); // flushes
    const names = await gfs.readdir('o');
    expect(names.sort()).toEqual(['x', 'y']);
  });

  it('stat distinguishes files and dirs', async () => {
    const fs = fsWithSeed();
    await fs.write('d/f.txt', 'x');
    const gfs = createGitFs(fs);
    expect((await gfs.stat('d/f.txt')).isFile()).toBe(true);
    expect((await gfs.stat('d')).isDirectory()).toBe(true);
    await expect(gfs.stat('d/ghost')).rejects.toMatchObject({ code: 'ENOENT' });
  });
});

describe('workspaceGit', () => {
  it('parses owner/name from urls and shorthand', () => {
    expect(parseRepoRef('https://github.com/acme/api.git')).toEqual({
      owner: 'acme',
      name: 'api',
      url: 'https://github.com/acme/api.git',
    });
    expect(parseRepoRef('acme/api')).toEqual({
      owner: 'acme',
      name: 'api',
      url: 'https://github.com/acme/api.git',
    });
    expect(() => parseRepoRef('not a repo')).toThrow();
  });

  it('repoDir builds the repos/owner/name layout', () => {
    expect(repoDir('acme/api')).toBe('repos/acme/api');
  });

  it('cloneRepo rejects an existing checkout', async () => {
    const fs = fsWithSeed();
    await fs.mkdir('repos/acme/api');
    await expect(cloneRepo('acme/api', { fs })).rejects.toThrow(/already exists/);
  });

  it('listRepos and removeRepo round-trip through the seam', async () => {
    const fs = fsWithSeed();
    await fs.write('repos/acme/api/.git/HEAD', 'ref: refs/heads/main');
    await fs.write('repos/acme/api/README.md', 'hi');
    await fs.write('repos/solo/README.md', 'not a repo');
    expect(await listRepos(fs)).toEqual(['acme/api']);
    expect(await removeRepo('acme/api', fs)).toBe(true);
    expect(await listRepos(fs)).toEqual([]);
    expect(await removeRepo('acme/api', fs)).toBe(false);
  });

  it('isomorphic-git init works over the adapter (offline smoke)', async () => {
    const fs = fsWithSeed();
    const gfs = bindGitFs(createGitFs(fs));
    await fs.mkdir('wt');
    await git.init({ fs: gfs as never, dir: 'wt' });
    await fs.write('wt/hello.txt', 'world');
    await git.add({ fs: gfs as never, dir: 'wt', filepath: 'hello.txt' });
    const sha = await git.commit({
      fs: gfs as never,
      dir: 'wt',
      message: 'init',
      author: { name: 't', email: 't@t' },
    });
    expect(sha).toMatch(/^[0-9a-f]{40}$/);
    const log = await git.log({ fs: gfs as never, dir: 'wt', depth: 1 });
    expect(log[0].commit.message).toBe('init\n');
  });
});
