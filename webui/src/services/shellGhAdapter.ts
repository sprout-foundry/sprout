/**
 * shellGhAdapter.ts — backs the WASM shell's `gh` command with browser-side
 * isomorphic-git (clone/checkout/fetch) plus the GitHub REST API.
 *
 * The WASM shell (pkg/wasmshell/commands_gh.go) answers a focused `gh`
 * subset in-browser instead of exiting 127 — which is the trigger for the
 * transactional container escalation (ETH-2). The commands a user reaches for
 * most when working a repo (clone it, list/check out/create pull requests) run
 * free in the browser; anything else stays a 127 and escalates.
 *
 * Contract with the Go side (cmd/wasm/shell_executor.go):
 *   globalThis.__sproutShellGh.execute(subcommand, args)
 *     → Promise<{ stdout: string; stderr: string; exitCode: number }>
 *
 * Auth: a GitHub PAT from localStorage (`github_pat`), the same credential the
 * git panel uses; the platform-account path routes through the egress proxy.
 *
 * Local-first: PR-branch checkout fetches the branch and checks it out with
 * isomorphic-git (no `gh` binary, no processes). REST calls go to
 * api.github.com directly, which works when an egress proxy is configured (the
 * browser's network allowlist otherwise blocks it).
 */

import { gitCheckout, gitClone, gitFetch, gitOriginUrl } from './browserGit';
import { GITHUB_API_BASE, GITHUB_API_VERSION, GITHUB_TOKEN_KEY } from './githubService';

/** Shape the Go bridge expects from execute(). */
export interface ShellGhResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

const ok = (stdout: string): ShellGhResult => ({ stdout, stderr: '', exitCode: 0 });
const fail = (stderr: string, exitCode = 1): ShellGhResult => ({ stdout: '', stderr, exitCode });

function token(): string | null {
  if (typeof localStorage === 'undefined') return null;
  return localStorage.getItem(GITHUB_TOKEN_KEY) ?? null;
}

function authHeaders(t: string): Record<string, string> {
  return {
    Authorization: `Bearer ${t}`,
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': GITHUB_API_VERSION,
  };
}

/** owner/name parsed from the current repo's origin URL, or null. */
async function currentRepo(): Promise<{ owner: string; name: string } | null> {
  const url = await gitOriginUrl();
  if (!url) return null;
  const m = url.replace(/\.git$/, '').match(/\/([^/]+)\/([^/]+)$/);
  if (!m) return null;
  return { owner: m[1], name: m[2] };
}

function parseFlags(args: string[]): { positionals: string[]; flags: Map<string, string | boolean> } {
  const positionals: string[] = [];
  const flags = new Map<string, string | boolean>();
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (a.startsWith('--')) {
      const eq = a.indexOf('=');
      if (eq >= 0) {
        flags.set(a.slice(2, eq), a.slice(eq + 1));
      } else if (i + 1 < args.length && !args[i + 1].startsWith('-')) {
        flags.set(a.slice(2), args[i + 1]);
        i++;
      } else {
        flags.set(a.slice(2), true);
      }
    } else if (a.startsWith('-') && a.length > 1) {
      flags.set(a.slice(1), true);
    } else {
      positionals.push(a);
    }
  }
  return { positionals, flags };
}

async function githubFetch(path: string, t: string, init?: RequestInit): Promise<Response> {
  let res: Response;
  try {
    res = await fetch(`${GITHUB_API_BASE}${path}`, {
      ...init,
      headers: { ...authHeaders(t), ...(init?.headers ?? {}) },
    });
  } catch {
    throw new Error(
      'could not reach api.github.com — in the browser this needs a GitHub connection or an egress proxy',
    );
  }
  if (!res.ok) {
    let detail = '';
    try {
      const body = (await res.json()) as { message?: string };
      detail = typeof body?.message === 'string' ? body.message : '';
    } catch {
      // best-effort: non-JSON body — fall back to the status.
    }
    throw new Error(detail ? `GitHub API ${res.status}: ${detail}` : `GitHub API request failed (${res.status})`);
  }
  return res;
}

// ── Subcommand handlers ────────────────────────────────────────────────────

/** gh repo clone <owner/name|url> — clone into the workspace. */
async function repoClone(args: string[]): Promise<ShellGhResult> {
  const { positionals } = parseFlags(args);
  const target = positionals[0];
  if (!target) return fail('gh repo clone: expected <owner/name> or <url>\n', 1);
  const url = /^https?:\/\//.test(target) ? target : `https://github.com/${target}.git`;
  const res = (await gitClone(url)) as { files?: number };
  return ok(`Cloning into '${target}'...\ndone.${res.files != null ? ` (${res.files} files)` : ''}\n`);
}

