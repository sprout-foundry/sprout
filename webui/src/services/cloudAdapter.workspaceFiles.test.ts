/**
 * One filesystem view for the in-browser workspace: a repo imported the way
 * the startup import does it (CloudAdapter.restoreRepo) is what the file API
 * lists and reads, at every spelling of the workspace root, and the editor,
 * the file tree and the agent's shell all see each other's writes.
 *
 * The shell is an in-memory model of the WASM bridge: paths from the host
 * resolve the way cmd/wasm's workspacePath resolves them (absolute paths
 * stand, anything else is relative to the workspace root), and the few shell
 * commands the file routes run (mkdir -p, rm -rf, mv) plus the agent-side
 * ones these tests use (cat, echo >) operate on the same store.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CloudAdapter } from './cloudAdapter';
import { getVfsManifestSnapshot, untrackFileWrite } from './vfsFiles';
import type { WasmDirEntry, WasmShell } from './wasmShell';
import { createRestFs } from './workspaceFs/restFs';

const ROOT = '/workspace';

function collapse(p: string): string {
  const out: string[] = [];
  for (const part of p.split('/')) {
    if (part === '' || part === '.') continue;
    if (part === '..') out.pop();
    else out.push(part);
  }
  return `/${out.join('/')}`;
}

/** cmd/wasm workspacePath: absolute paths stand, the rest is relative to the root. */
function bridgePath(p: string): string {
  return p.startsWith('/') ? collapse(p) : collapse(`${ROOT}/${p}`);
}

