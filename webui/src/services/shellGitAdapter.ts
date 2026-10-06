/**
 * shellGitAdapter.ts — backs the WASM shell's `git` command with
 * browser-side isomorphic-git: the read-only subcommands plus the local and
 * remote write subcommands (add, commit, checkout, branch, fetch, push, pull,
 * clone, init, rm, mv).
 *
 * The WASM shell (pkg/wasmshell/commands_git.go) answers these in-browser
 * instead of exiting 127 — which is the trigger for the transactional
 * container escalation (ETH-2). Without this adapter, every git call would
 * cost a container txn; with it, the read-only and write subcommands the
 * agent audit shows dominating usage run free in the browser. Subcommands it
 * does not implement (rebase, merge, reset, stash, …) stay a 127 and escalate.
 *
 * Contract with the Go side (cmd/wasm/shell_executor.go):
 *   globalThis.__sproutShellGit.execute(subcommand, args)
 *     → Promise<{ stdout: string; stderr: string; exitCode: number }>
 *
 * Output formats mirror the local CLI closely enough that agent workflows
 * behave identically in browser-IDE vs. local (`git status` porcelain-ish
 * labels, `git log --oneline` shape, branch markers).
 */

import {
  gitAdd,
  gitBranch,
  gitCheckout,
  gitClone,
  gitCommit,
  gitCreateBranch,
  gitDiff,
  gitFetch,
  gitInit,
  gitLog,
  gitMove,
  gitOriginUrl,
  gitPull,
  gitPush,
  gitRemoteBranches,
  gitRemove,
  gitStatus,
} from './browserGit';

/** Shape the Go bridge expects from execute(). */
export interface ShellGitResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

const ok = (stdout: string): ShellGitResult => ({ stdout, stderr: '', exitCode: 0 });
const fail = (stderr: string, exitCode = 1): ShellGitResult => ({ stdout: '', stderr, exitCode });

// ── Subcommand formatting ────────────────────────────────────────────────

/** git status — git's long format by default, `-s`/`--short` for XY lines. */
async function runStatus(args: string[]): Promise<ShellGitResult> {
  const short = args.includes('-s') || args.includes('--short');
  const { staged, unstaged, untracked = [] } = await gitStatus();

  if (short) {
    const lines = [
      ...staged.map((f) => `${statusChar(f.status)}  ${f.path}`),
      ...unstaged.map((f) => ` ${statusChar(f.status)} ${f.path}`),
      ...untracked.map((f) => `?? ${f.path}`),
    ];
    return ok(lines.length ? lines.join('\n') + '\n' : '');
  }

  const branch = (await gitBranch()).find((b) => b.current)?.name;
  const sections: string[] = [branch ? `On branch ${branch}` : 'Not currently on any branch.'];
  const listed = (list: Array<{ path: string; status: string }>) =>
    list.map((f) => `\t${describeStatus(f.status)}:   ${f.path}`).join('\n');
  if (staged.length > 0) {
    sections.push('Changes to be committed:\n  (use "git restore --staged <file>..." to unstage)\n' + listed(staged));
  }
  if (unstaged.length > 0) {
    sections.push(
      'Changes not staged for commit:\n  (use "git add <file>..." to update what will be committed)\n' +
        listed(unstaged),
    );
  }
  if (untracked.length > 0) {
    sections.push(
      'Untracked files:\n  (use "git add <file>..." to include in what will be committed)\n' +
        untracked.map((f) => `\t${f.path}`).join('\n'),
    );
  }
  if (sections.length === 1) sections.push('nothing to commit, working tree clean');
  return ok(sections.join('\n\n') + '\n');
}

function statusChar(status: string): string {
  switch (status) {
    case 'new':
    case 'added':
    case 'untracked':
      return 'A';
    case 'deleted':
      return 'D';
    case 'modified':
      return 'M';
    default:
      return '?';
  }
}

function describeStatus(status: string): string {
  switch (status) {
    case 'new':
      return 'new file';
    case 'deleted':
      return 'deleted';
    case 'modified':
      return 'modified';
    default:
      return status;
  }
}