/** gh repo view [owner/name] — show repository metadata. */
async function repoView(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const { positionals } = parseFlags(args);
  let repo = positionals[0];
  if (!repo) {
    const cur = await currentRepo();
    if (!cur) return fail('gh repo view: not in a repository with a GitHub origin\n', 1);
    repo = `${cur.owner}/${cur.name}`;
  }
  const res = await githubFetch(`/repos/${repo}`, t);
  const data = (await res.json()) as {
    full_name: string;
    description?: string | null;
    default_branch?: string;
    html_url?: string;
    private?: boolean;
  };
  const lines = [
    `${data.full_name}${data.private ? ' (private)' : ''}`,
    data.description ? `  ${data.description}` : '',
    data.default_branch ? `  default branch: ${data.default_branch}` : '',
    data.html_url ? `  ${data.html_url}` : '',
  ].filter(Boolean);
  return ok(lines.join('\n') + '\n');
}

/** gh pr list [--state open|closed|all] — list pull requests. */
async function prList(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const cur = await currentRepo();
  if (!cur) return fail('gh pr list: not in a repository with a GitHub origin\n', 1);
  const { flags } = parseFlags(args);
  const state = (flags.get('state') as string) || 'open';
  const res = await githubFetch(`/repos/${cur.owner}/${cur.name}/pulls?state=${state}&per_page=30`, t);
  const prs = (await res.json()) as Array<{
    number: number;
    title: string;
    user?: { login?: string };
    head?: { ref?: string };
    base?: { ref?: string };
  }>;
  if (prs.length === 0) return ok(`No ${state} pull requests.\n`);
  const lines = prs.map(
    (p) =>
      `#${String(p.number).padEnd(5)} ${p.title} (${p.head?.ref ?? '?'} → ${p.base?.ref ?? '?'}) @${p.user?.login ?? '?'}`,
  );
  return ok(lines.join('\n') + '\n');
}

/** gh pr view <number> — show a pull request. */
async function prView(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const cur = await currentRepo();
  if (!cur) return fail('gh pr view: not in a repository with a GitHub origin\n', 1);
  const { positionals } = parseFlags(args);
  const number = positionals[0];
  if (!number) return fail('gh pr view: expected <number>\n', 1);
  const res = await githubFetch(`/repos/${cur.owner}/${cur.name}/pulls/${number}`, t);
  const pr = (await res.json()) as {
    number: number;
    title: string;
    state: string;
    body?: string | null;
    user?: { login?: string };
    head?: { ref?: string };
    base?: { ref?: string };
    html_url?: string;
  };
  const lines = [
    `${pr.title} #${pr.number}`,
    `${pr.state} — ${pr.head?.ref ?? '?'} → ${pr.base?.ref ?? '?'} by @${pr.user?.login ?? '?'}`,
    pr.html_url ? pr.html_url : '',
    '',
    pr.body ?? '',
  ].filter((l) => l !== undefined);
  return ok(lines.join('\n') + '\n');
}

/** gh pr diff <number> — fetch the patch for a pull request. */
async function prDiff(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const cur = await currentRepo();
  if (!cur) return fail('gh pr diff: not in a repository with a GitHub origin\n', 1);
  const { positionals } = parseFlags(args);
  const number = positionals[0];
  if (!number) return fail('gh pr diff: expected <number>\n', 1);
  const res = await fetch(`${GITHUB_API_BASE}/repos/${cur.owner}/${cur.name}/pulls/${number}`, {
    headers: { ...authHeaders(t), Accept: 'application/vnd.github.v3.diff' },
  });
  if (!res.ok) return fail(`gh pr diff: GitHub API request failed (${res.status})\n`, 1);
  const diff = await res.text();
  return ok(diff.endsWith('\n') ? diff : diff + '\n');
}

/** gh pr checkout <number> — fetch and check out a PR's head branch. */
async function prCheckout(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const cur = await currentRepo();
  if (!cur) return fail('gh pr checkout: not in a repository with a GitHub origin\n', 1);
  const { positionals } = parseFlags(args);
  const number = positionals[0];
  if (!number) return fail('gh pr checkout: expected <number>\n', 1);
  const res = await githubFetch(`/repos/${cur.owner}/${cur.name}/pulls/${number}`, t);
  const pr = (await res.json()) as { head?: { ref?: string } };
  const branch = pr.head?.ref;
  if (!branch) return fail(`gh pr checkout: could not determine the head branch for PR #${number}\n`, 1);
  // Fetch the branch (a shallow clone won't have it), then check it out.
  await gitFetch({ ref: `refs/heads/${branch}` });
  await gitCheckout(branch);
  return ok(`Switched to branch '${branch}' (PR #${number})\n`);
}

