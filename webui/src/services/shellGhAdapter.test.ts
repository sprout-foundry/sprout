/**
 * shellGhAdapter.test.ts — per-subcommand tests for the browser-side `gh`
 * backing of the WASM shell's gh command.
 *
 * browserGit and fetch are mocked: these tests pin the command shapes
 * (pr list/view/create/checkout, repo clone) and the escalation contract
 * (unknown subcommand → 127, unauthenticated → exit 1).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./browserGit', () => ({
  gitClone: vi.fn(),
  gitCheckout: vi.fn(),
  gitFetch: vi.fn(),
  gitOriginUrl: vi.fn(),
}));

import { gitCheckout, gitClone, gitFetch, gitOriginUrl } from './browserGit';
import { SHELL_GH_SUBCOMMANDS, registerShellGhGlobal } from './shellGhAdapter';

const mockClone = vi.mocked(gitClone);
const mockCheckout = vi.mocked(gitCheckout);
const mockOriginUrl = vi.mocked(gitOriginUrl);
void gitFetch; // referenced via vi.mocked(gitFetch) in the checkout test

// A localStorage stub holding a github_pat.
const store = new Map<string, string>();
Object.defineProperty(globalThis, 'localStorage', {
  value: {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => store.set(k, v),
    removeItem: (k: string) => store.delete(k),
    clear: () => store.clear(),
  },
  writable: true,
  configurable: true,
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  store.clear();
  mockOriginUrl.mockResolvedValue('https://github.com/octocat/Hello-World.git');
  mockClone.mockResolvedValue({ message: 'ok', url: '', branch: 'main', files: 3 } as never);
});

describe('gh repo clone', () => {
  it('clones owner/name shorthand', async () => {
    const r = await SHELL_GH_SUBCOMMANDS['repo clone'](['octocat/Hello-World']);
    expect(r.exitCode).toBe(0);
    expect(mockClone).toHaveBeenCalledWith('https://github.com/octocat/Hello-World.git');
  });

  it('clones a full URL unchanged', async () => {
    const r = await SHELL_GH_SUBCOMMANDS['repo clone'](['https://github.com/o/r.git']);
    expect(r.exitCode).toBe(0);
    expect(mockClone).toHaveBeenCalledWith('https://github.com/o/r.git');
  });

  it('fails without a target', async () => {
    const r = await SHELL_GH_SUBCOMMANDS['repo clone']([]);
    expect(r.exitCode).not.toBe(0);
  });
});

describe('gh auth status', () => {
  it('reports not logged in without a token', async () => {
    const r = await SHELL_GH_SUBCOMMANDS['auth status']([]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('not logged in');
  });

  it('reports the account when a token is present', async () => {
    store.set('github_pat', 'ghp_x');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ login: 'octocat' })));
    const r = await SHELL_GH_SUBCOMMANDS['auth status']([]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('octocat');
  });
});

describe('gh pr list', () => {
  it('requires a token', async () => {
    const r = await SHELL_GH_SUBCOMMANDS['pr list']([]);
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('not authenticated');
  });

  it('lists open pull requests', async () => {
    store.set('github_pat', 'ghp_x');
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse([
            { number: 12, title: 'Fix thing', user: { login: 'octocat' }, head: { ref: 'fix' }, base: { ref: 'main' } },
          ]),
        ),
    );
    const r = await SHELL_GH_SUBCOMMANDS['pr list']([]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('#12');
    expect(r.stdout).toContain('Fix thing');
  });

  it('reports none when the list is empty', async () => {
    store.set('github_pat', 'ghp_x');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([])));
    const r = await SHELL_GH_SUBCOMMANDS['pr list']([]);
    expect(r.stdout).toContain('No open pull requests');
  });
});

describe('gh pr checkout', () => {
  it('fetches and checks out the PR head branch', async () => {
    store.set('github_pat', 'ghp_x');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ head: { ref: 'feature-1' } })));
    const r = await SHELL_GH_SUBCOMMANDS['pr checkout'](['7']);
    expect(r.exitCode).toBe(0);
    expect(vi.mocked(gitFetch)).toHaveBeenCalledWith({ ref: 'refs/heads/feature-1' });
    expect(mockCheckout).toHaveBeenCalledWith('feature-1');
    expect(r.stdout).toContain('feature-1');
  });

  it('requires a number', async () => {
    store.set('github_pat', 'ghp_x');
    const r = await SHELL_GH_SUBCOMMANDS['pr checkout']([]);
    expect(r.exitCode).not.toBe(0);
  });
});

describe('gh pr create', () => {
  it('creates a pull request and prints its url', async () => {
    store.set('github_pat', 'ghp_x');
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ number: 3, html_url: 'https://github.com/o/r/pull/3' }));
    vi.stubGlobal('fetch', fetchMock);
    const r = await SHELL_GH_SUBCOMMANDS['pr create'](['--title', 'My PR', '--body', 'Body']);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('https://github.com/o/r/pull/3');
    const call = fetchMock.mock.calls[0];
    expect(String(call[0])).toContain('/repos/octocat/Hello-World/pulls');
  });

  it('requires --title', async () => {
    store.set('github_pat', 'ghp_x');
    const r = await SHELL_GH_SUBCOMMANDS['pr create'](['--body', 'Body']);
    expect(r.exitCode).not.toBe(0);
  });
});

describe('global registration', () => {
  it('installs __sproutShellGh and routes subcommands', async () => {
    registerShellGhGlobal();
    const g = globalThis.__sproutShellGh;
    expect(g).toBeTruthy();
    mockClone.mockResolvedValue({ message: 'ok', url: '', branch: 'main', files: 1 } as never);
    const r = await g!.execute('repo clone', ['o/r']);
    expect(r.exitCode).toBe(0);
  });

  it('unknown subcommands stay 127 (escalate to container)', async () => {
    registerShellGhGlobal();
    const r = await globalThis.__sproutShellGh!.execute('release create', []);
    expect(r.exitCode).toBe(127);
  });

  it('executor errors surface as exit 1 with stderr', async () => {
    registerShellGhGlobal();
    store.set('github_pat', 'ghp_x');
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    const r = await globalThis.__sproutShellGh!.execute('pr checkout', ['1']);
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('api.github.com');
  });
});