/** git diff — per-file patch blocks from browserGit's status-backed diff. */
async function runDiff(args: string[]): Promise<ShellGitResult> {
  const cached = args.includes('--cached') || args.includes('--staged');
  void cached; // browserGit diffs the working tree; staged-only is approximated.
  const pathIdx = args.indexOf('--');
  const pathFilter = pathIdx >= 0 ? args[pathIdx + 1] : undefined;
  const changes = await gitDiff({ path: pathFilter });

  if (changes.length === 0) return ok('');

  const blocks = changes.map((c) => {
    const head = `diff --git a/${c.path} b/${c.path}`;
    if (c.type === 'added') {
      return `${head}\nnew file mode 100644\n--- /dev/null\n+++ b/${c.path}\n${linePrefix(c.content, '+')}`;
    }
    if (c.type === 'deleted') {
      return `${head}\ndeleted file mode 100644\n--- a/${c.path}\n+++ /dev/null\n`;
    }
    return `${head}\n--- a/${c.path}\n+++ b/${c.path}\n${linePrefix(c.content, '+')}`;
  });
  return ok(blocks.join('\n'));
}

function linePrefix(content: string, prefix: string): string {
  if (!content) return '';
  return content
    .split('\n')
    .filter((l) => l.length > 0)
    .map((l) => prefix + l)
    .join('\n');
}

/** git log — --oneline, -n <count>, and default medium format. */
async function runLog(args: string[]): Promise<ShellGitResult> {
  const oneline = args.includes('--oneline');
  let count = 50;
  const nIdx = args.indexOf('-n');
  if (nIdx >= 0 && nIdx + 1 < args.length) {
    const n = parseInt(args[nIdx + 1], 10);
    if (!Number.isNaN(n)) count = n;
  }
  // `git log -5` style counts.
  for (const a of args) {
    const m = /^-(\d+)$/.exec(a);
    if (m) {
      count = parseInt(m[1], 10);
      break;
    }
  }

  const commits = await gitLog(count);
  if (commits.length === 0) return ok('');

  if (oneline) {
    return ok(commits.map((c) => `${c.hash.slice(0, 7)} ${firstLine(c.message)}`).join('\n') + '\n');
  }
  const blocks = commits.map((c) => `commit ${c.hash}\nAuthor: ${c.author}\nDate:   ${c.date}\n\n    ${c.message}\n`);
  return ok(blocks.join('\n'));
}

function firstLine(s: string): string {
  const idx = s.indexOf('\n');
  return idx === -1 ? s : s.slice(0, idx);
}

/** git branch — list with the current-branch marker; create/delete branches. */
async function runBranch(args: string[]): Promise<ShellGitResult> {
  const positional = args.filter((a) => !a.startsWith('-'));
  const create = args.includes('-c') || (positional.length === 1 && !args.includes('-d') && !args.includes('-m'));
  if (create && positional.length >= 1) {
    const name = positional[positional.length - 1];
    await gitCreateBranch(name);
    return ok(`Switched to a new branch '${name}'\n`);
  }
  if (args.includes('-r') || args.includes('--remotes') || args.includes('-a') || args.includes('--all')) {
    const remote = await gitRemoteBranches('origin');
    const local = args.includes('-a') || args.includes('--all') ? (await gitBranch()).map((b) => b.name) : [];
    const lines = [...local.map((b) => `  ${b}`), ...remote.map((b) => `  ${b}`)];
    return ok(lines.length ? lines.join('\n') + '\n' : '');
  }
  const branches = await gitBranch();
  if (branches.length === 0) return ok('');
  return ok(branches.map((b) => (b.current ? `* ${b.name}` : `  ${b.name}`)).join('\n') + '\n');
}

/** git remote — origin with the clone URL when known, else empty. */
async function runRemote(args: string[]): Promise<ShellGitResult> {
  if (args.includes('-v')) {
    const url = await gitOriginUrl();
    if (url) return ok(`origin\t${url} (fetch)\norigin\t${url} (push)\n`);
    return ok('origin\t(push/fetch not tracked in browser git)\n');
  }
  return ok('origin\n');
}

/** git ls-files — every tracked/untracked working-tree file. */
async function runLsFiles(args: string[]): Promise<ShellGitResult> {
  void args;
  const bridge = (await import('./browserGit')).getBrowserGitVfsBridge();
  if (!bridge) return ok('');
  const files = await bridge.readVfsFiles();
  return ok(
    files
      .map((f) => f.path)
      .sort()
      .join('\n') + (files.length ? '\n' : ''),
  );
}