/** gh pr create --title T [--body B] — create a PR from the current branch. */
async function prCreate(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const cur = await currentRepo();
  if (!cur) return fail('gh pr create: not in a repository with a GitHub origin\n', 1);
  const { flags } = parseFlags(args);
  const title = (flags.get('title') as string) || '';
  if (!title) return fail('gh pr create: --title is required\n', 1);
  const body = (flags.get('body') as string) || '';
  const head = (flags.get('head') as string) || undefined;
  const base = (flags.get('base') as string) || undefined;
  const res = await githubFetch(`/repos/${cur.owner}/${cur.name}/pulls`, t, {
    method: 'POST',
    body: JSON.stringify({ title, body, head, base }),
  });
  const pr = (await res.json()) as { number: number; html_url?: string };
  return ok(`${pr.html_url ?? `PR #${pr.number}`}\n`);
}

/** gh pr status — pull requests for the current branch. */
async function prStatus(args: string[]): Promise<ShellGhResult> {
  const t = token();
  if (!t) return fail('gh: not authenticated — connect GitHub first (Settings › Git providers)\n', 1);
  const cur = await currentRepo();
  if (!cur) return fail('gh pr status: not in a repository with a GitHub origin\n', 1);
  const { flags } = parseFlags(args);
  const branch = flags.get('branch') as string | undefined;
  const res = await githubFetch(`/repos/${cur.owner}/${cur.name}/pulls?state=open&per_page=30`, t);
  const prs = (await res.json()) as Array<{ number: number; title: string; head?: { ref?: string } }>;
  const matching = branch ? prs.filter((p) => p.head?.ref === branch) : prs;
  if (matching.length === 0) return ok('No open pull requests for this branch.\n');
  return ok(matching.map((p) => `#${p.number} ${p.title}`).join('\n') + '\n');
}

/** gh auth status — report the GitHub connection state. */
async function authStatus(): Promise<ShellGhResult> {
  const t = token();
  if (!t) {
    return ok('You are not logged in to any GitHub hosts.\n  (Connect GitHub in Settings › Git providers.)\n');
  }
  try {
    const res = await githubFetch('/user', t);
    const user = (await res.json()) as { login?: string };
    return ok(`✓ Logged in to github.com account ${user.login ?? 'unknown'}\n`);
  } catch (err) {
    return fail(`gh: authentication check failed — ${err instanceof Error ? err.message : String(err)}\n`, 1);
  }
}

// ── Registry & global installation ──────────────────────────────────────────

export const SHELL_GH_SUBCOMMANDS: Record<string, (args: string[]) => Promise<ShellGhResult>> = {
  'repo clone': repoClone,
  'repo view': repoView,
  'pr list': prList,
  'pr view': prView,
  'pr checkout': prCheckout,
  'pr create': prCreate,
  'pr diff': prDiff,
  'pr status': prStatus,
  'auth status': authStatus,
};

export interface SproutShellGhGlobal {
  execute(subcommand: string, args: string[]): Promise<ShellGhResult>;
  readonly names: ReadonlySet<string>;
}

declare global {
  interface Window {
    __sproutShellGh?: SproutShellGhGlobal;
  }
  /* eslint-disable no-var -- `var` is required in `declare global` blocks. */
  var __sproutShellGh: SproutShellGhGlobal | undefined;
  /* eslint-enable no-var */
}

/**
 * Install globalThis.__sproutShellGh — the bridge target for the WASM shell's
 * gh command. Idempotent.
 */
export function registerShellGhGlobal(): void {
  const impl: SproutShellGhGlobal = {
    async execute(subcommand: string, args: string[]): Promise<ShellGhResult> {
      const fn = SHELL_GH_SUBCOMMANDS[subcommand];
      if (!fn) {
        return {
          stdout: '',
          stderr: `gh: '${subcommand}' is not available in this shell (run it in a cloud container)\n`,
          exitCode: 127,
        };
      }
      try {
        return await fn(args ?? []);
      } catch (err) {
        return fail(`gh ${subcommand}: ${err instanceof Error ? err.message : String(err)}\n`, 1);
      }
    },
    names: new Set(Object.keys(SHELL_GH_SUBCOMMANDS)),
  };

  globalThis.__sproutShellGh = impl;
  if (typeof window !== 'undefined') {
    window.__sproutShellGh = impl;
  }
}