function shellWords(input: string): string[] {
  const words: string[] = [];
  const re = /'((?:[^']|'\\'')*)'|(\S+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(input))) words.push(m[1] !== undefined ? m[1].replace(/'\\''/g, "'") : m[2]);
  return words;
}

function createMemoryShell() {
  const files = new Map<string, string>();
  const dirs = new Set<string>(['/', ROOT]);
  const addParents = (p: string) => {
    for (let d = p.slice(0, p.lastIndexOf('/')) || '/'; d !== '/'; d = d.slice(0, d.lastIndexOf('/')) || '/') {
      dirs.add(d);
    }
  };
  const isDir = (p: string) => dirs.has(p);
  const write = (abs: string, content: string) => {
    addParents(abs);
    files.set(abs, content);
  };
  const removeTree = (abs: string) => {
    for (const k of [...files.keys()]) if (k === abs || k.startsWith(`${abs}/`)) files.delete(k);
    for (const d of [...dirs]) if (d === abs || d.startsWith(`${abs}/`)) dirs.delete(d);
  };
  const ok = (stdout = '') => ({ stdout, stderr: '', exitCode: 0 });
  const fail = (stderr: string) => ({ stdout: '', stderr, exitCode: 1 });

  const shell = {
    getCwd: () => ROOT,
    getWorkspaceRoot: () => ROOT,
    writeFile(path: string, content: string) {
      write(bridgePath(path), content);
      return '';
    },
    readFile(path: string) {
      const content = files.get(bridgePath(path));
      return content === undefined ? { content: '', error: `open ${bridgePath(path)}: no such file` } : { content };
    },
    readFileBytes: () => ({ error: 'not supported' }),
    listDir(path: string) {
      const abs = bridgePath(path);
      if (!isDir(abs)) return { entries: [], error: `open ${abs}: no such directory` };
      const prefix = abs === '/' ? '/' : `${abs}/`;
      const entries = new Map<string, WasmDirEntry>();
      for (const d of dirs) {
        const rest = d.startsWith(prefix) ? d.slice(prefix.length) : '';
        if (rest && !rest.includes('/')) entries.set(rest, { name: rest, type: 'dir', size: 0 } as WasmDirEntry);
      }
      for (const [k, v] of files) {
        const rest = k.startsWith(prefix) ? k.slice(prefix.length) : '';
        if (rest && !rest.includes('/'))
          entries.set(rest, { name: rest, type: 'file', size: v.length } as WasmDirEntry);
      }
      return { entries: [...entries.values()].sort((a, b) => a.name.localeCompare(b.name)) };
    },
    deleteFile(path: string) {
      const abs = bridgePath(path);
      if (isDir(abs)) return `remove ${abs}: is a directory`;
      files.delete(abs);
      return '';
    },
    executeCommand(input: string) {
      const echo = /^echo (.*) > (\S+)$/.exec(input);
      if (echo) {
        write(bridgePath(echo[2]), `${echo[1]}\n`);
        return ok();
      }
      const [cmd, ...args] = shellWords(input);
      const operands = args.filter((a) => !a.startsWith('-'));
      switch (cmd) {
        case 'cat': {
          const content = files.get(bridgePath(operands[0]));
          return content === undefined ? fail(`cat: ${operands[0]}: No such file or directory`) : ok(content);
        }
        case 'mkdir': {
          const abs = bridgePath(operands[0]);
          addParents(`${abs}/x`);
          return ok();
        }
        case 'rm':
          removeTree(bridgePath(operands[0]));
          return ok();
        case 'mv': {
          const [from, to] = operands.map(bridgePath);
          if (!files.has(from) && !isDir(from)) return fail(`mv: cannot stat '${from}': No such file or directory`);
          for (const [k, v] of [...files]) {
            if (k === from || k.startsWith(`${from}/`)) {
              files.delete(k);
              write(to + k.slice(from.length), v);
            }
          }
          if (isDir(from)) {
            removeTree(from);
            dirs.add(to);
          }
          return ok();
        }
        default:
          return fail(`${cmd}: command not found`);
      }
    },
  };
  return { shell: shell as unknown as WasmShell, files };
}

const mocks = vi.hoisted(() => ({
  shell: null as unknown,
  proxy: undefined as string | undefined,
  cloneFiles: [] as Array<{ path: string; content: string }>,
}));

vi.mock('./wasmShell', () => ({
  initWasmShell: vi.fn(() => Promise.resolve(mocks.shell)),
  resetWasmShell: vi.fn(),
}));
vi.mock('./repoImportCache', () => ({
  loadRepoImport: vi.fn(() => Promise.resolve(null)),
  saveRepoImport: vi.fn(() => Promise.resolve()),
  setLastRepo: vi.fn(() => Promise.resolve()),
  getLastRepo: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('./gitCorsProxy', () => ({ gitCorsProxy: () => mocks.proxy }));
// The hosted IDE's clone: isomorphic-git checks the repo out into its own
// store, then the app's VFS bridge (useAppInitialization) writes each file
// through the shell by its repo-relative path.
vi.mock('./browserGit', () => ({
  whenBrowserGitConfigured: () => Promise.resolve(),
  gitOriginUrl: () => Promise.resolve(null),
  restoreGitWorkingTree: () => Promise.resolve(0),
  gitClone: vi.fn(async () => {
    const shell = mocks.shell as WasmShell;
    for (const f of mocks.cloneFiles) shell.writeFile(f.path, f.content);
    return { message: 'ok', files: mocks.cloneFiles.length };
  }),
}));

const REPO = 'https://github.com/octocat/hello';
const REPO_FILES = [
  { path: 'README.md', content: '# hello\n' },
  { path: 'src/main.ts', content: 'export const answer = 42;\n' },
  { path: 'src/lib/util.ts', content: 'export const noop = () => {};\n' },
];

/** Every way a caller names the workspace root. */
const ROOT_SPELLINGS = ['', '.', './', '/workspace', '/workspace/', 'workspace', 'workspace/'];

let memory: ReturnType<typeof createMemoryShell>;
let adapter: CloudAdapter;
let fetchSpy: ReturnType<typeof vi.fn>;

async function listing(path: string | null): Promise<Array<{ name: string; path: string; is_dir: boolean }>> {
  const url = path === null ? '/api/files' : `/api/files?path=${encodeURIComponent(path)}`;
  const res = await adapter.fetch(url);
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.message).toBe('success');
  return body.files;
}

const names = (entries: Array<{ name: string }>) => entries.map((e) => e.name).sort();

async function readText(path: string): Promise<{ status: number; text: string }> {
  const res = await adapter.fetch(`/api/file?path=${encodeURIComponent(path)}`);
  return { status: res.status, text: await res.text() };
}

function post(url: string, body: unknown) {
  return adapter.fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

beforeEach(() => {
  memory = createMemoryShell();
  mocks.shell = memory.shell;
  mocks.proxy = undefined;
  mocks.cloneFiles = REPO_FILES;
  for (const p of getVfsManifestSnapshot()) untrackFileWrite(p);
  fetchSpy = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith('/api/repo/import')) {
      return new Response(JSON.stringify({ repo: 'octocat/hello', files: REPO_FILES }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    // The platform's pre-WASM safety net answers the file routes with an
    // empty listing; reaching it here means a request left the workspace.
    return new Response(JSON.stringify({ message: 'success', files: [] }), { status: 200 });
  });
  vi.stubGlobal('fetch', fetchSpy);
  adapter = new CloudAdapter({ apiBase: 'https://platform.test', wsUrl: 'wss://platform.test/ws', navItems: [] });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe.each([
  ['the file import (/api/repo/import)', undefined],
  ['the browser git clone', 'https://platform.test/git-proxy'],
])('after a repo import through %s', (_label, proxy) => {
  beforeEach(async () => {
    mocks.proxy = proxy;
    const result = await adapter.restoreRepo(REPO);
    expect(result).toMatchObject({ success: true });
  });

  it('reports the workspace root the file routes serve', async () => {
    const res = await adapter.fetch('/api/workspace');
    expect(await res.json()).toMatchObject({ workspace_root: ROOT, daemon_root: ROOT });
  });

  it.each([null, ...ROOT_SPELLINGS])('lists the imported files at the root spelled %j', async (spelling) => {
    const files = await listing(spelling);
    expect(names(files)).toEqual(['README.md', 'src']);
    expect(files.find((f) => f.name === 'src')).toMatchObject({ path: '/workspace/src', is_dir: true });
  });

  it('lists a subdirectory by relative and absolute path alike', async () => {
    expect(names(await listing('src'))).toEqual(['lib', 'main.ts']);
    expect(names(await listing('/workspace/src'))).toEqual(['lib', 'main.ts']);
    expect(names(await listing('./src/'))).toEqual(['lib', 'main.ts']);
  });

  it('reads an imported file by relative and absolute path', async () => {
    for (const path of ['src/main.ts', './src/main.ts', '/workspace/src/main.ts', 'workspace/src/main.ts']) {
      expect(await readText(path)).toEqual({ status: 200, text: 'export const answer = 42;\n' });
    }
  });

  it("makes an editor save visible to the agent's shell", async () => {
    const res = await post(`/api/file?path=${encodeURIComponent('src/main.ts')}`, {
      content: 'export const answer = 7;\n',
    });
    expect(res.status).toBe(200);
    expect(memory.shell.executeCommand('cat src/main.ts').stdout).toBe('export const answer = 7;\n');

    await post(`/api/file?path=${encodeURIComponent('docs/guide.md')}`, { content: 'guide\n' });
    expect(memory.shell.readFile('docs/guide.md').content).toBe('guide\n');
    expect(names(await listing('.'))).toEqual(['README.md', 'docs', 'src']);
  });

  it("lists files the agent's shell writes", async () => {
    memory.shell.executeCommand('echo notes > NOTES.md');
    memory.shell.writeFile('src/agent.ts', 'export {};\n');
    expect(names(await listing(''))).toEqual(['NOTES.md', 'README.md', 'src']);
    expect(names(await listing('src'))).toEqual(['agent.ts', 'lib', 'main.ts']);
    expect(await readText('NOTES.md')).toEqual({ status: 200, text: 'notes\n' });
  });

  it("keeps the file tree's create, rename and delete in the workspace", async () => {
    // The tree names a root-level child by its bare name (parent '.').
    expect((await post('/api/create', { path: 'todo.txt' })).status).toBe(200);
    expect((await post('/api/create', { path: 'assets', directory: true })).status).toBe(200);
    expect(names(await listing('.'))).toEqual(['README.md', 'assets', 'src', 'todo.txt']);
    expect(memory.shell.readFile('todo.txt').error).toBeUndefined();

    expect((await post('/api/rename', { old_path: 'todo.txt', new_path: 'assets/todo.txt' })).status).toBe(200);
    expect(names(await listing('assets'))).toEqual(['todo.txt']);
    expect(memory.shell.executeCommand('cat assets/todo.txt').exitCode).toBe(0);

    expect((await post('/api/delete', { path: 'README.md' })).status).toBe(200);
    expect(names(await listing('.'))).toEqual(['assets', 'src']);
    expect(memory.shell.readFile('README.md').error).toBeTruthy();
  });

  it('serves the workspaceFs seam (workspace-relative paths) from the same files', async () => {
    const fs = createRestFs(adapter.fetch.bind(adapter));
    expect(await fs.read('src/main.ts')).toMatchObject({ ok: true, content: 'export const answer = 42;\n' });
    expect(await fs.write('repos/octocat/other/index.ts', 'x\n')).toEqual({ ok: true });
    expect(memory.shell.readFile('repos/octocat/other/index.ts').content).toBe('x\n');
    const listed = await fs.list('repos/octocat', 2);
    expect(listed).toMatchObject({ ok: true });
    expect(listed.ok && listed.files.map((f) => f.path)).toContain('repos/octocat/other/index.ts');
    expect(await fs.rename('src/main.ts', 'src/entry.ts')).toEqual({ ok: true });
    expect(memory.shell.readFile('src/entry.ts').content).toBe('export const answer = 42;\n');
  });

  it('never leaves the workspace for the platform safety net', async () => {
    for (const spelling of ROOT_SPELLINGS) await listing(spelling);
    await readText('README.md');
    await post('/api/create', { path: 'x.txt' });
    const urls = fetchSpy.mock.calls.map(([input]) => String(input));
    expect(urls.filter((u) => !u.endsWith('/api/repo/import'))).toEqual([]);
  });
});

describe('a workspace that holds its own "workspace" directory', () => {
  it('reads the bare name as that directory, not the root', async () => {
    mocks.cloneFiles = [...REPO_FILES, { path: 'workspace/config.json', content: '{}' }];
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ repo: 'octocat/hello', files: mocks.cloneFiles }))),
    );
    expect((await adapter.restoreRepo(REPO)).success).toBe(true);
    expect(names(await listing('workspace'))).toEqual(['config.json']);
    expect(names(await listing('/workspace'))).toEqual(['README.md', 'src', 'workspace']);
  });
});