/** git show — commit message by hash prefix (best-effort in-browser). */
async function runShow(args: string[]): Promise<ShellGitResult> {
  const ref = args.find((a) => !a.startsWith('-')) ?? 'HEAD';
  const commits = await gitLog(50);
  const commit = ref === 'HEAD' ? commits[0] : commits.find((c) => c.hash.startsWith(ref));
  if (!commit) {
    return fail(`git show: ambiguous argument '${ref}': unknown revision\n`, 1);
  }
  return ok(`commit ${commit.hash}\nAuthor: ${commit.author}\nDate:   ${commit.date}\n\n    ${commit.message}\n`);
}

/** git rev-parse — HEAD hash (browserGit's resolveRef equivalent). */
async function runRevParse(_args: string[]): Promise<ShellGitResult> {
  const commits = await gitLog(1);
  if (commits.length === 0) return fail("HEAD\nfatal: ambiguous argument 'HEAD': unknown revision\n", 128);
  return ok(commits[0].hash + '\n');
}

/** git rev-list --count HEAD — commit count. */
async function runRevList(args: string[]): Promise<ShellGitResult> {
  const countOnly = args.includes('--count');
  const commits = await gitLog(1000);
  if (countOnly) return ok(commits.length + '\n');
  return ok(commits.map((c) => c.hash).join('\n') + (commits.length ? '\n' : ''));
}

/** git symbolic-ref --short HEAD — current branch name. */
async function runSymbolicRef(_args: string[]): Promise<ShellGitResult> {
  const branches = await gitBranch();
  const current = branches.find((b) => b.current);
  if (!current) return fail('fatal: ref HEAD is not a symbolic ref\n', 1);
  return ok(current.name + '\n');
}

// ── Write subcommands ────────────────────────────────────────────────────

/** git add — stage the named paths (or every changed path with -A/.). */
async function runAdd(args: string[]): Promise<ShellGitResult> {
  const paths = args.filter((a) => !a.startsWith('-'));
  if (args.includes('-A') || args.includes('--all') || paths.includes('.')) {
    const { staged, unstaged, untracked = [] } = await gitStatus();
    const all = [...staged, ...unstaged, ...untracked].map((f) => f.path);
    if (all.length === 0) return ok('');
    await gitAdd(all);
    return ok('');
  }
  if (paths.length === 0) return fail('Nothing specified, nothing added.\n', 1);
  await gitAdd(paths);
  return ok('');
}

/** git commit -m <msg> — commit staged changes. */
async function runCommit(args: string[]): Promise<ShellGitResult> {
  const mIdx = args.findIndex((a) => a === '-m' || a === '--message');
  const message = mIdx >= 0 ? args[mIdx + 1] : undefined;
  if (!message) return fail('error: switch `m` requires a value\n', 128);
  const res = (await gitCommit(message)) as { sha?: string };
  const sha = res.sha ?? '';
  return ok(`[${await currentBranchLabel()} ${sha.slice(0, 7)}] ${message}\n`);
}

async function currentBranchLabel(): Promise<string> {
  const branches = await gitBranch();
  return branches.find((b) => b.current)?.name ?? 'HEAD';
}

/** git checkout / git switch — switch to an existing branch or ref. */
async function runCheckout(args: string[]): Promise<ShellGitResult> {
  const create = args.includes('-b') || args.includes('-c') || args.includes('--create');
  const positional = args.filter((a) => !a.startsWith('-'));
  const target = positional[positional.length - 1];
  if (!target) return fail('fatal: missing branch or commit argument\n', 128);
  if (create) {
    await gitCreateBranch(target);
    return ok(`Switched to a new branch '${target}'\n`);
  }
  await gitCheckout(target);
  return ok(`Switched to branch '${target}'\n`);
}

/** git fetch — update remote-tracking refs without touching the working tree. */
async function runFetch(args: string[]): Promise<ShellGitResult> {
  const depthIdx = args.indexOf('--depth');
  const depth = depthIdx >= 0 ? parseInt(args[depthIdx + 1], 10) : undefined;
  await gitFetch({
    depth: Number.isNaN(depth as number) ? undefined : depth,
    tags: args.includes('--tags'),
    prune: args.includes('--prune') || args.includes('-p'),
  });
  return ok('');
}

/** git push [remote] [branch] — push the current (or named) branch. */
async function runPush(args: string[]): Promise<ShellGitResult> {
  const setArgs = args.filter((a) => !a.startsWith('-'));
  const remote = setArgs[0];
  const branch = setArgs[1];
  await gitPush(remote, branch);
  const target = branch ?? (await currentBranchLabel());
  return ok(`To ${remote ?? 'origin'}\n   ${target} -> ${target}\n`);
}

/** git pull — fast-forward the current branch from its remote. */
async function runPull(_args: string[]): Promise<ShellGitResult> {
  await gitPull();
  return ok('Already up to date.\n');
}

/** git init — initialize a repository in the workspace. */
async function runInit(_args: string[]): Promise<ShellGitResult> {
  await gitInit();
  return ok(`Initialized empty Git repository\n`);
}

/** git clone <url> [dir] — clone a repository into the workspace. */
async function runClone(args: string[]): Promise<ShellGitResult> {
  const url = args.find((a) => !a.startsWith('-'));
  if (!url) return fail('fatal: repository URL not given\n', 128);
  const res = (await gitClone(url)) as { branch?: string | null; files?: number };
  return ok(`Cloning into '${url}'...\n` + `done.${res.files != null ? ` (${res.files} files)` : ''}\n`);
}

/** git rm <path> — stage a deletion. */
async function runRm(args: string[]): Promise<ShellGitResult> {
  const paths = args.filter((a) => !a.startsWith('-'));
  if (paths.length === 0) return fail('fatal: No pathspec was given.\n', 128);
  await gitRemove(paths);
  return ok(paths.map((p) => `rm '${p}'`).join('\n') + '\n');
}

/** git mv <from> <to> — move/rename a tracked file. */
async function runMv(args: string[]): Promise<ShellGitResult> {
  const paths = args.filter((a) => !a.startsWith('-'));
  if (paths.length < 2) return fail('fatal: bad source, or missing destination\n', 128);
  await gitMove(paths[0], paths[1]);
  return ok('');
}

// ── Registry & global installation ───────────────────────────────────────

export const SHELL_GIT_SUBCOMMANDS: Record<string, (args: string[]) => Promise<ShellGitResult>> = {
  status: runStatus,
  diff: runDiff,
  log: runLog,
  show: runShow,
  branch: runBranch,
  remote: runRemote,
  'ls-files': runLsFiles,
  'rev-list': runRevList,
  'rev-parse': runRevParse,
  'symbolic-ref': runSymbolicRef,
  // write commands
  add: runAdd,
  commit: runCommit,
  checkout: runCheckout,
  switch: runCheckout,
  fetch: runFetch,
  push: runPush,
  pull: runPull,
  init: runInit,
  clone: runClone,
  rm: runRm,
  mv: runMv,
};

export interface SproutShellGitGlobal {
  execute(subcommand: string, args: string[]): Promise<ShellGitResult>;
  readonly names: ReadonlySet<string>;
}

declare global {
  interface Window {
    __sproutShellGit?: SproutShellGitGlobal;
  }
  /* eslint-disable no-var -- `var` is required in `declare global` blocks. */
  var __sproutShellGit: SproutShellGitGlobal | undefined;
  /* eslint-enable no-var */
}

/**
 * Install globalThis.__sproutShellGit — the bridge target for the WASM
 * shell's git command. Idempotent.
 */
export function registerShellGitGlobal(): void {
  const impl: SproutShellGitGlobal = {
    async execute(subcommand: string, args: string[]): Promise<ShellGitResult> {
      const fn = SHELL_GIT_SUBCOMMANDS[subcommand];
      if (!fn) {
        return {
          stdout: '',
          stderr: `git: '${subcommand}' is not available in this shell (run it in a cloud container)\n`,
          exitCode: 127,
        };
      }
      try {
        return await fn(args ?? []);
      } catch (err) {
        return fail(`git ${subcommand}: ${err instanceof Error ? err.message : String(err)}\n`, 1);
      }
    },
    names: new Set(Object.keys(SHELL_GIT_SUBCOMMANDS)),
  };

  globalThis.__sproutShellGit = impl;
  if (typeof window !== 'undefined') {
    window.__sproutShellGit = impl;
  }
}
